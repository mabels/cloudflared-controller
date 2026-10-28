package ingress

import (
	"context"
	"fmt"
	"strings"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/informer"
	"github.com/mabels/cloudflared-controller/controller/k8s_data"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/mabels/cloudflared-controller/utils"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

func introSpectTunnelName(cfc types.CFController, ingress *netv1.Ingress, tparams *k8s_data.UniqueTunnelParams) error {
	_, tNok := ingress.Annotations[config.AnnotationCloudflareTunnelName()]
	_, tENok := ingress.Annotations[config.AnnotationCloudflareTunnelExternalName()]
	if !tNok && !tENok {
		var tunnelName *string = nil
		for _, rule := range ingress.Spec.Rules {
			split := strings.SplitN(rule.Host, ".", 2)
			if tunnelName == nil && len(split) >= 2 && split[1] != "" {
				tunnelName = &split[1]
				continue
			} else if tunnelName != nil && len(split) >= 2 && split[1] != "" && *tunnelName != split[1] {
				err := fmt.Errorf("Found multiple tunnel names")
				cfc.Log().Error().Str("host", rule.Host).Err(err).Msg("")
				return err
			}
		}
		if tunnelName == nil {
			err := fmt.Errorf("Found no tunnel names")
			cfc.Log().Error().Err(err).Msg("")
			return err
		}
		tparams.Add(fmt.Sprintf("%s/%s", ingress.Namespace, *tunnelName), &types.CFTunnelParameter{
			Name:      *tunnelName,
			Namespace: ingress.Namespace,
		})
	}
	return nil
}

func findSchema(path netv1.HTTPIngressPath, ingress *netv1.Ingress) string {
	return "http"
}

func findClassMapping(mapping []types.ClassIngressAnnotationMapping, rule netv1.IngressRule, path netv1.HTTPIngressPath) types.ClassIngressAnnotationMapping {
	for _, m := range mapping {
		if m.Hostname == rule.Host && path.Path == m.Path {
			return m
		}
	}
	return types.ClassIngressAnnotationMapping{
		Hostname:   rule.Host,
		Schema:     "http",
		HostHeader: &rule.Host,
		Path:       path.Path,
	}
}

func classIngress(_cfc types.CFController, ev watch.Event, ingress *netv1.Ingress) {
	cfc := _cfc.WithComponent("classIngress", func(cfc types.CFController) {
		log := cfc.Log().With().Str("ingress", ingress.Name).Logger()
		cfc.SetLog(&log)
	})

	tparams := k8s_data.NewUniqueTunnelParams()
	err := introSpectTunnelName(cfc, ingress, tparams)
	if err != nil {
		return
	}

	// mapping
	// schema http/https/https-notlsverify
	// hostname/schema[/hostheader]|path,
	_mapping, ok := ingress.Annotations[config.AnnotationCloudflareTunnelMapping()]
	annotationMapping := []types.ClassIngressAnnotationMapping{}
	if ok {
		annotationMapping = utils.ParseClassIngressMapping(cfc.Log(), _mapping)
	}

	cfcis := []types.CFConfigIngress{}
	mapping := []types.CFEndpointMapping{}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			cfc.Log().Warn().Str("host", rule.Host).Msg("Skipping non-http ingress rule")
			continue
		}
		_, err := tparams.GetConfigMapTunnelParam(cfc, &ingress.ObjectMeta, fmt.Sprintf("%s/%s", ingress.Namespace, rule.Host))
		if err != nil {
			cfc.Log().Error().Err(err).Msg("Failed to find tunnel param")
			continue
		}
		for _, path := range rule.HTTP.Paths {

			amap := findClassMapping(annotationMapping, rule, path)

			notlsverify := false
			if amap.Schema == "https-notlsverify" {
				notlsverify = true
				amap.Schema = "https"
			}
			port := ""
			intPort := path.Backend.Service.Port.Number
			if 0 < intPort && intPort < 0x10000 {
				port = fmt.Sprintf(":%d", intPort)
			} else {
				cfc.Log().Warn().Msg(fmt.Sprintf("port by name(%v) or number(%v) not supported",
					path.Backend.Service.Port.Name, path.Backend.Service.Port.Number))
				continue
			}
			srvUrl := fmt.Sprintf("%s://%s.%s%s", amap.Schema, path.Backend.Service.Name, ingress.Namespace, port)
			mapping = append(mapping, types.CFEndpointMapping{
				External: rule.Host,
				Internal: srvUrl,
			})
			hostheader := rule.Host
			if amap.HostHeader != nil {
				hostheader = *amap.HostHeader
			}
			cci := types.CFConfigIngress{
				Hostname: rule.Host,
				Path:     path.Path,
				Service:  srvUrl,
				OriginRequest: &types.CFConfigOriginRequest{
					HttpHostHeader: hostheader,
					NoTLSVerify:    notlsverify,
				},
			}
			cfcis = append(cfcis, cci)
		}
	}
	// remove duplicates
	for _, tparam := range tparams.Get() {
		err := cfc.K8sData().TunnelConfigMaps.UpsertConfigMap(cfc, tparam, "ingress", &ingress.ObjectMeta, cfcis)
		if err != nil {
			cfc.Log().Error().Err(err).Msg("Failed to upsert configmap")
		}
	}
	cfc.Log().Info().Any("mapping", mapping).Msg("Wrote cloudflared config")
}

func findStackMapping(mapping []types.StackIngressAnnotationMapping, ingress *netv1.Ingress, rule netv1.IngressRule, path netv1.HTTPIngressPath) *types.StackIngressAnnotationMapping {
	for _, m := range mapping {
		if m.Hostname == rule.Host && path.Path == m.Path {
			for _, tls := range ingress.Spec.TLS {
				for _, thost := range tls.Hosts {
					if thost == rule.Host {
						if m.Schema == "http" {
							m.Schema = "https"
						}
						if m.InternPort == 80 {
							m.InternPort = 443
						}
						break
					}
				}
			}
			return &m
		}
	}
	return nil
}

func stackedIngress(_cfc types.CFController, ev watch.Event, ingress *netv1.Ingress) {
	cfc := _cfc.WithComponent("stackedIngress", func(cfc types.CFController) {
		log := cfc.Log().With().Str("ingress", ingress.Name).Logger()
		cfc.SetLog(&log)
	})

	_, ok := ingress.Annotations[config.AnnotationCloudflareTunnelName()]
	if !ok {
		cfc.Log().Debug().Str("kind", ingress.Kind).Str("name", ingress.Name).
			Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName())
		cfc.K8sData().TunnelConfigMaps.RemoveConfigMap(cfc, "ingress", &ingress.ObjectMeta)
		return
	}
	tparams := k8s_data.NewUniqueTunnelParams()
	// mapping
	// schema http/https/https-notlsverify
	// schema/hostname/int-port/hostheader/ext-host|path,
	_mapping, ok := ingress.Annotations[config.AnnotationCloudflareTunnelMapping()]
	annotationMapping := []types.StackIngressAnnotationMapping{}
	if ok {
		annotationMapping = utils.ParseStackIngressMapping(cfc.Log(), _mapping)
	}

	cfcis := []types.CFConfigIngress{}
	mapping := []types.CFEndpointMapping{}
	for _, rule := range ingress.Spec.Rules {

		tparam, err := tparams.GetConfigMapTunnelParam(cfc, &ingress.ObjectMeta, fmt.Sprintf("%s/%s", ingress.Namespace, rule.Host))
		if err != nil {
			cfc.Log().Error().Err(err).Msg("Failed to find tunnel param")
			continue
		}

		if rule.HTTP == nil {
			cfc.Log().Warn().Str("name", tparam.Name).Str("host", rule.Host).Msg("Skipping non-http ingress rule")
			continue
		}

		for _, path := range rule.HTTP.Paths {

			amap := findStackMapping(annotationMapping, ingress, rule, path)
			if amap == nil {
				cfc.Log().Warn().Str("path", path.Path).Str("rule", rule.Host).Msg("Skipping not cloudflare mapped")
				continue
			}

			notlsverify := false
			if amap.Schema == "https-notlsverify" {
				notlsverify = true
				amap.Schema = "https"
			}

			svcUrl := fmt.Sprintf("%s://%s:%d", amap.Schema, rule.Host, amap.InternPort)
			mapping = append(mapping, types.CFEndpointMapping{
				External: amap.ExtHostName,
				Internal: svcUrl,
			})
			hostHeader := rule.Host
			if amap.HostHeader != nil {
				hostHeader = *amap.HostHeader
			}
			cci := types.CFConfigIngress{
				Hostname: amap.ExtHostName,
				Path:     path.Path,
				Service:  svcUrl,
				OriginRequest: &types.CFConfigOriginRequest{
					HttpHostHeader: hostHeader,
					NoTLSVerify:    notlsverify,
				},
			}
			cfcis = append(cfcis, cci)
		}
	}
	for _, tparam := range tparams.Get() {
		err := cfc.K8sData().TunnelConfigMaps.UpsertConfigMap(cfc, tparam, "ingress", &ingress.ObjectMeta, cfcis)
		if err != nil {
			cfc.Log().Error().Err(err).Msg("Failed to upsert configmap")
		}
	}
	cfc.Log().Info().Any("mapping", mapping).Msg("Wrote cloudflared config")
}

func handleEvent(cfc types.CFController, ev watch.Event) {
	ingress, ok := ev.Object.(*netv1.Ingress)
	if !ok {
		cfc.Log().Error().Any("ev", ev).Msg("Failed to cast to Ingress")
		return
	}
	_, foundCTN := ingress.GetAnnotations()[config.AnnotationCloudflareTunnelName()]
	// the cloudflared class derives the tunnel name from the hosts
	if !foundCTN && !isCloudflaredClass(ingress) {
		cfc.Log().Debug().Str("uid", string(ingress.GetUID())).Str("name", ingress.Name).
			Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName())
		cfc.K8sData().TunnelConfigMaps.RemoveConfigMap(cfc, "ingress", &ingress.ObjectMeta)
		return
	}
	processEvent(ev, ingress, cfc)
}

func isCloudflaredClass(ingress *netv1.Ingress) bool {
	return ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName == "cloudflared"
}

func processEvent(ev watch.Event, ingress *netv1.Ingress, cfc types.CFController) {
	switch ev.Type {
	case watch.Added, watch.Modified:
		if isCloudflaredClass(ingress) {
			classIngress(cfc, ev, ingress)
		} else {
			stackedIngress(cfc, ev, ingress)
		}
	case watch.Deleted:
		cfc.K8sData().TunnelConfigMaps.RemoveConfigMap(cfc, "ingress", &ingress.ObjectMeta)
	default:
		cfc.Log().Error().Any("ev", ev).Str("type", string(ev.Type)).Msg("Got unknown event")
	}
}

// Start watches ingresses until ctx is done.
func Start(_cfc types.CFController, ctx context.Context) error {
	cfc := _cfc.WithComponent("ingress")
	inf := informer.New[*netv1.Ingress](cfc, func(f informers.SharedInformerFactory) cache.SharedIndexInformer {
		return f.Networking().V1().Ingresses().Informer()
	})
	_, err := inf.Register(func(ev watch.Event) {
		handleEvent(cfc, ev)
	})
	if err != nil {
		return err
	}
	err = inf.Start(ctx)
	cfc.Log().Info().Msg("Started watcher")
	return err
}

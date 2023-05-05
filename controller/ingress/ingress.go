package ingress

import (
	"fmt"

	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/tunnel"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func classIngress(_cfc *controller.CFController, ev watch.Event, ingress *netv1.Ingress) {
	cfc := _cfc.WithComponent("classIngress", func(cfc *controller.CFController) {
		log := cfc.Log.With().Str("ingress", ingress.Name).Logger()
		cfc.Log = &log
	})

	annotations := ingress.GetAnnotations()
	tp, ts, err := tunnel.PrepareTunnel(cfc, ingress.Namespace, annotations, ingress.GetLabels())
	if err != nil {
		return
	}
	cfcis := []config.CFConfigIngress{}
	mapping := []tunnel.CFEndpointMapping{}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			cfc.Log.Warn().Str("host", rule.Host).Msg("Skipping non-http ingress rule")
			continue
		}
		for _, path := range rule.HTTP.Paths {
			port := ""
			intPort := path.Backend.Service.Port.Number
			if intPort != 0 {
				port = fmt.Sprintf(":%d", intPort)
			}
			srvUrl := fmt.Sprintf("http://%s%s", path.Backend.Service.Name, port)
			mapping = append(mapping, tunnel.CFEndpointMapping{
				External: rule.Host,
				Internal: srvUrl,
			})
			err = tunnel.RegisterCFDnsEndpoint(cfc, *tp.TunnelID, rule.Host)
			if err != nil {
				cfc.Log.Error().Err(err).Str("host", rule.Host).Msg("Failed to register CF DNS endpoint")
				continue
			}
			cci := config.CFConfigIngress{
				Hostname: rule.Host,
				Path:     path.Path,
				Service:  srvUrl,
				OriginRequest: &config.CFConfigOriginRequest{
					HttpHostHeader: rule.Host,
				},
			}
			cfcis = append(cfcis, cci)
		}
	}
	err = tunnel.WriteCloudflaredConfig(cfc, tp, ts, ingress.GetUID(), cfcis)
	if err != nil {
		return
	}
	cfc.Log.Info().Any("mapping", mapping).Msg("Wrote cloudflared config")
}

func stackedIngress(_cfc *controller.CFController, ev watch.Event, ingress *netv1.Ingress) {
	cfc := _cfc.WithComponent("stackedIngress", func(cfc *controller.CFController) {
		log := cfc.Log.With().Str("ingress", ingress.Name).Logger()
		cfc.Log = &log
	})

	annotations := ingress.GetAnnotations()
	externalName, ok := annotations[config.AnnotationCloudflareTunnelExternalName]
	if !ok {
		err := fmt.Errorf("does not have %s annotation", config.AnnotationCloudflareTunnelExternalName)
		cfc.Log.Debug().Err(err).Msg("Failed to find external name")
		return
	}
	tp, ts, err := tunnel.PrepareTunnel(cfc, ingress.Namespace, annotations, ingress.GetLabels())
	if err != nil {
		return
	}

	err = tunnel.RegisterCFDnsEndpoint(cfc, *tp.TunnelID, externalName)
	if err != nil {
		return
	}

	cfcis := []config.CFConfigIngress{}
	mapping := []tunnel.CFEndpointMapping{}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			cfc.Log.Warn().Str("name", *tp.Name).Str("host", rule.Host).Msg("Skipping non-http ingress rule")
			continue
		}
		schema := "http"
		port := ""
		for _, tls := range ingress.Spec.TLS {
			for _, thost := range tls.Hosts {
				if thost == rule.Host {
					schema = "https"
					break
				}
			}
		}
		_port, ok := annotations[config.AnnotationCloudflareTunnelPort]
		if ok {
			port = ":" + _port
		}
		for _, path := range rule.HTTP.Paths {
			svcUrl := fmt.Sprintf("%s://%s%s", schema, rule.Host, port)
			mapping = append(mapping, tunnel.CFEndpointMapping{
				External: externalName,
				Internal: svcUrl,
			})
			cci := config.CFConfigIngress{
				Hostname: externalName,
				Path:     path.Path,
				Service:  svcUrl,
				OriginRequest: &config.CFConfigOriginRequest{
					HttpHostHeader: rule.Host,
				},
			}
			cfcis = append(cfcis, cci)
		}
	}
	err = tunnel.WriteCloudflaredConfig(cfc, tp, ts, ingress.GetUID(), cfcis)
	if err != nil {
		return
	}
	cfc.Log.Info().Any("mapping", mapping).Msg("Wrote cloudflared config")
}

func WatchIngress(_cfc *controller.CFController, namespace string) (watch.Interface, error) {
	cfc := _cfc.WithComponent("watchIngress", func(cfc *controller.CFController) {
		my := cfc.Log.With().Str("ns", namespace).Logger()
		cfc.Log = &my
	})
	wif, err := cfc.Rest.K8s.NetworkingV1().Ingresses(namespace).Watch(cfc.Context, metav1.ListOptions{})
	if err != nil {
		cfc.Log.Error().Err(err).Msg("Error watching ingress")
	}
	go func() {
		cfc.Log.Info().Msg("Start watching ingress")
		for {
			ev, more := <-wif.ResultChan()
			if !more {
				break
			}
			ingress, ok := ev.Object.(*netv1.Ingress)
			if !ok {
				cfc.Log.Error().Msg("Failed to cast to Ingress")
				continue
			}
			if ingress.Namespace != namespace {
				cfc.Log.Error().Msg("Ingress not in watched namespace")
				continue
			}
			annotations := ingress.GetAnnotations()
			_, foundCTN := annotations[config.AnnotationCloudflareTunnelName]
			// _, foundCID := annotations[config.AnnotationCloudflareTunnelId]
			if !foundCTN {
				cfc.Log.Debug().Str("name", ingress.Name).Msgf("skipping not cloudflared annotated(%s)",
					config.AnnotationCloudflareTunnelName)
				continue
			}
			switch ev.Type {
			case watch.Added:
				if ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName == "cloudfared" {
					classIngress(cfc, ev, ingress)
				} else {
					stackedIngress(cfc, ev, ingress)
				}
			case watch.Modified:
				if ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName == "cloudfared" {
					classIngress(cfc, ev, ingress)
				} else {
					stackedIngress(cfc, ev, ingress)
				}
			case watch.Deleted:
				// o := ev.Object.(*metav1.ObjectMeta)
				tunnel.RemoveFromCloudflaredConfig(cfc, &ingress.ObjectMeta)
			default:
				cfc.Log.Error().Msgf("Unknown event type: %s", ev.Type)
			}

		}
	}()
	return wif, nil
}

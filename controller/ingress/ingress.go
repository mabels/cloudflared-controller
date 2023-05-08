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
		//err := fmt.Errorf("does not have %s annotation", config.AnnotationCloudflareTunnelExternalName)
		cfc.Log.Debug().Str("kind", ingress.Kind).Str("name", ingress.Name).
			Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName)
		tunnel.RemoveFromCloudflaredConfig(cfc, ingress.Kind, &ingress.ObjectMeta)
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
	err = tunnel.WriteCloudflaredConfig(cfc, ingress.Kind, tp, ts, cfcis)
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
		cfc.Log.Debug().Str("kind", ingress.Kind).Str("name", ingress.Name).
			Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName)
		tunnel.RemoveFromCloudflaredConfig(cfc, ingress.Kind, &ingress.ObjectMeta)

		// err := fmt.Errorf("does not have %s annotation", config.AnnotationCloudflareTunnelExternalName)
		// cfc.Log.Debug().Err(err).Msg("Failed to find external name")
		return
	}
	tp, ts, err := tunnel.PrepareTunnel(cfc, ingress.Namespace, annotations, ingress.GetLabels())
	if err != nil {
		cfc.Log.Debug().Str("kind", ingress.Kind).Str("name", ingress.Name).
			Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName)
		tunnel.RemoveFromCloudflaredConfig(cfc, ingress.Kind, &ingress.ObjectMeta)
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
	err = tunnel.WriteCloudflaredConfig(cfc, ingress.Kind, tp, ts, cfcis)
	if err != nil {
		return
	}
	cfc.Log.Info().Any("mapping", mapping).Msg("Wrote cloudflared config")
}

func getAllIngresses(cfc *controller.CFController, namespace string) ([]watch.Event, error) {
	log := cfc.Log.With().Str("component", "getAllIngress").Str("namespace", namespace).Logger()
	ingresses, err := cfc.Rest.K8s.NetworkingV1().Ingresses(namespace).List(cfc.Context, metav1.ListOptions{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to list Services")
	}
	out := make([]watch.Event, 0, len(ingresses.Items))
	for _, ingress := range ingresses.Items {
		out = append(out, watch.Event{
			Type:   watch.Added,
			Object: &ingress,
		})
	}
	return out, nil
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
	events := make(chan []watch.Event, cfc.Cfg.ChannelSize)

	evs, err := getAllIngresses(cfc, namespace)
	if err != nil {
		cfc.Log.Error().Err(err).Msg("Failed to get all Services")
		return nil, err
	}
	events <- evs

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
			events <- []watch.Event{ev}
		}
	}()

	go func() {
		for {
			evs, more := <-events
			if !more {
				break
			}
			for _, ev := range evs {
				ingress, ok := ev.Object.(*netv1.Ingress)
				if !ok {
					cfc.Log.Error().Msg("Failed to cast to Ingress")
					continue
				}
				annotations := ingress.GetAnnotations()
				_, foundCTN := annotations[config.AnnotationCloudflareTunnelName]
				// _, foundCID := annotations[config.AnnotationCloudflareTunnelId]
				if !foundCTN {
					cfc.Log.Debug().Str("uid", string(ingress.GetUID())).Str("name", ingress.Name).
						Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName)
					tunnel.RemoveFromCloudflaredConfig(cfc, ingress.Kind, &ingress.ObjectMeta)
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
					tunnel.RemoveFromCloudflaredConfig(cfc, ingress.Kind, &ingress.ObjectMeta)
				default:
					cfc.Log.Error().Msgf("Unknown event type: %s", ev.Type)
				}

			}
		}
	}()
	return wif, nil
}

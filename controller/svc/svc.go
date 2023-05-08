package svc

import (
	"fmt"

	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/tunnel"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func updateConfigMap(_cfc *controller.CFController, svc *corev1.Service) error {
	cfc := _cfc.WithComponent("watchSvc", func(cfc *controller.CFController) {
		log := cfc.Log.With().Str("svc", svc.Name).Logger()
		cfc.Log = &log
	})

	annotations := svc.GetAnnotations()
	externalName, ok := annotations[config.AnnotationCloudflareTunnelExternalName]
	if !ok {
		//err := fmt.Errorf("does not have %s annotation", config.AnnotationCloudflareTunnelExternalName)
		cfc.Log.Debug().Str("kind", svc.Kind).Str("name", svc.Name).
			Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName)
		tunnel.RemoveFromCloudflaredConfig(cfc, svc.Kind, &svc.ObjectMeta)
		return nil
	}
	tp, ts, err := tunnel.PrepareTunnel(cfc, svc.Namespace, annotations, svc.GetLabels())
	if err != nil {
		return err
	}

	err = tunnel.RegisterCFDnsEndpoint(cfc, *tp.TunnelID, externalName)
	if err != nil {
		return err
	}
	cfcis := []config.CFConfigIngress{}
	mapping := []tunnel.CFEndpointMapping{}
	for _, port := range svc.Spec.Ports {
		if port.Protocol != corev1.ProtocolTCP {
			cfc.Log.Warn().Str("port", port.Name).Msg("Skipping non-TCP port")
			continue
		}
		urlPort := fmt.Sprintf(":%d", port.Port)
		schema := "http"
		if port.TargetPort.Type == intstr.Int {
			cfc.Log.Warn().Int32("TargetPort", port.TargetPort.IntVal).Msg("Skipping non-http(s) port")
			continue
		}
		if port.TargetPort.Type == intstr.String {
			switch port.TargetPort.StrVal {
			case "http":
			case "https":
				schema = "https"
			default:
			}
		}
		svcUrl := fmt.Sprintf("%s://%s%s", schema, svc.Name, urlPort)
		mapping = append(mapping, tunnel.CFEndpointMapping{
			External: externalName,
			Internal: svcUrl,
		})
		cci := config.CFConfigIngress{
			Hostname: externalName,
			Path:     "/",
			Service:  svcUrl,
			OriginRequest: &config.CFConfigOriginRequest{
				HttpHostHeader: svc.Name,
			},
		}
		cfcis = append(cfcis, cci)
	}
	err = tunnel.WriteCloudflaredConfig(cfc, svc.Kind, tp, ts, cfcis)
	if err != nil {
		return err
	}
	cfc.Log.Info().Any("mapping", mapping).Msg("Wrote cloudflared config")
	return nil
}

func getAllServices(cfc *controller.CFController, namespace string) ([]watch.Event, error) {
	log := cfc.Log.With().Str("component", "getAllServices").Str("namespace", namespace).Logger()
	svcs, err := cfc.Rest.K8s.CoreV1().Services(namespace).List(cfc.Context, metav1.ListOptions{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to list Services")
	}
	out := make([]watch.Event, 0, len(svcs.Items))
	for _, svc := range svcs.Items {
		out = append(out, watch.Event{
			Type:   watch.Added,
			Object: &svc,
		})
	}
	return out, nil
}

func WatchSvc(cfc *controller.CFController, namespace string) (watch.Interface, error) {
	log := cfc.Log.With().Str("component", "watchSvc").Str("namespace", namespace).Logger()
	wif, err := cfc.Rest.K8s.CoreV1().Services(namespace).Watch(cfc.Context, metav1.ListOptions{})
	if err != nil {
		log.Error().Err(err).Msg("Failed to watch Services")
		return nil, err
	}
	events := make(chan []watch.Event, cfc.Cfg.ChannelSize)

	evs, err := getAllServices(cfc, namespace)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get all Services")
		return nil, err
	}
	events <- evs
	go func() {
		log.Info().Msg("Start watching svc")
		for {
			ev, more := <-wif.ResultChan()
			if !more {
				break
			}
			svc, ok := ev.Object.(*corev1.Service)
			if !ok {
				cfc.Log.Error().Msg("Failed to cast to Ingress")
				continue
			}
			if svc.Namespace != namespace {
				cfc.Log.Error().Msg("Ingress not in watched namespace")
				continue
			}
			events <- []watch.Event{ev}

		}
		log.Info().Msg("Stop watching svc")
	}()
	go func() {
		for {
			evs, more := <-events
			if !more {
				break
			}
			for _, ev := range evs {
				svc, ok := ev.Object.(*corev1.Service)
				if !ok {
					cfc.Log.Error().Msg("Failed to cast to Ingress")
					continue
				}
				annotations := svc.GetAnnotations()
				_, foundCTN := annotations[config.AnnotationCloudflareTunnelName]
				// _, foundCID := annotations[config.AnnotationCloudflareTunnelId]
				if !foundCTN {
					cfc.Log.Debug().Str("name", svc.Name).Msgf("skipping not cloudflared annotated(%s)",
						config.AnnotationCloudflareTunnelName)
					continue
				}
				var err error
				switch ev.Type {
				case watch.Added:
					err = updateConfigMap(cfc, svc)
				case watch.Modified:
					err = updateConfigMap(cfc, svc)
				case watch.Deleted:
					tunnel.RemoveFromCloudflaredConfig(cfc, svc.Kind, &svc.ObjectMeta)
				default:
					cfc.Log.Error().Msgf("Unknown event type: %s", ev.Type)
				}
				if err != nil {
					cfc.Log.Error().Err(err).Msg("Failed to update config")
				}
			}
		}
	}()
	return wif, nil
}

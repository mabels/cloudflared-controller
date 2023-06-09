package svc

import (
	"fmt"
	"sync"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/k8s_data"
	"github.com/mabels/cloudflared-controller/controller/namespaces"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/mabels/cloudflared-controller/controller/watcher"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
)

func updateConfigMap(_cfc types.CFController, svc *corev1.Service) error {
	cfc := _cfc.WithComponent("watchSvc", func(cfc types.CFController) {
		log := cfc.Log().With().Str("svc", svc.Name).Logger()
		cfc.SetLog(&log)
	})

	annotations := svc.GetAnnotations()
	externalName, ok := annotations[config.AnnotationCloudflareTunnelExternalName()]
	if !ok {
		//err := fmt.Errorf("does not have %s annotation", config.AnnotationCloudflareTunnelExternalName)
		cfc.Log().Debug().Str("kind", svc.Kind).Str("name", svc.Name).
			Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName())
		cfc.K8sData().TunnelConfigMaps.RemoveConfigMap(cfc, "service", &svc.ObjectMeta)
		return nil
	}

	tparam, err := k8s_data.NewUniqueTunnelParams().GetConfigMapTunnelParam(cfc, &svc.ObjectMeta)
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Failed to find tunnel param")
		return err
	}

	// tp, err := cloudflared.PrepareTunnel(cfc, &svc.ObjectMeta)
	// if err != nil {
	// 	return err
	// }

	// err = cloudflared.RegisterCFDnsEndpoint(cfc, tp.ID, externalName)
	// if err != nil {
	// 	return err
	// }
	cfcis := []types.CFConfigIngress{}
	mapping := []types.CFEndpointMapping{}
	for _, port := range svc.Spec.Ports {
		if port.Protocol != corev1.ProtocolTCP {
			cfc.Log().Warn().Str("port", port.Name).Msg("Skipping non-TCP port")
			continue
		}
		urlPort := fmt.Sprintf(":%d", port.Port)
		schema := "http"
		// if port.TargetPort.Type == intstr.Int {
		// 	cfc.Log().Warn().Int32("TargetPort", port.TargetPort.IntVal).Msg("Skipping non-http(s) port")
		// 	continue
		// }
		if port.TargetPort.Type == intstr.String {
			switch port.TargetPort.StrVal {
			case "http":
			case "https":
				schema = "https"
			default:
			}
		}
		svcUrl := fmt.Sprintf("%s://%s%s", schema, svc.Name, urlPort)
		mapping = append(mapping, types.CFEndpointMapping{
			External: externalName,
			Internal: svcUrl,
		})
		cci := types.CFConfigIngress{
			Hostname: externalName,
			Path:     "/",
			Service:  svcUrl,
			OriginRequest: &types.CFConfigOriginRequest{
				HttpHostHeader: svc.Name,
			},
		}
		cfcis = append(cfcis, cci)
	}
	err = cfc.K8sData().TunnelConfigMaps.UpsertConfigMap(cfc, tparam, "service", &svc.ObjectMeta, cfcis)
	if err != nil {
		return err
	}
	cfc.Log().Info().Any("mapping", mapping).Msg("Wrote cloudflared config")
	return nil
}

// func getAllServices(cfc types.CFController, namespace string) ([]watch.Event, error) {
// 	log := cfc.Log().With().Str("component", "getAllServices").Str("namespace", namespace).Logger()
// 	svcs, err := cfc.Rest.K8s.CoreV1().Services(namespace).List(cfc.Context, metav1.ListOptions{})
// 	if err != nil {
// 		log.Error().Err(err).Msg("Failed to list Services")
// 	}
// 	out := make([]watch.Event, 0, len(svcs.Items))
// 	for _, svc := range svcs.Items {
// 		out = append(out, watch.Event{
// 			Type:   watch.Added,
// 			Object: &svc,
// 		})
// 	}
// 	return out, nil
// }

type watcherBindingServices struct {
	watcher         types.Watcher[*corev1.Service]
	unregisterEvent func()
	namespace       string
}

type services struct {
	lock  sync.Mutex
	items map[string]watcherBindingServices
}

func startServiceWatcher(cfc types.CFController, ns string) (watcherBindingServices, error) {
	log := cfc.Log().With().Str("watcher", "service").Str("namespace", ns).Logger()
	wt := watcher.NewWatcher(
		types.WatcherConfig[corev1.Service, *corev1.Service, types.WatcherBindingService, types.WatcherBindingServiceClient]{
			Log:     &log,
			Context: cfc.Context(),
			K8sClient: types.WatcherBindingServiceClient{
				Sif: cfc.Rest().K8s().CoreV1().Services(ns),
			},
		})
	unreg := wt.RegisterEvent(func(_ []*corev1.Service, ev watch.Event) {
		log.Debug().Str("event", string(ev.Type)).Msg("Received event")
		svc, ok := ev.Object.(*corev1.Service)
		if !ok {
			cfc.Log().Error().Msg("Failed to cast to Service")
			return
		}
		annotations := svc.GetAnnotations()
		_, foundCTN := annotations[config.AnnotationCloudflareTunnelName()]
		// _, foundCID := annotations[config.AnnotationCloudflareTunnelId]
		if !foundCTN {
			log.Debug().Str("uid", string(svc.GetUID())).Str("name", svc.Name).
				Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName())
			cfc.K8sData().TunnelConfigMaps.RemoveConfigMap(cfc, "service", &svc.ObjectMeta)
			return
		}
		var err error
		switch ev.Type {
		case watch.Added:
			err = updateConfigMap(cfc, svc)
		case watch.Modified:
			err = updateConfigMap(cfc, svc)
		case watch.Deleted:
			cfc.K8sData().TunnelConfigMaps.RemoveConfigMap(cfc, "service", &svc.ObjectMeta)
		default:
			log.Error().Msgf("Unknown event type: %s", ev.Type)
		}
		if err != nil {
			log.Error().Err(err).Msg("Failed to update config")
		}
	})
	err := wt.Start()
	log.Debug().Msg("Started watcher")
	return watcherBindingServices{
		watcher:         wt,
		unregisterEvent: unreg,
		namespace:       ns,
	}, err
}

func Start(cfc types.CFController) func() {
	svcs := &services{
		items: make(map[string]watcherBindingServices),
	}
	unreg := cfc.K8sData().Namespaces.RegisterEvent(func(_ []*corev1.Namespace, ev watch.Event) {
		ns, ok := ev.Object.(*corev1.Namespace)
		if !ok {
			cfc.Log().Error().Msg("Failed to cast to Namespace")
			return
		}
		if namespaces.SkipNamespace(cfc, ns.Name) {
			return
		}
		svcs.lock.Lock()
		defer svcs.lock.Unlock()
		switch ev.Type {
		case watch.Added:
			if _, ok := svcs.items[ns.Name]; !ok {
				wif, err := startServiceWatcher(cfc, ns.Name)
				if err != nil {
					cfc.Log().Error().Err(err).Msg("Failed to start ingress watcher")
					return
				}
				svcs.items[ns.Name] = wif
			}
		case watch.Modified:
		case watch.Deleted:
			my, ok := svcs.items[ns.Name]
			if !ok {
				delete(svcs.items, ns.Name)
				my.unregisterEvent()
				my.watcher.Stop()
			}
		default:
			cfc.Log().Error().Msgf("Unknown event type: %s", ev.Type)
		}
	})
	cfc.Log().Debug().Str("component", "svc").Msg("Started watcher")
	return func() {
		svcs.lock.Lock()
		defer svcs.lock.Unlock()
		for _, v := range svcs.items {
			v.unregisterEvent()
			v.watcher.Stop()
		}
		unreg()
	}

}

// func Start(cfc types.CFController) func() {
// 	unreg := cfc.K8sData.Namespaces.RegisterEvent(func(_ []*corev1.Namespace, ev watch.Event) {
// 		ns, ok := ev.Object.(*corev1.Namespace)
// 		if !ok {
// 			cfc.Log().Error().Msg("Failed to cast to Namespace")
// 			return
// 		}
// 		if namespaces.SkipNamespace(cfc, ns.Name) {
// 			return
// 		}
// 		switch ev.Type {
// 		case watch.Added:
// 		case watch.Modified:
// 		case watch.Deleted:
// 		default:
// 			cfc.Log().Error().Msgf("Unknown event type: %s", ev.Type)
// 		}
// 	})
// 	return func() {
// 		unreg()
// 	}
// }

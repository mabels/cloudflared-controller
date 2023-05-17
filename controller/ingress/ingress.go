package ingress

import (
	"fmt"
	"sync"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/namespaces"
	"github.com/mabels/cloudflared-controller/controller/watcher"
	"github.com/rs/zerolog/log"

	// "github.com/mabels/cloudflared-controller/controller/tunnel"
	"github.com/mabels/cloudflared-controller/controller/k8s_data"
	"github.com/mabels/cloudflared-controller/controller/types"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func classIngress(_cfc types.CFController, ev watch.Event, ingress *netv1.Ingress) {
	cfc := _cfc.WithComponent("classIngress", func(cfc types.CFController) {
		log := cfc.Log().With().Str("ingress", ingress.Name).Logger()
		cfc.SetLog(&log)
	})

	// annotations := ingress.GetAnnotations()
	// tp, err := cloudflared.PrepareTunnel(cfc, &ingress.ObjectMeta)
	// if err != nil {
	// 	//err := fmt.Errorf("does not have %s annotation", config.AnnotationCloudflareTunnelExternalName)
	// 	cfc.Log().Debug().Str("kind", ingress.Kind).Str("name", ingress.Name).
	// 		Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName)
	// 	cfc.K8sData().TunnelConfigMaps.RemoveConfigMap(cfc, "ingress", &ingress.ObjectMeta)
	// 	return
	// }

	tparams := k8s_data.NewUniqueTunnelParams()

	cfcis := []types.CFConfigIngress{}
	mapping := []types.CFEndpointMapping{}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			cfc.Log().Warn().Str("host", rule.Host).Msg("Skipping non-http ingress rule")
			continue
		}
		_, err := tparams.GetConfigMapTunnelParam(cfc, &ingress.ObjectMeta, rule.Host)
		if err != nil {
			cfc.Log().Error().Err(err).Msg("Failed to find tunnel param")
			continue
		}
		for _, path := range rule.HTTP.Paths {
			port := ""
			intPort := path.Backend.Service.Port.Number
			if intPort != 0 {
				port = fmt.Sprintf(":%d", intPort)
			}
			srvUrl := fmt.Sprintf("http://%s%s", path.Backend.Service.Name, port)
			mapping = append(mapping, types.CFEndpointMapping{
				External: rule.Host,
				Internal: srvUrl,
			})

			cci := types.CFConfigIngress{
				Hostname: rule.Host,
				Path:     path.Path,
				Service:  srvUrl,
				OriginRequest: &types.CFConfigOriginRequest{
					HttpHostHeader: rule.Host,
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
	return
}

func stackedIngress(_cfc types.CFController, ev watch.Event, ingress *netv1.Ingress) {
	cfc := _cfc.WithComponent("stackedIngress", func(cfc types.CFController) {
		log := cfc.Log().With().Str("ingress", ingress.Name).Logger()
		cfc.SetLog(&log)
	})

	annotations := ingress.GetAnnotations()
	externalName, ok := annotations[config.AnnotationCloudflareTunnelExternalName()]
	if !ok {
		cfc.Log().Debug().Str("kind", ingress.Kind).Str("name", ingress.Name).
			Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName())
		cfc.K8sData().TunnelConfigMaps.RemoveConfigMap(cfc, "ingress", &ingress.ObjectMeta)

		// err := fmt.Errorf("does not have %s annotation", config.AnnotationCloudflareTunnelExternalName)
		// cfc.Log().Debug().Err(err).Msg("Failed to find external name")
		return
	}

	// tp, err := cloudflared.PrepareTunnel(cfc, &ingress.ObjectMeta)
	// if err != nil {
	// 	cfc.Log().Debug().Str("kind", ingress.Kind).Str("name", ingress.Name).
	// 		Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName)
	// 	cfc.K8sData().TunnelConfigMaps.RemoveConfigMap(cfc, "ingress", &ingress.ObjectMeta)
	// 	return
	// }

	// err = cloudflared.RegisterCFDnsEndpoint(cfc, tp.ID, externalName)
	// if err != nil {
	// 	return
	// }

	cfcis := []types.CFConfigIngress{}
	mapping := []types.CFEndpointMapping{}
	tparams := k8s_data.NewUniqueTunnelParams()
	for _, rule := range ingress.Spec.Rules {

		tparam, err := tparams.GetConfigMapTunnelParam(cfc, &ingress.ObjectMeta, rule.Host)
		if err != nil {
			cfc.Log().Error().Err(err).Msg("Failed to find tunnel param")
			continue
		}

		if rule.HTTP == nil {
			cfc.Log().Warn().Str("name", tparam.Name).Str("host", rule.Host).Msg("Skipping non-http ingress rule")
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
		_port, ok := annotations[config.AnnotationCloudflareTunnelPort()]
		if ok {
			port = ":" + _port
		}
		for _, path := range rule.HTTP.Paths {
			svcUrl := fmt.Sprintf("%s://%s%s", schema, rule.Host, port)
			mapping = append(mapping, types.CFEndpointMapping{
				External: externalName,
				Internal: svcUrl,
			})
			cci := types.CFConfigIngress{
				Hostname: externalName,
				Path:     path.Path,
				Service:  svcUrl,
				OriginRequest: &types.CFConfigOriginRequest{
					HttpHostHeader: rule.Host,
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

// func getAllIngresses(cfc types.CFController, namespace string) ([]watch.Event, error) {
// 	log := cfc.Log().With().Str("component", "getAllIngress").Str("namespace", namespace).Logger()
// 	ingresses, err := cfc.Rest.K8s.NetworkingV1().Ingresses(namespace).List(cfc.Context, metav1.ListOptions{})
// 	if err != nil {
// 		log.Error().Err(err).Msg("Failed to list Services")
// 	}
// 	out := make([]watch.Event, 0, len(ingresses.Items))
// 	for _, ingress := range ingresses.Items {
// 		out = append(out, watch.Event{
// 			Type:   watch.Added,
// 			Object: &ingress,
// 		})
// 	}
// 	return out, nil
// }

type watcherBindingIngresses struct {
	watcher         types.Watcher[*netv1.Ingress]
	unregisterEvent func()
	namespace       string
}

type ingresses struct {
	lock  sync.Mutex
	items map[string]watcherBindingIngresses
}

func startIngressWatcher(_cfc types.CFController, ns string) (watcherBindingIngresses, error) {
	cfc := _cfc.WithComponent("ingress", func(cfc types.CFController) {
		log := cfc.Log().With().Str("namespace", ns).Logger()
		cfc.SetLog(&log)
	})
	wt := watcher.NewWatcher(
		types.WatcherConfig[netv1.Ingress, *netv1.Ingress, types.WatcherBindingIngress, types.WatcherBindingIngressClient]{
			Log:     cfc.Log(),
			Context: cfc.Context(),
			K8sClient: types.WatcherBindingIngressClient{
				Cif: cfc.Rest().K8s().NetworkingV1().Ingresses(ns),
			},
		})
	unreg := wt.RegisterEvent(func(_ []*netv1.Ingress, ev watch.Event) {
		ingress, ok := ev.Object.(*netv1.Ingress)
		if !ok {
			cfc.Log().Error().Any("ev", ev).Msg("Failed to cast to Ingress")
			return
		}

		annotations := ingress.GetAnnotations()
		_, foundCTN := annotations[config.AnnotationCloudflareTunnelName()]
		// _, foundCID := annotations[config.AnnotationCloudflareTunnelId]
		if !foundCTN {
			cfc.Log().Debug().Str("uid", string(ingress.GetUID())).Str("name", ingress.Name).
				Msgf("skipping not cloudflared annotated(%s)", config.AnnotationCloudflareTunnelName())
			cfc.K8sData().TunnelConfigMaps.RemoveConfigMap(cfc, "ingress", &ingress.ObjectMeta)
			return
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
			cfc.K8sData().TunnelConfigMaps.RemoveConfigMap(cfc, "ingress", &ingress.ObjectMeta)

		default:
			log.Error().Any("ev", ev).Str("type", string(ev.Type)).Msg("Got unknown event")
		}
	})
	err := wt.Start()
	cfc.Log().Info().Msg("Started watcher")
	return watcherBindingIngresses{
		watcher:         wt,
		unregisterEvent: unreg,
		namespace:       ns,
	}, err
}

func Start(_cfc types.CFController) func() {
	cfc := _cfc.WithComponent("ingress")
	igs := &ingresses{
		items: make(map[string]watcherBindingIngresses),
	}
	unreg := cfc.K8sData().Namespaces.RegisterEvent(func(_ []*corev1.Namespace, ev watch.Event) {
		cfc.Log().Debug().Any("ev", ev).Msg("Got event")
		ns, ok := ev.Object.(*corev1.Namespace)
		if !ok {
			cfc.Log().Error().Msg("Failed to cast to Namespace")
			return
		}
		if namespaces.SkipNamespace(cfc, ns.Name) {
			return
		}
		igs.lock.Lock()
		defer igs.lock.Unlock()
		switch ev.Type {
		case watch.Added:
			if _, ok := igs.items[ns.Name]; !ok {
				wif, err := startIngressWatcher(cfc, ns.Name)
				if err != nil {
					cfc.Log().Error().Err(err).Msg("Failed to start ingress watcher")
					return
				}
				igs.items[ns.Name] = wif
			}
		case watch.Modified:
		case watch.Deleted:
			my, ok := igs.items[ns.Name]
			if !ok {
				delete(igs.items, ns.Name)
				my.unregisterEvent()
				my.watcher.Stop()
			}
		default:
			cfc.Log().Error().Msgf("Unknown event type: %s", ev.Type)
		}
	})
	cfc.Log().Debug().Msg("Started watcher")
	return func() {
		igs.lock.Lock()
		defer igs.lock.Unlock()
		for _, v := range igs.items {
			v.unregisterEvent()
			v.watcher.Stop()
		}
		unreg()
	}

}

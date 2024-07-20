package svc

import (
	"fmt"
	"sort"
	"sync"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/k8s_data"
	"github.com/mabels/cloudflared-controller/controller/namespaces"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/mabels/cloudflared-controller/controller/watcher"
	"github.com/mabels/cloudflared-controller/utils"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
)

type mappingCFEndpointMapping struct {
	order int
	cfem  types.CFEndpointMapping
	cfci  types.CFConfigIngress
}

func prefereHttps(ports []corev1.ServicePort, isHttps func(port corev1.ServicePort) bool) []types.SvcAnnotationMapping {
	if len(ports) == 0 {
		return []types.SvcAnnotationMapping{}
	}
	for _, port := range ports {
		if isHttps(port) {
			return []types.SvcAnnotationMapping{
				{
					PortName: port.Name,
					Schema:   "https",
					Path:     "/",
					Order:    0,
				}}
		}
	}
	port := ports[0]
	return []types.SvcAnnotationMapping{{
		PortName: port.Name,
		Schema:   "http",
		Path:     "/",
		Order:    0,
	}}
}

func mappingFromPorts(ports []corev1.ServicePort) []types.SvcAnnotationMapping {

	searchByPortName := []corev1.ServicePort{}
	searchByPortTypeString := []corev1.ServicePort{}
	searchByPortTypeInt := []corev1.ServicePort{}
	for _, port := range ports {
		// search portname http or https
		if port.Name == "http" || port.Name == "https" {
			searchByPortName = append(searchByPortName, port)
			continue
		}
		if port.TargetPort.Type == intstr.String {
			if port.TargetPort.StrVal == "http" || port.TargetPort.StrVal == "https" {
				searchByPortTypeString = append(searchByPortTypeString, port)
				continue
			}
		}
		if port.TargetPort.Type == intstr.Int {
			if port.TargetPort.IntVal == 80 || port.TargetPort.IntVal == 443 {
				searchByPortTypeInt = append(searchByPortTypeInt, port)
				continue
			}
		}
	}
	if len(searchByPortName) > 0 {
		return prefereHttps(searchByPortName, func(port corev1.ServicePort) bool {
			return port.Name == "https"
		})
	} else if len(searchByPortTypeString) > 0 {
		return prefereHttps(searchByPortTypeString, func(port corev1.ServicePort) bool {
			return port.TargetPort.StrVal == "https"
		})
	} else if len(searchByPortTypeInt) > 0 {
		return prefereHttps(searchByPortTypeInt, func(port corev1.ServicePort) bool {
			return port.TargetPort.IntVal == 443
		})
	}
	return []types.SvcAnnotationMapping{}
}

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
	// Mapping
	// name/schema/path
	// name is name of the port
	// schema is http, https, https-notlsverify
	// path is the path to match
	_mappings, ok := annotations[config.AnnotationCloudflareTunnelMapping()]
	annotedMapping := []types.SvcAnnotationMapping{}
	if ok {
		annotedMapping = utils.ParseSvcMapping(cfc.Log(), _mappings)
	}
	if len(annotedMapping) == 0 {
		annotedMapping = mappingFromPorts(svc.Spec.Ports)
	}
	mappings := []mappingCFEndpointMapping{}
	for _, port := range svc.Spec.Ports {
		if port.Protocol != corev1.ProtocolTCP {
			cfc.Log().Warn().Str("port", port.Name).Msg("Skipping non-TCP port")
			continue
		}

		selectedMapping := []types.SvcAnnotationMapping{}
		for _, am := range annotedMapping {
			if am.PortName == port.Name {
				selectedMapping = append(selectedMapping, am)
			}
		}

		urlPort := fmt.Sprintf(":%d", port.Port)
		for _, sm := range selectedMapping {
			schema := sm.Schema
			noTLSVerify := false
			if schema == "https-notlsverify" {
				schema = "https"
				noTLSVerify = true
			}
			name := svc.Name + "." + svc.Namespace
			if svc.Spec.ExternalName != "" {
				name = svc.Spec.ExternalName
			}
			svcUrl := fmt.Sprintf("%s://%s%s", schema, name, urlPort)
			path := sm.Path
			cci := mappingCFEndpointMapping{
				order: sm.Order,
				cfem: types.CFEndpointMapping{
					Path:     path,
					External: externalName,
					Internal: svcUrl,
				},
				cfci: types.CFConfigIngress{
					Hostname: externalName,
					Path:     path,
					Service:  svcUrl,
					OriginRequest: &types.CFConfigOriginRequest{
						HttpHostHeader: svc.Name,
						NoTLSVerify:    noTLSVerify,
					},
				},
			}
			mappings = append(mappings, cci)
		}
	}
	if len(mappings) == 0 {
		return nil
	}
	sort.Slice(mappings, func(i, j int) bool {
		return mappings[i].order < mappings[j].order
	})
	cfcis := make([]types.CFConfigIngress, 0, len(mappings))
	for _, m := range mappings {
		cfcis = append(cfcis, m.cfci)
	}
	err = cfc.K8sData().TunnelConfigMaps.UpsertConfigMap(cfc, tparam, "service", &svc.ObjectMeta, cfcis)
	if err != nil {
		return err
	}
	cfms := make([]types.CFEndpointMapping, 0, len(mappings))
	for _, m := range mappings {
		cfms = append(cfms, m.cfem)
	}
	cfc.Log().Info().Any("mapping", cfms).Msg("Wrote cloudflared config")
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

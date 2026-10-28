package svc

import (
	"context"
	"fmt"
	"sort"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/informer"
	"github.com/mabels/cloudflared-controller/controller/k8s_data"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/mabels/cloudflared-controller/utils"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

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

			svcUrl := fmt.Sprintf("%s://%s.%s%s", schema, svc.Name, svc.Namespace, urlPort)
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

func handleEvent(cfc types.CFController, ev watch.Event) {
	svc, ok := ev.Object.(*corev1.Service)
	if !ok {
		cfc.Log().Error().Msg("Failed to cast to Service")
		return
	}
	log := cfc.Log().With().Str("namespace", svc.Namespace).Logger()
	log.Debug().Str("event", string(ev.Type)).Str("name", svc.Name).Msg("Received event")
	annotations := svc.GetAnnotations()
	_, foundCTN := annotations[config.AnnotationCloudflareTunnelName()]
	if !foundCTN || ev.Type == watch.Deleted {
		cfc.K8sData().TunnelConfigMaps.RemoveConfigMap(cfc, "service", &svc.ObjectMeta)
		return
	}
	switch ev.Type {
	case watch.Added, watch.Modified:
		err := updateConfigMap(cfc, svc)
		if err != nil {
			log.Error().Err(err).Msg("Failed to update config")
		}
	default:
		log.Error().Msgf("Unknown event type: %s", ev.Type)
	}
}

// Start watches services until ctx is done.
func Start(_cfc types.CFController, ctx context.Context) error {
	cfc := _cfc.WithComponent("svc")
	inf := informer.New[*corev1.Service](cfc, func(f informers.SharedInformerFactory) cache.SharedIndexInformer {
		return f.Core().V1().Services().Informer()
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

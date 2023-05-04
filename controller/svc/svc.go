package svc

import (
	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/tunnel"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func WatchSvc(cfc *controller.CFController, namespace string) (watch.Interface, error) {
	log := cfc.Log.With().Str("namespace", namespace).Str("component", "watchSvc").Logger()
	cmIf, err := cfc.Rest.K8s.CoreV1().Services(namespace).Watch(cfc.Context, metav1.ListOptions{})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to watch Services")
	}
	go func() {
		for {
			ev, more := <-cmIf.ResultChan()
			if !more {
				break
			}
			cm, ok := ev.Object.(*corev1.Service)
			if !ok {
				log.Error().Msg("Failed to cast to Service")
				continue
			}
			if cm.Namespace != namespace {
				log.Error().Msg("Services not in watched namespace")
				continue
			}
			// tunnelId, foundTunnelId := cm.ObjectMeta.Annotations[config.AnnotationCloudflareTunnelId]
			tunnelName, foundTunnelName := cm.ObjectMeta.Annotations[config.AnnotationCloudflareTunnelName]
			if !foundTunnelName {
				// log.Warn().Str("name", cm.Name).Msg("ConfigMap missing CloudflareTunnelId or CloudflareTunnelName annotation")
				continue
			}
			_, err := tunnel.GetTunnel(cfc, tunnel.UpsertTunnelParams{
				Name: &tunnelName,
			})
			if err != nil {
				log.Error().Str("tunnel", tunnelName).Err(err).Msg("Failed to get tunnel")
				continue
			}
			switch ev.Type {
			case watch.Added:
				// start tunnel
				log.Info().Str("tunnel", tunnelName).Str("name", cm.Name).Msg("Service Added")
			case watch.Deleted:
				// delete tunnel
				log.Info().Str("tunnel", tunnelName).Str("name", cm.Name).Msg("Service Delete")
			case watch.Modified:
				// update tunnel
				log.Info().Str("tunnel", tunnelName).Str("name", cm.Name).Msg("Service Modified")
			case watch.Error:
			default:
				log.Error().Str("type", string(ev.Type)).Msg("Unknown event type")
				continue
			}
		}
	}()
	return cmIf, nil
}

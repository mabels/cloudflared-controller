package svc

import (
	"context"

	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/config"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func WatchSvc(cfc *controller.CFController, namespace string) (watch.Interface, error) {
	log := cfc.Log.With().Str("namespace", namespace).Str("component", "watchSvc").Logger()
	cmIf, err := cfc.Rest.K8s.CoreV1().Services(namespace).Watch(context.Background(), metav1.ListOptions{})
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
			tunnelId, foundTunnelId := cm.ObjectMeta.Annotations[config.AnnotationCloudflareTunnelId]
			tunnelName, foundTunnelName := cm.ObjectMeta.Annotations[config.AnnotationCloudflareTunnelName]
			if !(foundTunnelId || foundTunnelName) {
				// log.Warn().Str("name", cm.Name).Msg("ConfigMap missing CloudflareTunnelId or CloudflareTunnelName annotation")
				continue
			}
			var tunnel string
			if foundTunnelId {
				tunnel = tunnelId
			} else {
				tunnel = tunnelName
			}
			switch ev.Type {
			case watch.Added:
				// start tunnel
				log.Info().Str("tunnel", tunnel).Str("name", cm.Name).Msg("Service Added")
			case watch.Deleted:
				// delete tunnel
				log.Info().Str("tunnel", tunnel).Str("name", cm.Name).Msg("Service Delete")
			case watch.Modified:
				// update tunnel
				log.Info().Str("tunnel", tunnel).Str("name", cm.Name).Msg("Service Modified")
			case watch.Error:
			default:
				log.Error().Str("type", string(ev.Type)).Msg("Unknown event type")
				continue
			}
		}
	}()
	return cmIf, nil
}

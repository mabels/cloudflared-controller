package config_maps

import (
	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/tunnel"

	// "github.com/mabels/cloudflared-controller/controller/tunnel"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func buildConfigMap(log *zerolog.Logger, configMap *corev1.ConfigMap) ([]byte, error) {
	cmap := config.CFConfigYaml{}
	err := yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), &cmap)
	if err != nil {
		return nil, err
	}
	for k, v := range configMap.Data {
		if k == "config.yaml" {
			continue
		}
		cci := config.CFConfigIngress{}
		err := yaml.Unmarshal([]byte(v), &cci)
		if err != nil {
			log.Error().Str("key", k).Str("value", v).Err(err).Msg("Failed to unmarshal config item")
			continue
		}
		cmap.Ingress = append(cmap.Ingress, cci)
	}
	cmap.Ingress = append(cmap.Ingress, config.CFConfigIngress{Service: "http_status:404"})
	return yaml.Marshal(cmap)

}

type ConfigMapTunnelEvent struct {
	Cm corev1.ConfigMap
	Ev watch.Event
}

func WatchTunnelConfigMap(cfc *controller.CFController, cteEv chan ConfigMapTunnelEvent) controller.WatchFunc {
	return func(_cfc *controller.CFController, namespace string) (watch.Interface, error) {
		cfc := _cfc.WithComponent("WatchTunnelConfigMap", func(cfc *controller.CFController) {
			my := cfc.Log.With().Str("namespace", namespace).Logger()
			cfc.Log = &my
		})
		log := cfc.Log
		wif, err := cfc.Rest.K8s.CoreV1().ConfigMaps(namespace).Watch(cfc.Context, metav1.ListOptions{})
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to WatchTunnelConfigMap")
		}
		go func() {
			log.Info().Msg("Start Watching TunnelConfigMap")
			for {
				ev, more := <-wif.ResultChan()
				if !more {
					close(cteEv)
					break
				}
				cm, ok := ev.Object.(*corev1.ConfigMap)
				if !ok {
					log.Error().Msg("Failed to cast to ConfigMap")
					continue
				}
				if cm.Namespace != namespace {
					log.Error().Msg("ConfigMap not in watched namespace")
					continue
				}
				// tunnelId, foundTunnelId := cm.ObjectMeta.Annotations[config.AnnotationCloudflareTunnelId]
				tunnelName, foundTunnelName := cm.ObjectMeta.Annotations[config.AnnotationCloudflareTunnelName]
				if !foundTunnelName {
					log.Debug().Msg("skip -- TunnelConfigMap without the required annotations")
					continue
				}
				// id, err := uuid.Parse(tunnelId)
				// if err != nil {
				// 	log.Error().Err(err).Msg("Failed to parse UUID")
				// 	continue
				// }
				_, err = tunnel.GetTunnel(cfc, tunnel.UpsertTunnelParams{
					// TunnelID:  &id,
					Name:      &tunnelName,
					Namespace: namespace,
				})
				if err != nil {
					log.Error().Err(err).Msg("Failed to get tunnel")
					continue
				}
				cteEv <- ConfigMapTunnelEvent{
					Cm: *cm,
					Ev: ev,
				}
			}
			log.Info().Msg("Stop Watching TunnelConfigMap")
		}()
		return wif, nil
	}
}

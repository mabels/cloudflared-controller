package config_maps

import (
	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/namespaces"
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

func WatchTunnelConfigMap(cfc *controller.CFController, cteEv chan ConfigMapTunnelEvent) namespaces.WatchFunc {
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

// func WatchConfigMaps(_cfc *controller.CFController, namespace string) (watch.Interface, error) {
// 	cfc := _cfc.WithComponent("watchConfigMaps", func(cfc *controller.CFController) {
// 		my := cfc.Log.With().Str("namespace", namespace).Logger()
// 		cfc.Log = &my
// 	})
// 	log := cfc.Log
// 	wif, err := cfc.Rest.K8s.CoreV1().ConfigMaps(namespace).Watch(context.Background(), metav1.ListOptions{})
// 	if err != nil {
// 		log.Fatal().Err(err).Msg("Failed to watch ConfigMaps")
// 	}
// 	go func() {
// 		log.Info().Msg("Start Watching ConfigMap")
// 		for {
// 			ev, more := <-wif.ResultChan()
// 			if !more {
// 				break
// 			}
// 			cm, ok := ev.Object.(*corev1.ConfigMap)
// 			if !ok {
// 				log.Error().Msg("Failed to cast to ConfigMap")
// 				continue
// 			}
// 			if cm.Namespace != namespace {
// 				log.Error().Msg("ConfigMap not in watched namespace")
// 				continue
// 			}
// 			// tunnelId, foundTunnelId := cm.ObjectMeta.Annotations[config.AnnotationCloudflareTunnelId]
// 			tunnelName, foundTunnelName := cm.ObjectMeta.Annotations[config.AnnotationCloudflareTunnelName]
// 			if !foundTunnelName {
// 				// log.Warn().Str("name", cm.Name).Msg("ConfigMap missing CloudflareTunnelId or CloudflareTunnelName annotation")
// 				continue
// 			}
// 			utp := tunnel.UpsertTunnelParams{
// 				Name: &tunnelName,
// 			}
// 			log := log.With().Str("name", tunnelName).Logger()
// 			ctoken, err := tunnel.GetTunnel(cfc, utp)
// 			if err != nil {
// 				log.Error().Err(err).Msg("Failed to get tunnel")
// 				continue
// 			}
// 			log = log.With().Str("id", ctoken.ID.String()).Logger()
// 			secretName := fmt.Sprintf("cf-tunnel-key.%s", utp.Name)
// 			secret, err := cfc.Rest.K8s.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
// 			if err != nil {
// 				log.Error().Str("secretName", secretName).Err(err).Msg("Failed to get secret")
// 				continue
// 			}
// 			secretFilename := fmt.Sprintf("%s.json", ctoken.ID)
// 			err = os.WriteFile(secretFilename, secret.Data["credentials.json"], 0600)
// 			if err != nil {
// 				log.Error().Str("name", secretFilename).Err(err).Msg("Failed to write secret")
// 				continue
// 			}
// 			configMapName := fmt.Sprintf("cf-tunnel-cfg.%s", *utp.Name)
// 			configMap, err := cfc.Rest.K8s.CoreV1().ConfigMaps(namespace).Get(context.Background(), configMapName, metav1.GetOptions{})
// 			if err != nil {
// 				log.Error().Str("name", configMapName).Err(err).Msg("Failed to get configMap")
// 				continue
// 			}
// 			configMapBytes, err := buildConfigMap(cfc.Log, configMap)
// 			if err != nil {
// 				log.Error().Str("name", configMapName).Err(err).Msg("Failed to build config.yaml")
// 				continue
// 			}
// 			configMapFilename := "config.yaml"
// 			err = os.WriteFile(configMapFilename, configMapBytes, 0600)
// 			if err != nil {
// 				log.Error().Str("name", configMapFilename).Err(err).Msg("Failed to write config.yaml")
// 				continue
// 			}

// 			switch ev.Type {
// 			case watch.Added:
// 				// start tunnel
// 				log.Info().Str("name", cm.Name).Msg("Start Tunnel")
// 			case watch.Deleted:
// 				// delete tunnel
// 				log.Info().Str("name", cm.Name).Msg("Delete Tunnel")
// 			case watch.Modified:
// 				// update tunnel
// 				log.Info().Str("name", cm.Name).Msg("Update Tunnel")
// 			case watch.Error:
// 			default:
// 				log.Error().Str("type", string(ev.Type)).Msg("Unknown event type")
// 				continue
// 			}
// 		}
// 		log.Info().Msg("Stopped Watching ConfigMap")
// 	}()
// 	return wif, nil
// }

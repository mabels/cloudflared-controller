package config_maps

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
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

func WatchConfigMaps(_cfc *controller.CFController, namespace string) (watch.Interface, error) {
	cfc := _cfc.WithComponent("watchConfigMaps", func(cfc *controller.CFController) {
		my := cfc.Log.With().Str("namespace", namespace).Logger()
		cfc.Log = &my
	})
	log := cfc.Log
	cmIf, err := cfc.Rest.K8s.CoreV1().ConfigMaps(namespace).Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to watch ConfigMaps")
	}
	go func() {
		for {
			ev := <-cmIf.ResultChan()
			cm, ok := ev.Object.(*corev1.ConfigMap)
			if !ok {
				log.Error().Msg("Failed to cast to ConfigMap")
				continue
			}
			if cm.Namespace != namespace {
				log.Error().Msg("ConfigMap not in watched namespace")
				continue
			}
			tunnelId, foundTunnelId := cm.ObjectMeta.Annotations[config.AnnotationCloudflareTunnelId]
			tunnelName, foundTunnelName := cm.ObjectMeta.Annotations[config.AnnotationCloudflareTunnelName]
			if !(foundTunnelId && foundTunnelName) {
				// log.Warn().Str("name", cm.Name).Msg("ConfigMap missing CloudflareTunnelId or CloudflareTunnelName annotation")
				continue
			}
			utp := tunnel.UpsertTunnelParams{}
			if foundTunnelId {
				uid := uuid.MustParse(tunnelId)
				utp.TunnelID = &uid
			} else {
				utp.Name = &tunnelName
			}
			ctoken, err := tunnel.GetTunnel(cfc, utp)
			if err != nil {
				log.Error().Str("id", tunnelId).Str("name", tunnelName).Err(err).Msg("Failed to get tunnel")
				continue
			}
			secretName := fmt.Sprintf("cf-tunnel-key.%s", ctoken.Name)
			secret, err := cfc.Rest.K8s.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
			if err != nil {
				log.Error().Str("name", secretName).Err(err).Msg("Failed to get secret")
				continue
			}
			secretFilename := fmt.Sprintf("%s.json", ctoken.ID)
			err = os.WriteFile(secretFilename, secret.Data["credentials.json"], 0600)
			if err != nil {
				log.Error().Str("name", secretFilename).Err(err).Msg("Failed to write secret")
				continue
			}
			configMapName := fmt.Sprintf("cf-tunnel-cfg.%s", ctoken.ID.String())
			configMap, err := cfc.Rest.K8s.CoreV1().ConfigMaps(namespace).Get(context.Background(), configMapName, metav1.GetOptions{})
			if err != nil {
				log.Error().Str("name", configMapName).Err(err).Msg("Failed to get configMap")
				continue
			}
			configMapBytes, err := buildConfigMap(cfc.Log, configMap)
			if err != nil {
				log.Error().Str("name", configMapName).Err(err).Msg("Failed to build config.yaml")
				continue
			}
			configMapFilename := "config.yaml"
			err = os.WriteFile(configMapFilename, configMapBytes, 0600)
			if err != nil {
				log.Error().Str("name", configMapFilename).Err(err).Msg("Failed to write config.yaml")
				continue
			}

			switch ev.Type {
			case watch.Added:
				// start tunnel
				log.Info().Str("name", cm.Name).Msg("Start Tunnel")
			case watch.Deleted:
				// delete tunnel
				log.Info().Str("name", cm.Name).Msg("Delete Tunnel")
			case watch.Modified:
				// update tunnel
				log.Info().Str("name", cm.Name).Msg("Update Tunnel")
			case watch.Error:
			default:
				log.Error().Str("type", string(ev.Type)).Msg("Unknown event type")
				continue
			}
		}
	}()
	return cmIf, nil
}

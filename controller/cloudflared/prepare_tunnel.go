package cloudflared

import (
	"crypto/rand"
	"fmt"

	"github.com/cloudflare/cloudflare-go/v7/shared"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/k8s_data"
	"github.com/mabels/cloudflared-controller/controller/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func findTunnelFromCF(cfc types.CFController, tp *types.CFTunnelParameter) (*shared.CloudflareTunnel, error) {
	tunnel, err := cfc.Rest().CF().FindTunnel(cfc.Context(), config.CfTunnelName(cfc, tp))
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Error listing tunnels")
		return nil, err
	}
	if tunnel == nil {
		cfc.Log().Debug().Str("name", config.CfTunnelName(cfc, tp)).Msg("No tunnel found")
		return nil, nil
	}
	cfc.Log().Debug().Msgf("Found tunnel: %s/%s", tunnel.ID, tunnel.Name)
	return tunnel, nil
}

func routeCFDns(cfc types.CFController, tunnelId uuid.UUID, cm *corev1.ConfigMap) {
	for _, yamlRules := range cm.Data {
		rules := []types.CFConfigIngress{}
		err := yaml.Unmarshal([]byte(yamlRules), &rules)
		if err != nil {
			cfc.Log().Error().Err(err).Str("rules", yamlRules).Msg("error unmarshalling rules")
			continue
		}
		for _, rule := range rules {
			if rule.Hostname == "" {
				continue
			}
			err := cfc.Rest().CF().RouteDNS(cfc.Context(), tunnelId.String(), rule.Hostname)
			if err != nil {
				cfc.Log().Error().Str("dnsName", rule.Hostname).Err(err).Msg("Error routing tunnel")
			}
		}
	}
}

func updateCFTunnel(cfc types.CFController, tparam *types.CFTunnelParameterWithID, cm *corev1.ConfigMap) error {
	routeCFDns(cfc, tparam.ID, cm)
	cm = cm.DeepCopy()
	if cm.Annotations == nil {
		cm.Annotations = make(map[string]string)
	}
	cm.Annotations[config.AnnotationCloudflareTunnelId()] = tparam.ID.String()
	cm.Annotations[config.AnnotationCloudflareTunnelCFDName()] = config.CfTunnelName(cfc, &tparam.CFTunnelParameter)
	return k8s_data.UpsertConfigMap(cfc, &tparam.CFTunnelParameter, cm)
}

func createCFTunnel(cfc types.CFController, tp *types.CFTunnelParameter, ometa *metav1.ObjectMeta) (*types.CFTunnelParameterWithID, error) {
	byteSecret := make([]byte, 32)
	if _, err := rand.Read(byteSecret); err != nil {
		return nil, err
	}

	ts, err := cfc.Rest().CF().CreateTunnel(cfc.Context(), config.CfTunnelName(cfc, tp), byteSecret)
	if err != nil {
		cfc.Log().Error().Str("name", tp.Name).Err(err).Msg("Error creating tunnel")
		return nil, err
	}
	id, err := uuid.Parse(ts.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid tunnel id %s: %w", ts.ID, err)
	}
	cfc.Log().Info().Str("name", ts.Name).Str("id", ts.ID).Msg("Created tunnel")
	tpwi := &types.CFTunnelParameterWithID{
		CFTunnelParameter: *tp,
		ID:                id,
	}
	_, err = k8s_data.CreateSecret(cfc, tpwi, byteSecret, ometa)
	if err != nil {
		cfc.Log().Error().Str("name", tp.Name).Err(err).Msg("Error creating secret")
		if derr := cfc.Rest().CF().DeleteTunnel(cfc.Context(), ts.ID); derr != nil {
			cfc.Log().Error().Str("name", tp.Name).Err(derr).Msg("Error deleting tunnel")
		}
		return nil, err
	}
	return tpwi, nil
}

func validateCFTunnel(cfc types.CFController, tp *types.CFTunnelParameter, cm *corev1.ConfigMap) error {
	tunnel, err := findTunnelFromCF(cfc, tp)
	if err != nil {
		return err
	}
	if tunnel != nil {
		_, err := k8s_data.FetchSecret(cfc, tp.K8SSecretName().Namespace, tp.K8SSecretName().Name, tunnel.ID)
		if err != nil {
			cfc.Log().Error().Err(err).Str("tunnelId", tunnel.ID).Msg("Tunnel exists but its secret is missing or does not match")
			return err
		}
		id, err := uuid.Parse(tunnel.ID)
		if err != nil {
			return fmt.Errorf("invalid tunnel id %s: %w", tunnel.ID, err)
		}
		return updateCFTunnel(cfc, &types.CFTunnelParameterWithID{
			CFTunnelParameter: *tp,
			ID:                id,
		}, cm)
	}
	tpwi, err := createCFTunnel(cfc, tp, &cm.ObjectMeta)
	if err != nil {
		return err
	}
	return updateCFTunnel(cfc, tpwi, cm)
}

func deleteCFTunnel(cfc types.CFController, tp *types.CFTunnelParameter) {
	tunnel, err := findTunnelFromCF(cfc, tp)
	if err != nil {
		return
	}
	if tunnel != nil {
		err = cfc.Rest().CF().DeleteTunnel(cfc.Context(), tunnel.ID)
		if err != nil {
			// keep the secret, the tunnel still exists
			cfc.Log().Error().Err(err).Str("tunnelId", tunnel.ID).Msg("Error deleting tunnel")
			return
		}
		cfc.Log().Info().Str("name", tunnel.Name).Str("id", tunnel.ID).Msg("Deleted tunnel")
	}
	err = k8s_data.DeleteSecret(cfc, tp)
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Error deleting secret")
	}
}

func ConfigMapHandlerPrepareCloudflared(_cfc types.CFController) func() {
	cfc := _cfc.WithComponent("ConfigMapHandlerPrepareCloudflared")
	return cfc.K8sData().TunnelConfigMaps.Register(func(_ []*corev1.ConfigMap, ev watch.Event) {
		cm, found := ev.Object.(*corev1.ConfigMap)
		if !found {
			cfc.Log().Error().Msg("error casting object")
			return
		}

		tparam, err := k8s_data.NewUniqueTunnelParams().GetConfigMapTunnelParam(cfc, &cm.ObjectMeta)
		if err != nil {
			cfc.Log().Error().Err(err).Msg("error getting tunnel param")
			return
		}
		cfc := cfc.WithComponent("ConfigMapHandlerPrepareCloudflared", func(c types.CFController) {
			log := c.Log().With().Str("tunnel", tparam.Name).Logger()
			c.SetLog(&log)
		})

		switch ev.Type {
		case watch.Added, watch.Modified:
			validateCFTunnel(cfc, tparam, cm)
		case watch.Deleted:
			deleteCFTunnel(cfc, tparam)
		default:
			cfc.Log().Error().Str("event", string(ev.Type)).Msg("unknown event type")
		}
	})
}

package cloudflared

import (
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/cloudflare/cloudflared/cfapi"
	"gopkg.in/yaml.v3"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/k8s_data"
	"github.com/mabels/cloudflared-controller/controller/types"

	// "github.com/mabels/cloudflared-controller/controller/config_maps"

	"github.com/google/uuid"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

// type UpsertTunnelParams struct {
// 	Name     *string
// 	TunnelID *uuid.UUID
// 	// ExternalName string
// 	Namespace string
// 	// SecretName        string
// 	// DefaultSecretName bool
// 	// x Labels      map[string]string
// 	// x Annotations map[string]string
// 	// Ingress           *netv1.Ingress
// 	// rs                cfapi.ResourceContainer
// }

// var reSanitzeAlpha = regexp.MustCompile(`[^a-zA-Z0-9]+`)
// var reSanitzeNice = regexp.MustCompile(`[^_\\-\\.a-zA-Z0-9]+`)

func findTunnelFromCF(cfc types.CFController, tp *types.CFTunnelParameter) ([]cfapi.TunnelWithToken, error) {
	tf := cfapi.NewTunnelFilter()
	tf.ByName(config.CfTunnelName(cfc, tp))

	cfclient, err := cfc.Rest().CFClientWithoutZoneID()
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Can't find CF client")
		return nil, err
	}
	ts, err := cfclient.ListTunnels(tf)
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Error listing tunnels")
		return nil, err
	}
	var foundTs *cfapi.Tunnel
	for _, t := range ts {
		if t.DeletedAt.IsZero() {
			foundTs = t
			break
		}
	}
	if foundTs == nil {
		// err := fmt.Errorf("No tunnel found for name %v", tp.Name)
		cfc.Log().Debug().Err(err).Any("ts", ts).Msg("No tunnels found")
		return nil, err
	}
	cfc.Log().Debug().Msgf("Found tunnel: %s/%s", foundTs.ID, foundTs.Name)
	twt := cfapi.TunnelWithToken{
		Tunnel: *foundTs,
	}
	return []cfapi.TunnelWithToken{twt}, nil
}

// func upsertTunnel(cfc types.CFController, tp *types.CFTunnelParameter, ometa *metav1.ObjectMeta) (*types.CFTunnelSecret, error) {
// 	cfClient, err := cfc.Rest().CFClientWithoutZoneID()
// 	if err != nil {
// 		cfc.Log().Error().Err(err).Msg("Can't find CF client")
// 		return nil, err
// 	}
// 	ts, err := findTunnelFromCF(cfc, tp)
// 	var cts *types.CFTunnelSecret

// 	if err != nil && strings.HasPrefix(err.Error(), "No tunnel found for name ") {

// 		var secretStr string
// 		byteSecret := make([]byte, 32)
// 		rand.Read(byteSecret)
// 		secretStr = base64.StdEncoding.EncodeToString(byteSecret)
// 		cfc.Log().Debug().Str("secretName", tp.K8SSecretName().FQDN).Msg("Secret not found, creating new secret")
// 		ts, err = cfClient.CreateTunnel(cfTunnelName(cfc, tp), byteSecret)
// 		if err != nil {
// 			cfc.Log().Error().Str("name", tp.Name).Err(err).Msg("Error creating tunnel")
// 			return nil, err
// 		}
// 		cfc.Log().Debug().Str("name", tp.Name).Str("id", ts.ID.String()).Err(err).Msg("created tunnel")
// 		cts = &types.CFTunnelSecret{
// 			AccountTag:   cfc.Cfg().CloudFlare.AccountId,
// 			TunnelSecret: secretStr,
// 			TunnelID:     ts.ID,
// 		}
// 		// tp.TunnelID = &ts.ID
// 		ctsBytes, err := json.Marshal(cts)
// 		if err != nil {
// 			cfc.Log().Error().Err(err).Str("name", tp.Name).Msg("Error marshalling credentials")
// 			return nil, err
// 		}

// 		secretClient := cfc.Rest().K8s().CoreV1().Secrets(tp.K8SSecretName().Namespace)
// 		_, err = secretClient.Get(cfc.Context(), tp.K8SConfigMapName().Name, metav1.GetOptions{})
// 		k8sSecret := corev1.Secret{
// 			ObjectMeta: metav1.ObjectMeta{
// 				Name:        tp.K8SSecretName().Name,
// 				Namespace:   tp.K8SConfigMapName().Namespace,
// 				Annotations: config.CfAnnotations(ometa.Annotations, tp),
// 				Labels:      config.CfLabels(ometa.Labels, cfc),
// 			},
// 			Data: map[string][]byte{
// 				"credentials.json": ctsBytes,
// 			},
// 		}

// 		if err != nil {
// 			secret, err := secretClient.Create(cfc.Context(), &k8sSecret, metav1.CreateOptions{})
// 			if err != nil {
// 				cfc.Log().Error().Str("name", secret.GetObjectMeta().GetNamespace()+"/"+secret.GetObjectMeta().GetName()).Err(err).Msg("Error creating secret")
// 				return nil, err
// 			}
// 		} else {
// 			secret, err := secretClient.Update(cfc.Context(), &k8sSecret, metav1.UpdateOptions{})
// 			if err != nil {
// 				cfc.Log().Error().Str("name", secret.GetObjectMeta().GetNamespace()+"/"+secret.GetObjectMeta().GetName()).Err(err).Msg("Error update secret")
// 				return nil, err
// 			}
// 		}
// 	} else if err != nil {
// 		cfc.Log().Error().Str("name", tp.Name).Err(err).Msg("Error getting tunnel")
// 		return nil, err
// 	} else {
// 		// tp.Name = ts.Name
// 		// tp.TunnelID = &ts.ID
// 		cts, err = MatchK8SSecret(cfc, tp)
// 		if err != nil {
// 			cfc.Log().Error().Str("tunnelId", ts.ID.String()).Str("secretName", tp.K8SConfigMapName().FQDN).Err(err).Msg("Error matching secret")
// 			return nil, err
// 		}
// 		cfc.Log().Debug().Str("name", tp.Name).Str("id", ts.ID.String()).Err(err).Msg("found tunnel")
// 	}

// 	return cts, nil
// }

// func retTunnelNameFromIngress(ingress *netv1.Ingress) *string {
// 	name, ok := ingress.Annotations[config.AnnotationCloudflareTunnelName]
// 	if ok {
// 		return &name
// 	}
// 	return &ingress.Name
// }

func registerCFDnsEndpoint(cfc types.CFController, tunnelId uuid.UUID, name string) error {
	parts := strings.Split(strings.Trim(strings.TrimSpace(name), "."), ".")
	if len(parts) < 2 {
		err := fmt.Errorf("Invalid DNS name: %s", name)
		return err
	}
	domain := fmt.Sprintf("%s.%s", parts[len(parts)-2], parts[len(parts)-1])
	cfClient, err := cfc.Rest().GetCFClientForDomain(domain)
	if err != nil {
		cfc.Log().Error().Str("dnsName", name).Err(err).Msg("Error getting CF client")
		return err
	}
	_, err = cfClient.RouteTunnel(tunnelId, cfapi.NewDNSRoute(name, true))
	if err != nil && !strings.HasPrefix(err.Error(), "Failed to add route: code: 1003") {
		cfc.Log().Error().Str("dnsName", name).Err(err).Msg("Error routing tunnel")
		return err
	}
	return nil
}

// func parseTunnelName(cfc types.CFController, ometa *v1.ObjectMeta) (ns string, name string, err error) {
// 	ns, ok := ometa.Annotations[config.AnnotationCloudflareTunnelName]
// 	if !ok {
// 		return "", "", fmt.Errorf("No tunnel name annotation")
// 	}
// 	parts := strings.Split(strings.TrimSpace(name), "/")
// 	if len(parts) >= 2 {
// 		ns = parts[0]
// 		name = parts[1]
// 	} else {
// 		ns = cfc.Cfg().CloudFlare.TunnelConfigMapNamespace
// 		name = parts[0]
// 	}
// 	if name == "" {
// 		return "", "", fmt.Errorf("No tunnel name")
// 	}
// 	return
// }

// func prepareTunnel(cfc types.CFController, ometa *v1.ObjectMeta) (*types.CFTunnelParameter, error) {
// 	ns, name, err := ParseTunnelName(cfc, ometa)
// 	if err != nil {
// 		return nil, err
// 	}
// 	tp := types.CFTunnelParameter{
// 		Namespace: ns,
// 		Name:      name,
// 	}
// 	ts, err := UpsertTunnel(cfc, &tp, ometa)
// 	if err != nil {
// 		cfc.Log().Error().Err(err).Msg("Failed to upsert tunnel")
// 		return nil, err
// 	}
// 	tp.ID = ts.TunnelID
// 	cfc.Log().Debug().Str("tunnel", tp.Name).Str("tunnelId", tp.ID.String()).Msg("Upserted tunnel")
// 	return &tp, nil
// }

func updateCFTunnel(cfc types.CFController, tparam *types.CFTunnelParameterWithID, cm *corev1.ConfigMap) error {
	// registerCFDnsEndpoint
	for _, yamlRules := range cm.Data {
		rules := []types.CFConfigIngress{}
		err := yaml.Unmarshal([]byte(yamlRules), &rules)
		if err != nil {
			cfc.Log().Error().Err(err).Str("rules", yamlRules).Msg("error unmarshalling rules")
			continue
		}
		for _, rule := range rules {
			registerCFDnsEndpoint(cfc, tparam.ID, rule.Hostname)
		}
	}
	// updateConfigMap state
	cm.Annotations[config.AnnotationCloudflareTunnelState()] = "ready"
	cm.Annotations[config.AnnotationCloudflareTunnelId()] = tparam.ID.String()
	cm.Annotations[config.AnnotationCloudflareTunnelCFDName()] = config.CfTunnelName(cfc, &tparam.CFTunnelParameter)
	return k8s_data.UpsertConfigMap(cfc, &tparam.CFTunnelParameter, cm)
}

func createCFTunnel(cfc types.CFController, tp *types.CFTunnelParameter, ometa *metav1.ObjectMeta) (*types.CFTunnelParameterWithID, error) {
	cfClient, err := cfc.Rest().CFClientWithoutZoneID()
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Can't find CF client")
		return nil, err
	}

	byteSecret := make([]byte, 32)
	rand.Read(byteSecret)

	// add cluster name from config
	ts, err := cfClient.CreateTunnel(config.CfTunnelName(cfc, tp), byteSecret)
	if err != nil {
		cfc.Log().Error().Str("name", tp.Name).Err(err).Msg("Error creating tunnel")
		return nil, err
	}
	_, err = k8s_data.CreateSecret(cfc, &types.CFTunnelParameterWithID{
		CFTunnelParameter: *tp,
		ID:                ts.ID,
	}, byteSecret, ometa)
	if err != nil {
		deleteCFTunnel(cfc, tp)
		cfc.Log().Error().Str("name", tp.Name).Err(err).Msg("Error creating secret")
		return nil, err
	}
	return &types.CFTunnelParameterWithID{
		CFTunnelParameter: *tp,
		ID:                ts.ID,
	}, nil
}

func validateCFTunnel(cfc types.CFController, tp *types.CFTunnelParameter, cm *corev1.ConfigMap) error {
	// findCFTunnel
	tunnels, err := findTunnelFromCF(cfc, tp)
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Error finding tunnel")
		return err
	}
	if len(tunnels) > 0 {
		// found
		_, err := k8s_data.FetchSecret(cfc, tp.K8SConfigMapName().Namespace, tp.K8SSecretName().Name, tunnels[0].ID.String())
		if err != nil {
			cfc.Log().Error().Err(err).Msg("Error fetching secret")
			return err
		}
		tpwi := types.CFTunnelParameterWithID{
			CFTunnelParameter: *tp,
			ID:                tunnels[0].ID,
		}
		return updateCFTunnel(cfc, &tpwi, cm)
	}
	tpwi, err := createCFTunnel(cfc, tp, &cm.ObjectMeta)
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Error creating tunnel")
		return err
	}
	return updateCFTunnel(cfc, tpwi, cm)
}

func deleteCFTunnel(cfc types.CFController, tp *types.CFTunnelParameter) {
	tunnels, err := findTunnelFromCF(cfc, tp)
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Error finding tunnel")
		return
	}
	if len(tunnels) != 0 {
		cfClient, err := cfc.Rest().CFClientWithoutZoneID()
		if err != nil {
			cfc.Log().Error().Err(err).Msg("Can't find CF client")
			return
		}
		err = cfClient.DeleteTunnel(tunnels[0].ID)
		if err != nil {
			cfc.Log().Error().Err(err).Msg("Error deleting tunnel")
			return
		}
	} else {
		cfc.Log().Info().Str("name", tp.Name).Msg("Tunnel not found")
		err = k8s_data.DeleteSecret(cfc, tp)
		if err != nil {
			cfc.Log().Error().Err(err).Msg("Error deleting tunnel")
			return
		}
	}
}

func ConfigMapHandlerPrepareCloudflared(_cfc types.CFController) func() {
	cfc := _cfc.WithComponent("ConfigMapHandlerPrepareCloudflared")
	unreg := cfc.K8sData().TunnelConfigMaps.Register(func(cms []*corev1.ConfigMap, ev watch.Event) {
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

		state, found := cm.Annotations[config.AnnotationCloudflareTunnelState()]
		if !found {
			cfc.Log().Error().Msg("error getting state")
			return
		}
		switch state {
		case "ready":
			cfc.Log().Debug().Msg("ignoring preparing state")
			return
		case "preparing":
		default:
			cfc.Log().Error().Str("state", state).Msg("unknown state")
			return
		}

		switch ev.Type {
		case watch.Added:
			validateCFTunnel(cfc, tparam, cm)
		case watch.Modified:
			validateCFTunnel(cfc, tparam, cm)
		case watch.Deleted:
			deleteCFTunnel(cfc, tparam)
		default:
			cfc.Log().Error().Str("event", string(ev.Type)).Msg("unknown event type")
		}
	})
	return func() {
		unreg()
	}
}

package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudflare/cloudflared/cfapi"

	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/config"

	// "github.com/mabels/cloudflared-controller/controller/config_maps"
	"github.com/rs/zerolog"

	"gopkg.in/yaml.v3"

	"github.com/google/uuid"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgo_corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type UpsertTunnelParams struct {
	Name              *string
	TunnelID          *uuid.UUID
	ExternalName      string
	Namespace         string
	SecretName        string
	DefaultSecretName bool
	Ingress           *netv1.Ingress
	// rs                cfapi.ResourceContainer
}

type CFTunnelSecret struct {
	AccountTag   string    `json:"AccountTag"`
	TunnelSecret string    `json:"TunnelSecret"`
	TunnelID     uuid.UUID `json:"TunnelID"`
}

func GetTunnel(cfc *controller.CFController, tp UpsertTunnelParams) (*cfapi.TunnelWithToken, error) {
	tf := cfapi.NewTunnelFilter()
	if tp.Name != nil {
		tf.ByName(*tp.Name)
	} else if tp.TunnelID != nil {
		tf.ByTunnelID(*tp.TunnelID)
	}
	ts, err := cfc.Rest.Cf.ListTunnels(tf)
	if err != nil {
		cfc.Log.Error().Err(err).Msg("Error listing tunnels")
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
		err := fmt.Errorf("No tunnel found for name %v", tp.Name)
		cfc.Log.Error().Err(err).Any("ts", ts).Msg("No tunnels found")
		return nil, err
	}
	cfc.Log.Debug().Msgf("Found tunnel: %s/%s", foundTs.ID, foundTs.Name)
	twt := cfapi.TunnelWithToken{
		Tunnel: *foundTs,
	}
	return &twt, nil
}

func GetTunnelSecret(log *zerolog.Logger, secret *corev1.Secret) (CFTunnelSecret, error) {
	credentialsJson := secret.Data["credentials.json"]
	// credentialsJson := make([]byte, base64.StdEncoding.DecodedLen(len(credentialsBytes)))
	// n, err := base64.StdEncoding.Decode(credentialsJson, credentialsBytes)
	// if err != nil {
	// 	log.Error().Err(err).Str("name", secret.GetObjectMeta().GetNamespace()+"/"+secret.GetObjectMeta().GetName()).Str("secretData", string(credentialsBytes)).Msg("Error decoding credentials")
	// 	return CFTunnelSecret{}, err
	// }
	var cts CFTunnelSecret
	err := json.Unmarshal(credentialsJson, &cts)
	if err != nil {
		log.Error().Err(err).Str("name", secret.GetObjectMeta().GetNamespace()+"/"+secret.GetObjectMeta().GetName()).Str("secretJson", string(credentialsJson)).Msg("Error unmarshal credentials")
		return CFTunnelSecret{}, err
	}
	return cts, nil
}

func MatchK8SSecret(cfc *controller.CFController, secretClient clientgo_corev1.SecretInterface, tunnelId string, tp UpsertTunnelParams) (*CFTunnelSecret, error) {
	secret, err := secretClient.Get(context.Background(), tp.SecretName, metav1.GetOptions{})
	if err != nil {
		cfc.Log.Error().Err(err).Str("secretName", tp.SecretName).Msg("Secret not found")
		return nil, err
	}

	cts, err := GetTunnelSecret(cfc.Log, secret)
	if err != nil {
		return nil, err
	}
	if cts.TunnelID.String() != tunnelId || cts.AccountTag != cfc.Cfg.CloudFlare.AccountId {
		cfc.Log.Error().Msgf("Secret does not match tunnelId or accountTag")
		return nil, fmt.Errorf("Secret does not match tunnelId or accountTag")
	}
	return &cts, nil
}

func UpsertTunnel(cfc *controller.CFController, tp UpsertTunnelParams) (*CFTunnelSecret, error) {
	secretClient := cfc.Rest.K8s.CoreV1().Secrets(tp.Namespace)
	ts, err := GetTunnel(cfc, tp)
	var cts *CFTunnelSecret
	if err != nil && strings.HasPrefix(err.Error(), "No tunnel found for name ") {
		if tp.Name == nil {
			err := fmt.Errorf("To create a new tunnel, a name must be provided")
			cfc.Log.Error().Err(err).Msg("Error creating tunnel")
			return nil, err
		}
		var secretStr string
		byteSecret := make([]byte, 32)
		rand.Read(byteSecret)
		secretStr = base64.StdEncoding.EncodeToString(byteSecret)
		cfc.Log.Debug().Str("secretName", tp.SecretName).Msg("Secret not found, creating new secret")
		ts, err = cfc.Rest.Cf.CreateTunnel(*tp.Name, byteSecret)
		if err != nil {
			cfc.Log.Error().Str("name", *tp.Name).Err(err).Msg("Error creating tunnel")
			return nil, err
		}
		cfc.Log.Debug().Str("name", *tp.Name).Str("id", ts.ID.String()).Err(err).Msg("created tunnel")
		cts = &CFTunnelSecret{
			AccountTag:   cfc.Cfg.CloudFlare.AccountId,
			TunnelSecret: secretStr,
			TunnelID:     ts.ID,
		}

		tp.TunnelID = &ts.ID

		ctsBytes, err := json.Marshal(cts)
		if err != nil {
			cfc.Log.Error().Err(err).Str("name", *tp.Name).Msg("Error marshalling credentials")
			return nil, err
		}

		_, err = secretClient.Get(context.Background(), tp.SecretName, metav1.GetOptions{})
		if err != nil {
			secret, err := secretClient.Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tp.SecretName,
					Namespace: tp.Namespace,
					Labels:    tp.Ingress.GetLabels(),
				},
				Data: map[string][]byte{
					"credentials.json": ctsBytes,
				},
			}, metav1.CreateOptions{})
			if err != nil {
				cfc.Log.Error().Str("name", secret.GetObjectMeta().GetNamespace()+"/"+secret.GetObjectMeta().GetName()).Err(err).Msg("Error creating secret")
				return nil, err
			}
		} else {
			secret, err := secretClient.Update(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tp.SecretName,
					Namespace: tp.Namespace,
					Labels:    tp.Ingress.GetLabels(),
				},
				Data: map[string][]byte{
					"credentials.json": ctsBytes,
				},
			}, metav1.UpdateOptions{})
			if err != nil {
				cfc.Log.Error().Str("name", secret.GetObjectMeta().GetNamespace()+"/"+secret.GetObjectMeta().GetName()).Err(err).Msg("Error update secret")
				return nil, err
			}
		}
	} else if err != nil {
		cfc.Log.Error().Str("name", *tp.Name).Err(err).Msg("Error getting tunnel")
		return nil, err
	} else {
		tp.Name = &ts.Name
		tp.TunnelID = &ts.ID
		cts, err = MatchK8SSecret(cfc, secretClient, ts.ID.String(), tp)
		if err != nil {
			cfc.Log.Error().Str("tunnelId", ts.ID.String()).Str("secretName", tp.SecretName).Err(err).Msg("Error matching secret")
			return nil, err
		}
		cfc.Log.Debug().Str("name", *tp.Name).Str("id", ts.ID.String()).Err(err).Msg("found tunnel")
	}

	return cts, nil
}

func GetTunnelNameFromIngress(ingress *netv1.Ingress) *string {
	name, ok := ingress.Annotations[config.AnnotationCloudflareTunnelName]
	if ok {
		return &name
	}
	return &ingress.Name
}

func WriteCloudflaredConfig(cfc *controller.CFController, tp UpsertTunnelParams, cts *CFTunnelSecret) error {
	_, err := cfc.Rest.Cf.RouteTunnel(cts.TunnelID, cfapi.NewDNSRoute(tp.ExternalName, true))
	if err != nil && !strings.HasPrefix(err.Error(), "Failed to add route: code: 1003") {
		cfc.Log.Error().Str("name", *tp.Name).Str("externalName", tp.ExternalName).Err(err).Msg("Error routing tunnel")
		return err
	}
	// jsonTunnelSecret, err := json.Marshal(cts)
	// if err != nil {
	// 	log.Error().Str("name", tp.name).Err(err).Msg("Marshaling credentials")
	// 	return err
	// }
	credFile := fmt.Sprintf("%s.json", cts.TunnelID.String())
	// err = os.WriteFile(credFile, jsonTunnelSecret, 0600)
	// if err != nil {
	// 	log.Error().Str("filename", credFile).Err(err).Msg("Writing credentials file")
	// 	return err
	// }

	cfcis := []config.CFConfigIngress{
		{
			Service: "http_status:404",
		},
	}
	for _, rule := range tp.Ingress.Spec.Rules {
		if rule.HTTP == nil {
			cfc.Log.Warn().Str("name", *tp.Name).Str("host", rule.Host).Msg("Skipping non-http ingress rule")
			continue
		}
		schema := "http"
		for _, tls := range tp.Ingress.Spec.TLS {
			for _, thost := range tls.Hosts {
				if thost == rule.Host {
					schema = "https"
					break
				}
			}
		}
		for _, path := range rule.HTTP.Paths {
			cci := config.CFConfigIngress{
				Hostname: tp.ExternalName,
				Path:     path.Path,
				Service:  fmt.Sprintf("%s://%s", schema, rule.Host),
				OriginRequest: &config.CFConfigOriginRequest{
					HttpHostHeader: rule.Host,
				},
			}
			cfcis = append([]config.CFConfigIngress{cci}, cfcis...)
		}
	}

	igss := config.CFConfigYaml{
		Tunnel:          cts.TunnelID.String(),
		CredentialsFile: credFile,
		Ingress:         []config.CFConfigIngress{},
	}
	yConfigYamlByte, err := yaml.Marshal(igss)
	if err != nil {
		cfc.Log.Error().Err(err).Msg("Error marshaling ingress")
		return err
	}

	yCFConfigIngressByte, err := yaml.Marshal(cfcis)
	if err != nil {
		cfc.Log.Error().Err(err).Msg("Error marshaling ingress")
		return err
	}
	// err = os.WriteFile("./config.yml", yByte, 0644)
	// if err != nil {
	// 	log.Error().Err(err).Msg("can't write config.yml")
	// 	return err
	// }
	cmName := fmt.Sprintf("cf-tunnel-cfg.%s", cts.TunnelID.String())
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: tp.Namespace,
			Labels:    tp.Ingress.GetLabels(),
			Annotations: map[string]string{
				config.AnnotationCloudflareTunnelId:   cts.TunnelID.String(),
				config.AnnotationCloudflareTunnelName: *tp.Name,
			},
		},
		Data: map[string]string{
			"config.yaml":                          string(yConfigYamlByte),
			string(tp.Ingress.ObjectMeta.GetUID()): string(yCFConfigIngressByte),
		},
	}

	_, err = cfc.Rest.K8s.CoreV1().ConfigMaps(tp.Namespace).Get(context.Background(), cmName, metav1.GetOptions{})
	if err != nil {
		_, err = cfc.Rest.K8s.CoreV1().ConfigMaps(tp.Namespace).Create(context.Background(), &cm, metav1.CreateOptions{})
	} else {
		_, err = cfc.Rest.K8s.CoreV1().ConfigMaps(tp.Namespace).Update(context.Background(), &cm, metav1.UpdateOptions{})
	}

	// 	tunnel: 82a2a30a-e48f-401a-b6be-e595f0ba47e2
	// credentials-file: /Users/menabe/.cloudflared/82a2a30a-e48f-401a-b6be-e595f0ba47e2.json

	// ingress:
	//   - hostname: meno-test.codebar.world
	//     service: https://hass-io-hh.adviser.com:443
	//     originRequest:
	//        httpHostHeader: hass-io-hh.adviser.com
	//   - service: http_status:404

	// tp.cfRestClient.UpdateTunnelConfiguration(context.Background(), &tp.rs, cfapi.TunnelConfigurationParams{
	// 	TunnelID: ts.ID,
	// 	Config: cfapi.TunnelConfiguration{
	// 		Ingress: []cfapi.UnvalidatedIngressRule{
	// 			cfapi.UnvalidatedIngressRule{
	// 				Hostname: tp.hostname,
	// 				Path:     "/",
	// 				Service:  "https://hass-io-hh.adviser.com:443",
	// 			},
	// 		},
	// 		// 	- hostname: meno-test.codebar.world
	// 		// 	  service: https://hass-io-hh.adviser.com:443
	// 		// 	  originRequest:
	// 		// 	    httpHostHeader: hass-io-hh.adviser.com
	// 		//   - service: http_status:404
	// 	},
	// })
	return err
}

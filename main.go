package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes"

	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"

	// "k8s.io/client-go/pkg/fields"
	networkingv1 "k8s.io/client-go/informers/networking/v1"
	clientgo_corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	// "github.com/cloudflare/cloudflare-go"
	"github.com/cloudflare/cloudflared/cfapi"
	"github.com/google/uuid"

	"github.com/rs/zerolog"
)

func getTunnel(log *zerolog.Logger, tp upsertTunnelParams) (*cfapi.TunnelWithToken, error) {
	tf := cfapi.NewTunnelFilter()
	tf.ByName(tp.name)
	ts, err := tp.cfRestClient.ListTunnels(tf)
	if err != nil {
		log.Error().Err(err).Msg("Error listing tunnels")
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
		err := fmt.Errorf("No tunnel found for name %v", tp.name)
		log.Error().Err(err).Any("ts", ts).Msg("No tunnels found")
		return nil, err
	}
	log.Debug().Msgf("Found tunnel: %s/%s", foundTs.ID, foundTs.Name)
	twt := cfapi.TunnelWithToken{
		Tunnel: *foundTs,
	}
	return &twt, nil
}

type upsertTunnelParams struct {
	cfRestClient      *cfapi.RESTClient
	accountID         string
	zoneID            string
	apiToken          string
	name              string
	externalName      string
	namespace         string
	secretName        string
	defaultSecretName bool
	ingress           *netv1.Ingress
	// rs                cfapi.ResourceContainer
}

type CFTunnelSecret struct {
	AccountTag   string    `json:"AccountTag"`
	TunnelSecret string    `json:"TunnelSecret"`
	TunnelID     uuid.UUID `json:"TunnelID"`
}

type CFConfigOriginRequest struct {
	HttpHostHeader string `yaml:"httpHostHeader,omitempty"`
}
type CFConfigIngress struct {
	Hostname      string                 `yaml:"hostname,omitempty"`
	Path          string                 `yaml:"path,omitempty"`
	Service       string                 `yaml:"service,omitempty"`
	OriginRequest *CFConfigOriginRequest `yaml:"originRequest,omitempty"`
}

type CFConfigYaml struct {
	Tunnel          string            `yaml:"tunnel"`
	CredentialsFile string            `yaml:"credentials-file"`
	Ingress         []CFConfigIngress `yaml:"ingress"`
}

func getTunnelSecret(log *zerolog.Logger, secret *corev1.Secret) (CFTunnelSecret, error) {
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

func matchK8SSecret(secretClient clientgo_corev1.SecretInterface, log *zerolog.Logger, tunnelId string, tp upsertTunnelParams) (*CFTunnelSecret, error) {
	secret, err := secretClient.Get(context.Background(), tp.secretName, metav1.GetOptions{})
	if err != nil {
		log.Error().Err(err).Str("secretName", tp.secretName).Msg("Secret not found")
		return nil, err
	}

	cts, err := getTunnelSecret(log, secret)
	if err != nil {
		return nil, err
	}
	if cts.TunnelID.String() != tunnelId || cts.AccountTag != tp.accountID {
		log.Error().Msgf("Secret does not match tunnelId or accountTag")
		return nil, fmt.Errorf("Secret does not match tunnelId or accountTag")
	}
	return &cts, nil
}

func upsertTunnel(log *zerolog.Logger, k8sClient *kubernetes.Clientset, tp upsertTunnelParams) (*CFTunnelSecret, error) {
	secretClient := k8sClient.CoreV1().Secrets(tp.namespace)
	ts, err := getTunnel(log, tp)
	var cts *CFTunnelSecret
	if err != nil && err.Error() == "No tunnel found for name "+tp.name {
		var secretStr string
		byteSecret := make([]byte, 32)
		rand.Read(byteSecret)
		secretStr = base64.StdEncoding.EncodeToString(byteSecret)
		log.Debug().Str("secretName", tp.secretName).Msg("Secret not found, creating new secret")
		ts, err = tp.cfRestClient.CreateTunnel(tp.name, byteSecret)
		if err != nil {
			log.Error().Str("name", tp.name).Err(err).Msg("Error creating tunnel")
			return nil, err
		}
		log.Debug().Str("name", tp.name).Str("id", ts.ID.String()).Err(err).Msg("created tunnel")
		cts = &CFTunnelSecret{
			AccountTag:   tp.accountID,
			TunnelSecret: secretStr,
			TunnelID:     ts.ID,
		}

		ctsBytes, err := json.Marshal(cts)
		if err != nil {
			log.Error().Err(err).Str("name", tp.name).Msg("Error marshalling credentials")
			return nil, err
		}

		_, err = secretClient.Get(context.Background(), tp.secretName, metav1.GetOptions{})
		if err != nil {
			secret, err := secretClient.Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tp.secretName,
					Namespace: tp.namespace,
					Labels:    tp.ingress.GetLabels(),
				},
				Data: map[string][]byte{
					"credentials.json": ctsBytes,
				},
			}, metav1.CreateOptions{})
			if err != nil {
				log.Error().Str("name", secret.GetObjectMeta().GetNamespace()+"/"+secret.GetObjectMeta().GetName()).Err(err).Msg("Error creating secret")
				return nil, err
			}
		} else {
			secret, err := secretClient.Update(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tp.secretName,
					Namespace: tp.namespace,
					Labels:    tp.ingress.GetLabels(),
				},
				Data: map[string][]byte{
					"credentials.json": ctsBytes,
				},
			}, metav1.UpdateOptions{})
			if err != nil {
				log.Error().Str("name", secret.GetObjectMeta().GetNamespace()+"/"+secret.GetObjectMeta().GetName()).Err(err).Msg("Error update secret")
				return nil, err
			}
		}
	} else if err != nil {
		log.Error().Str("name", tp.name).Err(err).Msg("Error getting tunnel")
		return nil, err
	} else {
		cts, err = matchK8SSecret(secretClient, log, ts.ID.String(), tp)
		if err != nil {
			log.Error().Str("tunnelId", ts.ID.String()).Str("secretName", tp.secretName).Err(err).Msg("Error matching secret")
			return nil, err
		}
		log.Debug().Str("name", tp.name).Str("id", ts.ID.String()).Err(err).Msg("found tunnel")
	}

	return cts, nil
}

const (
	CloudflareTunnelNameAnnotation = "cloudflare.com/tunnel-name"
	CloudflareTunnelExternalName   = "cloudflare.com/tunnel-external-name"
	CloudflareTunnelAccountId      = "cloudflare.com/tunnel-account-id"
	CloudflareTunnelZoneId         = "cloudflare.com/tunnel-zone-id"
	CloudflareTunnelKeySecret      = "cloudflare.com/tunnel-key-secret"
)

func getTunnelNameFromIngress(ingress *netv1.Ingress) string {
	name, ok := ingress.Annotations[CloudflareTunnelNameAnnotation]
	if ok {
		return name
	}
	return ingress.Name
}

func writeCloudflaredConfig(log *zerolog.Logger, ingress *netv1.Ingress, tp upsertTunnelParams, cts *CFTunnelSecret) error {

	_, err := tp.cfRestClient.RouteTunnel(cts.TunnelID, cfapi.NewDNSRoute(tp.externalName, true))
	if err != nil && !strings.HasPrefix(err.Error(), "Failed to add route: code: 1003") {
		log.Error().Str("name", tp.name).Str("externalName", tp.externalName).Err(err).Msg("Error routing tunnel")
		return err
	}
	jsonTunnelSecret, err := json.Marshal(cts)
	if err != nil {
		log.Error().Str("name", tp.name).Err(err).Msg("Marshaling credentials")
		return err
	}
	credFile := path.Join("./", fmt.Sprintf("%s.json", cts.TunnelID.String()))
	err = os.WriteFile(credFile, jsonTunnelSecret, 0600)
	if err != nil {
		log.Error().Str("filename", credFile).Err(err).Msg("Writing credentials file")
		return err
	}

	cfcis := []CFConfigIngress{
		{
			Service: "http_status:404",
		},
	}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			log.Warn().Str("name", tp.name).Str("host", rule.Host).Msg("Skipping non-http ingress rule")
			continue
		}
		schema := "http"
		for _, tls := range ingress.Spec.TLS {
			for _, thost := range tls.Hosts {
				if thost == rule.Host {
					schema = "https"
					break
				}
			}
		}
		for _, path := range rule.HTTP.Paths {
			cci := CFConfigIngress{
				Hostname: tp.externalName,
				Path:     path.Path,
				Service:  fmt.Sprintf("%s://%s", schema, rule.Host),
				OriginRequest: &CFConfigOriginRequest{
					HttpHostHeader: rule.Host,
				},
			}
			cfcis = append([]CFConfigIngress{cci}, cfcis...)
		}
	}

	igss := CFConfigYaml{
		Tunnel:          cts.TunnelID.String(),
		CredentialsFile: credFile,
		Ingress:         cfcis,
	}
	yByte, err := yaml.Marshal(igss)
	if err != nil {
		log.Error().Err(err).Msg("Error marshaling ingress")
		return err
	}
	err = os.WriteFile("./config.yml", yByte, 0644)
	if err != nil {
		log.Error().Err(err).Msg("can't write config.yml")
		return err
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
	return nil
}

func apiUrl() string {
	api_url, found := os.LookupEnv("TUUNNEL_API_URL")
	if !found {
		api_url = "https://api.cloudflare.com/client/v4"
	}
	return api_url
}

func main() {
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	kubeconfig := flag.String("kubeconfig", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")), "absolute path to the kubeconfig file")
	namespace := flag.String("namespace", "default", "namespace to watch")
	flag.Parse()
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatal().Err(err).Msg("Error building kubeconfig")
	}
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal().Err(err).Msg("Error building kubernetes clientset")
	}

	// cfapi, err := cfapi.New(os.Getenv("CLOUDFLARE_API_KEY"), os.Getenv("CLOUDFLARE_API_EMAIL"))
	// alternatively, you can use a scoped API token
	// cfRestClient, err := cfapi.NewWithAPIToken(os.Getenv("CLOUDFLARE_API_TOKEN"))

	// c.cert.AccountID,
	// c.cert.ZoneID,

	// u, err := cfapi.UserDetails(context.Background())
	// if err != nil {
	// 	log.Fatal(err)
	// }

	controller := networkingv1.NewIngressInformer(k8sClient, *namespace, time.Second,
		cache.Indexers{cache.NamespaceIndex: func(obj interface{}) ([]string, error) {
			ingress, ok := obj.(*netv1.Ingress)
			if !ok {
				log.Debug().Msgf("not an ingress %v", obj)
				return []string{}, nil
			}
			tp := upsertTunnelParams{}
			log.Debug().Str("name", ingress.Name).Msg("found ingress")
			annotations := ingress.GetAnnotations()
			tp.externalName, ok = annotations[CloudflareTunnelExternalName]
			if !ok {
				log.Debug().Msgf("does not have %s annotation", CloudflareTunnelExternalName)
				return []string{}, nil
			}
			tp.accountID, ok = annotations[CloudflareTunnelAccountId]
			if !ok {
				tp.accountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
			}
			tp.zoneID, ok = annotations[CloudflareTunnelZoneId]
			if !ok {
				tp.zoneID = os.Getenv("CLOUDFLARE_ZONE_ID")
			}
			if tp.accountID == "" || tp.zoneID == "" {
				err := fmt.Errorf("accountID and zoneID must be set")
				log.Error().Err(err).Msg("missing accountID or zoneID")
				return []string{}, nil
			}
			tp.apiToken, ok = os.LookupEnv("CLOUDFLARE_API_TOKEN")
			if !ok || tp.apiToken == "" {
				log.Error().Msg("missing CLOUDFLARE_API_TOKEN")
				return []string{}, nil
			}

			tp.cfRestClient, err = cfapi.NewRESTClient(
				apiUrl(),
				tp.accountID, // accountTag string,
				tp.zoneID,    // zoneTag string,
				tp.apiToken,
				"cloudflared-controller",
				&log)
			if err != nil {
				log.Fatal().Err(err).Msg("Failed to create cloudflare client")
			}
			tp.defaultSecretName = false
			tp.secretName, ok = ingress.Annotations[CloudflareTunnelKeySecret]
			if !ok {
				tp.defaultSecretName = true
				reSanitze := regexp.MustCompile(`[^a-zA-Z0-9]+`)
				tp.secretName = fmt.Sprintf("cf-tunnel-key.%s",
					reSanitze.ReplaceAllString(strings.ToLower(getTunnelNameFromIngress(ingress)), "-"))
			}
			tp.namespace = ingress.Namespace
			// tp.rs.Identifier = accountId
			tp.name = getTunnelNameFromIngress(ingress)
			tp.ingress = ingress
			ts, err := upsertTunnel(&log, k8sClient, tp)
			if err != nil {
				log.Error().Err(err).Msg("Failed to upsert tunnel")
				return []string{}, nil
			}
			log.Info().Str("tunnel", ts.TunnelID.String()).Str("externalName", tp.externalName).Msg("Upserted tunnel")

			writeCloudflaredConfig(&log, ingress, tp, ts)

			return []string{}, nil
		}},
	)
	stop := make(chan struct{})
	log.Debug().Str("kubeconfig", *kubeconfig).Msg("Starting controller")
	go controller.Run(stop)
	for {
		time.Sleep(time.Second)
	}
}

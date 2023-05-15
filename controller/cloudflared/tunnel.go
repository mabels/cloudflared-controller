package cloudflared

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudflare/cloudflared/cfapi"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/types"

	// "github.com/mabels/cloudflared-controller/controller/config_maps"
	"github.com/rs/zerolog"

	"gopkg.in/yaml.v3"

	"github.com/google/uuid"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CFEndpointMapping struct {
	External string
	Internal string
}

type UpsertTunnelParams struct {
	Name     *string
	TunnelID *uuid.UUID
	// ExternalName string
	Namespace string
	// SecretName        string
	// DefaultSecretName bool
	Labels      map[string]string
	Annotations map[string]string
	// Ingress           *netv1.Ingress
	// rs                cfapi.ResourceContainer
}

var reSanitzeAlpha = regexp.MustCompile(`[^a-zA-Z0-9]+`)
var reSanitzeNice = regexp.MustCompile(`[^_\\-\\.a-zA-Z0-9]+`)

type K8SResourceName struct {
	Namespace string
	Name      string
	FQDN      string
}

func FromFQDN(fqdn string, ns string) K8SResourceName {
	parts := strings.Split(fqdn, "/")
	name := parts[0]
	if len(parts) > 1 {
		ns = parts[0]
		name = parts[1]
	}
	return K8SResourceName{
		Namespace: ns,
		Name:      name,
		FQDN:      fqdn,
	}
}

func (tp UpsertTunnelParams) buildK8SResourceName(prefix string) K8SResourceName {
	name := fmt.Sprintf("%s.%s", prefix, reSanitzeAlpha.ReplaceAllString(*tp.Name, "-"))
	return K8SResourceName{
		Namespace: tp.Namespace,
		Name:      name,
		FQDN:      fmt.Sprintf("%s/%s", tp.Namespace, name),
	}
}

func (tp UpsertTunnelParams) K8SConfigMapName() K8SResourceName {
	return tp.buildK8SResourceName("cfd-tunnel-cfg")
}

func (tp UpsertTunnelParams) K8SSecretName() K8SResourceName {
	return tp.buildK8SResourceName("cfd-tunnel-key")
}

type CFTunnelSecret struct {
	AccountTag   string    `json:"AccountTag"`
	TunnelSecret string    `json:"TunnelSecret"`
	TunnelID     uuid.UUID `json:"TunnelID"`
}

func GetTunnel(cfc types.CFController, tp UpsertTunnelParams) (*cfapi.TunnelWithToken, error) {
	tf := cfapi.NewTunnelFilter()
	if tp.Name != nil {
		tf.ByName(*tp.Name)
	} else if tp.TunnelID != nil {
		tf.ByTunnelID(*tp.TunnelID)
	}
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
		err := fmt.Errorf("No tunnel found for name %v", *tp.Name)
		// cfc.Log().Error().Err(err).Any("ts", ts).Msg("No tunnels found")
		return nil, err
	}
	cfc.Log().Debug().Msgf("Found tunnel: %s/%s", foundTs.ID, foundTs.Name)
	twt := cfapi.TunnelWithToken{
		Tunnel: *foundTs,
	}
	return &twt, nil
}

func GetTunnelSecret(log *zerolog.Logger, tp UpsertTunnelParams, secret *corev1.Secret) (CFTunnelSecret, error) {
	credentialsJson, ok := secret.Data["credentials.json"]
	if !ok {
		log.Error().Str("name", tp.K8SSecretName().FQDN).Msg("Secret does not contain credentials.json")
		return CFTunnelSecret{}, fmt.Errorf("Secret %s does not contain credentials.json", tp.K8SSecretName().FQDN)
	}
	// credentialsJson := make([]byte, base64.StdEncoding.DecodedLen(len(credentialsBytes)))
	// n, err := base64.StdEncoding.Decode(credentialsJson, credentialsBytes)
	// if err != nil {
	// 	log.Error().Err(err).Str("name", secret.GetObjectMeta().GetNamespace()+"/"+secret.GetObjectMeta().GetName()).Str("secretData", string(credentialsBytes)).Msg("Error decoding credentials")
	// 	return CFTunnelSecret{}, err
	// }
	var cts CFTunnelSecret
	err := json.Unmarshal(credentialsJson, &cts)
	if err != nil {
		log.Error().Err(err).Str("name", tp.K8SSecretName().FQDN).Str("secretJson", string(credentialsJson)).Msg("Error unmarshal credentials")
		return CFTunnelSecret{}, err
	}
	return cts, nil
}

func MatchK8SSecret(cfc types.CFController, tunnelId string, tp UpsertTunnelParams) (*CFTunnelSecret, error) {
	secret, err := cfc.Rest().K8s().CoreV1().Secrets(tp.K8SSecretName().Namespace).Get(cfc.Context(), tp.K8SSecretName().Name, metav1.GetOptions{})
	if err != nil {
		cfc.Log().Error().Err(err).Str("secretName", tp.K8SSecretName().FQDN).Msg("Secret not found")
		return nil, err
	}

	cts, err := GetTunnelSecret(cfc.Log(), tp, secret)
	if err != nil {
		return nil, err
	}
	if cts.TunnelID.String() != tunnelId || cts.AccountTag != cfc.Cfg().CloudFlare.AccountId {
		cfc.Log().Error().Msgf("Secret does not match tunnelId or accountTag")
		return nil, fmt.Errorf("Secret does not match tunnelId or accountTag")
	}
	return &cts, nil
}

func UpsertTunnel(cfc types.CFController, tp UpsertTunnelParams) (*CFTunnelSecret, error) {
	cfClient, err := cfc.Rest().CFClientWithoutZoneID()
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Can't find CF client")
		return nil, err
	}
	var cts *CFTunnelSecret
	ts, err := GetTunnel(cfc, tp)
	if err != nil && strings.HasPrefix(err.Error(), "No tunnel found for name ") {
		if tp.Name == nil {
			err := fmt.Errorf("To create a new tunnel, a name must be provided")
			cfc.Log().Error().Err(err).Msg("Error creating tunnel")
			return nil, err
		}
		var secretStr string
		byteSecret := make([]byte, 32)
		rand.Read(byteSecret)
		secretStr = base64.StdEncoding.EncodeToString(byteSecret)
		cfc.Log().Debug().Str("secretName", tp.K8SSecretName().FQDN).Msg("Secret not found, creating new secret")
		ts, err = cfClient.CreateTunnel(*tp.Name, byteSecret)
		if err != nil {
			cfc.Log().Error().Str("name", *tp.Name).Err(err).Msg("Error creating tunnel")
			return nil, err
		}
		cfc.Log().Debug().Str("name", *tp.Name).Str("id", ts.ID.String()).Err(err).Msg("created tunnel")
		cts = &CFTunnelSecret{
			AccountTag:   cfc.Cfg().CloudFlare.AccountId,
			TunnelSecret: secretStr,
			TunnelID:     ts.ID,
		}
		tp.TunnelID = &ts.ID
		ctsBytes, err := json.Marshal(cts)
		if err != nil {
			cfc.Log().Error().Err(err).Str("name", *tp.Name).Msg("Error marshalling credentials")
			return nil, err
		}

		secretClient := cfc.Rest().K8s().CoreV1().Secrets(tp.K8SSecretName().Namespace)
		_, err = secretClient.Get(cfc.Context(), tp.K8SConfigMapName().Name, metav1.GetOptions{})
		k8sSecret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        tp.K8SSecretName().Name,
				Namespace:   tp.K8SConfigMapName().Namespace,
				Annotations: cfAnnotations(tp.Annotations, *tp.Name, ts.ID.String()),
				Labels:      cfLabels(tp.Labels, cfc),
			},
			Data: map[string][]byte{
				"credentials.json": ctsBytes,
			},
		}

		if err != nil {
			secret, err := secretClient.Create(cfc.Context(), &k8sSecret, metav1.CreateOptions{})
			if err != nil {
				cfc.Log().Error().Str("name", secret.GetObjectMeta().GetNamespace()+"/"+secret.GetObjectMeta().GetName()).Err(err).Msg("Error creating secret")
				return nil, err
			}
		} else {
			secret, err := secretClient.Update(cfc.Context(), &k8sSecret, metav1.UpdateOptions{})
			if err != nil {
				cfc.Log().Error().Str("name", secret.GetObjectMeta().GetNamespace()+"/"+secret.GetObjectMeta().GetName()).Err(err).Msg("Error update secret")
				return nil, err
			}
		}
	} else if err != nil {
		cfc.Log().Error().Str("name", *tp.Name).Err(err).Msg("Error getting tunnel")
		return nil, err
	} else {
		tp.Name = &ts.Name
		tp.TunnelID = &ts.ID
		cts, err = MatchK8SSecret(cfc, ts.ID.String(), tp)
		if err != nil {
			cfc.Log().Error().Str("tunnelId", ts.ID.String()).Str("secretName", tp.K8SConfigMapName().FQDN).Err(err).Msg("Error matching secret")
			return nil, err
		}
		cfc.Log().Debug().Str("name", *tp.Name).Str("id", ts.ID.String()).Err(err).Msg("found tunnel")
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

func cfAnnotations(annos map[string]string, tunnelName string, tunnelId string) map[string]string {
	ret := make(map[string]string)
	if annos == nil {
		for k, v := range annos {
			ret[k] = v
		}
	}
	ret[config.AnnotationCloudflareTunnelName] = tunnelName
	ret[config.AnnotationCloudflareTunnelId] = tunnelId
	return ret
}

// var reLabelValues = regexp.MustCompile("[A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?")

func cfLabels(labels map[string]string, cfc types.CFController) map[string]string {
	ret := make(map[string]string)
	if labels == nil {
		for k, v := range labels {
			ret[k] = v
		}
	}
	v := reSanitzeNice.ReplaceAllString(cfc.Cfg().Version, "-")
	ret[config.LabelCloudflaredControllerVersion] = fmt.Sprintf("%s", v)
	tokens := strings.Split(strings.TrimSpace(cfc.Cfg().ConfigMapLabelSelector), "=")
	if len(tokens) < 2 {
		ret["app"] = "cloudflared-controller"
		cfc.Log().Warn().Str("labelSelector", cfc.Cfg().ConfigMapLabelSelector).Msg("Invalid label selector, using default")
	} else {
		ret[tokens[0]] = tokens[1]
	}
	return ret
}

func RegisterCFDnsEndpoint(cfc types.CFController, tunnelId uuid.UUID, name string) error {
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

func cmKey(kind, ns, name string) string {
	if kind == "" {
		panic("kind cannot be empty")
	}
	return reSanitzeNice.ReplaceAllString(fmt.Sprintf("%s-%s-%s", kind, ns, name), "_")
}

func WriteCloudflaredConfig(cfc types.CFController, kind string, resName string, tp *UpsertTunnelParams, cts *CFTunnelSecret, cfcis []config.CFConfigIngress) error {
	yCFConfigIngressByte, err := yaml.Marshal(cfcis)
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Error marshaling ingress")
		return err
	}
	tp.Annotations = map[string]string{
		config.AnnotationCloudflareTunnelKeySecret: tp.K8SSecretName().FQDN,
	}

	key := cmKey(kind, tp.Namespace, resName)
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tp.K8SConfigMapName().Name,
			Namespace:   tp.Namespace,
			Labels:      cfLabels(tp.Labels, cfc),
			Annotations: cfAnnotations(tp.Annotations, *tp.Name, cts.TunnelID.String()),
		},
		Data: map[string]string{
			key: string(yCFConfigIngressByte),
		},
	}
	client := cfc.Rest().K8s().CoreV1().ConfigMaps(tp.K8SConfigMapName().Namespace)
	toUpdate, err := client.Get(cfc.Context(), tp.K8SConfigMapName().Name, metav1.GetOptions{})
	if err != nil {
		_, err = client.Create(cfc.Context(), &cm, metav1.CreateOptions{})
	} else {
		for k, v := range cm.Data {
			toUpdate.Data[k] = v
		}
		_, err = client.Update(cfc.Context(), toUpdate, metav1.UpdateOptions{})
	}

	return err
}

func RemoveFromCloudflaredConfig(cfc types.CFController, kind string, meta *metav1.ObjectMeta) {
	// panic("Not implemented")
	name := meta.GetName()
	tp := &UpsertTunnelParams{
		Name:      &name,
		Namespace: meta.GetNamespace(),
	}
	for _, toUpdate := range cfc.K8sData().TunnelConfigMaps.Get() {
		key := cmKey(kind, meta.Namespace, meta.Name)
		needChange := len(toUpdate.Data)
		delete(toUpdate.Data, key)
		if needChange != len(toUpdate.Data) {
			client := cfc.Rest().K8s().CoreV1().ConfigMaps(tp.K8SConfigMapName().Namespace)
			_, err := client.Update(cfc.Context(), toUpdate, metav1.UpdateOptions{})
			if err != nil {
				cfc.Log().Error().Err(err).Str("name", tp.K8SConfigMapName().Name).Msg("Error updating config")
				continue
			}
			cfc.Log().Debug().Str("key", key).Msg("Removing from config")
		}
	}
}

func PrepareTunnel(cfc types.CFController, ns string, annotations map[string]string, labels map[string]string) (*UpsertTunnelParams, *CFTunnelSecret, error) {
	tp := UpsertTunnelParams{
		Namespace: ns,
	}
	nid, ok := annotations[config.AnnotationCloudflareTunnelName]
	if ok {
		my := nid
		tp.Name = &my
	}
	tp.Labels = labels
	ts, err := UpsertTunnel(cfc, tp)
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Failed to upsert tunnel")
		return nil, nil, err
	}
	tp.TunnelID = &ts.TunnelID
	cfc.Log().Debug().Str("tunnel", *tp.Name).Str("tunnelId", ts.TunnelID.String()).Msg("Upserted tunnel")
	return &tp, ts, nil
}

package k8s_data

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getTunnelSecret(log *zerolog.Logger, fqdn string, secret *corev1.Secret) (*types.CFTunnelSecret, error) {
	credentialsJson, ok := secret.Data["credentials.json"]
	if !ok {
		log.Error().Str("name", fqdn).Msg("Secret does not contain credentials.json")
		return nil, fmt.Errorf("Secret %s does not contain credentials.json", fqdn)
	}
	// credentialsJson := make([]byte, base64.StdEncoding.DecodedLen(len(credentialsBytes)))
	// n, err := base64.StdEncoding.Decode(credentialsJson, credentialsBytes)
	// if err != nil {
	// 	log.Error().Err(err).Str("name", secret.GetObjectMeta().GetNamespace()+"/"+secret.GetObjectMeta().GetName()).Str("secretData", string(credentialsBytes)).Msg("Error decoding credentials")
	// 	return CFTunnelSecret{}, err
	// }
	var cts types.CFTunnelSecret
	err := json.Unmarshal(credentialsJson, &cts)
	if err != nil {
		log.Error().Err(err).Str("name", fqdn).Str("secretJson", string(credentialsJson)).Msg("Error unmarshal credentials")
		return nil, err
	}
	return &cts, nil
}

func DeleteSecret(cfc types.CFController, tp *types.CFTunnelParameter) error {
	return cfc.Rest().K8s().CoreV1().Secrets(tp.K8SSecretName().Namespace).Delete(cfc.Context(), tp.K8SSecretName().Name, metav1.DeleteOptions{})
}

func FetchSecret(cfc types.CFController, ns, name, id string) (*types.CFTunnelSecret, error) {
	secret, err := cfc.Rest().K8s().CoreV1().Secrets(ns).Get(cfc.Context(), name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil, err
	}
	fqdn := fmt.Sprintf("%s/%s", ns, name)
	if err != nil {
		cfc.Log().Error().Err(err).Str("secretName", fqdn).Msg("K8s error")
		return nil, err
	}

	cts, err := getTunnelSecret(cfc.Log(), fqdn, secret)
	if err != nil {
		return nil, err
	}
	if cts.TunnelID.String() != id || cts.AccountTag != cfc.Cfg().CloudFlare.AccountId {
		err := fmt.Errorf("Secret does not match tunnelId or accountTag")
		cfc.Log().Error().Err(err).Str("secretName", fqdn).Msg("Secret not found")
		// cfc.Log().Error().Msgf("Secret does not match tunnelId or accountTag")
		return nil, err
	}
	return cts, nil
}

func CreateSecret(cfc types.CFController, tp *types.CFTunnelParameterWithID, byteSecret []byte, ometa *metav1.ObjectMeta) (*types.CFTunnelSecret, error) {
	secretStr := base64.StdEncoding.EncodeToString(byteSecret)
	cts := &types.CFTunnelSecret{
		AccountTag:   cfc.Cfg().CloudFlare.AccountId,
		TunnelSecret: secretStr,
		TunnelID:     tp.ID,
	}
	ctsBytes, err := json.Marshal(cts)
	if err != nil {
		cfc.Log().Error().Err(err).Str("name", tp.Name).Msg("Error marshalling credentials")
		return nil, err
	}
	anno := make(map[string]string)
	for k, v := range ometa.Annotations {
		anno[k] = v
	}
	delete(anno, config.AnnotationCloudflareTunnelK8sSecret())
	anno[config.AnnotationCloudflareTunnelId()] = tp.ID.String()
	anno[config.AnnotationCloudflareTunnelCFDName()] = config.CfTunnelName(cfc, &tp.CFTunnelParameter)
	// anno[config.AnnotationCloudflareTunnelName] = tp.Name
	anno[config.AnnotationCloudflareTunnelK8sConfigMap()] = tp.K8SConfigMapName().FQDN
	// anno[config.AnnotationCloudflareTunnelK8sSecret] = tp.K8SSecretName().FQDN
	secretClient := cfc.Rest().K8s().CoreV1().Secrets(tp.K8SSecretName().Namespace)
	k8sSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tp.K8SSecretName().Name,
			Namespace:   tp.K8SSecretName().Namespace,
			Annotations: anno,
			Labels:      config.CfLabels(ometa.Labels, cfc),
		},
		Data: map[string][]byte{
			"credentials.json": ctsBytes,
		},
	}
	_, err = secretClient.Get(cfc.Context(), tp.K8SConfigMapName().Name, metav1.GetOptions{})
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
	return cts, nil
}

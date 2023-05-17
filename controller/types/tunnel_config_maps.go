package types

import (
	"fmt"
	"regexp"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// type TunnelConfigMap struct {
// 	Cm corev1.ConfigMap
// }

type CFTunnelParameter struct {
	Name      string
	Namespace string
}

type CFTunnelParameterWithID struct {
	CFTunnelParameter
	ID uuid.UUID
}

func (cft *CFTunnelParameter) ObjectMeta(ometa *metav1.ObjectMeta) metav1.ObjectMeta {
	panic("implement me")
}

var reSanitzeAlpha = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func buildK8SResourceName(prefix string, cft *CFTunnelParameter) K8SResourceName {
	name := fmt.Sprintf("%s.%s", prefix, reSanitzeAlpha.ReplaceAllString(cft.Name, "-"))
	return K8SResourceName{
		Namespace: cft.Namespace,
		Name:      name,
		FQDN:      fmt.Sprintf("%s/%s", cft.Namespace, name),
	}
}

func (cft *CFTunnelParameter) K8SConfigMapName() K8SResourceName {
	return buildK8SResourceName("cfd-tunnel-cfg", cft)
}

func (cft *CFTunnelParameter) K8SSecretName() K8SResourceName {
	return buildK8SResourceName("cfd-tunnel-key", cft)
}

type TunnelConfigMaps interface {
	Register(func([]*corev1.ConfigMap, watch.Event)) func()
	Get() []*corev1.ConfigMap

	UpsertConfigMap(cfc CFController, tparam *CFTunnelParameter, kind string, meta *metav1.ObjectMeta, cfcis []CFConfigIngress) error
	RemoveConfigMap(cfc CFController, kind string, meta *metav1.ObjectMeta)
	// func (cfmh *CloudFlaredConfigMapHandler) WriteCloudflaredConfig(cfc types.CFController, kind string, resName string, tp *UpsertTunnelParams, cts *CFTunnelSecret, cfcis []config.CFConfigIngress) error {
	// func (cfmh *CloudFlaredConfigMapHandler) RemoveFromCloudflaredConfig(cfc types.CFController, kind string, meta *metav1.ObjectMeta) {

	// Get func() []TunnelConfigMap
	// CmsLock sync.Mutex
	// Cms     map[string]TunnelConfigMap
}

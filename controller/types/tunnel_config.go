package types

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
)

// type TunnelConfigMap struct {
// 	Cm corev1.ConfigMap
// }

type TunnelConfigMaps interface {
	Register(func([]*corev1.ConfigMap, watch.Event)) func()
	Get() []*corev1.ConfigMap
	// Get func() []TunnelConfigMap
	// CmsLock sync.Mutex
	// Cms     map[string]TunnelConfigMap
}

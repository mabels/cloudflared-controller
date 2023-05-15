package types

import (
	corev1 "k8s.io/api/core/v1"
	// panic("implement me")
)

type K8sData struct {
	// Get              func() []TunnelConfigMap
	TunnelConfigMaps TunnelConfigMaps
	Namespaces       Watcher[*corev1.Namespace] // [corev1.Namespace, *corev1.Namespace, WatcherBindingNamespace, WatcherBindingNamespaceClient]
}

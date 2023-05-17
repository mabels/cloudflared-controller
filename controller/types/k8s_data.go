package types

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	// panic("implement me")
)

type K8sData struct {
	// Get              func() []TunnelConfigMap
	TunnelConfigMaps TunnelConfigMaps
	Namespaces       Watcher[*corev1.Namespace] // [corev1.Namespace, *corev1.Namespace, WatcherBindingNamespace, WatcherBindingNamespaceClient]
}

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

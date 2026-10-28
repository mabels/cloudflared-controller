package types

import (
	"strings"
)

type K8sData struct {
	TunnelConfigMaps TunnelConfigMaps
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

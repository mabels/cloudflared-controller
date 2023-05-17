package k8s_data

import (
	"fmt"
	"strings"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type UniqueTunnelParams struct {
	params map[string]*types.CFTunnelParameter
}

func NewUniqueTunnelParams() *UniqueTunnelParams {
	return &UniqueTunnelParams{
		params: make(map[string]*types.CFTunnelParameter),
	}
}

func (utp *UniqueTunnelParams) Get() []*types.CFTunnelParameter {
	ret := make([]*types.CFTunnelParameter, 0, len(utp.params))
	for _, v := range utp.params {
		ret = append(ret, v)
	}
	return ret
}

func (utp *UniqueTunnelParams) GetConfigMapTunnelParam(cfc types.CFController, ometa *metav1.ObjectMeta, hints ...string) (*types.CFTunnelParameter, error) {
	annotations := ometa.Annotations

	tunnelName, ok := ometa.Annotations[config.AnnotationCloudflareTunnelName()]
	if !ok {
		// strip domain from tunnelName
		externalName, ok := annotations[config.AnnotationCloudflareTunnelExternalName()]
		if !ok {
			return nil, fmt.Errorf("No tunnel name annotation(%s) or external name annotation(%s)",
				config.AnnotationCloudflareTunnelName(), config.AnnotationCloudflareTunnelExternalName())
		}
		domainparts := strings.Split(strings.Trim(strings.TrimSpace(externalName), "."), ".")
		if len(domainparts) >= 2 {
			tunnelName = fmt.Sprintf("%s.%s", domainparts[len(domainparts)-2], domainparts[len(domainparts)-1])
		} else {
			return nil, fmt.Errorf("No usable external name annotation: %s", externalName)
		}
	}
	ret := types.CFTunnelParameter{}
	parts := strings.Split(strings.TrimSpace(tunnelName), "/")
	if len(parts) >= 2 {
		ret.Namespace = parts[0]
		ret.Name = parts[1]
	} else {
		ret.Namespace = cfc.Cfg().CloudFlare.TunnelConfigMapNamespace
		ret.Name = tunnelName
	}
	if ret.Name == "" || ret.Namespace == "" {
		return nil, fmt.Errorf("No usable tunnel name annotation: %s", tunnelName)
	}
	utp.params[fmt.Sprintf("%s/%s", ret.Namespace, ret.Name)] = &ret
	return &ret, nil
}

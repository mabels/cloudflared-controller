package controller

import (
	"sync"

	"github.com/mabels/cloudflared-controller/controller/cfclient"
	"github.com/mabels/cloudflared-controller/controller/types"
	"k8s.io/client-go/kubernetes"
)

type RestClients struct {
	cfc types.CFController

	cfOnce sync.Once
	cf     *cfclient.Client

	clientSet kubernetes.Interface
}

func NewRestClients(cfc types.CFController) *RestClients {
	return &RestClients{
		cfc: cfc,
	}
}

func (rc *RestClients) K8s() kubernetes.Interface {
	return rc.clientSet
}

func (rc *RestClients) SetK8s(cs kubernetes.Interface) {
	rc.clientSet = cs
}

// CF is created lazily because the config is set after the controller.
func (rc *RestClients) CF() types.CFClient {
	rc.cfOnce.Do(func() {
		log := rc.cfc.Log().With().Str("component", "cfclient").Logger()
		rc.cf = cfclient.New(
			rc.cfc.Cfg().CloudFlare.ApiUrl,
			rc.cfc.Cfg().CloudFlare.ApiToken,
			rc.cfc.Cfg().CloudFlare.AccountId,
			&log)
	})
	return rc.cf
}

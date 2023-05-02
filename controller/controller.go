package controller

import (
	"github.com/cloudflare/cloudflared/cfapi"
	"github.com/rs/zerolog"
	"k8s.io/client-go/kubernetes"

	"github.com/mabels/cloudflared-controller/controller/config"
)

type RestClients struct {
	Cf  *cfapi.RESTClient
	K8s *kubernetes.Clientset
}

type CFController struct {
	Log  *zerolog.Logger
	Cfg  *config.CFControllerConfig
	Rest *RestClients
}

func (cfc *CFController) WithComponent(component string, fns ...func(*CFController)) *CFController {
	cf := *cfc
	log := cf.Log.With().Str("component", component).Logger()
	cf.Log = &log
	if len(fns) > 0 && fns[0] != nil {
		fns[0](&cf)
	}
	return &cf
}

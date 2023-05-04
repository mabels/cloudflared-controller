package controller

import (
	"context"

	"github.com/cloudflare/cloudflared/cfapi"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"k8s.io/client-go/kubernetes"

	"github.com/mabels/cloudflared-controller/controller/config"
)

type RestClients struct {
	Cf  *cfapi.RESTClient
	K8s *kubernetes.Clientset
}

type CFController struct {
	Log         *zerolog.Logger
	Cfg         *config.CFControllerConfig
	Rest        *RestClients
	Context     context.Context
	CancelFunc  context.CancelFunc
	shutdownFns map[string]func()
}

func NewCFController(log *zerolog.Logger) *CFController {
	ctx, cancelFn := context.WithCancel(context.Background())
	cfc := CFController{
		Log:         log,
		Rest:        &RestClients{},
		Context:     ctx,
		CancelFunc:  cancelFn,
		shutdownFns: make(map[string]func()),
	}
	return &cfc
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

func (cfc *CFController) RegisterShutdown(sfn func()) func() {
	id := uuid.New().String()
	cfc.shutdownFns[id] = sfn
	return func() {
		delete(cfc.shutdownFns, id)
	}
}

func (cfc *CFController) Shutdown() {
	for _, sfn := range cfc.shutdownFns {
		sfn()
	}
}

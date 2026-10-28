package controller

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
)

type shutdownRegistry struct {
	lock sync.Mutex
	fns  map[string]func()
}

type localController struct {
	shutdown   *shutdownRegistry
	rest       *RestClients
	log        *zerolog.Logger
	cfg        *types.CFControllerConfig
	context    context.Context
	cancelFunc context.CancelFunc
	k8sData    *types.K8sData
}

func NewCFController(log *zerolog.Logger) types.CFController {
	ctx, cancelFn := context.WithCancel(context.Background())
	cfc := localController{
		log:        log,
		context:    ctx,
		cancelFunc: cancelFn,
		k8sData:    &types.K8sData{},
		shutdown:   &shutdownRegistry{fns: make(map[string]func())},
	}
	cfc.rest = NewRestClients(&cfc)
	return &cfc
}

func (cfc *localController) Cfg() *types.CFControllerConfig {
	return cfc.cfg
}

func (cfc *localController) SetCfg(cfg *types.CFControllerConfig) {
	cfc.cfg = cfg
}

func (cfc *localController) K8sData() *types.K8sData {
	return cfc.k8sData
}

func (cfc *localController) Rest() types.RestClients {
	return cfc.rest
}

func (cfc *localController) Log() *zerolog.Logger {
	return cfc.log
}

func (cfc *localController) SetLog(log *zerolog.Logger) {
	cfc.log = log
}

func (cfc *localController) Context() context.Context {
	return cfc.context
}

func (cfc *localController) CancelFunc() context.CancelFunc {
	return cfc.cancelFunc
}

func (cfc *localController) WithComponent(component string, fns ...func(types.CFController)) types.CFController {
	cf := *cfc
	log := cf.Log().With().Str("component", component).Logger()
	cf.SetLog(&log)
	if len(fns) > 0 && fns[0] != nil {
		fns[0](&cf)
	}
	return &cf
}

func (cfc *localController) RegisterShutdown(sfn func()) func() {
	id := uuid.New().String()
	cfc.shutdown.lock.Lock()
	cfc.shutdown.fns[id] = sfn
	cfc.shutdown.lock.Unlock()
	return func() {
		cfc.shutdown.lock.Lock()
		delete(cfc.shutdown.fns, id)
		cfc.shutdown.lock.Unlock()
	}
}

func (cfc *localController) Shutdown() error {
	cfc.shutdown.lock.Lock()
	fns := make([]func(), 0, len(cfc.shutdown.fns))
	for _, sfn := range cfc.shutdown.fns {
		fns = append(fns, sfn)
	}
	cfc.shutdown.lock.Unlock()
	// the shutdown functions unregister themselves
	for _, sfn := range fns {
		sfn()
	}
	cfc.cancelFunc()
	return nil
}

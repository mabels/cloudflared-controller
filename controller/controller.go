package controller

import (
	"context"

	"github.com/cloudflare/cloudflare-go/v3"
	"github.com/cloudflare/cloudflare-go/v3/option"
	"github.com/cloudflare/cloudflare-go/v3/zones"

	"github.com/google/uuid"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
)

type localController struct {
	// types.CFController
	shutdownFns map[string]func()
	rest        *RestClients
	log         *zerolog.Logger
	cfg         *types.CFControllerConfig
	context     context.Context
	cancelFunc  context.CancelFunc
	k8sData     *types.K8sData
}

func getZones(cfc types.CFController) ([]zones.Zone, error) {
	client := cloudflare.NewClient(
		option.WithAPIToken(cfc.Cfg().CloudFlare.ApiToken),
	)

	// cloudflare.New(&cloudflare.ClientParams{
	// 	Token:  cfc.Cfg().CloudFlare.ApiToken,
	// 	Logger: utils.NewLeveledLogger(cfc.Log()),
	// 	// Debug:  true,
	// })
	// if err != nil {
	// 	return nil, err
	// }
	zoneLst := []zones.Zone{}
	page := 0

	for {
		pages, err := client.Zones.List(cfc.Context(), zones.ZoneListParams{})
		if err != nil {
			return nil, err
		}
		zoneLst = append(zoneLst, pages.Result...)
		if len(pages.Result) == 0 {
			break
		}
		page++
	}
	return zoneLst, nil
}

func NewCFController(log *zerolog.Logger) types.CFController {
	ctx, cancelFn := context.WithCancel(context.Background())
	cfc := localController{
		// CFController: types.CFController{
		log:        log,
		context:    ctx,
		cancelFunc: cancelFn,
		k8sData:    &types.K8sData{},
		// },
		shutdownFns: make(map[string]func()),
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
	cfc.shutdownFns[id] = sfn
	return func() {
		delete(cfc.shutdownFns, id)
	}
}

func (cfc *localController) Shutdown() error {
	for _, sfn := range cfc.shutdownFns {
		sfn()
	}
	return nil
}

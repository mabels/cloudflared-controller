package controller

import (
	"context"
	"time"

	"github.com/cloudflare/cloudflare-go"
	"github.com/google/uuid"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/mabels/cloudflared-controller/utils"
	"github.com/rs/zerolog"

	"github.com/eko/gocache/lib/v4/cache"
	gocache_store "github.com/eko/gocache/store/go_cache/v4"
	gocache "github.com/patrickmn/go-cache"
)

type localController struct {
	// types.CFController
	shutdownFns map[string]func()
	rest        *RestClients
	cacher      types.Cacher
	log         *zerolog.Logger
	cfg         *types.CFControllerConfig
	context     context.Context
	cancelFunc  context.CancelFunc
	k8sData     *types.K8sData
}

func getZones(cfc types.CFController) ([]cloudflare.Zone, error) {
	return cfc.Cache().GetZones(func() ([]cloudflare.Zone, error) {
		client, err := cloudflare.NewExperimental(&cloudflare.ClientParams{
			Token:  cfc.Cfg().CloudFlare.ApiToken,
			Logger: utils.NewLeveledLogger(cfc.Log()),
			// Debug:  true,
		})
		if err != nil {
			cfc.Log().Error().Err(err).Msg("error creating cloudflare client")
			return nil, err
		}
		zones, _, err := client.Zones.List(cfc.Context(), &cloudflare.ZoneListParams{})
		if err != nil {
			cfc.Log().Error().Err(err).Msg("Zones List error")
			return nil, err
		}
		cfc.Log().Info().Int("zones", len(zones)).Msg("Zones List")
		return zones, nil
	})
}

type zonesItem struct {
	zones []cloudflare.Zone
	err   error
}
type reqCacher struct {
	geocacheClient    *gocache.Cache
	geocacheStore     *gocache_store.GoCacheStore
	zonesCacheManager *cache.Cache[zonesItem]
	cfc               types.CFController
}

func (rc *reqCacher) GetZones(f func() ([]cloudflare.Zone, error)) ([]cloudflare.Zone, error) {
	zones, err := rc.zonesCacheManager.Get(rc.cfc.Context(), "zones")
	if err != nil {
		zones, err := f()
		rc.zonesCacheManager.Set(rc.cfc.Context(), "zones", zonesItem{
			zones: zones,
			err:   err,
		})
	}
	return zones.zones, zones.err
}

/*
	err := cacheManager.Set(cfc.Context(), "my-key", []byte("my-value"))
	if err != nil {
		panic(err)
	}

	value, err := cacheManager.Get(cfc.Context(), "my-key")
	if err != nil {
		panic(err)
	}
*/

func newCache(cfc types.CFController) types.Cacher {
	req := reqCacher{
		cfc: cfc,
	}
	req.geocacheClient = gocache.New(2*time.Minute, 3*time.Minute)
	req.geocacheStore = gocache_store.NewGoCache(req.geocacheClient)
	req.zonesCacheManager = cache.New[zonesItem](req.geocacheStore)

	return &req
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
	cfc.cacher = newCache(&cfc)
	cfc.rest = NewRestClients(&cfc)
	return &cfc
}

func (cfc *localController) Cache() types.Cacher {
	return cfc.cacher
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

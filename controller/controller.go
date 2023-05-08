package controller

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudflare/cloudflare-go"
	"github.com/cloudflare/cloudflared/cfapi"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"k8s.io/client-go/kubernetes"

	"github.com/mabels/cloudflared-controller/controller/config"
)

type RestClients struct {
	cfc *CFController
	// Cf  *cfapi.RESTClient
	cfsLock sync.Mutex
	cfs     map[string]*cfapi.RESTClient

	K8s *kubernetes.Clientset
}

func getZones(cfc *CFController) ([]cloudflare.Zone, error) {
	client, err := cloudflare.NewExperimental(&cloudflare.ClientParams{
		Token: cfc.Cfg.CloudFlare.ApiToken,
		// Logger: cfc.Log,
	})
	if err != nil {
		return nil, err
	}
	zones, _, err := client.Zones.List(cfc.Context, &cloudflare.ZoneListParams{})
	if err != nil {
		return nil, err
	}
	return zones, nil
}

func (rc *RestClients) CFClientWithoutZoneID() (*cfapi.RESTClient, error) {
	rc.cfsLock.Lock()
	defer rc.cfsLock.Unlock()
	_, found := rc.cfs[""]
	var err error
	if !found {
		rc.cfs[""], err = cfapi.NewRESTClient(
			rc.cfc.Cfg.CloudFlare.ApiUrl,
			rc.cfc.Cfg.CloudFlare.AccountId, // accountTag string,
			"",                              // zoneTag string,
			rc.cfc.Cfg.CloudFlare.ApiToken,
			"cloudflared-controller",
			rc.cfc.Log)
	}
	return rc.cfs[""], err
}

func (rc *RestClients) GetCFClientForDomain(domain string) (*cfapi.RESTClient, error) {
	rc.cfsLock.Lock()
	defer rc.cfsLock.Unlock()
	rcl, found := rc.cfs[domain]
	if !found {
		zones, err := getZones(rc.cfc)
		if err != nil {
			rc.cfc.Log.Error().Err(err).Msg("Failed to get zones")
			return nil, err
		}
		for _, zone := range zones {
			rc.cfs[zone.Name], err = cfapi.NewRESTClient(
				rc.cfc.Cfg.CloudFlare.ApiUrl,
				rc.cfc.Cfg.CloudFlare.AccountId, // accountTag string,
				zone.ID,                         // zoneTag string,
				rc.cfc.Cfg.CloudFlare.ApiToken,
				fmt.Sprintf("cloudflared-controller(%s)", zone.Name),
				rc.cfc.Log)
			if err != nil {
				rc.cfc.Log.Fatal().Err(err).Msg("Failed to create cloudflare client")
			}
		}
		rcl, found = rc.cfs[domain]
		if !found {
			return nil, fmt.Errorf("domain %s not found", domain)
		}
	}
	return rcl, nil
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
		Context:     ctx,
		CancelFunc:  cancelFn,
		shutdownFns: make(map[string]func()),
	}
	cfc.Rest = &RestClients{
		cfc: &cfc,
		cfs: make(map[string]*cfapi.RESTClient),
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

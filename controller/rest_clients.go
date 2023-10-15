package controller

import (
	"fmt"
	"sync"

	cfgo "github.com/cloudflare/cloudflare-go"
	"github.com/cloudflare/cloudflared/cfapi"
	"github.com/mabels/cloudflared-controller/controller/types"
	"k8s.io/client-go/kubernetes"
)

type RestClients struct {
	// types.RestClients

	cfc types.CFController
	// Cf  *cfapi.RESTClient
	cfsLock sync.Mutex
	cfs     map[string]*cfapi.RESTClient

	cfgoAPI *cfgo.API

	clientSet *kubernetes.Clientset
}

func NewRestClients(cfc types.CFController) *RestClients {
	rc := RestClients{
		cfc: cfc,
		cfs: make(map[string]*cfapi.RESTClient),
	}
	return &rc
}

func (rc *RestClients) K8s() *kubernetes.Clientset {
	return rc.clientSet
}

func (rc *RestClients) SetK8s(cs *kubernetes.Clientset) {
	rc.clientSet = cs
}

type cfgoLogger struct {
	cfc types.CFController
}

// Printf implements cloudflare.Logger.
func (cl *cfgoLogger) Printf(format string, v ...interface{}) {
	cl.cfc.Log().Info().Str("cfgo", fmt.Sprintf(format, v...)).Msg("cfgo log")
}

func (rc *RestClients) Cfgo() (*cfgo.API, error) {
	rc.cfsLock.Lock()
	defer rc.cfsLock.Unlock()
	if rc.cfgoAPI == nil {
		var err error

		rc.cfgoAPI, err = cfgo.NewWithAPIToken(rc.cfc.Cfg().CloudFlare.ApiToken,
			cfgo.UsingLogger(&cfgoLogger{cfc: rc.cfc}))
		if err != nil {
			return nil, err
		}
	}
	return rc.cfgoAPI, nil

}

func (rc *RestClients) CFClientWithoutZoneID() (*cfapi.RESTClient, error) {
	rc.cfsLock.Lock()
	defer rc.cfsLock.Unlock()
	_, found := rc.cfs[""]
	var err error
	if !found {
		rc.cfs[""], err = cfapi.NewRESTClient(
			rc.cfc.Cfg().CloudFlare.ApiUrl,
			rc.cfc.Cfg().CloudFlare.AccountId, // accountTag string,
			"",                                // zoneTag string,
			rc.cfc.Cfg().CloudFlare.ApiToken,
			"cloudflared-controller",
			rc.cfc.Log())
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
			rc.cfc.Log().Error().Err(err).Msg("Failed to get zones")
			return nil, err
		}
		for _, zone := range zones {
			rc.cfs[zone.Name], err = cfapi.NewRESTClient(
				rc.cfc.Cfg().CloudFlare.ApiUrl,
				rc.cfc.Cfg().CloudFlare.AccountId, // accountTag string,
				zone.ID,                           // zoneTag string,
				rc.cfc.Cfg().CloudFlare.ApiToken,
				fmt.Sprintf("cloudflared-controller(%s)", zone.Name),
				rc.cfc.Log())
			if err != nil {
				rc.cfc.Log().Fatal().Err(err).Msg("Failed to create cloudflare client")
			}
		}
		rcl, found = rc.cfs[domain]
		if !found {
			return nil, fmt.Errorf("domain %s not found", domain)
		}
	}
	return rcl, nil
}

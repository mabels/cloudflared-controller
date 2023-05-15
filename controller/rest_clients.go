package controller

import (
	"fmt"
	"sync"

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

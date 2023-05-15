package types

import (
	"github.com/cloudflare/cloudflared/cfapi"
	"k8s.io/client-go/kubernetes"
)

type RestClients interface {
	// cfc *CFController
	// // Cf  *cfapi.RESTClient
	// cfsLock sync.Mutex
	// cfs     map[string]*cfapi.RESTClient
	CFClientWithoutZoneID() (*cfapi.RESTClient, error)
	GetCFClientForDomain(string) (*cfapi.RESTClient, error)
	K8s() *kubernetes.Clientset
	SetK8s(*kubernetes.Clientset)
}

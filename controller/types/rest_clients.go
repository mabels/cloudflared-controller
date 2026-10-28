package types

import (
	"context"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/shared"
	"k8s.io/client-go/kubernetes"
)

// CFClient is the small set of Cloudflare operations the controller needs.
type CFClient interface {
	// FindTunnel returns nil, nil if no active tunnel with that name exists
	FindTunnel(ctx context.Context, name string) (*shared.CloudflareTunnel, error)
	// GetTunnel returns nil, nil only if the tunnel is known to be gone
	GetTunnel(ctx context.Context, tunnelID string) (*shared.CloudflareTunnel, error)
	CreateTunnel(ctx context.Context, name string, secret []byte) (*shared.CloudflareTunnel, error)
	DeleteTunnel(ctx context.Context, tunnelID string) error
	// RouteDNS points hostname as proxied CNAME to the tunnel
	RouteDNS(ctx context.Context, tunnelID string, hostname string) error
	API() *cloudflare.Client
	AccountID() string
}

type RestClients interface {
	CF() CFClient
	K8s() kubernetes.Interface
	SetK8s(kubernetes.Interface)
}

package types

import (
	"context"

	"github.com/cloudflare/cloudflare-go"
	"github.com/rs/zerolog"
)

type Cacher interface {
	GetZones(func() ([]cloudflare.Zone, error)) ([]cloudflare.Zone, error)
}

type CFController interface {
	WithComponent(component string, fns ...func(CFController)) CFController
	RegisterShutdown(sfn func()) func()
	Shutdown() error
	Cache() Cacher
	Log() *zerolog.Logger
	SetLog(*zerolog.Logger)
	Cfg() *CFControllerConfig
	SetCfg(*CFControllerConfig)
	Rest() RestClients
	K8sData() *K8sData
	Context() context.Context
	CancelFunc() context.CancelFunc
	// shutdownFns   map[string]func()
}

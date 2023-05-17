package controller

import (
	"os"
	"testing"

	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
)

func TestNewCFController(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()
	cf := NewCFController(&log)
	if cf == nil {
		t.Fatal("NewCFController returned nil")
	}
	cfg := &types.CFControllerConfig{}
	cf.SetCfg(cfg)
	if cf.Cfg() != cfg {
		t.Fatal("NewCFController returned nil logger")
	}
	if cf.Log() != &log {
		t.Fatal("NewCFController returned nil logger")
	}
	if cf.Context() == nil {
		t.Fatal("NewCFController returned nil context")
	}
	if cf.Rest() == nil {
		t.Fatal("NewCFController returned nil RestClients")
	}
	// if cf.WithComponent == nil {
	// 	t.Fatal("NewCFController returned nil WithComponent")
	// }
	// if cf.RegisterShutdown == nil {
	// 	t.Fatal("NewCFController returned nil RegisterShutdown")
	// }
	// if cf.Shutdown == nil {
	// 	t.Fatal("NewCFController returned nil Shutdown")
	// }
	if cf.K8sData().TunnelConfigMaps != nil {
		t.Fatal("NewCFController returned nil K8sData")
	}
	if cf.K8sData().Namespaces != nil {
		t.Fatal("NewCFController returned nil K8sData")
	}
	// if cf.CancelFunc == nil {
	// 	t.Fatal("NewCFController returned nil CancelFunc")
	// }
}

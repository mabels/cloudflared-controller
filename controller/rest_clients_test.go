package controller

import (
	"os"
	"testing"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/rs/zerolog"
)

func TestRestCFClientWithoutZoneID(t *testing.T) {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cfc := NewCFController(&_log)

	os.Setenv("CLOUDFLARE_API_TOKEN", "api_token")
	os.Setenv("CLOUDFLARE_ACCOUNT_ID", "account_id")
	cfg, err := config.GetConfig(cfc.Log(), "test")
	if err != nil {
		cfc.Log().Fatal().Err(err).Msg("Failed to get config")
	}
	cfc.SetCfg(cfg)

	rc := NewRestClients(cfc)
	cf, err := rc.CFClientWithoutZoneID()
	if err != nil {
		t.Fatal(err)
	}
	if cf == nil {
		t.Fatal("NewRestClients returned nil")
	}
}

// func TestRestCFClientGetCFClientForDomain(t *testing.T) {
// 	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
// 	cfc := NewCFController(&_log)

// 	os.Setenv("CLOUDFLARE_API_TOKEN", "api_token")
// 	os.Setenv("CLOUDFLARE_ACCOUNT_ID", "account_id")
// 	cfg, err := config.GetConfig(cfc.Log(), "test")
// 	if err != nil {
// 		cfc.Log().Fatal().Err(err).Msg("Failed to get config")
// 	}
// 	cfc.SetCfg(cfg)

// 	rc := NewRestClients(cfc)
// 	cf, err := rc.GetCFClientForDomain("example.com")
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if cf == nil {
// 		t.Fatal("NewRestClients returned nil")
// 	}

// }

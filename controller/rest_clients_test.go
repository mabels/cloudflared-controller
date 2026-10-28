package controller

import (
	"os"
	"testing"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/rs/zerolog"
)

func TestRestCF(t *testing.T) {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cfc := NewCFController(&_log)

	t.Setenv("CLOUDFLARE_API_TOKEN", "api_token")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "account_id")
	cfg, err := config.GetConfig(cfc.Log(), "test")
	if err != nil {
		t.Fatal(err)
	}
	cfc.SetCfg(cfg)

	rc := NewRestClients(cfc)
	cf := rc.CF()
	if cf == nil {
		t.Fatal("CF returned nil")
	}
	if cf != rc.CF() {
		t.Fatal("CF is not cached")
	}
	if cf.AccountID() != "account_id" {
		t.Fatalf("unexpected account id %s", cf.AccountID())
	}
}

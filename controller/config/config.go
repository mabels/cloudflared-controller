package config

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
)

type CFControllerConfig struct {
	KubeConfigFile     string
	PresetNamespaces   []string
	Identity           string
	NoCloudFlared      bool
	Version            string
	Debug              bool
	ShowVersion        bool
	RunningInstanceDir string
	CloudFlaredFname   string
	ChannelSize        int
	RestartDelay       time.Duration
	CloudFlare         struct {
		ApiUrl    string
		ApiToken  string
		AccountId string
		// ZoneId    string
	}
	Leader struct {
		Name          string
		Namespace     string
		LeaseDuration time.Duration
		RenewDeadline time.Duration
		RetryPeriod   time.Duration
	}
}

func GetConfig(log *zerolog.Logger, version string) (*CFControllerConfig, error) {
	cfg := CFControllerConfig{
		Version: version,
	}
	cfg.CloudFlare.ApiUrl = os.Getenv("CLOUDFLARE_API_URL")
	if cfg.CloudFlare.ApiUrl == "" {
		cfg.CloudFlare.ApiUrl = "https://api.cloudflare.com/client/v4"
	}
	identity := os.Getenv("POD_NAME")
	if identity == "" {
		identity = uuid.NewString()
	}
	pflag.StringVarP(&cfg.KubeConfigFile, "kubeconfig", "c", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")), "absolute path to the kubeconfig file")
	pflag.StringArrayVarP(&cfg.PresetNamespaces, "namespace", "n", []string{}, "namespaces to watch")
	pflag.StringVarP(&cfg.CloudFlare.ApiToken, "cloudflare-api-token", "t", os.Getenv("CLOUDFLARE_API_TOKEN"), "Cloudflare API Key/Token")
	pflag.StringVarP(&cfg.CloudFlare.AccountId, "cloudflare-accountid", "a", os.Getenv("CLOUDFLARE_ACCOUNT_ID"), "Cloudflare Account ID")
	// pflag.StringVarP(&cfg.CloudFlare.ZoneId, "cloudflare-zoneid", "z", os.Getenv("CLOUDFLARE_ZONE_ID"), "Cloudflare Zone ID")
	pflag.StringVarP(&cfg.CloudFlare.ApiUrl, "cloudflare-api-url", "u", cfg.CloudFlare.ApiUrl, "Cloudflare API URL")
	pflag.StringVarP(&cfg.Identity, "identity", "i", identity, "identity of this running instance")
	pflag.StringVarP(&cfg.RunningInstanceDir, "running-instance-dir", "R", "./", "running instance directory")
	pflag.StringVar(&cfg.CloudFlaredFname, "cloudflared-fname", "cloudflared", "cloudflared binary filename")
	pflag.DurationVarP(&cfg.Leader.LeaseDuration, "leader-lease-duration", "l", 15*time.Second, "leader lease duration")
	pflag.DurationVarP(&cfg.Leader.RenewDeadline, "leader-renew-deadline", "r", 10*time.Second, "leader renew deadline")
	pflag.DurationVarP(&cfg.Leader.RetryPeriod, "leader-retry-period", "p", 2*time.Second, "leader retry period")
	pflag.BoolVarP(&cfg.NoCloudFlared, "no-cloudflared", "d", false, "do not run cloudflared")
	pflag.BoolVar(&cfg.ShowVersion, "version", false, "show version: "+version)
	pflag.BoolVar(&cfg.Debug, "debug", false, "enable debug logging")
	pflag.DurationVar(&cfg.RestartDelay, "restart-delay", 30*time.Second, "delay between restarts")
	pflag.StringVar(&cfg.Leader.Name, "leader-name", "cloudflared-controller", "leader elected name")
	pflag.StringVar(&cfg.Leader.Namespace, "leader-namespace", "default", "leader election namespace")
	pflag.IntVar(&cfg.ChannelSize, "channel-size", 10, "channel size")
	pflag.Parse()
	if cfg.CloudFlare.ApiToken == "" {
		return nil, fmt.Errorf("Cloudflare API Key is required")
	}
	if cfg.CloudFlare.AccountId == "" {
		return nil, fmt.Errorf("Cloudflare Account ID is required")
	}
	// if cfg.CloudFlare.ZoneId == "" {
	// 	return nil, fmt.Errorf("Cloudflare Zone ID is required")
	// }
	return &cfg, nil
}

const (
	AnnotationCloudflareTunnelName         = "cloudflare.com/tunnel-name"
	AnnotationCloudflareTunnelId           = "cloudflare.com/tunnel-id"
	AnnotationCloudflareTunnelExternalName = "cloudflare.com/tunnel-external-name"
	AnnotationCloudflareTunnelPort         = "cloudflare.com/tunnel-port"
	// CloudflareTunnelAccountId    = "cloudflare.com/tunnel-account-id"
	// CloudflareTunnelZoneId       = "cloudflare.com/tunnel-zone-id"
	AnnotationCloudflareTunnelKeySecret = "cloudflare.com/tunnel-key-secret"
)

const (
	LabelCloudflaredControllerVersion = "cloudflared-controller/version"
	// LabelCloudflaredControllerManaged = "cloudflared-controller/managed"
	// LabelCloudflaredControllerTunnelId = "cloudflared-controller/tunnel-id"
)

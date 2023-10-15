package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
)

func GetConfig(log *zerolog.Logger, version string) (*types.CFControllerConfig, error) {
	cfg := types.CFControllerConfig{
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
	pflag.StringVarP(&cfg.ConfigMapLabelSelector, "config-map-label", "C", "app=cloudflared-controller", "labelselector for our configmap")
	pflag.StringVar(&cfg.CloudFlaredFname, "cloudflared-fname", "cloudflared", "cloudflared binary filename")
	pflag.StringVar(&cfg.ClusterName, "cloudflared-clustername", "k8s", "prefix the CF tunnel name with this cluster name")
	pflag.StringVar(&cfg.CloudFlare.TunnelConfigMapNamespace, "cloudflared-tunnel-configmap-namespace", "default", "default namespace for cloudflared tunnel configmaps")
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
	pflag.BoolVar(&cfg.TestCreateAccess, "test-create-access", false, "test create access")
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

var AnnotationsPrefix = "cloudflare.com"

func AnnotationCloudflareTunnelName() string {
	return fmt.Sprintf("%s/%s", AnnotationsPrefix, "tunnel-name")
}
func AnnotationCloudflareTunnelId() string {
	return fmt.Sprintf("%s/%s", AnnotationsPrefix, "tunnel-id")
}
func AnnotationCloudflareTunnelCFDName() string {
	return fmt.Sprintf("%s/%s", AnnotationsPrefix, "tunnel-cfd-name")
}
func AnnotationCloudflareTunnelExternalName() string {
	return fmt.Sprintf("%s/%s", AnnotationsPrefix, "tunnel-external-name")
}

// func AnnotationCloudflareTunnelState() string {
// 	return fmt.Sprintf("%s/%s", AnnotationsPrefix, "tunnel-state")
// }

// AnnotationCloudflareTunnelConfigMap    = "cloudflare.com/tunnel-configmap"
// "preparing", "ready"
// func AnnotationCloudflareTunnelPort() string {
// 	return fmt.Sprintf("%s/%s", AnnotationsPrefix, "tunnel-port")
// }

func AnnotationCloudflareTunnelMapping() string {
	return fmt.Sprintf("%s/%s", AnnotationsPrefix, "tunnel-mapping")
}

// func AnnotationCloudflareTunnelSchema() string {
// 	return fmt.Sprintf("%s/%s", AnnotationsPrefix, "tunnel-schema")
// }

// CloudflareTunnelAccountId    = "cloudflare.com/tunnel-account-id"
// CloudflareTunnelZoneId       = "cloudflare.com/tunnel-zone-id"
func AnnotationCloudflareTunnelK8sSecret() string {
	return fmt.Sprintf("%s/%s", AnnotationsPrefix, "tunnel-k8s-secret")
}
func AnnotationCloudflareTunnelK8sConfigMap() string {
	return fmt.Sprintf("%s/%s", AnnotationsPrefix, "tunnel-k8s-configmap")
}

const (
	LabelCloudflaredControllerVersion = "cloudflared-controller/version"
	// LabelCloudflaredControllerManaged = "cloudflared-controller/managed"
	// LabelCloudflaredControllerTunnelId = "cloudflared-controller/tunnel-id"
)

// func CfAnnotations(annos map[string]string, tparam *types.CFTunnelParameter) map[string]string {
// 	ret := make(map[string]string)
// 	for k, v := range annos {
// 		ret[k] = v
// 	}
// 	return ret
// }

func CfTunnelName(cfc types.CFController, tp *types.CFTunnelParameter) string {
	return fmt.Sprintf("%s/%s/%s", cfc.Cfg().ClusterName, tp.Namespace, tp.Name)
}

// var reLabelValues = regexp.MustCompile("[A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?")
var reSanitzeNice = regexp.MustCompile(`[^_\\-\\.a-zA-Z0-9]+`)

func CfLabels(labels map[string]string, cfc types.CFController) map[string]string {
	ret := make(map[string]string)
	for k, v := range labels {
		ret[k] = v
	}
	v := reSanitzeNice.ReplaceAllString(cfc.Cfg().Version, "-")
	if !("a" <= strings.ToLower(string(v[0])) && strings.ToLower(string(v[0])) <= "z") {
		v = fmt.Sprintf("v%s", v)
	}
	ret[LabelCloudflaredControllerVersion] = v
	tokens := strings.Split(strings.TrimSpace(cfc.Cfg().ConfigMapLabelSelector), "=")
	if len(tokens) < 2 {
		ret["app"] = "cloudflared-controller"
		cfc.Log().Warn().Str("labelSelector", cfc.Cfg().ConfigMapLabelSelector).Msg("Invalid label selector, using default")
	} else {
		ret[tokens[0]] = tokens[1]
	}
	return ret
}

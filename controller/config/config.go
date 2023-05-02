package config

import (
	"fmt"
	"os"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CFControllerConfig struct {
	KubeConfigFile   string
	PresetNamespaces []string
	_watchEvents     []chan watch.Event
	_namespaces      map[string]string
	namespacesMutex  sync.Mutex
	Identity         string
	CloudFlare       struct {
		ApiUrl    string
		ApiToken  string
		AccountId string
		ZoneId    string
	}
}

func (c *CFControllerConfig) AddWatch(wch chan watch.Event) {
	c.namespacesMutex.Lock()
	defer c.namespacesMutex.Unlock()
	c._watchEvents = append(c._watchEvents, wch)
	for _, ns := range c._namespaces {
		wch <- watch.Event{
			Type: watch.Added,
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ns,
				},
			},
		}
	}
}

func (c *CFControllerConfig) AddNamespace(we watch.Event) {
	c.namespacesMutex.Lock()
	defer c.namespacesMutex.Unlock()
	n := we.Object.(*corev1.Namespace).Name
	_, found := c._namespaces[n]
	if !found {
		c._namespaces[n] = n
	}
	for _, ch := range c._watchEvents {
		ch <- we
	}
}

func (c *CFControllerConfig) DelNamespace(we watch.Event) {
	c.namespacesMutex.Lock()
	defer c.namespacesMutex.Unlock()
	n := we.Object.(*corev1.Namespace).Name
	_, found := c._namespaces[n]
	if found {
		for _, ch := range c._watchEvents {
			ch <- we
			close(ch)
		}
		delete(c._namespaces, n)
	}
}

func (c *CFControllerConfig) Namespaces() []string {
	c.namespacesMutex.Lock()
	defer c.namespacesMutex.Unlock()
	out := make([]string, len(c._namespaces))
	for n := range c._namespaces {
		out = append(out, n)
	}
	return out
}

func GetConfig(log *zerolog.Logger) (*CFControllerConfig, error) {
	cfg := CFControllerConfig{
		_namespaces: make(map[string]string),
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
	pflag.StringVarP(&cfg.CloudFlare.ZoneId, "cloudflare-zoneid", "z", os.Getenv("CLOUDFLARE_ZONE_ID"), "Cloudflare Zone ID")
	pflag.StringVarP(&cfg.CloudFlare.ApiUrl, "cloudflare-api-url", "u", cfg.CloudFlare.ApiUrl, "Cloudflare API URL")
	pflag.StringVarP(&cfg.Identity, "identity", "i", identity, "identity of this running instance")
	pflag.Parse()
	if cfg.CloudFlare.ApiToken == "" {
		return nil, fmt.Errorf("Cloudflare API Key is required")
	}
	if cfg.CloudFlare.AccountId == "" {
		return nil, fmt.Errorf("Cloudflare Account ID is required")
	}
	if cfg.CloudFlare.ZoneId == "" {
		return nil, fmt.Errorf("Cloudflare Zone ID is required")
	}

	return &cfg, nil
}

const (
	AnnotationCloudflareTunnelName         = "cloudflare.com/tunnel-name"
	AnnotationCloudflareTunnelId           = "cloudflare.com/tunnel-id"
	AnnotationCloudflareTunnelExternalName = "cloudflare.com/tunnel-external-name"
	// CloudflareTunnelAccountId    = "cloudflare.com/tunnel-account-id"
	// CloudflareTunnelZoneId       = "cloudflare.com/tunnel-zone-id"
	AnnotationCloudflareTunnelKeySecret = "cloudflare.com/tunnel-key-secret"
)

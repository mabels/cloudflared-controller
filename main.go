package main

import (
	"os"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/cloudflare/cloudflared/cfapi"
	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/config_maps"
	"github.com/mabels/cloudflared-controller/controller/ingress"
	"github.com/mabels/cloudflared-controller/controller/namespaces"
	"github.com/mabels/cloudflared-controller/controller/svc"

	"github.com/rs/zerolog"
)

func main() {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cfc := controller.CFController{
		Log:  &_log,
		Rest: &controller.RestClients{},
	}
	var err error
	cfc.Cfg, err = config.GetConfig(cfc.Log)
	if err != nil {
		cfc.Log.Fatal().Err(err).Msg("Failed to get config")
	}
	config, err := clientcmd.BuildConfigFromFlags("", cfc.Cfg.KubeConfigFile)
	if err != nil {
		cfc.Log.Info().Str("kubeconfig", cfc.Cfg.KubeConfigFile).Err(err).Msg("kubeconfig no good, trying in-cluster")
		config, err = rest.InClusterConfig()
		if err != nil {
			cfc.Log.Fatal().Err(err).Msg("Failed to get kubeconfig")
		}
	}
	cfc.Rest.K8s, err = kubernetes.NewForConfig(config)
	if err != nil {
		cfc.Log.Fatal().Err(err).Msg("Error building kubernetes clientset")
	}

	cfc.Rest.Cf, err = cfapi.NewRESTClient(cfc.Cfg.CloudFlare.ApiUrl,
		cfc.Cfg.CloudFlare.AccountId, // accountTag string,
		cfc.Cfg.CloudFlare.ZoneId,    // zoneTag string,
		cfc.Cfg.CloudFlare.ApiToken,
		"cloudflared-controller",
		cfc.Log)
	if err != nil {
		cfc.Log.Fatal().Err(err).Msg("Failed to create cloudflare client")
	}

	namespaces.Start(&cfc, svc.WatchSvc, ingress.WatchIngress, config_maps.WatchConfigMaps)

	cfc.Log.Debug().Str("kubeconfig", cfc.Cfg.KubeConfigFile).Msg("Starting controller")
	for {
		time.Sleep(time.Second)
	}
}

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"

	"github.com/cloudflare/cloudflared/cfapi"
	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/config_maps"
	"github.com/mabels/cloudflared-controller/controller/ingress"
	"github.com/mabels/cloudflared-controller/controller/leader"
	"github.com/mabels/cloudflared-controller/controller/namespaces"
	"github.com/mabels/cloudflared-controller/controller/svc"
	"github.com/mabels/cloudflared-controller/controller/tunnel"

	"github.com/rs/zerolog"
)

var Version = "dev"

func main() {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cfc := controller.NewCFController(&_log)
	var err error
	cfc.Cfg, err = config.GetConfig(cfc.Log, Version)
	if err != nil {
		cfc.Log.Fatal().Err(err).Msg("Failed to get config")
	}
	if cfc.Cfg.ShowVersion {
		fmt.Printf("Version:%s\n", Version)
		os.Exit(0)
	}
	if cfc.Cfg.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	// signal.Notify(c, os.Kill)
	go func() {
		<-c
		cfc.Log.Info().Msg("Received SIGINT, shutting down")
		cfc.Shutdown()
		os.Exit(130)
	}()

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

	cfc.Log.Info().Str("kubeconfig", cfc.Cfg.KubeConfigFile).Msg("Starting controller")

	if !cfc.Cfg.NoCloudFlared {
		// 	// start configmap controller for cloudflared
		// 	// cloudflared-controller  namespace
		// 	// get my namespace
		// 	// tunnelid
		// }
		out := make(chan config_maps.ConfigMapTunnelEvent)
		namespaces.Start(cfc, config_maps.WatchTunnelConfigMap(cfc, out))
		go func() {
			cfc.Log.Info().Msg("Start Tunnel Event loop")
			tr := tunnel.NewTunnelRunner()
			for {
				event, more := <-out
				if !more {
					break
				}
				switch event.Ev.Type {
				case watch.Added:
					tr.Start(cfc, &event.Cm)
				case watch.Modified:
					tr.Start(cfc, &event.Cm)
				case watch.Deleted:
					tr.Stop(cfc, &event.Cm)
				default:
					cfc.Log.Err(err).Str("namespace", event.Cm.Namespace).Str("name", event.Cm.Name).Str("type", string(event.Ev.Type)).Msg("Got unknown event")
				}
			}
			cfc.Log.Info().Msg("Stop Tunnel Event loop")

		}()
	}

	var runningLeader func() = nil
	leader.LeaderSelection(cfc, leaderelection.LeaderCallbacks{
		OnStartedLeading: func(c context.Context) {
			if runningLeader != nil {
				cfc.Log.Fatal().Msg("Already running leader")
			}
			cfc.Log.Info().Str("id", cfc.Cfg.Identity).Msg("became leader, starting work.")
			runningLeader, err = namespaces.Start(cfc, ingress.WatchIngress, svc.WatchSvc)
			if err != nil {
				cfc.Log.Fatal().Err(err).Msg("Failed to start leader")
			}
		},
		OnStoppedLeading: func() {
			cfc.Log.Info().Str("id", cfc.Cfg.Identity).Msg("no longer the leader, staying inactive.")
			if runningLeader != nil {
				cfc.Log.Fatal().Msg("Try to stop leader, but not running")
			}
			my := runningLeader
			runningLeader = nil
			my()
		},
		OnNewLeader: func(current_id string) {
			if current_id == cfc.Cfg.Identity {
				return
			}
			cfc.Log.Info().Str("id", cfc.Cfg.Identity).Msgf("new leader is %s", current_id)
		},
	})

	// for {
	// 	time.Sleep(time.Second)
	// }
}

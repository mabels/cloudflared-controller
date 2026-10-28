package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"

	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/cloudflared"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/ingress"
	"github.com/mabels/cloudflared-controller/controller/k8s_data"
	"github.com/mabels/cloudflared-controller/controller/leader"
	"github.com/mabels/cloudflared-controller/controller/svc"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/mabels/cloudflared-controller/utils"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
)

var Version = "dev"

// runLeader does the work only one instance should do, until ctx is done.
func runLeader(cfc types.CFController, ctx context.Context) {
	unreg := cloudflared.ConfigMapHandlerPrepareCloudflared(cfc)
	defer unreg()
	if err := svc.Start(cfc, ctx); err != nil {
		cfc.Log().Error().Err(err).Msg("Failed to start service watcher")
	}
	if err := ingress.Start(cfc, ctx); err != nil {
		cfc.Log().Error().Err(err).Msg("Failed to start ingress watcher")
	}
	<-ctx.Done()
}

func main() {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	klog.SetOutput(utils.ConnectKlog2ZeroLog(&_log))
	klog.LogToStderr(false)

	err := godotenv.Overload()
	if err != nil {
		_log.Debug().Err(err).Msg("not loading from .env file")
	} else {
		_log.Info().Msg("found .env file")
	}

	cfc := controller.NewCFController(&_log)
	cfg, err := config.GetConfig(cfc.Log(), Version)
	if err != nil {
		cfc.Log().Fatal().Err(err).Msg("Failed to get config")
	}

	if cfg.TestCreateAccess {
		if err := controller.CreateAccessApp(cfc.Log(), cfg); err != nil {
			cfc.Log().Fatal().Err(err).Msg("test create access")
		}
		return
	}

	cfc.SetCfg(cfg)
	if cfc.Cfg().ShowVersion {
		fmt.Printf("Version:%s\n", Version)
		os.Exit(0)
	}
	if cfc.Cfg().Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-c
		cfc.Log().Info().Str("signal", sig.String()).Msg("Shutting down")
		cfc.Shutdown()
		os.Exit(130)
	}()

	config, err := clientcmd.BuildConfigFromFlags("", cfc.Cfg().KubeConfigFile)
	if err != nil {
		cfc.Log().Info().Str("kubeconfig", cfc.Cfg().KubeConfigFile).Err(err).Msg("kubeconfig no good, trying in-cluster")
		config, err = rest.InClusterConfig()
		if err != nil {
			cfc.Log().Fatal().Err(err).Msg("Failed to get kubeconfig")
		}
	}
	k8s, err := kubernetes.NewForConfig(config)
	if err != nil {
		cfc.Log().Fatal().Err(err).Msg("Error building kubernetes clientset")
	}
	cfc.Rest().SetK8s(k8s)

	cfc.Log().Info().Str("serverName", config.ServerName).Str("version", Version).Msg("Starting controller")

	cfc.K8sData().TunnelConfigMaps, err = k8s_data.StartTunnelConfigMaps(cfc)
	if err != nil {
		cfc.Log().Fatal().Err(err).Msg("Failed to start tunnel configmap watcher")
	}
	if !cfc.Cfg().NoCloudFlared {
		cfc.RegisterShutdown(
			cfc.K8sData().TunnelConfigMaps.Register(
				cloudflared.ConfigMapHandlerStartCloudflared(cfc)))
	}

	for cfc.Context().Err() == nil {
		leader.LeaderSelection(cfc, leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				cfc.Log().Info().Str("id", cfc.Cfg().Identity).Msg("became leader, starting work.")
				runLeader(cfc, ctx)
			},
			OnStoppedLeading: func() {
				cfc.Log().Info().Str("id", cfc.Cfg().Identity).Msg("no longer the leader, staying inactive.")
			},
			OnNewLeader: func(current_id string) {
				if current_id == cfc.Cfg().Identity {
					return
				}
				cfc.Log().Info().Str("id", cfc.Cfg().Identity).Msgf("new leader is %s", current_id)
			},
		})
		time.Sleep(time.Second * 5)
		cfc.Log().Info().Msg("Restarting leader selection")
	}
}

package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
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
	"github.com/mabels/cloudflared-controller/controller/watcher"
	"github.com/mabels/cloudflared-controller/utils"

	"github.com/rs/zerolog"

	corev1 "k8s.io/api/core/v1"
)

var Version = "dev"

// type namespacesWatcher = types.Watcher[corev1.Namespace, *corev1.Namespace, types.WatcherBindingNamespace, types.WatcherBindingNamespaceClient]

func watchedNamespaces(cfc types.CFController) (types.Watcher[*corev1.Namespace], error) {
	log := cfc.Log().With().Str("watcher", "namespaces").Logger()
	wt := watcher.NewWatcher(
		types.WatcherConfig[corev1.Namespace, *corev1.Namespace, types.WatcherBindingNamespace, types.WatcherBindingNamespaceClient]{
			Log:     &log,
			Context: cfc.Context(),
			K8sClient: types.WatcherBindingNamespaceClient{
				Nif: cfc.Rest().K8s().CoreV1().Namespaces(),
			},
		})
	err := wt.Start()
	return wt, err
}

func main() {
	rand.New(rand.NewSource(time.Now().UnixNano()))
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	klog.SetOutput(utils.ConnectKlog2ZeroLog(&_log))
	klog.LogToStderr(false)
	cfc := controller.NewCFController(&_log)
	cfg, err := config.GetConfig(cfc.Log(), Version)
	if err != nil {
		cfc.Log().Fatal().Err(err).Msg("Failed to get config")
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
	signal.Notify(c, os.Interrupt)
	// signal.Notify(c, os.Kill)
	go func() {
		<-c
		cfc.Log().Info().Msg("Received SIGINT, shutting down")
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

	cfc.Rest().K8s().CoreV1().Namespaces()

	cfc.K8sData().Namespaces, err = watchedNamespaces(cfc)
	if err != nil {
		cfc.Log().Fatal().Err(err).Msg("Failed to start namespace watcher")
	}
	// err = cfc.K8sData.Namespaces.Start()
	// if err != nil {
	// 	cfc.Log().Fatal().Err(err).Msg("Failed to start namespace watcher")
	// }
	// cfc.K8sData.TunnelConfigMaps = k8s_data.NewTunnelConfigMaps()

	cfc.Log().Info().Str("serverName", config.ServerName).Str("version", Version).Msg("Starting controller")

	if !cfc.Cfg().NoCloudFlared {
		cfc.K8sData().TunnelConfigMaps = k8s_data.StartWaitForTunnelConfigMaps(cfc)
		cfc.RegisterShutdown(
			cfc.K8sData().TunnelConfigMaps.Register(
				cloudflared.ConfigMapHandlerStartCloudflared(cfc)))
	}

	for {
		runningLeaders := []func(){}
		leader.LeaderSelection(cfc, leaderelection.LeaderCallbacks{
			OnStartedLeading: func(c context.Context) {
				if len(runningLeaders) > 0 {
					cfc.Log().Fatal().Msg("Already running leader")
				}
				cfc.Log().Info().Str("id", cfc.Cfg().Identity).Msg("became leader, starting work.")
				go func() {
					// this might been take to long for the election selection
					runningLeaders = append(runningLeaders,
						ingress.Start(cfc),
						svc.Start(cfc),
						cloudflared.ConfigMapHandlerPrepareCloudflared(cfc))
				}()
			},
			OnStoppedLeading: func() {
				cfc.Log().Info().Str("id", cfc.Cfg().Identity).Msg("no longer the leader, staying inactive.")
				if len(runningLeaders) == 0 {
					cfc.Log().Fatal().Msg("Try to stop leader, but not running")
				}
				my := runningLeaders
				runningLeaders = []func(){}
				for _, f := range my {
					f()
				}
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

	// for {
	// 	time.Sleep(time.Second)
	// }
}

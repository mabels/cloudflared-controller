package namespaces

import (
	"context"

	"github.com/mabels/cloudflared-controller/controller"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func namespaceObserver(_cfc *controller.CFController) {
	cfc := _cfc.WithComponent("namespaceObserver")
	nIf, err := cfc.Rest.K8s.CoreV1().Namespaces().Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		cfc.Log.Fatal().Err(err).Msg("Error watching namespaces")
	}
	cfc.Log.Info().Msg("Start Watching Namespaces")
	for {
		ev, more := <-nIf.ResultChan()
		if !more {
			break
		}
		ns, found := ev.Object.(*corev1.Namespace)
		if !found {
			cfc.Log.Error().Any("ev", ev).Msg("Unknown Namespace event")
			continue
		}
		found = false
		for _, n := range cfc.Cfg.PresetNamespaces {
			if n == ns.Name {
				found = true
				break
			}
		}
		if len(cfc.Cfg.PresetNamespaces) > 0 && !found {
			cfc.Log.Info().Str("namespace", ns.Name).Msg("Skipping Namespace event")
			continue
		}
		switch ev.Type {
		case watch.Added:
			cfc.Log.Info().Str("namespace", ns.Name).Msg("Add Namespace event")
			cfc.Cfg.AddNamespace(ev)
		case watch.Deleted:
			cfc.Log.Info().Str("namespace", ns.Name).Msg("Del Namespace event")
			cfc.Cfg.DelNamespace(ev)
		default:
			cfc.Log.Error().Str("namespace", ns.Name).Str("type", string(ev.Type)).Msg("Unknown Namespace event")
		}

	}
	cfc.Log.Info().Msg("Done Watching Namespaces")
}

type watchFunc func(*controller.CFController, string) (watch.Interface, error)

func perNamespaceObserver(_cfc *controller.CFController, fns ...watchFunc) {
	cfc := _cfc.WithComponent("perNamespaceObserver")
	wes := make(chan watch.Event, 1+len(cfc.Cfg.Namespaces()))
	cfc.Cfg.AddWatch(wes)
	ns2wif := make(map[string][]watch.Interface)
	cfc.Log.Debug().Msg("Start Watching Namespaces")
	for {
		ev, more := <-wes
		if !more {
			break
		}
		namespace := ev.Object.(*corev1.Namespace).Name
		log := cfc.Log.With().Str("namespace", namespace).Logger()
		ns2wif[namespace] = []watch.Interface{}
		switch ev.Type {
		case watch.Added:
			for _, fn := range fns {
				my := *cfc
				my.Log = &log
				wif, err := fn(&my, namespace)
				if err != nil {
					log.Error().Err(err).Msg("Failed to watch")
				} else {
					ns2wif[namespace] = append(ns2wif[namespace], wif)
				}
			}
		case watch.Deleted:
			log.Info().Msg("Stop watching")
			for _, wif := range ns2wif[namespace] {
				wif.Stop()
			}
		default:
			log.Error().Any("ev", ev).Msg("Unknown event")
		}
	}
	cfc.Log.Debug().Msg("Stop Watching Namespaces")
}

func Start(cfc *controller.CFController, fns ...watchFunc) {
	go namespaceObserver(cfc)
	go perNamespaceObserver(cfc, fns...)
}

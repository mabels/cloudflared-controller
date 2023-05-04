package namespaces

import (
	"github.com/google/uuid"
	"github.com/mabels/cloudflared-controller/controller"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type WatchFunc func(*controller.CFController, string) (watch.Interface, error)

func namespaceObserver(_cfc *controller.CFController, nsHandler *Namespaces) (watch.Interface, error) {
	cfc := _cfc.WithComponent("namespaceObserver")
	nIf, err := cfc.Rest.K8s.CoreV1().Namespaces().Watch(cfc.Context, metav1.ListOptions{})
	if err != nil {
		cfc.Log.Error().Err(err).Msg("Error watching namespaces")
		return nil, err
	}
	go func() {
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
				nsHandler.AddNamespace(ev)
			case watch.Deleted:
				cfc.Log.Info().Str("namespace", ns.Name).Msg("Del Namespace event")
				nsHandler.DelNamespace(ev)
			default:
				cfc.Log.Error().Str("namespace", ns.Name).Str("type", string(ev.Type)).Msg("Unknown Namespace event")
			}
		}
		cfc.Log.Info().Msg("Done Watching Namespaces")
	}()
	return nIf, nil
}

func perNamespaceObserver(_cfc *controller.CFController, nsHandler *Namespaces, fns ...WatchFunc) (chan watch.Event, error) {
	cfc := _cfc.WithComponent("perNamespaceObserver", func(c *controller.CFController) {
		log := c.Log.With().Str("uuid", uuid.NewString()).Logger()
		c.Log = &log
	})
	wes := make(chan watch.Event, 1+len(nsHandler.Namespaces()))
	nsHandler.AddWatch(wes)
	go func() {
		ns2wif := make(map[string][]watch.Interface)
		cfc.Log.Debug().Int("fns", len(fns)).Msg("Start Watching Namespaces")
		for {
			ev, more := <-wes
			if !more {
				break
			}
			namespace := ev.Object.(*corev1.Namespace).Name
			log := cfc.Log.With().Str("namespace", namespace).Logger()
			ns2wif[namespace] = []watch.Interface{}
			log.Debug().Str("type", string(ev.Type)).Str("ns", namespace).Int("wifs", len(ns2wif[namespace])).Msg("Namespace event")
			switch ev.Type {
			case watch.Added:
				if len(ns2wif[namespace]) != 0 {
					log.Info().Msg("Already watching")
					continue
				}
				for _, fn := range fns {
					wif, err := fn(_cfc, namespace)
					if err != nil {
						log.Error().Err(err).Msg("Failed to watch")
					} else {
						ns2wif[namespace] = append(ns2wif[namespace], wif)
						log.Debug().Str("type", string(ev.Type)).Str("ns", namespace).Int("wifs", len(ns2wif[namespace])).Msg("Namespace event added wif")
					}
				}
			case watch.Deleted:
				log.Info().Msg("Stop watching")
				wifs := ns2wif[namespace]
				delete(ns2wif, namespace)
				for _, wif := range wifs {
					wif.Stop()
				}
			default:
				log.Error().Any("ev", ev).Msg("Unknown event")
			}
		}
		cfc.Log.Debug().Msg("Stop Watching Namespaces")
	}()
	return wes, nil
}

func Start(cfc *controller.CFController, fns ...WatchFunc) (func(), error) {
	ns := New()
	wif, err := namespaceObserver(cfc, ns)
	if err != nil {
		return nil, err
	}
	cha, err := perNamespaceObserver(cfc, ns, fns...)
	if err != nil {
		return nil, err
	}
	return func() {
		wif.Stop()
		close(cha)
	}, nil
}

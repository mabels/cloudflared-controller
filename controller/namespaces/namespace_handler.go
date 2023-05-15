package namespaces

import (
	"github.com/mabels/cloudflared-controller/controller/types"
)

func SkipNamespace(cfc types.CFController, ns string) bool {
	found := false
	for _, n := range cfc.Cfg().PresetNamespaces {
		if n == ns {
			found = true
			break
		}
	}
	if len(cfc.Cfg().PresetNamespaces) > 0 && !found {
		cfc.Log().Debug().Str("namespace", ns).Msg("Skipping Namespace event")
		return true
	}
	return false
}

// func namespaceObserver(_cfc types.CFController, nsHandler *Namespaces) (watch.Interface, error) {
// 	cfc := _cfc.WithComponent("namespaceObserver")
// 	client := cfc.Rest.K8s.CoreV1().Namespaces()
// 	nIf, err := client.Watch(cfc.Context, metav1.ListOptions{})
// 	if err != nil {
// 		cfc.Log().Error().Err(err).Msg("Error watching namespaces")
// 		return nil, err
// 	}
// 	nl, err := client.List(cfc.Context, metav1.ListOptions{})
// 	if err != nil {
// 		cfc.Log().Error().Err(err).Msg("Error listing namespaces")
// 		return nil, err
// 	}
// 	for _, ns := range nl.Items {
// 		if skipNamespace(cfc, ns.Name) {
// 			continue
// 		}
// 		nsHandler.AddNamespace(watch.Event{Type: watch.Added, Object: &ns}, []chan watch.Event{})
// 	}

// 	go func() {
// 		cfc.Log().Info().Msg("Start Watching Namespaces")
// 		for {
// 			ev, more := <-nIf.ResultChan()
// 			if !more {
// 				break
// 			}
// 			ns, found := ev.Object.(*corev1.Namespace)
// 			if !found {
// 				cfc.Log().Error().Any("ev", ev).Msg("Unknown Namespace event")
// 				continue
// 			}
// 			if skipNamespace(cfc, ns.Name) {
// 				continue
// 			}
// 			switch ev.Type {
// 			case watch.Added:
// 				cfc.Log().Info().Str("namespace", ns.Name).Msg("Add Namespace event")
// 				nsHandler.AddNamespace(ev)
// 			case watch.Deleted:
// 				cfc.Log().Info().Str("namespace", ns.Name).Msg("Del Namespace event")
// 				nsHandler.DelNamespace(ev)
// 			default:
// 				cfc.Log().Error().Str("namespace", ns.Name).Str("type", string(ev.Type)).Msg("Unknown Namespace event")
// 			}
// 		}
// 		cfc.Log().Info().Msg("Done Watching Namespaces")
// 	}()
// 	return nIf, nil
// }

// func perNamespaceObserver(_cfc types.CFController, nsHandler *Namespaces, fns ...types.WatchFunc) (chan watch.Event, error) {
// 	cfc := _cfc.WithComponent("perNamespaceObserver", func(c types.CFController) {
// 		log := c.Log.With().Str("uuid", uuid.NewString()).Logger()
// 		c.Log = &log
// 	})
// 	wes := make(chan watch.Event, 1+len(nsHandler.Namespaces()))
// 	nsHandler.AddWatch(wes)
// 	go func() {
// 		ns2wif := make(map[string][]watch.Interface)
// 		cfc.Log().Debug().Int("fns", len(fns)).Msg("Start Watching Namespaces")
// 		for {
// 			ev, more := <-wes
// 			if !more {
// 				break
// 			}
// 			namespace := ev.Object.(*corev1.Namespace).Name
// 			log := cfc.Log().With().Str("namespace", namespace).Logger()
// 			ns2wif[namespace] = []watch.Interface{}
// 			log.Debug().Str("type", string(ev.Type)).Str("ns", namespace).Int("wifs", len(ns2wif[namespace])).Msg("Namespace event")
// 			switch ev.Type {
// 			case watch.Added:
// 				if len(ns2wif[namespace]) != 0 {
// 					log.Info().Msg("Already watching")
// 					continue
// 				}
// 				for _, fn := range fns {
// 					wif, err := fn(_cfc, namespace)
// 					if err != nil {
// 						log.Error().Err(err).Msg("Failed to watch")
// 					} else {
// 						ns2wif[namespace] = append(ns2wif[namespace], wif)
// 						log.Debug().Str("type", string(ev.Type)).Str("ns", namespace).Int("wifs", len(ns2wif[namespace])).Msg("Namespace event added wif")
// 					}
// 				}
// 			case watch.Deleted:
// 				log.Info().Msg("Stop watching")
// 				wifs := ns2wif[namespace]
// 				delete(ns2wif, namespace)
// 				for _, wif := range wifs {
// 					wif.Stop()
// 				}
// 			default:
// 				log.Error().Any("ev", ev).Msg("Unknown event")
// 			}
// 		}
// 		cfc.Log().Debug().Msg("Stop Watching Namespaces")
// 	}()
// 	return wes, nil
// }

// func Start(cfc types.CFController, fns ...types.WatchFunc) (func(), error) {
// 	ns := New()
// 	wif, err := namespaceObserver(cfc, ns)
// 	if err != nil {
// 		return nil, err
// 	}
// 	cha, err := perNamespaceObserver(cfc, ns, fns...)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return func() {
// 		wif.Stop()
// 		close(cha)
// 	}, nil
// }

package k8s_data

import (
	"github.com/mabels/cloudflared-controller/controller/namespaces"
	"github.com/mabels/cloudflared-controller/controller/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func StartWaitForTunnelConfigMaps(cfc types.CFController) types.TunnelConfigMaps {
	tcm := newTunnelConfigMaps()
	cfc.K8sData().Namespaces.RegisterEvent(func(state []*corev1.Namespace, ev watch.Event) {
		ns, found := ev.Object.(*corev1.Namespace)
		if !found {
			cfc.Log().Error().Any("ev", ev).Msg("Failed to cast to Namespace")
			return
		}
		if namespaces.SkipNamespace(cfc, ns.Name) {
			return
		}
		switch ev.Type {
		case watch.Added:
			tcm.watcherLock.Lock()
			if _, ok := tcm.watchers[ns.Name]; !ok {
				wns, err := perNamespaceStartConfigMapsWatcher(cfc, tcm, ns.Name)
				if err != nil {
					cfc.Log().Error().Err(err).Msg("Failed to start ConfigMap watcher")
					tcm.watcherLock.Unlock()
					return
				}
				tcm.watchers[ns.Name] = wns
			}
			tcm.watcherLock.Unlock()
		case watch.Modified:
			// tr.Start(cfc, &ev.Cm)
		case watch.Deleted:
			// tr.Stop(cfc, &ev.Cm)
			tcm.watcherLock.Lock()
			if _, ok := tcm.watchers[ns.Name]; ok {
				wns := tcm.watchers[ns.Name]
				delete(tcm.watchers, ns.Name)
				wns.unregisterEvent()
				wns.stop()
			}
			tcm.watcherLock.Unlock()
		default:
			cfc.Log().Error().Any("ev", ev).Str("type", string(ev.Type)).Msg("Got unknown event")
		}
	})
	return tcm
}

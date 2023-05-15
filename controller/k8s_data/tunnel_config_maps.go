package k8s_data

import (
	"sync"

	"github.com/google/uuid"
	"github.com/mabels/cloudflared-controller/controller/namespaces"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/mabels/cloudflared-controller/controller/watcher"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type watcherBindingNamespace struct {
	watcher          types.Watcher[*corev1.ConfigMap]
	unregisterEvent  func()
	tunnelConfigMaps *tunnelConfigMaps
	namespace        string
}

func (wbn watcherBindingNamespace) stop() {
	wbn.watcher.Stop()
}

type configMapBindings struct {
	cm corev1.ConfigMap
}

type tunnelConfigMapEvent func([]*corev1.ConfigMap, watch.Event)

type tunnelConfigMaps struct {
	cmsLock sync.Mutex
	// key namespace/name
	cms map[string]configMapBindings

	fnsLock sync.Mutex
	// key uuid
	fns map[string]tunnelConfigMapEvent

	watcherLock sync.Mutex
	// key namespace
	watchers map[string]watcherBindingNamespace
}

func newTunnelConfigMaps() *tunnelConfigMaps {
	ret := &tunnelConfigMaps{
		cms:      make(map[string]configMapBindings),
		fns:      make(map[string]tunnelConfigMapEvent),
		watchers: make(map[string]watcherBindingNamespace),
	}
	return ret
}

func (tcm *tunnelConfigMaps) Register(fn func([]*corev1.ConfigMap, watch.Event)) func() {
	tcm.fnsLock.Lock()
	uid := uuid.NewString()
	tcm.fns[uid] = fn
	tcm.fnsLock.Unlock()
	cms := tcm.Get()
	for _, cm := range cms {
		fn(cms, watch.Event{
			Type:   watch.Added,
			Object: cm,
		})
	}
	return func() {
		tcm.fnsLock.Lock()
		delete(tcm.fns, uid)
		tcm.fnsLock.Unlock()
	}
}

func (tcm *tunnelConfigMaps) Get() []*corev1.ConfigMap {
	tcm.cmsLock.Lock()
	defer tcm.cmsLock.Unlock()
	ret := make([]*corev1.ConfigMap, 0, len(tcm.cms))
	for _, cm := range tcm.cms {
		ret = append(ret, &cm.cm)
	}
	return ret
}

func cmsKey(cm *corev1.ConfigMap) string {
	return cm.Namespace + "/" + cm.Name
}
func (tcm *tunnelConfigMaps) upsert(cm *corev1.ConfigMap) {
	key := cmsKey(cm)
	tcm.cmsLock.Lock()
	defer tcm.cmsLock.Unlock()
	tcm.cms[key] = configMapBindings{
		cm: *cm,
	}
}

func (tcm *tunnelConfigMaps) delete(cm *corev1.ConfigMap) {
	key := cmsKey(cm)
	tcm.cmsLock.Lock()
	defer tcm.cmsLock.Unlock()
	delete(tcm.watchers, key)
}

// func (tcm *tunnelConfigMaps) Get() []types.TunnelConfigMap {
// 	tcm.cmsLock.Lock()
// 	defer tcm.cmsLock.Unlock()
// 	ret := make([]types.TunnelConfigMap, 0, len(tcm.cms))
// 	for _, cm := range tcm.cms {
// 		ret = append(ret, cm)
// 	}
// 	return ret
// }

// func (tcm *tunnelConfigMaps) WatchConfigMaps() types.WatchFunc {
// 	return func(_cfc types.CFController, namespace string) (watch.Interface, error) {
// 		cfc := _cfc.WithComponent("WatchConfigMaps", func(cfc types.CFController) {
// 			my := cfc.Log().With().Str("namespace", namespace).Logger()
// 			cfc.Log = &my
// 		})
// 		log := cfc.Log
// 		wif, err := cfc.Rest.K8s.CoreV1().ConfigMaps(namespace).Watch(context.Background(), metav1.ListOptions{
// 			LabelSelector: "app=cloudflared-controller",
// 		})
// 		if err != nil {
// 			log.Error().Err(err).Msg("Failed to watch ConfigMaps")
// 			return nil, err
// 		}

// 		cms, err := cfc.Rest.K8s.CoreV1().ConfigMaps(namespace).List(context.Background(), metav1.ListOptions{
// 			LabelSelector: "app=cloudflared-controller",
// 		})
// 		if err != nil {
// 			log.Error().Err(err).Msg("Failed to list ConfigMaps")
// 			return nil, err
// 		}
// 		{
// 			// there was set a lock in NewTunnelConfigMaps this should released
// 			// until the inital load
// 			defer tcm.cmsLock.Unlock()
// 			for _, cm := range cms.Items {
// 				key := fmt.Sprintf("%s/%s", cm.Namespace, cm.Name)
// 				log.Debug().Str("key", key).Msg("Found ConfigMap")
// 				tcm.cms[key] = types.TunnelConfigMap{Cm: cm}
// 			}
// 		}

// 		go func() {
// 			log.Info().Msg("Start Watching ConfigMap")
// 			for {
// 				ev, more := <-wif.ResultChan()
// 				if !more {
// 					break
// 				}
// 				cm, ok := ev.Object.(*corev1.ConfigMap)
// 				if !ok {
// 					log.Error().Msg("Failed to cast to ConfigMap")
// 					continue
// 				}
// 				if cm.Namespace != namespace {
// 					log.Error().Msg("ConfigMap not in watched namespace")
// 					continue
// 				}
// 				{
// 					key := fmt.Sprintf("%s/%s", cm.Namespace, cm.Name)
// 					tcm.cmsLock.Lock()
// 					defer tcm.cmsLock.Unlock()

// 					switch ev.Type {
// 					case watch.Modified:
// 						tcm.cms[key] = types.TunnelConfigMap{Cm: *cm}
// 					case watch.Added:
// 						tcm.cms[key] = types.TunnelConfigMap{Cm: *cm}
// 					case watch.Deleted:
// 						delete(tcm.cms, key)
// 					default:
// 						log.Error().Str("type", string(ev.Type)).Msg("Unknown event type")
// 						continue
// 					}
// 				}
// 			}
// 			log.Info().Msg("Stopped Watching ConfigMap")
// 		}()
// 		return wif, nil
// 	}
// }

func startConfigMapsWatcher(cfc types.CFController, tcm *tunnelConfigMaps, ns string) (watcherBindingNamespace, error) {
	log := cfc.Log().With().Str("watcher", "configMaps").Str("namespace", ns).Logger()
	wt := watcher.NewWatcher(
		types.WatcherConfig[corev1.ConfigMap, *corev1.ConfigMap, types.WatcherBindingConfigMap, types.WatcherBindingConfigMapClient]{
			ListOptions: metav1.ListOptions{
				LabelSelector: cfc.Cfg().ConfigMapLabelSelector,
			},
			Log:     &log,
			Context: cfc.Context(),
			K8sClient: types.WatcherBindingConfigMapClient{
				Cif: cfc.Rest().K8s().CoreV1().ConfigMaps(ns),
			},
		})
	unreg := wt.RegisterEvent(func(_ []*corev1.ConfigMap, ev watch.Event) {
		cm, ok := ev.Object.(*corev1.ConfigMap)
		if !ok {
			cfc.Log().Error().Any("ev", ev).Msg("Failed to cast to ConfigMap")
			return
		}
		switch ev.Type {
		case watch.Added:
			tcm.upsert(cm)
		case watch.Modified:
			tcm.upsert(cm)
		case watch.Deleted:
			tcm.delete(cm)
		default:
			cfc.Log().Error().Any("ev", ev).Str("type", string(ev.Type)).Msg("Got unknown event")
		}
		tcm.fnsLock.Lock()
		fns := make([]tunnelConfigMapEvent, 0, len(tcm.fns))
		for _, fn := range tcm.fns {
			fns = append(fns, fn)
		}
		tcm.fnsLock.Unlock()
		cms := tcm.Get()
		for _, fn := range fns {
			fn(cms, ev)
		}

	})
	err := wt.Start()
	return watcherBindingNamespace{
		watcher:          wt,
		unregisterEvent:  unreg,
		tunnelConfigMaps: tcm,
		namespace:        ns,
	}, err
}

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
				wns, err := startConfigMapsWatcher(cfc, tcm, ns.Name)
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

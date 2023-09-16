package k8s_data

import (
	"fmt"
	"reflect"
	"regexp"
	"sync"

	"github.com/google/uuid"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/mabels/cloudflared-controller/controller/watcher"
	"gopkg.in/yaml.v3"
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
	cm   *corev1.ConfigMap
	lock sync.Mutex
}

type tunnelConfigMapEvent func([]*corev1.ConfigMap, watch.Event)

var reSanitzeNice = regexp.MustCompile(`[^_\\-\\.a-zA-Z0-9]+`)
var reSanitzeAlpha = regexp.MustCompile(`[^a-zA-Z0-9]+`)

type tunnelConfigMaps struct {
	cmsLock sync.Mutex
	// key namespace/name
	cms map[string]*configMapBindings

	fnsLock sync.Mutex
	// key uuid
	fns map[string]tunnelConfigMapEvent

	watcherLock sync.Mutex
	// key namespace
	watchers map[string]watcherBindingNamespace
}

func newTunnelConfigMaps() *tunnelConfigMaps {
	ret := &tunnelConfigMaps{
		cms:      make(map[string]*configMapBindings),
		fns:      make(map[string]tunnelConfigMapEvent),
		watchers: make(map[string]watcherBindingNamespace),
	}
	return ret
}

func cmKey(kind, ns, name string) string {
	if kind == "" {
		panic("kind cannot be empty")
	}
	return reSanitzeNice.ReplaceAllString(fmt.Sprintf("%s-%s-%s", kind, ns, name), "_")
}

func UpsertConfigMap(cfc types.CFController, tparam *types.CFTunnelParameter, cm *corev1.ConfigMap) error {
	client := cfc.Rest().K8s().CoreV1().ConfigMaps(tparam.K8SConfigMapName().Namespace)
	toUpdate, err := client.Get(cfc.Context(), tparam.K8SConfigMapName().Name, metav1.GetOptions{})
	if err != nil {
		_, err = client.Create(cfc.Context(), cm, metav1.CreateOptions{})
	} else {
		for k, v := range cm.ObjectMeta.Annotations {
			toUpdate.ObjectMeta.Annotations[k] = v
		}
		for k, v := range cm.Data {
			toUpdate.Data[k] = v
		}
		_, err = client.Update(cfc.Context(), toUpdate, metav1.UpdateOptions{})
	}
	return err
}

func (ts *tunnelConfigMaps) lockConfigMap(kind string, tparam *types.CFTunnelParameter) func() {
	key := cmKey(kind, tparam.K8SConfigMapName().Namespace, tparam.K8SConfigMapName().Name)
	ts.cmsLock.Lock()
	cmb, ok := ts.cms[key]
	if !ok {
		cmb = &configMapBindings{}
		ts.cms[key] = cmb
	}
	ts.cmsLock.Unlock()
	cmb.lock.Lock()
	return func() {
		cmb.lock.Unlock()
	}
}

func (ts *tunnelConfigMaps) UpsertConfigMap(cfc types.CFController, tparam *types.CFTunnelParameter, kind string, meta *metav1.ObjectMeta, cfcis []types.CFConfigIngress) error {
	yCFConfigIngressByte, err := yaml.Marshal(cfcis)
	if err != nil {
		cfc.Log().Error().Err(err).Msg("Error marshaling cfcis")
		return err
	}

	annos := make(map[string]string)
	for k, v := range meta.Annotations {
		annos[k] = v
	}
	annos[config.AnnotationCloudflareTunnelK8sSecret()] = tparam.K8SSecretName().FQDN
	annos[config.AnnotationCloudflareTunnelState()] = "preparing"

	delete(annos, config.AnnotationCloudflareTunnelExternalName())
	delete(annos, config.AnnotationCloudflareTunnelK8sConfigMap())

	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tparam.K8SConfigMapName().Name,
			Namespace:   tparam.K8SConfigMapName().Namespace,
			Labels:      config.CfLabels(meta.Labels, cfc),
			Annotations: annos,
		},
		Data: map[string]string{
			cmKey(kind, meta.Namespace, meta.Name): string(yCFConfigIngressByte),
		},
	}

	unlock := ts.lockConfigMap(kind, tparam)
	defer unlock()
	return UpsertConfigMap(cfc, tparam, &cm)
}

func (ts *tunnelConfigMaps) RemoveConfigMap(cfc types.CFController, kind string, meta *metav1.ObjectMeta) {
	for _, toUpdate := range cfc.K8sData().TunnelConfigMaps.Get() {
		key := cmKey(kind, meta.Namespace, meta.Name)
		needChange := len(toUpdate.Data)
		delete(toUpdate.Data, key)
		if needChange != len(toUpdate.Data) {
			unlock := ts.lockConfigMap(kind, &types.CFTunnelParameter{
				Namespace: toUpdate.GetNamespace(),
				Name:      toUpdate.GetName(),
			})
			client := cfc.Rest().K8s().CoreV1().ConfigMaps(toUpdate.GetNamespace())
			toUpdate.Annotations[config.AnnotationCloudflareTunnelState()] = "preparing"
			_, err := client.Update(cfc.Context(), toUpdate, metav1.UpdateOptions{})
			unlock()
			if err != nil {
				cfc.Log().Error().Err(err).Str("name", key).Msg("Error updating config")
				continue
			}
			cfc.Log().Debug().Str("key", key).Msg("Removing from config")
		}
	}
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
		if cm.cm != nil {
			ret = append(ret, cm.cm)
		}
	}
	return ret
}

func cmsKey(cm *corev1.ConfigMap) string {
	return cm.Namespace + "/" + cm.Name
}
func (tcm *tunnelConfigMaps) upsert(cm *corev1.ConfigMap) {
	key := cmsKey(cm)
	tcm.cmsLock.Lock()
	currentCm, found := tcm.cms[key]
	if found && currentCm.cm != nil &&
		reflect.DeepEqual(currentCm.cm.Data, cm.Data) &&
		reflect.DeepEqual(currentCm.cm.Labels, cm.Labels) &&
		reflect.DeepEqual(currentCm.cm.Annotations, cm.Annotations) {
		tcm.cmsLock.Unlock()
		return
	}
	if !found {
		tcm.cms[key] = &configMapBindings{}
	}
	tcm.cms[key].cm = cm.DeepCopy()
	tcm.cmsLock.Unlock()
	typ := watch.Modified
	if !found {
		typ = watch.Added
	}
	tcm.fireEvents(cm, typ)
}

func (tcm *tunnelConfigMaps) fireEvents(cm *corev1.ConfigMap, typ watch.EventType) {
	tcm.fnsLock.Lock()
	defer tcm.fnsLock.Unlock()
	cms := tcm.Get()
	for _, fn := range tcm.fns {
		fn(cms, watch.Event{
			Type:   typ,
			Object: cm,
		})
	}
}

func (tcm *tunnelConfigMaps) delete(cm *corev1.ConfigMap) {
	key := cmsKey(cm)
	tcm.cmsLock.Lock()
	ocm, found := tcm.cms[key]
	if found {
		delete(tcm.watchers, key)
		tcm.cmsLock.Unlock()
		tcm.fireEvents(ocm.cm, watch.Deleted)
	} else {
		tcm.cmsLock.Unlock()
	}
}

func perNamespaceStartConfigMapsWatcher(cfc types.CFController, tcm *tunnelConfigMaps, ns string) (watcherBindingNamespace, error) {
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

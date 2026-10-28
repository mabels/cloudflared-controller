package k8s_data

import (
	"fmt"
	"reflect"
	"regexp"
	"sync"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/informer"
	"github.com/mabels/cloudflared-controller/controller/types"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// the character class is kept as is (it does not include '-'),
// the generated keys are stored in existing configmaps
var reSanitzeNice = regexp.MustCompile(`[^_\\-\\.a-zA-Z0-9]+`)

func cmKey(kind, ns, name string) string {
	if kind == "" {
		panic("kind cannot be empty")
	}
	return reSanitzeNice.ReplaceAllString(fmt.Sprintf("%s-%s-%s", kind, ns, name), "_")
}

// serializes all writes to one configmap within this process
var cmLocks = struct {
	sync.Mutex
	locks map[string]*sync.Mutex
}{locks: make(map[string]*sync.Mutex)}

func lockConfigMap(ns, name string) func() {
	key := ns + "/" + name
	cmLocks.Lock()
	l, ok := cmLocks.locks[key]
	if !ok {
		l = &sync.Mutex{}
		cmLocks.locks[key] = l
	}
	cmLocks.Unlock()
	l.Lock()
	return l.Unlock
}

// UpsertConfigMap merges annotations and data of cm into the configmap of tparam.
func UpsertConfigMap(cfc types.CFController, tparam *types.CFTunnelParameter, cm *corev1.ConfigMap) error {
	name := tparam.K8SConfigMapName()
	unlock := lockConfigMap(name.Namespace, name.Name)
	defer unlock()
	client := cfc.Rest().K8s().CoreV1().ConfigMaps(name.Namespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := client.Get(cfc.Context(), name.Name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			_, err = client.Create(cfc.Context(), cm, metav1.CreateOptions{})
			return err
		}
		if err != nil {
			return err
		}
		toUpdate := current.DeepCopy()
		if toUpdate.Annotations == nil {
			toUpdate.Annotations = make(map[string]string)
		}
		for k, v := range cm.Annotations {
			toUpdate.Annotations[k] = v
		}
		if toUpdate.Data == nil {
			toUpdate.Data = make(map[string]string)
		}
		for k, v := range cm.Data {
			toUpdate.Data[k] = v
		}
		if reflect.DeepEqual(current.Annotations, toUpdate.Annotations) && reflect.DeepEqual(current.Data, toUpdate.Data) {
			return nil
		}
		_, err = client.Update(cfc.Context(), toUpdate, metav1.UpdateOptions{})
		return err
	})
}

type tunnelConfigMaps struct {
	informer *informer.Informer[*corev1.ConfigMap]
}

// StartTunnelConfigMaps watches the configmaps labeled for this controller.
func StartTunnelConfigMaps(cfc types.CFController) (types.TunnelConfigMaps, error) {
	inf := informer.New[*corev1.ConfigMap](cfc,
		func(f informers.SharedInformerFactory) cache.SharedIndexInformer {
			return f.Core().V1().ConfigMaps().Informer()
		},
		informers.WithTweakListOptions(func(o *metav1.ListOptions) {
			o.LabelSelector = cfc.Cfg().ConfigMapLabelSelector
		}))
	tcm := &tunnelConfigMaps{informer: inf}
	return tcm, inf.Start(cfc.Context())
}

func (tcm *tunnelConfigMaps) Get() []*corev1.ConfigMap {
	return tcm.informer.List()
}

func (tcm *tunnelConfigMaps) Register(fn func([]*corev1.ConfigMap, watch.Event)) func() {
	unreg, err := tcm.informer.Register(func(ev watch.Event) {
		fn(tcm.Get(), ev)
	})
	if err != nil {
		// only happens if the informer was already stopped
		return func() {}
	}
	return unreg
}

func (tcm *tunnelConfigMaps) UpsertConfigMap(cfc types.CFController, tparam *types.CFTunnelParameter, kind string, meta *metav1.ObjectMeta, cfcis []types.CFConfigIngress) error {
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

	delete(annos, config.AnnotationCloudflareTunnelExternalName())
	delete(annos, config.AnnotationCloudflareTunnelK8sConfigMap())

	key := cmKey(kind, meta.Namespace, meta.Name)
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        tparam.K8SConfigMapName().Name,
			Namespace:   tparam.K8SConfigMapName().Namespace,
			Labels:      config.CfLabels(meta.Labels, cfc),
			Annotations: annos,
		},
		Data: map[string]string{
			key: string(yCFConfigIngressByte),
		},
	}
	// the resource might have moved from another tunnel
	tcm.removeKey(cfc, key, tparam.K8SConfigMapName().FQDN)
	return UpsertConfigMap(cfc, tparam, &cm)
}

func (tcm *tunnelConfigMaps) RemoveConfigMap(cfc types.CFController, kind string, meta *metav1.ObjectMeta) {
	tcm.removeKey(cfc, cmKey(kind, meta.Namespace, meta.Name), "")
}

// removeKey drops key from every tunnel configmap except the one named exceptFQDN.
func (tcm *tunnelConfigMaps) removeKey(cfc types.CFController, key string, exceptFQDN string) {
	for _, cached := range tcm.Get() {
		if _, found := cached.Data[key]; !found {
			continue
		}
		if cached.Namespace+"/"+cached.Name == exceptFQDN {
			continue
		}
		err := func() error {
			unlock := lockConfigMap(cached.Namespace, cached.Name)
			defer unlock()
			client := cfc.Rest().K8s().CoreV1().ConfigMaps(cached.Namespace)
			return retry.RetryOnConflict(retry.DefaultRetry, func() error {
				toUpdate, err := client.Get(cfc.Context(), cached.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if _, found := toUpdate.Data[key]; !found {
					return nil
				}
				delete(toUpdate.Data, key)
				_, err = client.Update(cfc.Context(), toUpdate, metav1.UpdateOptions{})
				return err
			})
		}()
		if err != nil {
			cfc.Log().Error().Err(err).Str("key", key).Str("configMap", cached.Name).Msg("Error removing from config")
			continue
		}
		cfc.Log().Info().Str("key", key).Str("configMap", cached.Name).Msg("Removed from config")
	}
}

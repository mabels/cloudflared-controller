package controller

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TunnelConfigMap struct {
	Cm corev1.ConfigMap
}

type TunnelConfigMaps struct {
	CmsLock sync.Mutex
	Cms     map[string]TunnelConfigMap
}

func NewTunnelConfigMaps() *TunnelConfigMaps {
	ret := &TunnelConfigMaps{
		Cms: make(map[string]TunnelConfigMap),
	}
	ret.CmsLock.Lock()
	return ret
}

func (tcm *TunnelConfigMaps) ConfigMaps() []TunnelConfigMap {
	tcm.CmsLock.Lock()
	defer tcm.CmsLock.Unlock()
	ret := make([]TunnelConfigMap, 0, len(tcm.Cms))
	for _, cm := range tcm.Cms {
		ret = append(ret, cm)
	}
	return ret
}

func (tcm *TunnelConfigMaps) WatchConfigMaps() WatchFunc {
	return func(_cfc *CFController, namespace string) (watch.Interface, error) {
		cfc := _cfc.WithComponent("WatchConfigMaps", func(cfc *CFController) {
			my := cfc.Log.With().Str("namespace", namespace).Logger()
			cfc.Log = &my
		})
		log := cfc.Log
		wif, err := cfc.Rest.K8s.CoreV1().ConfigMaps(namespace).Watch(context.Background(), metav1.ListOptions{
			LabelSelector: "app=cloudflared-controller",
		})
		if err != nil {
			log.Error().Err(err).Msg("Failed to watch ConfigMaps")
			return nil, err
		}

		cms, err := cfc.Rest.K8s.CoreV1().ConfigMaps(namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=cloudflared-controller",
		})
		if err != nil {
			log.Error().Err(err).Msg("Failed to list ConfigMaps")
			return nil, err
		}
		{
			// there was set a lock in NewTunnelConfigMaps this should released
			// until the inital load
			defer tcm.CmsLock.Unlock()
			for _, cm := range cms.Items {
				key := fmt.Sprintf("%s/%s", cm.Namespace, cm.Name)
				log.Debug().Str("key", key).Msg("Found ConfigMap")
				tcm.Cms[key] = TunnelConfigMap{Cm: cm}
			}
		}

		go func() {
			log.Info().Msg("Start Watching ConfigMap")
			for {
				ev, more := <-wif.ResultChan()
				if !more {
					break
				}
				cm, ok := ev.Object.(*corev1.ConfigMap)
				if !ok {
					log.Error().Msg("Failed to cast to ConfigMap")
					continue
				}
				if cm.Namespace != namespace {
					log.Error().Msg("ConfigMap not in watched namespace")
					continue
				}
				{
					key := fmt.Sprintf("%s/%s", cm.Namespace, cm.Name)
					tcm.CmsLock.Lock()
					defer tcm.CmsLock.Unlock()

					switch ev.Type {
					case watch.Modified:
						tcm.Cms[key] = TunnelConfigMap{Cm: *cm}
					case watch.Added:
						tcm.Cms[key] = TunnelConfigMap{Cm: *cm}
					case watch.Deleted:
						delete(tcm.Cms, key)
					default:
						log.Error().Str("type", string(ev.Type)).Msg("Unknown event type")
						continue
					}
				}
			}
			log.Info().Msg("Stopped Watching ConfigMap")
		}()
		return wif, nil
	}
}

package types

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	// panic("implement me")
)

type WatcherBindingConfigMap struct {
	item corev1.ConfigMap
}

func (nl WatcherBindingConfigMap) GetUID() types.UID {
	return nl.item.GetUID()
}

func (nl WatcherBindingConfigMap) GetItem() *corev1.ConfigMap {
	return &nl.item
}

type WatcherBindingConfigMapList struct {
	list *corev1.ConfigMapList
}

func (nl *WatcherBindingConfigMapList) GetItems() []WatcherBindingConfigMap {
	ret := make([]WatcherBindingConfigMap, 0, len(nl.list.Items))
	for _, item := range nl.list.Items {
		ret = append(ret, WatcherBindingConfigMap{
			item: item,
		})
	}
	return ret
}

type WatcherBindingConfigMapClient struct {
	Cif v1.ConfigMapInterface
}

func (nl WatcherBindingConfigMapClient) Watch(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
	return nl.Cif.Watch(ctx, options)
}

func (nl WatcherBindingConfigMapClient) List(ctx context.Context, options metav1.ListOptions) (K8SList[*corev1.ConfigMap, WatcherBindingConfigMap], error) {
	list, err := nl.Cif.List(ctx, options)
	if err != nil {
		return nil, err
	}
	ret := WatcherBindingConfigMapList{list: list}
	return &ret, nil
}

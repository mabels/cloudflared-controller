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

type WatcherBindingNamespace struct {
	item corev1.Namespace
}

func (nl WatcherBindingNamespace) GetUID() types.UID {
	return nl.item.GetUID()
}

func (nl WatcherBindingNamespace) GetItem() *corev1.Namespace {
	return &nl.item
}

type WatcherBindingNamespaceList struct {
	list *corev1.NamespaceList
}

func (nl *WatcherBindingNamespaceList) GetItems() []WatcherBindingNamespace {
	ret := make([]WatcherBindingNamespace, 0, len(nl.list.Items))
	for _, item := range nl.list.Items {
		ret = append(ret, WatcherBindingNamespace{
			item: item,
		})
	}
	return ret
}

type WatcherBindingNamespaceClient struct {
	Nif v1.NamespaceInterface
}

func (nl WatcherBindingNamespaceClient) Watch(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
	return nl.Nif.Watch(ctx, options)
}

func (nl WatcherBindingNamespaceClient) List(ctx context.Context, options metav1.ListOptions) (K8SList[*corev1.Namespace, WatcherBindingNamespace], error) {
	list, err := nl.Nif.List(ctx, options)
	if err != nil {
		return nil, err
	}
	ret := WatcherBindingNamespaceList{list: list}
	return &ret, nil
}

package types

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	v1 "k8s.io/client-go/kubernetes/typed/networking/v1"

	netv1 "k8s.io/api/networking/v1"
)

type WatcherBindingIngress struct {
	item netv1.Ingress
}

func (nl WatcherBindingIngress) GetUID() types.UID {
	return nl.item.GetUID()
}

func (nl WatcherBindingIngress) GetItem() *netv1.Ingress {
	return &nl.item
}

type WatcherBindingIngressList struct {
	list *netv1.IngressList
}

func (nl *WatcherBindingIngressList) GetItems() []WatcherBindingIngress {
	ret := make([]WatcherBindingIngress, 0, len(nl.list.Items))
	for _, item := range nl.list.Items {
		ret = append(ret, WatcherBindingIngress{
			item: item,
		})
	}
	return ret
}

type WatcherBindingIngressClient struct {
	Cif v1.IngressInterface
}

func (nl WatcherBindingIngressClient) Watch(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
	return nl.Cif.Watch(ctx, options)
}

func (nl WatcherBindingIngressClient) List(ctx context.Context, options metav1.ListOptions) (K8SList[*netv1.Ingress, WatcherBindingIngress], error) {
	list, err := nl.Cif.List(ctx, options)
	if err != nil {
		return nil, err
	}
	ret := WatcherBindingIngressList{list: list}
	return &ret, nil
}

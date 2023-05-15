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

type WatcherBindingService struct {
	item corev1.Service
}

func (nl WatcherBindingService) GetUID() types.UID {
	return nl.item.GetUID()
}

func (nl WatcherBindingService) GetItem() *corev1.Service {
	return &nl.item
}

type WatcherBindingServiceList struct {
	list *corev1.ServiceList
}

func (nl *WatcherBindingServiceList) GetItems() []WatcherBindingService {
	ret := make([]WatcherBindingService, 0, len(nl.list.Items))
	for _, item := range nl.list.Items {
		ret = append(ret, WatcherBindingService{
			item: item,
		})
	}
	return ret
}

type WatcherBindingServiceClient struct {
	Sif v1.ServiceInterface
}

func (nl WatcherBindingServiceClient) Watch(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
	return nl.Sif.Watch(ctx, options)
}

func (nl WatcherBindingServiceClient) List(ctx context.Context, options metav1.ListOptions) (K8SList[*corev1.Service, WatcherBindingService], error) {
	list, err := nl.Sif.List(ctx, options)
	if err != nil {
		return nil, err
	}
	ret := WatcherBindingServiceList{list: list}
	return &ret, nil
}

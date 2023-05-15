package types

import (
	"context"

	"github.com/rs/zerolog"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type K8SItem[R runtime.Object] interface {
	GetUID() types.UID
	GetItem() R
}

// type K8SItem struct {
// 	metav1.TypeMeta
// 	metav1.ObjectMeta
// 	// GetUID() types.UID
// 	// UID types.UID
// }

type K8SList[R runtime.Object, T K8SItem[R]] interface {
	GetItems() []T
}

// struct {
// 	metav1.TypeMeta `json:",inline"`
// 	// Standard list metadata.
// 	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
// 	// +optional
// 	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

// 	Items []any
// }

type K8SClient[R runtime.Object, L K8SItem[R]] interface {
	Watch(ctx context.Context, options metav1.ListOptions) (watch.Interface, error)
	List(ctx context.Context, options metav1.ListOptions) (K8SList[R, L], error)
}

// type WatcherBinding struct {
// 	ListType
// }

type WatcherConfig[R any, RO runtime.Object, I K8SItem[RO], C K8SClient[RO, I]] struct {
	ListOptions metav1.ListOptions
	Log         *zerolog.Logger
	Context     context.Context
	K8sClient   C
}

type WatchFunc[RO runtime.Object] func(state []RO, ev watch.Event)

type Watcher[RO runtime.Object] interface {
	Start() error
	Stop()
	GetState() []RO
	GetContext() context.Context
	RegisterEvent(WatchFunc[RO]) func()
}

// func (w *Watcher[R, RO, I, L]) Start() error {
// 	panic("implement me")
// }

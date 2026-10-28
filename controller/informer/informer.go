package informer

import (
	"context"
	"fmt"

	"github.com/mabels/cloudflared-controller/controller/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// Informer wraps one shared informer per watched namespace (or one for all
// namespaces) and hands out deep copies only, so handlers can't corrupt the cache.
type Informer[T runtime.Object] struct {
	factories []informers.SharedInformerFactory
	informers []cache.SharedIndexInformer
}

func New[T runtime.Object](cfc types.CFController, pick func(informers.SharedInformerFactory) cache.SharedIndexInformer, opts ...informers.SharedInformerOption) *Informer[T] {
	namespaces := cfc.Cfg().PresetNamespaces
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	}
	inf := &Informer[T]{}
	for _, ns := range namespaces {
		nsOpts := append(append([]informers.SharedInformerOption{}, opts...), informers.WithNamespace(ns))
		f := informers.NewSharedInformerFactoryWithOptions(cfc.Rest().K8s(), 0, nsOpts...)
		inf.factories = append(inf.factories, f)
		inf.informers = append(inf.informers, pick(f))
	}
	return inf
}

// Start runs the informers until ctx is done and waits for the initial sync.
func (inf *Informer[T]) Start(ctx context.Context) error {
	for _, f := range inf.factories {
		f.Start(ctx.Done())
	}
	for _, i := range inf.informers {
		if !cache.WaitForCacheSync(ctx.Done(), i.HasSynced) {
			return fmt.Errorf("informer cache sync failed")
		}
	}
	return nil
}

func (inf *Informer[T]) List() []T {
	out := []T{}
	for _, i := range inf.informers {
		for _, obj := range i.GetStore().List() {
			if t, ok := obj.(T); ok {
				out = append(out, t.DeepCopyObject().(T))
			}
		}
	}
	return out
}

func toObject[T runtime.Object](obj interface{}) (T, bool) {
	if tomb, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tomb.Obj
	}
	t, ok := obj.(T)
	if !ok {
		return t, false
	}
	return t.DeepCopyObject().(T), true
}

// Register calls fn for every change; existing objects are replayed as Added.
// The returned func unregisters fn.
func (inf *Informer[T]) Register(fn func(watch.Event)) (func(), error) {
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if t, ok := toObject[T](obj); ok {
				fn(watch.Event{Type: watch.Added, Object: t})
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			o, okO := oldObj.(metav1.Object)
			n, okN := newObj.(metav1.Object)
			if okO && okN && o.GetResourceVersion() == n.GetResourceVersion() {
				return
			}
			if t, ok := toObject[T](newObj); ok {
				fn(watch.Event{Type: watch.Modified, Object: t})
			}
		},
		DeleteFunc: func(obj interface{}) {
			if t, ok := toObject[T](obj); ok {
				fn(watch.Event{Type: watch.Deleted, Object: t})
			}
		},
	}
	regs := make([]cache.ResourceEventHandlerRegistration, 0, len(inf.informers))
	unregister := func() {
		for idx, reg := range regs {
			_ = inf.informers[idx].RemoveEventHandler(reg)
		}
	}
	for _, i := range inf.informers {
		reg, err := i.AddEventHandler(handler)
		if err != nil {
			unregister()
			return nil, err
		}
		regs = append(regs, reg)
	}
	return unregister, nil
}

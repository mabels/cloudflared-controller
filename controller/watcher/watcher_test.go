package watcher

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func TestWatcher(t *testing.T) {
	config, _ := clientcmd.BuildConfigFromFlags("", os.Getenv("HOME")+"/.kube/config")
	k8s, _ := kubernetes.NewForConfig(config)
	wt := NewWatcher(
		types.WatcherConfig[corev1.Namespace, *corev1.Namespace, types.WatcherBindingNamespace, types.WatcherBindingNamespaceClient]{
			K8sClient: types.WatcherBindingNamespaceClient{
				Nif: k8s.CoreV1().Namespaces(),
			},
		})
	if wt == nil {
		t.Fatal("NewWatcher returned nil")
	}
	if wt.GetContext() == nil {
		t.Fatal("NewWatcher returned nil context")
	}
	for i := 0; i < 3; i++ {
		err := wt.Start()
		if err != nil {
			t.Error("Start returned error:", err)
		}
		states := wt.GetState()
		if len(states) == 0 {
			t.Error("GetState returned non-zero length")
		}
		found := false
		for _, state := range states {
			if state.Name == "default" {
				found = true
			}
		}
		if !found {
			t.Error("GetState returned non-default namespace")
		}
		wt.Stop()
	}
}

func TestWatcherRetry(t *testing.T) {
	log := zerolog.New(os.Stderr).With().Logger()
	config, _ := clientcmd.BuildConfigFromFlags("", os.Getenv("HOME")+"/.kube/config")
	k8s, _ := kubernetes.NewForConfig(config)
	restartWg := sync.WaitGroup{}

	wt := &Watcher[corev1.Namespace, *corev1.Namespace, types.WatcherBindingNamespace, types.WatcherBindingNamespaceClient]{
		watchState:  watchStateStopped,
		restartFunc: func() { restartWg.Done() },
		state:       make(map[string]*corev1.Namespace),
		bindings:    make(map[string]types.WatchFunc[*corev1.Namespace]),
		WatcherConfig: types.WatcherConfig[corev1.Namespace, *corev1.Namespace, types.WatcherBindingNamespace, types.WatcherBindingNamespaceClient]{
			Log:     &log,
			Context: context.Background(),
			K8sClient: types.WatcherBindingNamespaceClient{
				Nif: k8s.CoreV1().Namespaces(),
			},
		},
	}
	for i := 0; i < 3; i++ {
		err := wt.Start()
		if err != nil {
			t.Error("Start returned error:", err)
		}
		states := wt.GetState()
		if len(states) == 0 {
			t.Error("GetState returned non-zero length")
		}
		found := false
		for _, state := range states {
			if state.Name == "default" {
				found = true
			}
		}
		if !found {
			t.Error("GetState returned non-default namespace")
		}
		if wt.restartCount != 0 {
			t.Error("restartCount is not zero")
		}
		for i := 0; i < 10; i++ {
			restartWg.Add(1)
			wt.wif.Stop()
			restartWg.Wait()
			if wt.restartCount != i+1 {
				t.Errorf("restartCount is not one: %d", wt.restartCount)
			}
		}

		wt.Stop()
	}
}

func TestWatcherModify(t *testing.T) {
	config, _ := clientcmd.BuildConfigFromFlags("", os.Getenv("HOME")+"/.kube/config")
	k8s, _ := kubernetes.NewForConfig(config)
	wt := NewWatcher(
		types.WatcherConfig[corev1.Namespace, *corev1.Namespace, types.WatcherBindingNamespace, types.WatcherBindingNamespaceClient]{
			K8sClient: types.WatcherBindingNamespaceClient{
				Nif: k8s.CoreV1().Namespaces(),
			},
		})
	err := wt.Start()
	if err != nil {
		t.Error("Start returned error:", err)
	}

	state := wt.GetState()
	initialLen := len(state)

	name := uuid.NewString()

	// create ns test
	wg := sync.WaitGroup{}
	wg.Add(1)
	skip := false
	unreg := wt.RegisterEvent(func(state []*corev1.Namespace, ev watch.Event) {
		if skip {
			return
		}
		// t.Log("Event:", ev.Type)
		if ev.Type != watch.Added {
			t.Error("Event type is not Added")
		}
		// if ev.Object.(*corev1.Namespace).Name != name {
		// 	t.Error("Event object name is not", name)
		// }
		found := false
		for _, ns := range state {
			if ns.Name == name {
				found = true
				break
			}
		}
		if found {
			wg.Done()
			skip = true
		}
	})
	_, err = k8s.CoreV1().Namespaces().Create(wt.GetContext(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Error("Create returned error:", err)
	}
	wg.Wait()
	unreg()
	state = wt.GetState()
	if len(state) != initialLen+1 {
		t.Error("GetState returned wrong length")
	}
	found := false
	for _, ns := range state {
		if ns.Name == name {
			found = true
			break
		}
	}
	if !found {
		t.Error("Event object not found in state")
	}
	// delete ns test
	wg = sync.WaitGroup{}
	wg.Add(1)
	unreg = wt.RegisterEvent(func(state []*corev1.Namespace, ev watch.Event) {
		if ev.Type == watch.Added {
			return
		}
		// t.Log("Event:", ev.Type)
		if ev.Object.(*corev1.Namespace).Name != name {
			t.Error("Event object name is not", name, ev.Object.(*corev1.Namespace).Name)
		}
		if ev.Type == watch.Modified {
			o := ev.Object.(*corev1.Namespace)
			if o.Status.Phase != corev1.NamespaceTerminating {
				t.Error("Event object status is not Terminating")
			}
			return
		}
		if ev.Type != watch.Deleted {
			t.Error("Event type is not Deleted:", ev.Type)
		}

		found := false
		for _, ns := range state {
			if ns.Name == name {
				found = true
				break
			}
		}
		if found {
			t.Error("Event object not found in state")
		}
		wg.Done()
	})
	err = k8s.CoreV1().Namespaces().Delete(wt.GetContext(), name, metav1.DeleteOptions{})
	if err != nil {
		t.Error("Delete returned error:", err)
	}
	wg.Wait()
	unreg()
	state = wt.GetState()
	if len(state) != initialLen {
		t.Error("GetState returned wrong length")
	}
	found = false
	for _, ns := range state {
		if ns.Name == name {
			found = true
			break
		}
	}
	if found {
		t.Error("Event object not found in state")
	}

	wt.Stop()
}

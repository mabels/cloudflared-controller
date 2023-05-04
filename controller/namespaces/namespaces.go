package namespaces

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

type Namespaces struct {
	_watchEvents    []chan watch.Event
	_namespaces     map[string]string
	namespacesMutex sync.Mutex
}

func New() *Namespaces {
	return &Namespaces{
		_namespaces: make(map[string]string),
	}
}

func (c *Namespaces) AddWatch(wch chan watch.Event) {
	c.namespacesMutex.Lock()
	defer c.namespacesMutex.Unlock()
	c._watchEvents = append(c._watchEvents, wch)
	for _, ns := range c._namespaces {
		wch <- watch.Event{
			Type: watch.Added,
			Object: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ns,
				},
			},
		}
	}
}

func (c *Namespaces) AddNamespace(we watch.Event) {
	c.namespacesMutex.Lock()
	defer c.namespacesMutex.Unlock()
	n := we.Object.(*corev1.Namespace).Name
	_, found := c._namespaces[n]
	if !found {
		c._namespaces[n] = n
	}
	for _, ch := range c._watchEvents {
		ch <- we
	}
}

func (c *Namespaces) DelNamespace(we watch.Event) {
	c.namespacesMutex.Lock()
	defer c.namespacesMutex.Unlock()
	n := we.Object.(*corev1.Namespace).Name
	_, found := c._namespaces[n]
	if found {
		for _, ch := range c._watchEvents {
			ch <- we
			close(ch)
		}
		delete(c._namespaces, n)
	}
}

func (c *Namespaces) Namespaces() []string {
	c.namespacesMutex.Lock()
	defer c.namespacesMutex.Unlock()
	out := make([]string, len(c._namespaces))
	for n := range c._namespaces {
		out = append(out, n)
	}
	return out
}

package cloudflare

import (
	"context"
	"sync"
	"time"

	"github.com/mabels/cloudflared-controller/controller/namespaces"
	"github.com/mabels/cloudflared-controller/controller/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	corev1 "k8s.io/api/core/v1"
)

type CloudflareV1Beta1Interface interface {
	AccessGroups(namespace string) AccessGroupInterface
	CFDTunnels(namespace string) CFDTunnelInterface
	CFDTunnelConfigs(namespace string) CFDTunnelConfigInterface
}

type CloudflareV1Beta1Client struct {
	restClient rest.Interface
	ctx        context.Context
}

const GroupName = "cloudflare.adviser.com"
const GroupVersion = "v1alpha1"

var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: GroupVersion}

var (
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme   = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&AccessGroup{},
		&AccessGroupList{},
		&CFDTunnel{},
		&CFDTunnelList{},
		&CFDTunnelConfig{},
		&CFDTunnelConfigList{},
	)

	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}

func NewForConfig(ctx context.Context, c *rest.Config) (CloudflareV1Beta1Interface, error) {
	config := *c
	config.ContentConfig.GroupVersion = &schema.GroupVersion{Group: GroupName, Version: GroupVersion}
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.UserAgent = rest.DefaultKubernetesUserAgent()

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}

	return &CloudflareV1Beta1Client{restClient: client, ctx: ctx}, nil
}

func (c *CloudflareV1Beta1Client) AccessGroups(namespace string) AccessGroupInterface {
	return &accessGroupClient{
		restClient: c.restClient,
		ns:         namespace,
		ctx:        c.ctx,
	}
}

func (c *CloudflareV1Beta1Client) CFDTunnels(namespace string) CFDTunnelInterface {
	return &cfdTunnelClient{
		restClient: c.restClient,
		ns:         namespace,
		ctx:        c.ctx,
	}
}

func (c *CloudflareV1Beta1Client) CFDTunnelConfigs(namespace string) CFDTunnelConfigInterface {
	return &cfdTunnelConfigClient{
		restClient: c.restClient,
		ns:         namespace,
		ctx:        c.ctx,
	}
}

func WatchCFDTunnels(clientSet CloudflareV1Beta1Interface, ns string, handler cache.ResourceEventHandler) cache.Store {
	projectStore, projectController := cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(lo metav1.ListOptions) (result runtime.Object, err error) {
				return clientSet.CFDTunnels(ns).List(lo)
			},
			WatchFunc: func(lo metav1.ListOptions) (watch.Interface, error) {
				return clientSet.CFDTunnels(ns).Watch(lo)
			},
		},
		&CFDTunnel{},
		1*time.Minute,
		handler,
	)

	projectController.Run(wait.NeverStop)
	return projectStore
}

func WatchCFDTunnelConfigs(clientSet CloudflareV1Beta1Interface, ns string, handler cache.ResourceEventHandler) cache.Store {
	projectStore, projectController := cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(lo metav1.ListOptions) (result runtime.Object, err error) {
				return clientSet.CFDTunnelConfigs(ns).List(lo)
			},
			WatchFunc: func(lo metav1.ListOptions) (watch.Interface, error) {
				return clientSet.CFDTunnelConfigs(ns).Watch(lo)
			},
		},
		&CFDTunnelConfig{},
		1*time.Minute,
		handler,
	)

	projectController.Run(wait.NeverStop)
	return projectStore
}

func WatchAccessGroups(clientSet CloudflareV1Beta1Interface, ns string, handler cache.ResourceEventHandler) cache.Store {
	projectStore, projectController := cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(lo metav1.ListOptions) (result runtime.Object, err error) {
				return clientSet.AccessGroups(ns).List(lo)
			},
			WatchFunc: func(lo metav1.ListOptions) (watch.Interface, error) {
				return clientSet.AccessGroups(ns).Watch(lo)
			},
		},
		&AccessGroup{},
		1*time.Minute,
		handler,
	)

	projectController.Run(wait.NeverStop)
	return projectStore
}

type watcherBindingCloudflare struct {
}

type cloudflareWatchers struct {
	lock  sync.Mutex
	items map[string]watcherBindingCloudflare
}

func Start(cfc types.CFController) func() {
	// svcs := &services{
	// 	items: make(map[string]watcherBindingServices),
	// }
	unreg := cfc.K8sData().Namespaces.RegisterEvent(func(_ []*corev1.Namespace, ev watch.Event) {
		ns, ok := ev.Object.(*corev1.Namespace)
		if !ok {
			cfc.Log().Error().Msg("Failed to cast to Namespace")
			return
		}
		if namespaces.SkipNamespace(cfc, ns.Name) {
			return
		}
		cloudflareWatchers.lock.Lock()
		defer cloudflareWatchers.lock.Unlock()
		switch ev.Type {
		case watch.Added:
			if _, ok := cloudflareWatchers.items[ns.Name]; !ok {
				wif, err := startServiceWatcher(cfc, ns.Name)
				if err != nil {
					cfc.Log().Error().Err(err).Msg("Failed to start ingress watcher")
					return
				}
				svcs.items[ns.Name] = wif
			}
		case watch.Modified:
		case watch.Deleted:
			my, ok := svcs.items[ns.Name]
			if !ok {
				delete(svcs.items, ns.Name)
				my.unregisterEvent()
				my.watcher.Stop()
			}
		default:
			cfc.Log().Error().Msgf("Unknown event type: %s", ev.Type)
		}
	})
	cfc.Log().Debug().Str("component", "svc").Msg("Started watcher")
	return func() {
		svcs.lock.Lock()
		defer svcs.lock.Unlock()
		for _, v := range svcs.items {
			v.unregisterEvent()
			v.watcher.Stop()
		}
		unreg()
	}
}

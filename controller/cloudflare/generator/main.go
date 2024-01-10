package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"reflect"

	"github.com/mabels/cloudflared-controller/controller/cloudflare"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var kubeconfig string

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to Kubernetes config file")
	flag.Parse()
}

type namedEventHandlerFuncs struct {
	name string
}

// OnAdd implements cache.ResourceEventHandler.
func (c namedEventHandlerFuncs) OnAdd(obj interface{}, isInInitialList bool) {
	fmt.Printf("OnAdd: %s:%+v\n", c.name, obj)
}

// OnDelete implements cache.ResourceEventHandler.
func (c namedEventHandlerFuncs) OnDelete(obj interface{}) {
	fmt.Printf("OnDelete: %s:%+v\n", c.name, obj)
}

// OnUpdate implements cache.ResourceEventHandler.
func (c namedEventHandlerFuncs) OnUpdate(oldObj interface{}, newObj interface{}) {
	if reflect.DeepEqual(oldObj, newObj) {
		return
	}
	fmt.Printf("OnUpdate: %s:%+v=>%+v\n", c.name, oldObj, newObj)
}

func main() {
	var config *rest.Config
	var err error

	if kubeconfig == "" {
		log.Printf("using in-cluster configuration")
		config, err = rest.InClusterConfig()
	} else {
		log.Printf("using configuration from '%s'", kubeconfig)
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	if err != nil {
		panic(err)
	}

	cloudflare.AddToScheme(scheme.Scheme)

	cif, err := cloudflare.NewForConfig(context.Background(), config)

	if err != nil {
		panic(err)
	}

	// ags, err := cif.AccessGroups("product-pipeline").List(metav1.ListOptions{})
	// if err != nil {
	// 	panic(err)
	// }

	// fmt.Printf("AccessGroup found: %+v\n", ags)

	go cloudflare.WatchAccessGroups(cif, "product-pipeline", namedEventHandlerFuncs{name: "AccessGroup"})
	go cloudflare.WatchCFDTunnels(cif, "product-pipeline", namedEventHandlerFuncs{name: "CFDTunnel"})
	cloudflare.WatchCFDTunnelConfigs(cif, "product-pipeline", namedEventHandlerFuncs{name: "CFDTunnelConfig"})
	// time.Sleep(30 * time.Second)
	// for _, item := range store.List() {
	// 	fmt.Printf("%+v\n", item)
	// }

	// cloudflare.AddToScheme(scheme.Scheme)

	// crdConfig := *config
	// crdConfig.ContentConfig.GroupVersion = &schema.GroupVersion{Group: cloudflare.GroupName, Version: cloudflare.GroupVersion}
	// crdConfig.APIPath = "/apis"
	// crdConfig.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)
	// crdConfig.UserAgent = rest.DefaultKubernetesUserAgent()

	// access, err := rest.UnversionedRESTClientFor(&crdConfig)
	// if err != nil {
	// 	panic(err)
	// }
	// var ag cloudflare.AccessGroup
	// access.Get().Namespace("product-pipeline").Resource("accessgroups").Do(context.Background()).Into(&ag)
	// fmt.Printf("%+v\n", ag)
}

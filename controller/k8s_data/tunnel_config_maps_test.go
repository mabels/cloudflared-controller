package k8s_data_test

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/k8s_data"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
)

func newController(t *testing.T) (types.CFController, *fake.Clientset) {
	log := zerolog.New(os.Stderr).Level(zerolog.WarnLevel)
	cfc := controller.NewCFController(&log)
	cfc.SetCfg(&types.CFControllerConfig{
		ClusterName:            "k8s",
		Version:                "test",
		ConfigMapLabelSelector: "app=cloudflared-controller",
		CloudFlare:             types.CFControllerCloudflareConfig{TunnelConfigMapNamespace: "default"},
	})
	k8s := fake.NewClientset()
	cfc.Rest().SetK8s(k8s)
	t.Cleanup(func() { cfc.Shutdown() })
	return cfc, k8s
}

func getCM(t *testing.T, cfc types.CFController, name string) *corev1.ConfigMap {
	cm, err := cfc.Rest().K8s().CoreV1().ConfigMaps("default").Get(cfc.Context(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return cm
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func svcMeta(name string, tunnel string) *metav1.ObjectMeta {
	return &metav1.ObjectMeta{
		Namespace:   "apps",
		Name:        name,
		Annotations: map[string]string{"cloudflare.com/tunnel-name": tunnel},
	}
}

func rule(host string) []types.CFConfigIngress {
	return []types.CFConfigIngress{{Hostname: host, Service: "http://x.apps:80"}}
}

func TestUpsertMergeMoveRemove(t *testing.T) {
	cfc, _ := newController(t)
	tcm, err := k8s_data.StartTunnelConfigMaps(cfc)
	if err != nil {
		t.Fatal(err)
	}
	cfc.K8sData().TunnelConfigMaps = tcm
	t1 := &types.CFTunnelParameter{Namespace: "default", Name: "t1.example.com"}
	t2 := &types.CFTunnelParameter{Namespace: "default", Name: "t2.example.com"}

	if err := tcm.UpsertConfigMap(cfc, t1, "service", svcMeta("a", t1.Name), rule("a.example.com")); err != nil {
		t.Fatal(err)
	}
	if err := tcm.UpsertConfigMap(cfc, t1, "service", svcMeta("b-x", t1.Name), rule("b.example.com")); err != nil {
		t.Fatal(err)
	}
	cm := getCM(t, cfc, t1.K8SConfigMapName().Name)
	// '-' is replaced in the key, existing configmaps depend on it
	if _, ok := cm.Data["service_apps_b_x"]; !ok || len(cm.Data) != 2 {
		t.Fatalf("unexpected data %v", cm.Data)
	}
	if cm.Labels["app"] != "cloudflared-controller" {
		t.Fatalf("unexpected labels %v", cm.Labels)
	}
	eventually(t, "cache", func() bool { return len(tcm.Get()) == 1 })

	// b moves to tunnel t2
	if err := tcm.UpsertConfigMap(cfc, t2, "service", svcMeta("b-x", t2.Name), rule("b.example.com")); err != nil {
		t.Fatal(err)
	}
	if cm := getCM(t, cfc, t1.K8SConfigMapName().Name); len(cm.Data) != 1 {
		t.Fatalf("b not removed from t1: %v", cm.Data)
	}
	if cm := getCM(t, cfc, t2.K8SConfigMapName().Name); len(cm.Data) != 1 {
		t.Fatalf("b not in t2: %v", cm.Data)
	}

	eventually(t, "cache", func() bool { return len(tcm.Get()) == 2 })
	tcm.RemoveConfigMap(cfc, "service", svcMeta("a", t1.Name))
	if cm := getCM(t, cfc, t1.K8SConfigMapName().Name); len(cm.Data) != 0 {
		t.Fatalf("a not removed: %v", cm.Data)
	}
}

// An ingress with class "cloudflared" and no tunnel-name annotation has its
// name inferred into the tparam. UpsertConfigMap must stamp that name onto the
// configmap, otherwise the configmap handler (which reads only the configmap's
// own annotations, no hints) cannot recover it and never creates the tunnel.
func TestUpsertPersistsInferredTunnelName(t *testing.T) {
	cfc, _ := newController(t)
	tcm, err := k8s_data.StartTunnelConfigMaps(cfc)
	if err != nil {
		t.Fatal(err)
	}
	cfc.K8sData().TunnelConfigMaps = tcm

	tp := &types.CFTunnelParameter{Namespace: "apps", Name: "example.com"}
	// meta without any tunnel-name annotation, as for an inferred ingress
	meta := &metav1.ObjectMeta{Namespace: "apps", Name: "web"}
	if err := tcm.UpsertConfigMap(cfc, tp, "ingress", meta, rule("a.example.com")); err != nil {
		t.Fatal(err)
	}

	cmName := tp.K8SConfigMapName()
	cm, err := cfc.Rest().K8s().CoreV1().ConfigMaps(cmName.Namespace).Get(cfc.Context(), cmName.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := cm.Annotations["cloudflare.com/tunnel-name"]; got != "apps/example.com" {
		t.Fatalf("tunnel-name annotation = %q, want %q", got, "apps/example.com")
	}
	// the configmap handler must recover the param from the configmap alone
	recovered, err := k8s_data.NewUniqueTunnelParams().GetConfigMapTunnelParam(cfc, &cm.ObjectMeta)
	if err != nil {
		t.Fatalf("handler could not recover tunnel param: %v", err)
	}
	if recovered.Namespace != "apps" || recovered.Name != "example.com" {
		t.Fatalf("recovered %s/%s, want apps/example.com", recovered.Namespace, recovered.Name)
	}
}

func TestUpsertWithoutChangeDoesNotWrite(t *testing.T) {
	cfc, k8s := newController(t)
	tcm, err := k8s_data.StartTunnelConfigMaps(cfc)
	if err != nil {
		t.Fatal(err)
	}
	t1 := &types.CFTunnelParameter{Namespace: "default", Name: "t1.example.com"}
	for i := 0; i < 3; i++ {
		if err := tcm.UpsertConfigMap(cfc, t1, "service", svcMeta("a", t1.Name), rule("a.example.com")); err != nil {
			t.Fatal(err)
		}
	}
	writes := 0
	for _, a := range k8s.Actions() {
		if a.GetResource().Resource == "configmaps" && (a.GetVerb() == "create" || a.GetVerb() == "update") {
			writes++
		}
	}
	if writes != 1 {
		t.Fatalf("expected 1 write, got %d", writes)
	}
}

// The handlers get copies; mutating them while other goroutines read and
// write the cache crashed production with "concurrent map iteration and map write".
func TestConcurrentHandlersDoNotShareMaps(t *testing.T) {
	cfc, _ := newController(t)
	tcm, err := k8s_data.StartTunnelConfigMaps(cfc)
	if err != nil {
		t.Fatal(err)
	}
	t1 := &types.CFTunnelParameter{Namespace: "default", Name: "t1.example.com"}
	var events sync.WaitGroup
	unreg := tcm.Register(func(cms []*corev1.ConfigMap, ev watch.Event) {
		cm := ev.Object.(*corev1.ConfigMap)
		for k := range cm.Data {
			delete(cm.Data, k)
		}
		for _, c := range cms {
			c.Data["scribble"] = "x"
		}
	})
	defer unreg()
	for i := 0; i < 8; i++ {
		events.Add(1)
		go func(i int) {
			defer events.Done()
			for j := 0; j < 10; j++ {
				name := fmt.Sprintf("s%d", i)
				if err := tcm.UpsertConfigMap(cfc, t1, "service", svcMeta(name, t1.Name), rule(name+".example.com")); err != nil {
					t.Error(err)
				}
				for _, cm := range tcm.Get() {
					delete(cm.Data, "service_apps_"+name)
				}
				tcm.RemoveConfigMap(cfc, "ingress", svcMeta(name, t1.Name))
			}
		}(i)
	}
	events.Wait()
	eventually(t, "all keys", func() bool {
		cms := tcm.Get()
		return len(cms) == 1 && len(cms[0].Data) == 8
	})
	if _, found := tcm.Get()[0].Data["scribble"]; found {
		t.Fatal("handler mutation leaked into the cache")
	}
}

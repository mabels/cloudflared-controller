package cloudflared

import (
	"context"
	"os"
	"testing"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/shared"
	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

type mockCF struct {
	tunnels map[string]*shared.CloudflareTunnel
	routes  map[string]string
	deleted []string
}

func (m *mockCF) FindTunnel(_ context.Context, name string) (*shared.CloudflareTunnel, error) {
	return m.tunnels[name], nil
}
func (m *mockCF) CreateTunnel(_ context.Context, name string, _ []byte) (*shared.CloudflareTunnel, error) {
	t := &shared.CloudflareTunnel{ID: tunnelID, Name: name}
	m.tunnels[name] = t
	return t, nil
}
func (m *mockCF) DeleteTunnel(_ context.Context, id string) error {
	m.deleted = append(m.deleted, id)
	for k, t := range m.tunnels {
		if t.ID == id {
			delete(m.tunnels, k)
		}
	}
	return nil
}
func (m *mockCF) RouteDNS(_ context.Context, id string, host string) error {
	m.routes[host] = id
	return nil
}
func (m *mockCF) API() *cloudflare.Client { return nil }
func (m *mockCF) AccountID() string       { return "acc" }

type mockRest struct {
	cf  *mockCF
	k8s kubernetes.Interface
}

func (m *mockRest) CF() types.CFClient            { return m.cf }
func (m *mockRest) K8s() kubernetes.Interface     { return m.k8s }
func (m *mockRest) SetK8s(k kubernetes.Interface) { m.k8s = k }

type mockController struct {
	types.CFController
	rest *mockRest
}

func (m *mockController) Rest() types.RestClients { return m.rest }

func TestValidateCreatesTunnelSecretAndRoutes(t *testing.T) {
	log := zerolog.New(os.Stderr).Level(zerolog.WarnLevel)
	inner := controller.NewCFController(&log)
	inner.SetCfg(&types.CFControllerConfig{
		ClusterName:            "k8s",
		Version:                "test",
		ConfigMapLabelSelector: "app=cloudflared-controller",
		CloudFlare:             types.CFControllerCloudflareConfig{AccountId: "acc", TunnelConfigMapNamespace: "default"},
	})
	// a stale secret from an earlier tunnel must be replaced, not block the create
	k8s := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cfd-tunnel-key.t1-example-com"},
		Data:       map[string][]byte{"credentials.json": []byte(`{}`)},
	})
	cf := &mockCF{tunnels: map[string]*shared.CloudflareTunnel{}, routes: map[string]string{}}
	cfc := &mockController{CFController: inner, rest: &mockRest{cf: cf, k8s: k8s}}

	tp := &types.CFTunnelParameter{Namespace: "default", Name: "t1.example.com"}
	cm := tunnelCM(map[string]string{"service_apps_a": "- hostname: a.example.com\n  service: http://a:80\n"})
	delete(cm.Annotations, "cloudflare.com/tunnel-id")
	if _, err := k8s.CoreV1().ConfigMaps("default").Create(context.Background(), cm, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := validateCFTunnel(cfc, tp, cm); err != nil {
		t.Fatal(err)
	}
	if cf.tunnels["k8s/default/t1.example.com"] == nil {
		t.Fatalf("tunnel not created: %v", cf.tunnels)
	}
	if cf.routes["a.example.com"] != tunnelID {
		t.Fatalf("dns not routed: %v", cf.routes)
	}
	if _, found := cm.Annotations["cloudflare.com/tunnel-id"]; found {
		t.Fatal("event object was mutated")
	}
	updated, _ := k8s.CoreV1().ConfigMaps("default").Get(context.Background(), cm.Name, metav1.GetOptions{})
	if updated.Annotations["cloudflare.com/tunnel-id"] != tunnelID {
		t.Fatalf("configmap not annotated: %v", updated.Annotations)
	}

	// second run finds the tunnel and the now matching secret
	if err := validateCFTunnel(cfc, tp, updated); err != nil {
		t.Fatal(err)
	}
	if len(cf.deleted) != 0 {
		t.Fatalf("unexpected deletes %v", cf.deleted)
	}

	deleteCFTunnel(cfc, tp)
	if len(cf.deleted) != 1 {
		t.Fatalf("tunnel not deleted %v", cf.deleted)
	}
	if _, err := k8s.CoreV1().Secrets("default").Get(context.Background(), "cfd-tunnel-key.t1-example-com", metav1.GetOptions{}); err == nil {
		t.Fatal("secret not deleted")
	}
}

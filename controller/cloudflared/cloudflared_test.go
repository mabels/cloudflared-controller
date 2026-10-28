package cloudflared

import (
	"encoding/json"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const tunnelID = "de0247ce-563a-47aa-a27c-b978b3168c87"

// fakeCloudflared writes a script which logs each start to starts and then
// either exits (crash) or keeps running.
func fakeCloudflared(t *testing.T, crash bool) (bin string, starts string) {
	dir := t.TempDir()
	starts = path.Join(dir, "starts")
	bin = path.Join(dir, "cloudflared")
	tail := "exec sleep 30"
	if crash {
		tail = "exit 1"
	}
	script := "#!/bin/sh\necho \"$4\" >> " + starts + "\n" + tail + "\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return bin, starts
}

func countStarts(starts string) int {
	b, err := os.ReadFile(starts)
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "\n")
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func newTestController(t *testing.T, bin string) types.CFController {
	log := zerolog.New(os.Stderr).Level(zerolog.WarnLevel)
	cfc := controller.NewCFController(&log)
	cfc.SetCfg(&types.CFControllerConfig{
		CloudFlaredFname:   bin,
		RunningInstanceDir: t.TempDir(),
		RestartDelay:       20 * time.Millisecond,
		CloudFlare: types.CFControllerCloudflareConfig{
			AccountId:                "acc",
			TunnelConfigMapNamespace: "default",
		},
	})
	creds, _ := json.Marshal(types.CFTunnelSecret{AccountTag: "acc", TunnelSecret: "c2VjcmV0", TunnelID: uuid.MustParse(tunnelID)})
	cfc.Rest().SetK8s(fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cfd-tunnel-key.t1-example-com"},
		Data:       map[string][]byte{"credentials.json": creds},
	}))
	t.Cleanup(func() { cfc.Shutdown() })
	return cfc
}

func tunnelCM(data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "cfd-tunnel-cfg.t1-example-com",
			Annotations: map[string]string{
				"cloudflare.com/tunnel-id":         tunnelID,
				"cloudflare.com/tunnel-name":       "t1.example.com",
				"cloudflare.com/tunnel-k8s-secret": "default/cfd-tunnel-key.t1-example-com",
			},
		},
		Data: data,
	}
}

func TestRestartsCrashedCloudflared(t *testing.T) {
	bin, starts := fakeCloudflared(t, true)
	cfc := newTestController(t, bin)
	tr := NewTunnelRunner()
	cm := tunnelCM(map[string]string{"service_a": "- hostname: a.example.com\n  service: http://a:80\n"})
	tr.Start(cfc, cm)
	eventually(t, "restarts", func() bool { return countStarts(starts) >= 3 })

	tr.Stop(cfc, cm)
	time.Sleep(50 * time.Millisecond)
	n := countStarts(starts)
	time.Sleep(100 * time.Millisecond)
	if countStarts(starts) != n {
		t.Fatal("restarted after stop")
	}
	entries, _ := os.ReadDir(cfc.Cfg().RunningInstanceDir)
	if len(entries) != 0 {
		t.Fatalf("runtime dir not cleaned: %v", entries)
	}
}

func TestStartOnlyOnChange(t *testing.T) {
	bin, starts := fakeCloudflared(t, false)
	cfc := newTestController(t, bin)
	tr := NewTunnelRunner()
	cm := tunnelCM(map[string]string{
		"service_b": "- hostname: b.example.com\n  service: http://b:80\n",
		"service_a": "- hostname: a.example.com\n  service: http://a:80\n",
	})
	tr.Start(cfc, cm)
	tr.Start(cfc, cm.DeepCopy())
	eventually(t, "first start", func() bool { return countStarts(starts) == 1 })

	ri := tr.getTunnel(cm).ri
	cfg, err := os.ReadFile(ri.configfname)
	if err != nil {
		t.Fatal(err)
	}
	a := strings.Index(string(cfg), "a.example.com")
	b := strings.Index(string(cfg), "b.example.com")
	if a < 0 || b < a || !strings.Contains(string(cfg), "http_status:404") {
		t.Fatalf("unexpected config:\n%s", cfg)
	}

	changed := cm.DeepCopy()
	changed.Data["service_c"] = "- hostname: c.example.com\n  service: http://c:80\n"
	tr.Start(cfc, changed)
	eventually(t, "restart on change", func() bool { return countStarts(starts) == 2 })
	if !ri.isStopped() {
		t.Fatal("old instance still running")
	}
	time.Sleep(100 * time.Millisecond)
	if countStarts(starts) != 2 {
		t.Fatal("unexpected restarts")
	}
}

func TestWaitsForPreparedTunnel(t *testing.T) {
	bin, starts := fakeCloudflared(t, false)
	cfc := newTestController(t, bin)
	tr := NewTunnelRunner()
	cm := tunnelCM(map[string]string{"service_a": "- hostname: a.example.com\n  service: http://a:80\n"})
	delete(cm.Annotations, "cloudflare.com/tunnel-id")
	tr.Start(cfc, cm)
	time.Sleep(50 * time.Millisecond)
	if countStarts(starts) != 0 {
		t.Fatal("started without tunnel id")
	}
	tr.Start(cfc, tunnelCM(cm.Data))
	eventually(t, "start", func() bool { return countStarts(starts) == 1 })
}

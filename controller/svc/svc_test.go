package svc

import (
	"context"
	"os"
	"testing"

	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
)

type mockUpsertCall struct {
	cfc    types.CFController
	tparam *types.CFTunnelParameter
	kind   string
	meta   *metav1.ObjectMeta
	cfcis  []types.CFConfigIngress
}
type mockTunnelConfigMaps struct {
	upsertCalls []mockUpsertCall
}

func (*mockTunnelConfigMaps) Register(func([]*corev1.ConfigMap, watch.Event)) func() {
	panic("implement me")
}
func (*mockTunnelConfigMaps) Get() []*corev1.ConfigMap {
	panic("implement me")

}
func (p *mockTunnelConfigMaps) UpsertConfigMap(cfc types.CFController, tparam *types.CFTunnelParameter, kind string, meta *metav1.ObjectMeta, cfcis []types.CFConfigIngress) error {
	p.upsertCalls = append(p.upsertCalls, mockUpsertCall{
		cfc:    cfc,
		tparam: tparam,
		kind:   kind,
		meta:   meta,
		cfcis:  cfcis,
	})
	return nil
}
func (*mockTunnelConfigMaps) RemoveConfigMap(cfc types.CFController, kind string, meta *metav1.ObjectMeta) {
	panic("implement me")

}

type mockController struct {
	log              *zerolog.Logger
	cfg              *types.CFControllerConfig
	tunnelConfigMaps mockTunnelConfigMaps
}

func (p *mockController) WithComponent(component string, fns ...func(types.CFController)) types.CFController {
	return p
}
func (*mockController) RegisterShutdown(sfn func()) func() {
	panic("implement me")
}
func (*mockController) Shutdown() error {
	panic("implement me")
}
func (p *mockController) Log() *zerolog.Logger {
	return p.log
}
func (p *mockController) SetLog(log *zerolog.Logger) {
	p.log = log
}
func (p mockController) Cfg() *types.CFControllerConfig {
	return p.cfg
}
func (*mockController) SetCfg(*types.CFControllerConfig) {
	panic("implement me")
}
func (*mockController) Rest() types.RestClients {
	panic("implement me")
}
func (p *mockController) K8sData() *types.K8sData {
	return &types.K8sData{
		TunnelConfigMaps: &p.tunnelConfigMaps,
	}
}
func (mockController) Context() context.Context {
	return context.Background()
}
func (mockController) CancelFunc() context.CancelFunc {
	panic("implement me")
}

// shutdownFns   map[string]func()

func TestMappingUpdateConfigMap(t *testing.T) {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cf := &mockController{
		log: &_log,
		cfg: &types.CFControllerConfig{
			CloudFlare: types.CFControllerCloudflareConfig{
				TunnelConfigMapNamespace: "what",
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "what-tech",
			Namespace: "what",
			Annotations: map[string]string{
				"cloudflare.com/tunnel-external-name": "cft.what.tech",
				"cloudflare.com/tunnel-name":          "what.tech",
				"cloudflare.com/tunnel-mapping":       "https/https-notlsverify/,http/http/doof",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					NodePort:   30731,
					Port:       1480,
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(8080),
				},
				{
					Name:       "https",
					NodePort:   30831,
					Port:       1443,
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(8443),
				},
			},
		},
	}
	err := updateConfigMap(cf, svc)
	assert.NoError(t, err)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 1)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, 2)
	assert.Equal(t, "/", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Path)
	assert.Equal(t, "https://what-tech.what:1443", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Service)

	assert.Equal(t, "/doof", cf.tunnelConfigMaps.upsertCalls[0].cfcis[1].Path)
	assert.Equal(t, "http://what-tech.what:1480", cf.tunnelConfigMaps.upsertCalls[0].cfcis[1].Service)

}

func TestSimpleNameHttpUpdateConfigMap(t *testing.T) {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cf := &mockController{
		log: &_log,
		cfg: &types.CFControllerConfig{
			CloudFlare: types.CFControllerCloudflareConfig{
				TunnelConfigMapNamespace: "what",
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "what-tech",
			Namespace: "what",
			Annotations: map[string]string{
				"cloudflare.com/tunnel-external-name": "cft.what.tech",
				"cloudflare.com/tunnel-name":          "what.tech",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					NodePort:   30731,
					Port:       80,
					Protocol:   "TCP",
					TargetPort: intstr.FromString("http"),
				},
				{
					Name:       "httpx",
					NodePort:   30831,
					Port:       443,
					Protocol:   "TCP",
					TargetPort: intstr.FromString("https"),
				},
			},
		},
	}
	err := updateConfigMap(cf, svc)
	assert.NoError(t, err)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 1)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, 1)
	assert.Equal(t, "/", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Path)
	assert.Equal(t, "http://what-tech.what:80", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Service)
}

func TestSimpleNameHttpsUpdateConfigMap(t *testing.T) {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cf := &mockController{
		log: &_log,
		cfg: &types.CFControllerConfig{
			CloudFlare: types.CFControllerCloudflareConfig{
				TunnelConfigMapNamespace: "what",
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "what-tech",
			Namespace: "what",
			Annotations: map[string]string{
				"cloudflare.com/tunnel-external-name": "cft.what.tech",
				"cloudflare.com/tunnel-name":          "what.tech",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					NodePort:   30731,
					Port:       80,
					Protocol:   "TCP",
					TargetPort: intstr.FromString("http"),
				},
				{
					Name:       "https",
					NodePort:   30831,
					Port:       443,
					Protocol:   "TCP",
					TargetPort: intstr.FromString("https"),
				},
			},
		},
	}
	err := updateConfigMap(cf, svc)
	assert.NoError(t, err)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 1)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, 1)
	assert.Equal(t, "/", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Path)
	assert.Equal(t, "https://what-tech.what:443", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Service)
}

func TestTargetPortHttpUpdateConfigMap(t *testing.T) {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cf := &mockController{
		log: &_log,
		cfg: &types.CFControllerConfig{
			CloudFlare: types.CFControllerCloudflareConfig{
				TunnelConfigMapNamespace: "what",
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "what-tech",
			Namespace: "what",
			Annotations: map[string]string{
				"cloudflare.com/tunnel-external-name": "cft.what.tech",
				"cloudflare.com/tunnel-name":          "what.tech",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "xhttp",
					NodePort:   30731,
					Port:       80,
					Protocol:   "TCP",
					TargetPort: intstr.FromString("http"),
				},
				{
					Name:       "xhttpx",
					NodePort:   30831,
					Port:       443,
					Protocol:   "TCP",
					TargetPort: intstr.FromString("xhttps"),
				},
			},
		},
	}
	err := updateConfigMap(cf, svc)
	assert.NoError(t, err)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 1)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, 1)
	assert.Equal(t, "/", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Path)
	assert.Equal(t, "http://what-tech.what:80", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Service)
}

func TestTargetPortHttpsUpdateConfigMap(t *testing.T) {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cf := &mockController{
		log: &_log,
		cfg: &types.CFControllerConfig{
			CloudFlare: types.CFControllerCloudflareConfig{
				TunnelConfigMapNamespace: "what",
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "what-tech",
			Namespace: "what",
			Annotations: map[string]string{
				"cloudflare.com/tunnel-external-name": "cft.what.tech",
				"cloudflare.com/tunnel-name":          "what.tech",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "xhttp",
					NodePort:   30731,
					Port:       80,
					Protocol:   "TCP",
					TargetPort: intstr.FromString("http"),
				},
				{
					Name:       "xhttps",
					NodePort:   30831,
					Port:       443,
					Protocol:   "TCP",
					TargetPort: intstr.FromString("https"),
				},
			},
		},
	}
	err := updateConfigMap(cf, svc)
	assert.NoError(t, err)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 1)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, 1)
	assert.Equal(t, "/", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Path)
	assert.Equal(t, "https://what-tech.what:443", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Service)
}

func TestTargetPort80UpdateConfigMap(t *testing.T) {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cf := &mockController{
		log: &_log,
		cfg: &types.CFControllerConfig{
			CloudFlare: types.CFControllerCloudflareConfig{
				TunnelConfigMapNamespace: "what",
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "what-tech",
			Namespace: "what",
			Annotations: map[string]string{
				"cloudflare.com/tunnel-external-name": "cft.what.tech",
				"cloudflare.com/tunnel-name":          "what.tech",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "xhttp",
					NodePort:   30731,
					Port:       80,
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(80),
				},
				{
					Name:       "xhttpx",
					NodePort:   30831,
					Port:       443,
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(90),
				},
			},
		},
	}
	err := updateConfigMap(cf, svc)
	assert.NoError(t, err)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 1)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, 1)
	assert.Equal(t, "/", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Path)
	assert.Equal(t, "http://what-tech.what:80", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Service)
}

func TestTargetPort443UpdateConfigMap(t *testing.T) {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cf := &mockController{
		log: &_log,
		cfg: &types.CFControllerConfig{
			CloudFlare: types.CFControllerCloudflareConfig{
				TunnelConfigMapNamespace: "what",
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "what-tech",
			Namespace: "what",
			Annotations: map[string]string{
				"cloudflare.com/tunnel-external-name": "cft.what.tech",
				"cloudflare.com/tunnel-name":          "what.tech",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "xhttp",
					NodePort:   30731,
					Port:       80,
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(80),
				},
				{
					Name:       "xhttps",
					NodePort:   30831,
					Port:       443,
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(443),
				},
			},
		},
	}
	err := updateConfigMap(cf, svc)
	assert.NoError(t, err)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 1)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, 1)
	assert.Equal(t, "/", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Path)
	assert.Equal(t, "https://what-tech.what:443", cf.tunnelConfigMaps.upsertCalls[0].cfcis[0].Service)
}

// metav1.ObjectMeta: metav1.ObjectMeta{

// },

func TestUnknownUpdateConfigMap(t *testing.T) {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cf := &mockController{
		log: &_log,
		cfg: &types.CFControllerConfig{
			CloudFlare: types.CFControllerCloudflareConfig{
				TunnelConfigMapNamespace: "what",
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "what-tech",
			Namespace: "what",
			Annotations: map[string]string{
				"cloudflare.com/tunnel-external-name": "cft.what.tech",
				"cloudflare.com/tunnel-name":          "what.tech",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "xhttp",
					NodePort:   30731,
					Port:       80,
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(180),
				},
				{
					Name:       "xhttps",
					NodePort:   30831,
					Port:       443,
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(1443),
				},
			},
		},
	}
	err := updateConfigMap(cf, svc)
	assert.NoError(t, err)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 0)
}

func TestSimpleSVC(t *testing.T) {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cf := &mockController{
		log: &_log,
		cfg: &types.CFControllerConfig{
			CloudFlare: types.CFControllerCloudflareConfig{
				TunnelConfigMapNamespace: "what",
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "what-tech",
			Namespace: "what",
			Annotations: map[string]string{
				"cloudflare.com/tunnel-external-name": "cft.what.tech",
				"cloudflare.com/tunnel-name":          "what.tech",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       5061,
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(5051),
				},
			},
		},
	}
	err := updateConfigMap(cf, svc)
	assert.NoError(t, err)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 1)
	assert.Equal(t, "what.tech", cf.tunnelConfigMaps.upsertCalls[0].tparam.Name)

	assert.Equal(t, []types.CFConfigIngress{
		{
			Hostname: "cft.what.tech",
			Path:     "/",
			Service:  "http://what-tech.what:5061",
			OriginRequest: &types.CFConfigOriginRequest{
				NoTLSVerify:    false,
				HttpHostHeader: "what-tech",
			},
		}}, cf.tunnelConfigMaps.upsertCalls[0].cfcis)
}

package ingress

import (
	"context"
	"testing"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func toPtr(s string) *string {
	return &s
}
func TestIngressForeignClass(t *testing.T) {
	cf, ingress, ev := setupIngress()
	ingress.Spec.IngressClassName = toPtr("wurstClass")

	processEvent(ev, ingress, cf)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 0)
}

func TestIngressMappingForeignClass(t *testing.T) {
	cf, ingress, ev := setupIngress()
	ingress.Spec.IngressClassName = toPtr("wurstClass")

	// schema/hostname/int-port/hostheader/ext-host|path,
	ingress.Annotations = map[string]string{
		config.AnnotationCloudflareTunnelName(): "cf-what-tunnel",
		config.AnnotationCloudflareTunnelMapping(): `
			https/xxx.what.tech/4711/root.luchs/xxx.ext.tech|/wurst,
			https-notlsverify/xxx.what.tech/4911//zzz.ext.tech|/,
			http/yyy.what.tech///yyy.ext.tech`,
	}

	processEvent(ev, ingress, cf)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 1)

	assert.Equal(t, cf.tunnelConfigMaps.upsertCalls[0].tparam.Namespace, "what")
	assert.Equal(t, cf.tunnelConfigMaps.upsertCalls[0].tparam.Name, "cf-what-tunnel")
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, 3)
	assert.Equal(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, []types.CFConfigIngress{
		{
			Hostname: "xxx.ext.tech",
			Path:     "/wurst",
			Service:  "https://xxx.what.tech:4711",
			OriginRequest: &types.CFConfigOriginRequest{
				NoTLSVerify:    false,
				HttpHostHeader: "root.luchs",
			},
		},
		{
			Hostname: "zzz.ext.tech",
			Path:     "/",
			Service:  "https://xxx.what.tech:4911",
			OriginRequest: &types.CFConfigOriginRequest{
				NoTLSVerify:    true,
				HttpHostHeader: "xxx.what.tech",
			},
		},
		{
			Hostname: "yyy.ext.tech",
			Path:     "/",
			Service:  "https://yyy.what.tech:443",
			OriginRequest: &types.CFConfigOriginRequest{
				NoTLSVerify:    false,
				HttpHostHeader: "yyy.what.tech",
			},
		},
	})

}

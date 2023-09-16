package ingress

import (
	"os"
	"testing"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	net1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

// shutdownFns   map[string]func()

func setupIngress() (*mockController, *net1.Ingress, watch.Event) {
	_log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	cf := &mockController{
		log: &_log,
		cfg: &types.CFControllerConfig{
			CloudFlare: types.CFControllerCloudflareConfig{
				TunnelConfigMapNamespace: "what",
			},
		},
	}
	className := "cloudflared"
	ingress := &net1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "what-tech",
			Namespace:   "what",
			Annotations: map[string]string{},
		},
		Spec: net1.IngressSpec{
			IngressClassName: &className,
			Rules: []net1.IngressRule{
				{
					Host: "xxx.what.tech",
					IngressRuleValue: net1.IngressRuleValue{
						HTTP: &net1.HTTPIngressRuleValue{
							Paths: []net1.HTTPIngressPath{
								{
									Backend: net1.IngressBackend{
										Service: &net1.IngressServiceBackend{
											Name: "svc-wurst-xxx-what-tech",
											Port: net1.ServiceBackendPort{
												Number: 4711,
											},
										},
									},
									Path: "/wurst",
								},
								{
									Backend: net1.IngressBackend{
										Service: &net1.IngressServiceBackend{
											Name: "svc-471-xxx-what-tech",
											Port: net1.ServiceBackendPort{
												Number: 471,
											},
										},
									},
									Path: "/",
								},
							},
						},
					},
				},
				{
					Host: "yyy.what.tech",
					IngressRuleValue: net1.IngressRuleValue{
						HTTP: &net1.HTTPIngressRuleValue{
							Paths: []net1.HTTPIngressPath{
								{
									Backend: net1.IngressBackend{
										Service: &net1.IngressServiceBackend{
											Name: "svc-yyy-wurst-what-tech",
											Port: net1.ServiceBackendPort{
												Number: 499,
											},
										},
									},
									Path: "/",
								},
							},
						},
					},
				},
			},
			TLS: []net1.IngressTLS{
				{
					Hosts:      []string{"yyy.what.tech"},
					SecretName: "xxx-what-tech",
				},
			},
		},
	}
	ev := watch.Event{
		Type: watch.Added,
	}
	return cf, ingress, ev
}

func TestIngressClassIntrospectTunnelNameHttpClass(t *testing.T) {
	cf, ingress, ev := setupIngress()

	processEvent(ev, ingress, cf)

	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 1)
	assert.Equal(t, cf.tunnelConfigMaps.upsertCalls[0].tparam.Namespace, "what")
	assert.Equal(t, cf.tunnelConfigMaps.upsertCalls[0].tparam.Name, "what.tech")
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, 3)
	assert.Equal(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, []types.CFConfigIngress{
		{
			Hostname: "xxx.what.tech",
			Path:     "/wurst",
			Service:  "http://svc-wurst-xxx-what-tech.what:4711",
			OriginRequest: &types.CFConfigOriginRequest{
				NoTLSVerify:    false,
				HttpHostHeader: "xxx.what.tech",
			},
		},
		{
			Hostname: "xxx.what.tech",
			Path:     "/",
			Service:  "http://svc-471-xxx-what-tech.what:471",
			OriginRequest: &types.CFConfigOriginRequest{
				NoTLSVerify:    false,
				HttpHostHeader: "xxx.what.tech",
			},
		},
		{
			Hostname: "yyy.what.tech",
			Path:     "/",
			Service:  "http://svc-yyy-wurst-what-tech.what:499",
			OriginRequest: &types.CFConfigOriginRequest{
				NoTLSVerify:    false,
				HttpHostHeader: "yyy.what.tech",
			},
		},
	})
}

func TestIngressClassIntrospectMappingHttpClass(t *testing.T) {
	cf, ingress, ev := setupIngress()

	ingress.Annotations = map[string]string{
		config.AnnotationCloudflareTunnelMapping(): `
			xxx.what.tech/https/root.luchs|/wurst,
			xxx.what.tech/https-notlsverify/max.lust,
			yyy.what.tech/https`,
	}
	processEvent(ev, ingress, cf)

	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 1)
	assert.Len(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, 3)
	assert.Equal(t, cf.tunnelConfigMaps.upsertCalls[0].cfcis, []types.CFConfigIngress{
		{
			Hostname: "xxx.what.tech",
			Path:     "/wurst",
			Service:  "https://svc-wurst-xxx-what-tech.what:4711",
			OriginRequest: &types.CFConfigOriginRequest{
				NoTLSVerify:    false,
				HttpHostHeader: "root.luchs",
			},
		},
		{
			Hostname: "xxx.what.tech",
			Path:     "/",
			Service:  "https://svc-471-xxx-what-tech.what:471",
			OriginRequest: &types.CFConfigOriginRequest{
				NoTLSVerify:    true,
				HttpHostHeader: "max.lust",
			},
		},
		{
			Hostname: "yyy.what.tech",
			Path:     "/",
			Service:  "https://svc-yyy-wurst-what-tech.what:499",
			OriginRequest: &types.CFConfigOriginRequest{
				NoTLSVerify:    false,
				HttpHostHeader: "yyy.what.tech",
			},
		},
	})
}

func TestIngressClassHttpClass(t *testing.T) {
	cf, ingress, ev := setupIngress()

	ingress.Annotations = map[string]string{
		config.AnnotationCloudflareTunnelName(): "murks/hello",
	}

	processEvent(ev, ingress, cf)

	assert.Len(t, cf.tunnelConfigMaps.upsertCalls, 1)
	assert.Equal(t, cf.tunnelConfigMaps.upsertCalls[0].tparam.Namespace, "murks")
	assert.Equal(t, cf.tunnelConfigMaps.upsertCalls[0].tparam.Name, "hello")
}

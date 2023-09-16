package ingress

import (
	"testing"

	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/k8s_data"
	"github.com/stretchr/testify/assert"
)

func TestIntrospectTunnelName(t *testing.T) {
	cf, ingress, _ := setupIngress()

	tparams := k8s_data.NewUniqueTunnelParams()
	err := introSpectTunnelName(cf, ingress, tparams)
	assert.NoError(t, err)
	assert.Len(t, tparams.Get(), 1)
	assert.Equal(t, "what.tech", tparams.Get()[0].Name)
	assert.Equal(t, "what", tparams.Get()[0].Namespace)
}

func TestIntrospectDuplicateTunnelName(t *testing.T) {
	cf, ingress, _ := setupIngress()

	ingress.Spec.Rules[0].Host = "murks.tech"

	tparams := k8s_data.NewUniqueTunnelParams()
	err := introSpectTunnelName(cf, ingress, tparams)
	assert.Error(t, err)
	assert.Len(t, tparams.Get(), 0)
}

func TestWithAnnotationTunnelNameIntrospectTunnelName(t *testing.T) {
	cf, ingress, _ := setupIngress()

	ingress.Annotations[config.AnnotationCloudflareTunnelName()] = "what-tech"
	tparams := k8s_data.NewUniqueTunnelParams()
	err := introSpectTunnelName(cf, ingress, tparams)
	assert.NoError(t, err)
	assert.Len(t, tparams.Get(), 0)
}
func TestWithAnnotationTunnelExternalIntrospectTunnelName(t *testing.T) {
	cf, ingress, _ := setupIngress()

	ingress.Annotations[config.AnnotationCloudflareTunnelExternalName()] = "what-tech"
	tparams := k8s_data.NewUniqueTunnelParams()
	err := introSpectTunnelName(cf, ingress, tparams)
	assert.NoError(t, err)
	assert.Len(t, tparams.Get(), 0)
}

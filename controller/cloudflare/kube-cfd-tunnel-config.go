package cloudflare

// https://www.martin-helmich.de/en/blog/kubernetes-crd-client.html
import (
	"context"

	cfgo "github.com/cloudflare/cloudflare-go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

type CFDTunnelConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec cfgo.UnvalidatedIngressRule `json:"spec"`
}

type CFDTunnelConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []CFDTunnelConfig `json:"items"`
}

const cfdTunnelConfigResource = "cfd-tunnel-configs"

// DeepCopyInto copies all properties of this object into another object of the
// same type that is provided as a pointer.
func (in *CFDTunnelConfig) DeepCopyInto(out *CFDTunnelConfig) {
	out.TypeMeta = in.TypeMeta
	out.ObjectMeta = in.ObjectMeta
	originRequest := *in.Spec.OriginRequest
	out.Spec = cfgo.UnvalidatedIngressRule{
		Hostname:      in.Spec.Hostname,
		Path:          in.Spec.Path,
		Service:       in.Spec.Service,
		OriginRequest: &originRequest,
	}
}

// DeepCopyObject returns a generically typed copy of an object
func (in *CFDTunnelConfig) DeepCopyObject() runtime.Object {
	out := CFDTunnelConfig{}
	in.DeepCopyInto(&out)
	return &out
}

// DeepCopyObject returns a generically typed copy of an object
func (in *CFDTunnelConfigList) DeepCopyObject() runtime.Object {
	out := CFDTunnelConfigList{}
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta

	if in.Items != nil {
		out.Items = make([]CFDTunnelConfig, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}

	return &out
}

type CFDTunnelConfigInterface interface {
	List(opts metav1.ListOptions) (*CFDTunnelConfigList, error)
	Get(name string, options metav1.GetOptions) (*CFDTunnelConfig, error)
	Create(*CFDTunnelConfig) (*CFDTunnelConfig, error)
	Watch(opts metav1.ListOptions) (watch.Interface, error)
	// ...
}

type cfdTunnelConfigClient struct {
	restClient rest.Interface
	ns         string
	ctx        context.Context
}

func (c *cfdTunnelConfigClient) List(opts metav1.ListOptions) (*CFDTunnelConfigList, error) {
	result := CFDTunnelConfigList{}
	err := c.restClient.
		Get().
		Namespace(c.ns).
		Resource(cfdTunnelConfigResource).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(c.ctx).
		Into(&result)

	return &result, err
}

func (c *cfdTunnelConfigClient) Get(name string, opts metav1.GetOptions) (*CFDTunnelConfig, error) {
	result := CFDTunnelConfig{}
	err := c.restClient.
		Get().
		Namespace(c.ns).
		Resource(cfdTunnelConfigResource).
		Name(name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(c.ctx).
		Into(&result)

	return &result, err
}

func (c *cfdTunnelConfigClient) Create(project *CFDTunnelConfig) (*CFDTunnelConfig, error) {
	result := CFDTunnelConfig{}
	err := c.restClient.
		Post().
		Namespace(c.ns).
		Resource(cfdTunnelConfigResource).
		Body(project).
		Do(c.ctx).
		Into(&result)

	return &result, err
}

func (c *cfdTunnelConfigClient) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.restClient.
		Get().
		Namespace(c.ns).
		Resource(cfdTunnelConfigResource).
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch(c.ctx)
}

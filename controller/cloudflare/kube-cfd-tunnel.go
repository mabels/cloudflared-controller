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

type CFDTunnel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec cfgo.TunnelCreateParams `json:"spec"`
}

type CFDTunnelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []CFDTunnel `json:"items"`
}

const cfgTunnelResource = "cfd-tunnels"

// DeepCopyInto copies all properties of this object into another object of the
// same type that is provided as a pointer.
func (in *CFDTunnel) DeepCopyInto(out *CFDTunnel) {
	out.TypeMeta = in.TypeMeta
	out.ObjectMeta = in.ObjectMeta
	out.Spec = cfgo.TunnelCreateParams{
		Name:      in.Spec.Name,
		Secret:    in.Spec.Secret,
		ConfigSrc: in.Spec.ConfigSrc,
	}
}

// DeepCopyObject returns a generically typed copy of an object
func (in *CFDTunnel) DeepCopyObject() runtime.Object {
	out := CFDTunnel{}
	in.DeepCopyInto(&out)
	return &out
}

// DeepCopyObject returns a generically typed copy of an object
func (in *CFDTunnelList) DeepCopyObject() runtime.Object {
	out := CFDTunnelList{}
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta

	if in.Items != nil {
		out.Items = make([]CFDTunnel, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}

	return &out
}

type CFDTunnelInterface interface {
	List(opts metav1.ListOptions) (*CFDTunnelList, error)
	Get(name string, options metav1.GetOptions) (*CFDTunnel, error)
	Create(*CFDTunnel) (*CFDTunnel, error)
	Watch(opts metav1.ListOptions) (watch.Interface, error)
	// ...
}

type cfdTunnelClient struct {
	restClient rest.Interface
	ns         string
	ctx        context.Context
}

func (c *cfdTunnelClient) List(opts metav1.ListOptions) (*CFDTunnelList, error) {
	result := CFDTunnelList{}
	err := c.restClient.
		Get().
		Namespace(c.ns).
		Resource(cfgTunnelResource).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(c.ctx).
		Into(&result)

	return &result, err
}

func (c *cfdTunnelClient) Get(name string, opts metav1.GetOptions) (*CFDTunnel, error) {
	result := CFDTunnel{}
	err := c.restClient.
		Get().
		Namespace(c.ns).
		Resource(cfgTunnelResource).
		Name(name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(c.ctx).
		Into(&result)

	return &result, err
}

func (c *cfdTunnelClient) Create(project *CFDTunnel) (*CFDTunnel, error) {
	result := CFDTunnel{}
	err := c.restClient.
		Post().
		Namespace(c.ns).
		Resource(cfgTunnelResource).
		Body(project).
		Do(c.ctx).
		Into(&result)

	return &result, err
}

func (c *cfdTunnelClient) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.restClient.
		Get().
		Namespace(c.ns).
		Resource(cfgTunnelResource).
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch(c.ctx)
}

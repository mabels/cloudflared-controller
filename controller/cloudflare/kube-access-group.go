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

type AccessGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec cfgo.AccessGroup `json:"spec"`
}

type AccessGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []AccessGroup `json:"items"`
}

const accessGroupResource = "access-groups"

// DeepCopyInto copies all properties of this object into another object of the
// same type that is provided as a pointer.
func (in *AccessGroup) DeepCopyInto(out *AccessGroup) {
	out.TypeMeta = in.TypeMeta
	out.ObjectMeta = in.ObjectMeta
	out.Spec = cfgo.AccessGroup{
		ID:        in.Spec.ID,
		CreatedAt: in.Spec.CreatedAt,
		UpdatedAt: in.Spec.UpdatedAt,
		Name:      in.Spec.Name,
		Include:   append([]interface{}{}, in.Spec.Include...),
		Exclude:   append([]interface{}{}, in.Spec.Exclude...),
		Require:   append([]interface{}{}, in.Spec.Require...),
	}
}

// DeepCopyObject returns a generically typed copy of an object
func (in *AccessGroup) DeepCopyObject() runtime.Object {
	out := AccessGroup{}
	in.DeepCopyInto(&out)
	return &out
}

// DeepCopyObject returns a generically typed copy of an object
func (in *AccessGroupList) DeepCopyObject() runtime.Object {
	out := AccessGroupList{}
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta

	if in.Items != nil {
		out.Items = make([]AccessGroup, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}

	return &out
}

type AccessGroupInterface interface {
	List(opts metav1.ListOptions) (*AccessGroupList, error)
	Get(name string, options metav1.GetOptions) (*AccessGroup, error)
	Create(*AccessGroup) (*AccessGroup, error)
	Watch(opts metav1.ListOptions) (watch.Interface, error)
	// ...
}

type accessGroupClient struct {
	restClient rest.Interface
	ns         string
	ctx        context.Context
}

func (c *accessGroupClient) List(opts metav1.ListOptions) (*AccessGroupList, error) {
	result := AccessGroupList{}
	err := c.restClient.
		Get().
		Namespace(c.ns).
		Resource(accessGroupResource).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(c.ctx).
		Into(&result)

	return &result, err
}

func (c *accessGroupClient) Get(name string, opts metav1.GetOptions) (*AccessGroup, error) {
	result := AccessGroup{}
	err := c.restClient.
		Get().
		Namespace(c.ns).
		Resource(accessGroupResource).
		Name(name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(c.ctx).
		Into(&result)

	return &result, err
}

func (c *accessGroupClient) Create(project *AccessGroup) (*AccessGroup, error) {
	result := AccessGroup{}
	err := c.restClient.
		Post().
		Namespace(c.ns).
		Resource(accessGroupResource).
		Body(project).
		Do(c.ctx).
		Into(&result)

	return &result, err
}

func (c *accessGroupClient) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.restClient.
		Get().
		Namespace(c.ns).
		Resource(accessGroupResource).
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch(c.ctx)
}

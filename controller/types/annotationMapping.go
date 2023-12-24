package types

type Meta struct {
	Unknown string
}
type SvcAnnotationMapping struct {
	PortName string
	Schema   string
	Path     string
	Order    int
	Meta     Meta
}

// hostname/schema[/hostheader]|path,
type ClassIngressAnnotationMapping struct {
	Hostname   string
	Schema     string
	HostHeader *string
	Path       string
}

// schema/hostname/int-port/hostheader/ext-host|path,
type StackIngressAnnotationMapping struct {
	Schema      string
	Hostname    string
	InternPort  int
	HostHeader  *string
	ExtHostName string
	Path        string
}

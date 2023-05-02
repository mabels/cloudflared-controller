package config

type CFConfigOriginRequest struct {
	HttpHostHeader string `yaml:"httpHostHeader,omitempty"`
}

type CFConfigIngress struct {
	Hostname      string                 `yaml:"hostname,omitempty"`
	Path          string                 `yaml:"path,omitempty"`
	Service       string                 `yaml:"service,omitempty"`
	OriginRequest *CFConfigOriginRequest `yaml:"originRequest,omitempty"`
}

type CFConfigYaml struct {
	Tunnel          string            `yaml:"tunnel"`
	CredentialsFile string            `yaml:"credentials-file"`
	Ingress         []CFConfigIngress `yaml:"ingress"`
}

package types

import "time"

type CFControllerCloudflareConfig struct {
	ApiUrl                   string
	ApiToken                 string
	AccountId                string
	TunnelConfigMapNamespace string
	// ZoneId    string
}

type CFControllerConfig struct {
	KubeConfigFile         string
	PresetNamespaces       []string
	Identity               string
	NoCloudFlared          bool
	Version                string
	Debug                  bool
	ShowVersion            bool
	RunningInstanceDir     string
	CloudFlaredFname       string
	ChannelSize            int
	ClusterName            string
	RestartDelay           time.Duration
	ConfigMapLabelSelector string
	CloudFlare             CFControllerCloudflareConfig
	Leader                 struct {
		Name          string
		Namespace     string
		LeaseDuration time.Duration
		RenewDeadline time.Duration
		RetryPeriod   time.Duration
	}
}

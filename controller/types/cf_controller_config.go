package types

import "time"

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
	CloudFlare             struct {
		ApiUrl                   string
		ApiToken                 string
		AccountId                string
		TunnelConfigMapNamespace string
		// ZoneId    string
	}
	Leader struct {
		Name          string
		Namespace     string
		LeaseDuration time.Duration
		RenewDeadline time.Duration
		RetryPeriod   time.Duration
	}
}

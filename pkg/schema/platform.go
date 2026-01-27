// Package schema defines the canonical Platform type for platform.yaml configuration.
// This is the public API consumed by other plasmactl plugins (e.g., plasmactl-node).
package schema

// Platform represents the platform.yaml configuration
type Platform struct {
	Name        string `yaml:"name"`
	Cluster     string `yaml:"cluster,omitempty"`
	Description string `yaml:"description,omitempty"`

	Infrastructure Infrastructure            `yaml:"infrastructure"`
	DNS            DNSConfig                  `yaml:"dns,omitempty"`
	Networking     Networking                 `yaml:"networking,omitempty"`
	Chassis        map[string][]ChassisProfile `yaml:"chassis,omitempty"`

	Defaults    PlatformDefaults  `yaml:"defaults,omitempty"`
	Features    PlatformFeatures  `yaml:"features,omitempty"`
	Environment EnvironmentConfig `yaml:"environment,omitempty"`
}

// Infrastructure defines the infrastructure provider configuration
type Infrastructure struct {
	MetalProvider string    `yaml:"metal_provider"` // scaleway, hetzner, aws, ovh, gcp, azure, manual
	API           APIConfig `yaml:"api,omitempty"`
}

// DNSConfig defines DNS provider configuration
type DNSConfig struct {
	Provider string `yaml:"provider"`          // ovh, cloudflare, route53, gcp, manual
	Domain   string `yaml:"domain"`            // e.g., dev.skilld.cloud
	// Records are auto-configured: MX, DKIM, DMARC, SPF, rDNS
}

// APIConfig defines API connection settings
type APIConfig struct {
	URI   string `yaml:"uri,omitempty"`
	Token string `yaml:"token,omitempty"`
}

// Networking defines network configuration
type Networking struct {
	PrivateNetwork    string    `yaml:"private_network,omitempty"`
	PrivateVIPNetwork string    `yaml:"private_vip_network,omitempty"`
	Bus               BusConfig `yaml:"bus,omitempty"`
}

// BusConfig defines message bus configuration
type BusConfig struct {
	IP    string         `yaml:"ip,omitempty"`
	Event EventBusConfig `yaml:"event,omitempty"`
	Data  DataBusConfig  `yaml:"data,omitempty"`
}

// EventBusConfig defines event bus (NATS) configuration
type EventBusConfig struct {
	Application string `yaml:"application,omitempty"`
	Port        int    `yaml:"port,omitempty"`
}

// DataBusConfig defines data bus (Kafka) configuration
type DataBusConfig struct {
	Application string `yaml:"application,omitempty"`
	Port        int    `yaml:"port,omitempty"`
	Service     string `yaml:"service,omitempty"`
	BrokerCount int    `yaml:"broker_count,omitempty"`
}

// ChassisProfile defines a hardware profile for a chassis attachment
type ChassisProfile struct {
	Type  string `yaml:"type"`  // Offer type (e.g., GP1-L, GPU-3090)
	Count int    `yaml:"count"` // Number of nodes
}

// PlatformDefaults defines default values for nodes
type PlatformDefaults struct {
	Chassis      string    `yaml:"chassis,omitempty"`
	Capabilities []string  `yaml:"capabilities,omitempty"`
	Resources    Resources `yaml:"resources,omitempty"`
}

// Resources defines resource specifications
type Resources struct {
	CPU    int    `yaml:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty"`
	GPU    string `yaml:"gpu,omitempty"`
}

// PlatformFeatures defines feature flags
type PlatformFeatures struct {
	DisplayOSRebuildConfirmation bool `yaml:"display_os_rebuild_confirmation,omitempty"`
	DisplayDataWipeConfirmation  bool `yaml:"display_data_wipe_confirmation,omitempty"`
	OSWipeData                   bool `yaml:"os_wipe_data,omitempty"`
}

// EnvironmentConfig defines environment-level settings
type EnvironmentConfig struct {
	Type            string `yaml:"type,omitempty"` // development, staging, production
	AutoDeploy      bool   `yaml:"auto_deploy,omitempty"`
	MonitoringLevel string `yaml:"monitoring_level,omitempty"`
}

// NewPlatform creates a new Platform with default values
func NewPlatform(name, metalProvider, dnsProvider, domain string) *Platform {
	return &Platform{
		Name: name,
		Infrastructure: Infrastructure{
			MetalProvider: metalProvider,
		},
		DNS: DNSConfig{
			Provider: dnsProvider,
			Domain:   domain,
		},
		Networking: Networking{
			PrivateNetwork: "192.168.0.0/16",
		},
		Chassis: make(map[string][]ChassisProfile),
	}
}

// PlatformInfo represents summarized platform information for listing
type PlatformInfo struct {
	Name          string `yaml:"name"`
	Domain        string `yaml:"domain"`
	MetalProvider string `yaml:"metal_provider"`
	DNSProvider   string `yaml:"dns_provider"`
	NodeCount     int    `yaml:"node_count"`
}

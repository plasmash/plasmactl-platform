// Package schema defines the canonical Platform type for platform.yaml configuration.
// This is the public API consumed by other plasmactl plugins (e.g., plasmactl-node).
package schema

// Platform represents the platform.yaml configuration
type Platform struct {
	Name        string `yaml:"name"                  json:"name"`
	Cluster     string `yaml:"cluster,omitempty"     json:"cluster,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	Infrastructure Infrastructure            `yaml:"infrastructure"       json:"infrastructure"`
	DNS            DNSConfig                  `yaml:"dns,omitempty"        json:"dns,omitempty"`
	Networking     Networking                 `yaml:"networking,omitempty" json:"networking,omitempty"`
	Pools          map[string]Pool            `yaml:"pools,omitempty"      json:"pools,omitempty"`

	Defaults    PlatformDefaults  `yaml:"defaults,omitempty"    json:"defaults,omitempty"`
	Features    PlatformFeatures  `yaml:"features,omitempty"    json:"features,omitempty"`
	Environment EnvironmentConfig `yaml:"environment,omitempty" json:"environment,omitempty"`
}

// Infrastructure defines the infrastructure provider configuration
type Infrastructure struct {
	MetalProvider string    `yaml:"metal_provider"        json:"metal_provider"`
	API           APIConfig `yaml:"api,omitempty"         json:"api,omitempty"`
	Zone          string    `yaml:"zone,omitempty"        json:"zone,omitempty"`
	Region        string    `yaml:"region,omitempty"      json:"region,omitempty"`
	ProjectID     string    `yaml:"project_id,omitempty"  json:"project_id,omitempty"`
	Image         string    `yaml:"image,omitempty"       json:"image,omitempty"`
	SSHKeyID      string    `yaml:"ssh_key_id,omitempty"  json:"ssh_key_id,omitempty"`
}

// DNSConfig defines DNS provider configuration
type DNSConfig struct {
	Provider string    `yaml:"provider"              json:"provider"`
	Domain   string    `yaml:"domain"                json:"domain"`
	Zone     string    `yaml:"zone,omitempty"        json:"zone,omitempty"`
	API      APIConfig `yaml:"api,omitempty"         json:"api,omitempty"`
	Region   string    `yaml:"region,omitempty"      json:"region,omitempty"`
	DKIMKey  string    `yaml:"dkim_key,omitempty"    json:"-"`
}

// APIConfig defines API connection settings
type APIConfig struct {
	URI   string `yaml:"uri,omitempty"   json:"uri,omitempty"`
	Token string `yaml:"token,omitempty" json:"-"`
}

// Networking defines network configuration
type Networking struct {
	PrivateNetwork    string    `yaml:"private_network,omitempty"     json:"private_network,omitempty"`
	PrivateVIPNetwork string    `yaml:"private_vip_network,omitempty" json:"private_vip_network,omitempty"`
	Bus               BusConfig `yaml:"bus,omitempty"                 json:"bus,omitempty"`
}

// BusConfig defines message bus configuration
type BusConfig struct {
	IP    string         `yaml:"ip,omitempty"    json:"ip,omitempty"`
	Event EventBusConfig `yaml:"event,omitempty" json:"event,omitempty"`
	Data  DataBusConfig  `yaml:"data,omitempty"  json:"data,omitempty"`
}

// EventBusConfig defines event bus (NATS) configuration
type EventBusConfig struct {
	Application string `yaml:"application,omitempty" json:"application,omitempty"`
	Port        int    `yaml:"port,omitempty"        json:"port,omitempty"`
}

// DataBusConfig defines data bus (Kafka) configuration
type DataBusConfig struct {
	Application string `yaml:"application,omitempty" json:"application,omitempty"`
	Port        int    `yaml:"port,omitempty"        json:"port,omitempty"`
	Service     string `yaml:"service,omitempty"     json:"service,omitempty"`
	BrokerCount int    `yaml:"broker_count,omitempty" json:"broker_count,omitempty"`
}

// Pool defines a group of nodes serving co-located zones
type Pool struct {
	Zones   []string `yaml:"zones"   json:"zones"`
	Machine string   `yaml:"machine" json:"machine"`
	Count   int      `yaml:"count"   json:"count"`
}

// PlatformDefaults defines default values for nodes
type PlatformDefaults struct {
	Zone         string    `yaml:"zone,omitempty"         json:"zone,omitempty"`
	Capabilities []string  `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Resources    Resources `yaml:"resources,omitempty"    json:"resources,omitempty"`
}

// Resources defines resource specifications
type Resources struct {
	CPU    int    `yaml:"cpu,omitempty"    json:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"`
	GPU    string `yaml:"gpu,omitempty"    json:"gpu,omitempty"`
}

// PlatformFeatures defines feature flags
type PlatformFeatures struct {
	DisplayOSRebuildConfirmation bool `yaml:"display_os_rebuild_confirmation,omitempty" json:"display_os_rebuild_confirmation,omitempty"`
	DisplayDataWipeConfirmation  bool `yaml:"display_data_wipe_confirmation,omitempty"  json:"display_data_wipe_confirmation,omitempty"`
	OSWipeData                   bool `yaml:"os_wipe_data,omitempty"                    json:"os_wipe_data,omitempty"`
}

// EnvironmentConfig defines environment-level settings
type EnvironmentConfig struct {
	Type            string `yaml:"type,omitempty"             json:"type,omitempty"`
	AutoDeploy      bool   `yaml:"auto_deploy,omitempty"      json:"auto_deploy,omitempty"`
	MonitoringLevel string `yaml:"monitoring_level,omitempty" json:"monitoring_level,omitempty"`
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
		Pools: make(map[string]Pool),
	}
}

// PlatformInfo represents summarized platform information for listing
type PlatformInfo struct {
	Name          string `yaml:"name"           json:"name"`
	Domain        string `yaml:"domain"         json:"domain"`
	MetalProvider string `yaml:"metal_provider" json:"metal_provider"`
	DNSProvider   string `yaml:"dns_provider"   json:"dns_provider"`
	NodeCount     int    `yaml:"node_count"     json:"node_count"`
}

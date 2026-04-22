package create

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-platform/pkg/dns"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// CreateResult is the structured output for platform:create
type CreateResult struct {
	Name          string `json:"name"`
	MetalProvider string `json:"metal_provider"`
	DNSProvider   string `json:"dns_provider"`
	Domain        string `json:"domain"`
	Zone          string `json:"zone,omitempty"`
	Region        string `json:"region,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	Image         string `json:"image,omitempty"`
	SSHKeyID      string `json:"ssh_key_id,omitempty"`
	Path          string `json:"path"`
}

// Create implements the platform:create command
type Create struct {
	action.WithLogger
	action.WithTerm

	Keyring keyring.Keyring

	Name          string
	MetalProvider string
	DNSProvider   string
	Domain        string
	Zone          string
	Region        string
	ProjectID     string
	Image         string
	SSHKeyID      string
	SkipDNS       bool

	result *CreateResult
}

// Result returns the structured result for JSON output
func (c *Create) Result() any {
	return c.result
}

// Execute runs the platform:create action
func (c *Create) Execute() error {
	platformDir := filepath.Join("platforms", c.Name)
	nodesDir := filepath.Join(platformDir, "nodes")
	platformFile := filepath.Join(platformDir, "platform.yaml")

	// Check if platform already exists
	if _, err := os.Stat(platformDir); !os.IsNotExist(err) {
		return fmt.Errorf("platform %q already exists at %s", c.Name, platformDir)
	}

	c.Term().Info().Printfln("Creating platform %q", c.Name)
	c.Term().Info().Printfln("  Metal provider: %s", c.MetalProvider)
	c.Term().Info().Printfln("  DNS provider: %s", c.DNSProvider)
	c.Term().Info().Printfln("  Domain: %s", c.Domain)

	// Create directories
	if err := os.MkdirAll(nodesDir, 0755); err != nil {
		return fmt.Errorf("failed to create nodes directory: %w", err)
	}

	// Create platform.yaml
	platform := schema.NewPlatform(c.Name, c.MetalProvider, c.DNSProvider, c.Domain)

	// Set provider-specific defaults for metal provider
	switch c.MetalProvider {
	case "scaleway":
		platform.Infrastructure.API = schema.APIConfig{
			URI:   "https://api.online.net/api/v1/",
			Token: "{{ .keyring.scaleway_api_token }}",
		}
		if c.Zone == "" {
			platform.Infrastructure.Zone = "fr-par-2"
		}
	case "hetzner":
		platform.Infrastructure.API = schema.APIConfig{
			Token: "{{ .keyring.hetzner_api_token }}",
		}
		if c.Zone == "" {
			platform.Infrastructure.Zone = "fsn1"
		}
		if c.Image == "" {
			platform.Infrastructure.Image = "ubuntu-24.04"
		}
	case "ovh":
		platform.Infrastructure.API = schema.APIConfig{
			Token: "{{ .keyring.ovh_api_token }}",
		}
		if c.Region == "" {
			platform.Infrastructure.Region = "eu"
		}
		if c.Zone == "" {
			platform.Infrastructure.Zone = "rbx"
		}
		if c.Image == "" {
			platform.Infrastructure.Image = "debian12_64"
		}
	case "aws":
		// AWS uses environment variables or SDK defaults for credentials
		if c.Region == "" {
			platform.Infrastructure.Region = "eu-west-1"
		}
		if c.Zone == "" {
			platform.Infrastructure.Zone = "eu-west-1a"
		}
	case "gcp", "azure":
		// Cloud providers use environment variables or SDK defaults
	case "manual":
		// No API or infrastructure configuration needed
	}

	// User-provided values override defaults
	if c.Zone != "" {
		platform.Infrastructure.Zone = c.Zone
	}
	if c.Region != "" {
		platform.Infrastructure.Region = c.Region
	}
	if c.ProjectID != "" {
		platform.Infrastructure.ProjectID = c.ProjectID
	}
	if c.Image != "" {
		platform.Infrastructure.Image = c.Image
	}
	if c.SSHKeyID != "" {
		platform.Infrastructure.SSHKeyID = c.SSHKeyID
	}

	// Set DNS provider defaults
	switch c.DNSProvider {
	case "ovh":
		platform.DNS.API = schema.APIConfig{
			Token: "{{ .keyring.ovh_api_token }}",
		}
		if platform.DNS.Region == "" {
			platform.DNS.Region = "eu"
		}
	case "cloudflare":
		platform.DNS.API = schema.APIConfig{
			Token: "{{ .keyring.cloudflare_api_token }}",
		}
	case "gandi":
		platform.DNS.API = schema.APIConfig{
			Token: "{{ .keyring.gandi_api_token }}",
		}
	case "inwx":
		platform.DNS.API = schema.APIConfig{
			Token: "{{ .keyring.inwx_api_token }}",
		}
	}

	// Derive DNS zone from domain if not set
	if platform.DNS.Zone == "" && platform.DNS.Domain != "" {
		platform.DNS.Zone = dns.DeriveZone(platform.DNS.Domain)
	}

	data, err := yaml.Marshal(platform)
	if err != nil {
		return fmt.Errorf("failed to marshal platform.yaml: %w", err)
	}

	if err := os.WriteFile(platformFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write platform.yaml: %w", err)
	}

	// Create .gitkeep in nodes directory to ensure it's tracked
	gitkeepFile := filepath.Join(nodesDir, ".gitkeep")
	if err := os.WriteFile(gitkeepFile, []byte{}, 0644); err != nil {
		return fmt.Errorf("failed to write .gitkeep: %w", err)
	}

	// Build result
	c.result = &CreateResult{
		Name:          c.Name,
		MetalProvider: c.MetalProvider,
		DNSProvider:   c.DNSProvider,
		Domain:        c.Domain,
		Zone:          platform.Infrastructure.Zone,
		Region:        platform.Infrastructure.Region,
		ProjectID:     platform.Infrastructure.ProjectID,
		Image:         platform.Infrastructure.Image,
		SSHKeyID:      platform.Infrastructure.SSHKeyID,
		Path:          platformDir,
	}

	c.Term().Success().Printfln("Created platform scaffold at %s", platformDir)

	// Print next steps
	c.Term().Info().Println()
	c.Term().Info().Println("Next steps:")
	if c.MetalProvider != "manual" {
		step := 1
		c.Term().Info().Printfln("  %d. Ensure credentials are configured: plasmactl keyring:login %s", step, c.MetalProvider)
		step++
		if c.DNSProvider != "manual" && c.DNSProvider != c.MetalProvider {
			c.Term().Info().Printfln("  %d. Ensure DNS credentials: plasmactl keyring:login %s", step, c.DNSProvider)
			step++
		}
		c.Term().Info().Printfln("  %d. Size the platform: plasmactl platform:size %s --suggest", step, c.Name)
		step++
		c.Term().Info().Printfln("  %d. Provision nodes: plasmactl node:provision %s", step, c.Name)
		step++
		c.Term().Info().Printfln("  %d. Deploy: plasmactl platform:deploy %s", step, c.Name)
	} else {
		c.Term().Info().Printfln("  1. Add nodes: plasmactl node:add %s --hostname <name> --public-ip <ip>", c.Name)
		c.Term().Info().Printfln("  2. Or create node YAML files directly in %s", nodesDir)
		c.Term().Info().Printfln("  3. Deploy: plasmactl platform:deploy %s", c.Name)
	}

	return nil
}


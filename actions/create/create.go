package create

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr/pkg/action"
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
	instDir := filepath.Join("inst", c.Name)
	nodesDir := filepath.Join(instDir, "nodes")
	platformFile := filepath.Join(instDir, "platform.yaml")

	// Check if platform already exists
	if _, err := os.Stat(instDir); !os.IsNotExist(err) {
		return fmt.Errorf("platform %q already exists at %s", c.Name, instDir)
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
		Path:          instDir,
	}

	c.Term().Success().Printfln("Created platform scaffold at %s", instDir)

	// Configure DNS if not skipped and not manual
	if !c.SkipDNS && c.DNSProvider != "manual" {
		c.Term().Info().Println()
		c.Term().Info().Println("Configuring DNS records...")
		if err := c.configureDNS(); err != nil {
			c.Term().Warning().Printfln("DNS configuration failed: %v", err)
			c.Term().Warning().Println("You can configure DNS manually or retry with platform:validate")
		} else {
			c.Term().Success().Println("DNS records configured successfully")
		}
	}

	// Print next steps
	c.Term().Info().Println()
	c.Term().Info().Println("Next steps:")
	if c.MetalProvider != "manual" {
		c.Term().Info().Printfln("  1. Ensure credentials are configured: plasmactl keyring:login %s", c.MetalProvider)
		if c.DNSProvider != "manual" && c.DNSProvider != c.MetalProvider {
			c.Term().Info().Printfln("  2. Ensure DNS credentials: plasmactl keyring:login %s", c.DNSProvider)
			c.Term().Info().Printfln("  3. Provision nodes: plasmactl node:provision %s -c <chassis>:<offer>:<count>", c.Name)
		} else {
			c.Term().Info().Printfln("  2. Provision nodes: plasmactl node:provision %s -c <chassis>:<offer>:<count>", c.Name)
		}
	} else {
		c.Term().Info().Printfln("  1. Add nodes: plasmactl node:add %s --hostname <name> --public-ip <ip>", c.Name)
		c.Term().Info().Printfln("  2. Or create node YAML files directly in %s", nodesDir)
	}
	c.Term().Info().Printfln("  3. Deploy: plasmactl platform:deploy %s", c.Name)

	return nil
}

// configureDNS sets up DNS records (MX, DKIM, DMARC, SPF, rDNS)
func (c *Create) configureDNS() error {
	// TODO: Implement DNS configuration via Terraform
	// This will use terraform-exec to:
	// 1. Generate Terraform configuration for the DNS provider
	// 2. Apply the configuration to create:
	//    - MX records
	//    - DKIM records
	//    - DMARC records
	//    - SPF records
	//    - rDNS (if supported by provider)

	c.Term().Info().Println("  DNS configuration via Terraform is not yet implemented")
	c.Term().Info().Println("  Manual DNS setup required for now")

	return nil
}

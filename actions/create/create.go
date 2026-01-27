package create

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// Create implements the platform:create command
type Create struct {
	Log     *launchr.Logger
	Term    *launchr.Terminal
	Keyring keyring.Keyring

	Name          string
	MetalProvider string
	DNSProvider   string
	Domain        string
	SkipDNS       bool
}

// SetLogger sets the logger for the action
func (c *Create) SetLogger(log *launchr.Logger) {
	c.Log = log
}

// SetTerm sets the terminal for the action
func (c *Create) SetTerm(term *launchr.Terminal) {
	c.Term = term
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

	c.Term.Info().Printfln("Creating platform %q", c.Name)
	c.Term.Info().Printfln("  Metal provider: %s", c.MetalProvider)
	c.Term.Info().Printfln("  DNS provider: %s", c.DNSProvider)
	c.Term.Info().Printfln("  Domain: %s", c.Domain)

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
	case "hetzner":
		platform.Infrastructure.API = schema.APIConfig{
			Token: "{{ .keyring.hetzner_api_token }}",
		}
	case "ovh":
		platform.Infrastructure.API = schema.APIConfig{
			Token: "{{ .keyring.ovh_api_token }}",
		}
	case "aws", "gcp", "azure":
		// Cloud providers use environment variables or SDK defaults
	case "manual":
		// No API configuration needed
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

	c.Term.Success().Printfln("Created platform scaffold at %s", instDir)

	// Configure DNS if not skipped and not manual
	if !c.SkipDNS && c.DNSProvider != "manual" {
		c.Term.Info().Println()
		c.Term.Info().Println("Configuring DNS records...")
		if err := c.configureDNS(); err != nil {
			c.Term.Warning().Printfln("DNS configuration failed: %v", err)
			c.Term.Warning().Println("You can configure DNS manually or retry with platform:validate")
		} else {
			c.Term.Success().Println("DNS records configured successfully")
		}
	}

	// Print next steps
	c.Term.Info().Println()
	c.Term.Info().Println("Next steps:")
	if c.MetalProvider != "manual" {
		c.Term.Info().Printfln("  1. Ensure credentials are configured: plasmactl keyring:login %s", c.MetalProvider)
		if c.DNSProvider != "manual" && c.DNSProvider != c.MetalProvider {
			c.Term.Info().Printfln("  2. Ensure DNS credentials: plasmactl keyring:login %s", c.DNSProvider)
			c.Term.Info().Printfln("  3. Provision nodes: plasmactl node:provision %s -c <chassis>:<offer>:<count>", c.Name)
		} else {
			c.Term.Info().Printfln("  2. Provision nodes: plasmactl node:provision %s -c <chassis>:<offer>:<count>", c.Name)
		}
	} else {
		c.Term.Info().Printfln("  1. Add nodes: plasmactl node:add %s --hostname <name> --public-ip <ip>", c.Name)
		c.Term.Info().Printfln("  2. Or create node YAML files directly in %s", nodesDir)
	}
	c.Term.Info().Printfln("  3. Deploy: plasmactl platform:deploy %s", c.Name)

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

	c.Term.Info().Println("  DNS configuration via Terraform is not yet implemented")
	c.Term.Info().Println("  Manual DNS setup required for now")

	return nil
}


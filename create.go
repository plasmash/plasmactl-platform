package plasmactlplatform

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-platform/pkg/types"
	"gopkg.in/yaml.v3"
)

// platformCreate implements the platform:create command
type platformCreate struct {
	log     *launchr.Logger
	term    *launchr.Terminal
	keyring keyring.Keyring

	name          string
	metalProvider string
	dnsProvider   string
	domain        string
	skipDNS       bool
}

// SetLogger sets the logger for the action
func (a *platformCreate) SetLogger(log *launchr.Logger) {
	a.log = log
}

// SetTerm sets the terminal for the action
func (a *platformCreate) SetTerm(term *launchr.Terminal) {
	a.term = term
}

// Execute runs the platform:create action
func (a *platformCreate) Execute() error {
	envDir := filepath.Join("inst", a.name)
	nodesDir := filepath.Join(envDir, "nodes")
	platformFile := filepath.Join(envDir, "platform.yaml")

	// Check if platform already exists
	if _, err := os.Stat(envDir); !os.IsNotExist(err) {
		return fmt.Errorf("platform %q already exists at %s", a.name, envDir)
	}

	a.term.Info().Printfln("Creating platform %q", a.name)
	a.term.Info().Printfln("  Metal provider: %s", a.metalProvider)
	a.term.Info().Printfln("  DNS provider: %s", a.dnsProvider)
	a.term.Info().Printfln("  Domain: %s", a.domain)

	// Create directories
	if err := os.MkdirAll(nodesDir, 0755); err != nil {
		return fmt.Errorf("failed to create nodes directory: %w", err)
	}

	// Create platform.yaml
	platform := types.NewPlatform(a.name, a.metalProvider, a.dnsProvider, a.domain)

	// Set provider-specific defaults for metal provider
	switch a.metalProvider {
	case "scaleway":
		platform.Infrastructure.API = types.APIConfig{
			URI:   "https://api.online.net/api/v1/",
			Token: "{{ .keyring.scaleway_api_token }}",
		}
	case "hetzner":
		platform.Infrastructure.API = types.APIConfig{
			Token: "{{ .keyring.hetzner_api_token }}",
		}
	case "ovh":
		platform.Infrastructure.API = types.APIConfig{
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

	a.term.Success().Printfln("Created platform scaffold at %s", envDir)

	// Configure DNS if not skipped and not manual
	if !a.skipDNS && a.dnsProvider != "manual" {
		a.term.Info().Println()
		a.term.Info().Println("Configuring DNS records...")
		if err := a.configureDNS(); err != nil {
			a.term.Warning().Printfln("DNS configuration failed: %v", err)
			a.term.Warning().Println("You can configure DNS manually or retry with platform:validate")
		} else {
			a.term.Success().Println("DNS records configured successfully")
		}
	}

	// Print next steps
	a.term.Info().Println()
	a.term.Info().Println("Next steps:")
	if a.metalProvider != "manual" {
		a.term.Info().Printfln("  1. Ensure credentials are configured: plasmactl keyring:login %s", a.metalProvider)
		if a.dnsProvider != "manual" && a.dnsProvider != a.metalProvider {
			a.term.Info().Printfln("  2. Ensure DNS credentials: plasmactl keyring:login %s", a.dnsProvider)
			a.term.Info().Printfln("  3. Provision nodes: plasmactl node:provision %s -c <chassis>:<offer>:<count>", a.name)
		} else {
			a.term.Info().Printfln("  2. Provision nodes: plasmactl node:provision %s -c <chassis>:<offer>:<count>", a.name)
		}
	} else {
		a.term.Info().Printfln("  1. Add nodes: plasmactl node:add %s --hostname <name> --public-ip <ip>", a.name)
		a.term.Info().Printfln("  2. Or create node YAML files directly in %s", nodesDir)
	}
	a.term.Info().Printfln("  3. Deploy: plasmactl platform:deploy %s", a.name)

	return nil
}

// configureDNS sets up DNS records (MX, DKIM, DMARC, SPF, rDNS)
func (a *platformCreate) configureDNS() error {
	// TODO: Implement DNS configuration via Terraform
	// This will use terraform-exec to:
	// 1. Generate Terraform configuration for the DNS provider
	// 2. Apply the configuration to create:
	//    - MX records
	//    - DKIM records
	//    - DMARC records
	//    - SPF records
	//    - rDNS (if supported by provider)

	a.term.Info().Println("  DNS configuration via Terraform is not yet implemented")
	a.term.Info().Println("  Manual DNS setup required for now")

	return nil
}

// confirmDestroy prompts user to type the resource name to confirm destruction
func confirmDestroy(term *launchr.Terminal, resourceType, resourceName string) (bool, error) {
	term.Warning().Printfln("⚠️  This will PERMANENTLY destroy %s '%s'.", resourceType, resourceName)
	term.Warning().Printf("Type '%s' to confirm: ", resourceName)

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input != resourceName {
		term.Error().Println("Confirmation failed. Aborting.")
		return false, nil
	}

	return true, nil
}

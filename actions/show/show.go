package show

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// DNSStatus represents the DNS configuration status
type DNSStatus struct {
	Provider   string `json:"provider"`
	Domain     string `json:"domain"`
	Configured bool   `json:"configured"`
}

// ShowResult is the structured output for platform:show
type ShowResult struct {
	Platform schema.Platform `json:"platform"`
	Nodes    []string        `json:"nodes"`
	DNS      DNSStatus       `json:"dns"`
}

// Show implements the platform:show command
type Show struct {
	action.WithLogger
	action.WithTerm

	Name string

	result *ShowResult
}

// Result returns the structured result for JSON output
func (s *Show) Result() any {
	return s.result
}

func (s *Show) Execute() error {
	instDir := filepath.Join("inst", s.Name)
	platformFile := filepath.Join(instDir, "platform.yaml")

	// Check if platform exists
	if _, err := os.Stat(platformFile); os.IsNotExist(err) {
		return fmt.Errorf("platform %q not found (no platform.yaml at %s)", s.Name, platformFile)
	}

	// Read platform.yaml
	data, err := os.ReadFile(platformFile)
	if err != nil {
		return fmt.Errorf("failed to read platform.yaml: %w", err)
	}

	var platform schema.Platform
	if err := yaml.Unmarshal(data, &platform); err != nil {
		return fmt.Errorf("failed to parse platform.yaml: %w", err)
	}

	// Count and list nodes
	nodesDir := filepath.Join(instDir, "nodes")
	var nodes []string
	if nodeEntries, err := os.ReadDir(nodesDir); err == nil {
		for _, nodeEntry := range nodeEntries {
			if !nodeEntry.IsDir() && filepath.Ext(nodeEntry.Name()) == ".yaml" && nodeEntry.Name() != ".gitkeep" {
				nodes = append(nodes, nodeEntry.Name()[:len(nodeEntry.Name())-5]) // Remove .yaml extension
			}
		}
	}

	// Determine DNS configuration status
	dnsConfigured := platform.DNS.Domain != "" && platform.DNS.Provider != ""
	dnsStatus := DNSStatus{
		Provider:   platform.DNS.Provider,
		Domain:     platform.DNS.Domain,
		Configured: dnsConfigured,
	}

	// Build result
	s.result = &ShowResult{
		Platform: platform,
		Nodes:    nodes,
		DNS:      dnsStatus,
	}

	// Human-readable output
	term := s.Term()
	term.Printfln("Name:      %s", platform.Name)
	term.Printfln("Domain:    %s", platform.DNS.Domain)
	term.Printfln("Provider:  %s", platform.Infrastructure.MetalProvider)
	if platform.Infrastructure.API.URI != "" {
		term.Printfln("API:       %s", platform.Infrastructure.API.URI)
	}
	// DNS Status
	term.Printfln("DNS:")
	term.Printfln("  Provider:   %s", platform.DNS.Provider)
	if dnsConfigured {
		term.Printfln("  Status:     configured")
	} else {
		term.Printfln("  Status:     not configured")
	}
	if platform.Networking.PrivateNetwork != "" {
		term.Printfln("Network:   %s", platform.Networking.PrivateNetwork)
	}
	term.Printfln("Nodes:     %d", len(nodes))
	if len(nodes) > 0 {
		for _, node := range nodes {
			term.Printfln("  - %s", node)
		}
	}
	if len(platform.Pools) > 0 {
		term.Printfln("Pools:")
		for name, pool := range platform.Pools {
			term.Printfln("  - %s: %s x%d (%s)", name, pool.Machine, pool.Count, strings.Join(pool.Chassis, ", "))
		}
	}

	return nil
}

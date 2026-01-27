package show

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// Show implements the platform:show command
type Show struct {
	Log    *launchr.Logger
	Term   *launchr.Terminal
	Name   string
	Format string
}

func (s *Show) SetLogger(log *launchr.Logger) { s.Log = log }
func (s *Show) SetTerm(term *launchr.Terminal) { s.Term = term }

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

	// Output based on format
	switch strings.ToLower(s.Format) {
	case "json":
		output := map[string]interface{}{
			"platform": platform,
			"nodes":    nodes,
		}
		jsonData, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(jsonData))

	case "yaml":
		output := map[string]interface{}{
			"platform": platform,
			"nodes":    nodes,
		}
		yamlData, err := yaml.Marshal(output)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
		fmt.Println(string(yamlData))

	default: // human-readable sections
		fmt.Printf("Name:      %s\n", platform.Name)
		fmt.Printf("Domain:    %s\n", platform.DNS.Domain)
		fmt.Printf("Provider:  %s\n", platform.Infrastructure.MetalProvider)
		if platform.Infrastructure.API.URI != "" {
			fmt.Printf("API:       %s\n", platform.Infrastructure.API.URI)
		}
		if platform.DNS.Provider != "" && platform.DNS.Provider != platform.Infrastructure.MetalProvider {
			fmt.Printf("DNS:       %s\n", platform.DNS.Provider)
		}
		if platform.Networking.PrivateNetwork != "" {
			fmt.Printf("Network:   %s\n", platform.Networking.PrivateNetwork)
		}
		fmt.Printf("Nodes:     %d\n", len(nodes))
		if len(nodes) > 0 {
			for _, node := range nodes {
				fmt.Printf("  - %s\n", node)
			}
		}
		if len(platform.Chassis) > 0 {
			fmt.Println("Chassis:")
			for chassis, profiles := range platform.Chassis {
				for _, profile := range profiles {
					fmt.Printf("  - %s: %s x%d\n", chassis, profile.Type, profile.Count)
				}
			}
		}
	}

	return nil
}

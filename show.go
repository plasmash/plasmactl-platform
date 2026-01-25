package plasmactlplatform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-platform/pkg/types"
	"gopkg.in/yaml.v3"
)

// platformShow implements the platform:show command
type platformShow struct {
	log    *launchr.Logger
	term   *launchr.Terminal
	name   string
	format string
}

// SetLogger sets the logger for the action
func (a *platformShow) SetLogger(log *launchr.Logger) {
	a.log = log
}

// SetTerm sets the terminal for the action
func (a *platformShow) SetTerm(term *launchr.Terminal) {
	a.term = term
}

// Execute runs the platform:show action
func (a *platformShow) Execute() error {
	instDir := filepath.Join("inst", a.name)
	platformFile := filepath.Join(instDir, "platform.yaml")

	// Check if platform exists
	if _, err := os.Stat(platformFile); os.IsNotExist(err) {
		return fmt.Errorf("platform %q not found (no platform.yaml at %s)", a.name, platformFile)
	}

	// Read platform.yaml
	data, err := os.ReadFile(platformFile)
	if err != nil {
		return fmt.Errorf("failed to read platform.yaml: %w", err)
	}

	var platform types.Platform
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
	switch a.format {
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

	default: // table
		a.term.Info().Printfln("Platform: %s", platform.Name)
		a.term.Info().Println()
		a.term.Info().Println("Infrastructure:")
		a.term.Info().Printfln("  Metal Provider: %s", platform.Infrastructure.MetalProvider)
		if platform.Infrastructure.API.URI != "" {
			a.term.Info().Printfln("  API URI: %s", platform.Infrastructure.API.URI)
		}
		a.term.Info().Println()
		a.term.Info().Println("DNS:")
		a.term.Info().Printfln("  Provider: %s", platform.DNS.Provider)
		a.term.Info().Printfln("  Domain: %s", platform.DNS.Domain)
		a.term.Info().Println()
		a.term.Info().Println("Networking:")
		a.term.Info().Printfln("  Private Network: %s", platform.Networking.PrivateNetwork)
		a.term.Info().Println()
		a.term.Info().Printfln("Nodes: %d", len(nodes))
		for _, node := range nodes {
			a.term.Info().Printfln("  - %s", node)
		}

		// Show chassis configuration if present
		if len(platform.Chassis) > 0 {
			a.term.Info().Println()
			a.term.Info().Println("Chassis Configuration:")
			for chassis, profiles := range platform.Chassis {
				for _, profile := range profiles {
					a.term.Info().Printfln("  %s: %s x%d", chassis, profile.Type, profile.Count)
				}
			}
		}
	}

	return nil
}

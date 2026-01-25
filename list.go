package plasmactlplatform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-platform/pkg/types"
	"gopkg.in/yaml.v3"
)

// platformList implements the platform:list command
type platformList struct {
	log    *launchr.Logger
	term   *launchr.Terminal
	format string
}

// SetLogger sets the logger for the action
func (a *platformList) SetLogger(log *launchr.Logger) {
	a.log = log
}

// SetTerm sets the terminal for the action
func (a *platformList) SetTerm(term *launchr.Terminal) {
	a.term = term
}

// Execute runs the platform:list action
func (a *platformList) Execute() error {
	envDir := "env"

	// Check if env directory exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		a.term.Info().Println("No platforms found (inst/ directory does not exist)")
		return nil
	}

	// List all directories in inst/
	entries, err := os.ReadDir(envDir)
	if err != nil {
		return fmt.Errorf("failed to read env directory: %w", err)
	}

	var platforms []types.PlatformInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		platformFile := filepath.Join(envDir, entry.Name(), "platform.yaml")
		if _, err := os.Stat(platformFile); os.IsNotExist(err) {
			continue // Not a valid platform directory
		}

		// Read platform.yaml
		data, err := os.ReadFile(platformFile)
		if err != nil {
			a.log.Warn("Failed to read %s: %v", platformFile, err)
			continue
		}

		var platform types.Platform
		if err := yaml.Unmarshal(data, &platform); err != nil {
			a.log.Warn("Failed to parse %s: %v", platformFile, err)
			continue
		}

		// Count nodes
		nodesDir := filepath.Join(envDir, entry.Name(), "nodes")
		nodeCount := 0
		if nodeEntries, err := os.ReadDir(nodesDir); err == nil {
			for _, nodeEntry := range nodeEntries {
				if !nodeEntry.IsDir() && filepath.Ext(nodeEntry.Name()) == ".yaml" && nodeEntry.Name() != ".gitkeep" {
					nodeCount++
				}
			}
		}

		platforms = append(platforms, types.PlatformInfo{
			Name:          platform.Name,
			Domain:        platform.DNS.Domain,
			MetalProvider: platform.Infrastructure.MetalProvider,
			DNSProvider:   platform.DNS.Provider,
			NodeCount:     nodeCount,
		})
	}

	if len(platforms) == 0 {
		a.term.Info().Println("No platforms found")
		return nil
	}

	// Output based on format
	switch a.format {
	case "json":
		output, err := json.MarshalIndent(platforms, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(output))

	case "yaml":
		output, err := yaml.Marshal(platforms)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
		fmt.Println(string(output))

	default: // table
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tDOMAIN\tMETAL\tDNS\tNODES")
		fmt.Fprintln(w, "----\t------\t-----\t---\t-----")
		for _, p := range platforms {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", p.Name, p.Domain, p.MetalProvider, p.DNSProvider, p.NodeCount)
		}
		w.Flush()
	}

	return nil
}

package list

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// List implements the platform:list command
type List struct {
	Log    *launchr.Logger
	Term   *launchr.Terminal
	Format string
}

func (l *List) SetLogger(log *launchr.Logger) { l.Log = log }
func (l *List) SetTerm(term *launchr.Terminal) { l.Term = term }

func (l *List) Execute() error {
	instDir := "inst"

	// Check if inst directory exists
	if _, err := os.Stat(instDir); os.IsNotExist(err) {
		l.Term.Info().Println("No platforms found (inst/ directory does not exist)")
		return nil
	}

	// List all directories in inst/
	entries, err := os.ReadDir(instDir)
	if err != nil {
		return fmt.Errorf("failed to read inst directory: %w", err)
	}

	var platforms []schema.PlatformInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		platformFile := filepath.Join(instDir, entry.Name(), "platform.yaml")
		if _, err := os.Stat(platformFile); os.IsNotExist(err) {
			continue // Not a valid platform directory
		}

		// Read platform.yaml
		data, err := os.ReadFile(platformFile)
		if err != nil {
			l.Log.Warn("Failed to read %s: %v", platformFile, err)
			continue
		}

		var platform schema.Platform
		if err := yaml.Unmarshal(data, &platform); err != nil {
			l.Log.Warn("Failed to parse %s: %v", platformFile, err)
			continue
		}

		// Count nodes
		nodesDir := filepath.Join(instDir, entry.Name(), "nodes")
		nodeCount := 0
		if nodeEntries, err := os.ReadDir(nodesDir); err == nil {
			for _, nodeEntry := range nodeEntries {
				if !nodeEntry.IsDir() && filepath.Ext(nodeEntry.Name()) == ".yaml" && nodeEntry.Name() != ".gitkeep" {
					nodeCount++
				}
			}
		}

		platforms = append(platforms, schema.PlatformInfo{
			Name:          platform.Name,
			Domain:        platform.DNS.Domain,
			MetalProvider: platform.Infrastructure.MetalProvider,
			DNSProvider:   platform.DNS.Provider,
			NodeCount:     nodeCount,
		})
	}

	if len(platforms) == 0 {
		l.Term.Info().Println("No platforms found")
		return nil
	}

	// Output based on format
	switch strings.ToLower(l.Format) {
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
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "NAME\tDOMAIN\tPROVIDER\tNODES")
		for _, p := range platforms {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", p.Name, p.Domain, p.MetalProvider, p.NodeCount)
		}
		w.Flush()
	}

	return nil
}

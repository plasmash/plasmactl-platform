package list

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// ListResult is the structured output for platform:list
type ListResult struct {
	Platforms []schema.PlatformInfo `json:"platforms"`
}

// List implements the platform:list command
type List struct {
	action.WithLogger
	action.WithTerm

	result *ListResult
}

// Result returns the structured result for JSON output
func (l *List) Result() any {
	return l.result
}

func (l *List) Execute() error {
	instDir := "inst"

	// Initialize result early so --json always returns an object, never null
	l.result = &ListResult{Platforms: []schema.PlatformInfo{}}

	// Check if inst directory exists
	if _, err := os.Stat(instDir); os.IsNotExist(err) {
		l.Term().Info().Println("No platforms found (inst/ directory does not exist)")
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
			l.Log().Warn("Failed to read platform file", "path", platformFile, "error", err)
			continue
		}

		var platform schema.Platform
		if err := yaml.Unmarshal(data, &platform); err != nil {
			l.Log().Warn("Failed to parse platform file", "path", platformFile, "error", err)
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

	// Build result
	l.result = &ListResult{
		Platforms: platforms,
	}

	if len(platforms) == 0 {
		l.Term().Info().Println("No platforms found")
		return nil
	}

	// Flat output - one per line, scriptable
	term := l.Term()
	for _, p := range platforms {
		term.Printfln("%s", p.Name)
	}

	return nil
}

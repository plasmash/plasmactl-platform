package size

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-platform/pkg/graph"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// SizeResult is the structured output for platform:size
type SizeResult struct {
	Pools []PoolInfo `json:"pools"`
}

// PoolInfo represents a pool in structured output
type PoolInfo struct {
	Name       string   `json:"name"`
	Zones      []string `json:"zones"`
	Machine    string   `json:"machine"`
	Count      int      `json:"count"`
	Components []string `json:"components,omitempty"`
}

// Size implements the platform:size command
type Size struct {
	action.WithLogger
	action.WithTerm

	Name    string
	Suggest bool
	Add     string
	Remove  string
	Zones []string
	Machine string
	Count   int

	result *SizeResult
}

// Result returns the structured result for JSON output
func (s *Size) Result() any {
	return s.result
}

// Execute runs the platform:size action
func (s *Size) Execute() error {
	platformDir := filepath.Join("platforms", s.Name)
	platformFile := filepath.Join(platformDir, "platform.yaml")

	if _, err := os.Stat(platformDir); os.IsNotExist(err) {
		return fmt.Errorf("platform %q not found", s.Name)
	}

	platformData, err := os.ReadFile(platformFile)
	if err != nil {
		return fmt.Errorf("failed to read platform.yaml: %w", err)
	}

	var platform schema.Platform
	if err := yaml.Unmarshal(platformData, &platform); err != nil {
		return fmt.Errorf("failed to parse platform.yaml: %w", err)
	}

	switch {
	case s.Suggest:
		return s.suggest(platformFile, &platform)
	case s.Add != "":
		return s.addPool(platformFile, &platform)
	case s.Remove != "":
		return s.removePool(platformFile, &platform)
	default:
		return s.list(platform)
	}
}

// list shows current pools with optional graph-based impact info.
func (s *Size) list(platform schema.Platform) error {
	s.result = &SizeResult{}

	if len(platform.Pools) == 0 {
		s.Term().Info().Println("No pools configured")
		s.Term().Println()
		s.Term().Info().Printfln("Use --suggest to generate pools from the platform graph:")
		s.Term().Info().Printfln("  plasmactl platform:size %s --suggest", s.Name)
		return nil
	}

	g, err := graph.Load()
	if err != nil {
		s.Log().Debug("Graph not available, component details will be omitted", "error", err)
	}

	names := sortedKeys(platform.Pools)

	for _, name := range names {
		pool := platform.Pools[name]
		info := PoolInfo{
			Name:    name,
			Zones: pool.Zones,
			Machine: pool.Machine,
			Count:   pool.Count,
		}

		s.Term().Info().Printfln("Pool %q (%s x%d):", name, pool.Machine, pool.Count)
		for _, zone := range pool.Zones {
			s.Term().Printfln("  %s", zone)
			if g != nil {
				for _, e := range g.EdgesFrom(zone, "distributes") {
					s.Term().Printfln("    → %s (%s)", e.To().Name, e.To().Kind)
					info.Components = append(info.Components, e.To().Name)
				}
			}
		}
		s.Term().Println()

		s.result.Pools = append(s.result.Pools, info)
	}

	return nil
}

// suggest uses the platform graph to interactively create pool sizing.
func (s *Size) suggest(platformFile string, platform *schema.Platform) error {
	g, err := graph.Load()
	if err != nil {
		return fmt.Errorf("platform graph not available: %w (have you run package:compose?)", err)
	}

	s.Term().Info().Println("Analyzing platform graph...")
	s.Term().Println()

	zoneNodes := g.NodesByKind("zone")
	if len(zoneNodes) == 0 {
		return fmt.Errorf("no zones found in platform graph")
	}

	// Group zones by layer (second path segment)
	groups := make(map[string][]string)
	for _, cn := range zoneNodes {
		segments := strings.Split(cn.Name, ".")
		var groupKey string
		if len(segments) >= 3 {
			// Use layer + third segment for more specific grouping
			// e.g., "platform.foundation.cluster" → "foundation-cluster"
			groupKey = segments[1] + "-" + segments[2]
		} else if len(segments) >= 2 {
			groupKey = segments[1]
		} else {
			groupKey = cn.Name
		}
		groups[groupKey] = append(groups[groupKey], cn.Name)
	}

	groupNames := sortedKeys(groups)

	reader := bufio.NewReader(os.Stdin)
	pools := make(map[string]schema.Pool)

	s.Term().Info().Println("Suggested pools based on zone structure:")
	s.Term().Info().Println("For each pool, enter a machine type and count, or 'skip' to exclude.")
	s.Term().Println()

	for _, groupName := range groupNames {
		zoneList := groups[groupName]
		sort.Strings(zoneList)

		s.Term().Info().Printfln("Pool %q (%d zones):", groupName, len(zoneList))
		for _, c := range zoneList {
			s.Term().Printfln("  %s", c)
			for _, e := range g.EdgesFrom(c, "distributes") {
				s.Term().Printfln("    → %s (%s)", e.To().Name, e.To().Kind)
			}
		}
		s.Term().Println()

		// Ask for machine type
		s.Term().Print(fmt.Sprintf("  Machine type for %q (or 'skip'): ", groupName))
		machine, _ := reader.ReadString('\n')
		machine = strings.TrimSpace(machine)
		if machine == "skip" || machine == "" {
			s.Term().Println()
			continue
		}

		// Ask for count
		s.Term().Print(fmt.Sprintf("  Node count for %q: ", groupName))
		countStr, _ := reader.ReadString('\n')
		countStr = strings.TrimSpace(countStr)
		count, err := strconv.Atoi(countStr)
		if err != nil || count <= 0 {
			s.Term().Warning().Printfln("  Invalid count %q, skipping pool", countStr)
			s.Term().Println()
			continue
		}

		pools[groupName] = schema.Pool{
			Zones: zoneList,
			Machine: machine,
			Count:   count,
		}
		s.Term().Println()
	}

	if len(pools) == 0 {
		s.Term().Info().Println("No pools configured")
		s.result = &SizeResult{}
		return nil
	}

	// Show summary
	s.Term().Println()
	s.Term().Info().Println("Pool summary:")
	for _, name := range sortedKeys(pools) {
		pool := pools[name]
		s.Term().Info().Printfln("  %s: %s x%d (%d zones)", name, pool.Machine, pool.Count, len(pool.Zones))
	}
	s.Term().Println()

	// Confirm
	s.Term().Print("Write pools to platform.yaml? [Y/n]: ")
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "n" || answer == "no" {
		s.Term().Info().Println("Aborted")
		return nil
	}

	return s.writePools(platformFile, platform, pools)
}

// addPool adds a new pool to platform.yaml.
func (s *Size) addPool(platformFile string, platform *schema.Platform) error {
	if s.Machine == "" {
		return fmt.Errorf("--machine is required when adding a pool")
	}
	if s.Count <= 0 {
		return fmt.Errorf("--count must be greater than 0")
	}
	if len(s.Zones) == 0 {
		return fmt.Errorf("--zone is required when adding a pool")
	}

	if platform.Pools == nil {
		platform.Pools = make(map[string]schema.Pool)
	}

	if _, exists := platform.Pools[s.Add]; exists {
		return fmt.Errorf("pool %q already exists (remove it first or edit platform.yaml directly)", s.Add)
	}

	platform.Pools[s.Add] = schema.Pool{
		Zones: s.Zones,
		Machine: s.Machine,
		Count:   s.Count,
	}

	if err := s.writePlatform(platformFile, platform); err != nil {
		return err
	}

	s.Term().Success().Printfln("Added pool %q (%s x%d, %d zones)", s.Add, s.Machine, s.Count, len(s.Zones))

	s.result = &SizeResult{}
	for name, pool := range platform.Pools {
		s.result.Pools = append(s.result.Pools, PoolInfo{
			Name:    name,
			Zones: pool.Zones,
			Machine: pool.Machine,
			Count:   pool.Count,
		})
	}

	return nil
}

// removePool removes a pool from platform.yaml.
func (s *Size) removePool(platformFile string, platform *schema.Platform) error {
	if _, exists := platform.Pools[s.Remove]; !exists {
		return fmt.Errorf("pool %q not found", s.Remove)
	}

	delete(platform.Pools, s.Remove)

	if err := s.writePlatform(platformFile, platform); err != nil {
		return err
	}

	s.Term().Success().Printfln("Removed pool %q", s.Remove)

	s.result = &SizeResult{}
	for name, pool := range platform.Pools {
		s.result.Pools = append(s.result.Pools, PoolInfo{
			Name:    name,
			Zones: pool.Zones,
			Machine: pool.Machine,
			Count:   pool.Count,
		})
	}

	return nil
}

// writePools merges pools into the platform and writes to disk.
func (s *Size) writePools(platformFile string, platform *schema.Platform, pools map[string]schema.Pool) error {
	platform.Pools = pools

	if err := s.writePlatform(platformFile, platform); err != nil {
		return err
	}

	s.Term().Success().Printfln("Updated %s with %d pools", platformFile, len(pools))

	// Build result
	s.result = &SizeResult{}
	for name, pool := range pools {
		s.result.Pools = append(s.result.Pools, PoolInfo{
			Name:    name,
			Zones: pool.Zones,
			Machine: pool.Machine,
			Count:   pool.Count,
		})
	}

	return nil
}

func (s *Size) writePlatform(path string, platform *schema.Platform) error {
	data, err := yaml.Marshal(platform)
	if err != nil {
		return fmt.Errorf("failed to marshal platform.yaml: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write platform.yaml: %w", err)
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

package impact

import (
	"fmt"
	"sort"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-platform/pkg/graph"
)

// ImpactEntry represents an impact in a specific category.
type ImpactEntry struct {
	Category string   `json:"category"`
	Count    int      `json:"count"`
	Items    []string `json:"items"`
}

// ImpactResult is the structured output for platform:impact.
type ImpactResult struct {
	Name       string        `json:"name"`
	NodeType   string        `json:"node_type"`
	NodeKind   string        `json:"node_kind"`
	Direct     []ImpactEntry `json:"direct"`
	Transitive []ImpactEntry `json:"transitive"`
}

// Impact implements the platform:impact command.
type Impact struct {
	action.WithLogger
	action.WithTerm

	Identifier string

	result *ImpactResult
}

// Result returns the structured result for JSON output.
func (imp *Impact) Result() any {
	return imp.result
}

// Execute runs the platform:impact action.
func (imp *Impact) Execute() error {
	g, err := graph.Load()
	if err != nil {
		return fmt.Errorf("failed to load graph: %w", err)
	}

	node := g.Node(imp.Identifier)
	if node == nil {
		return fmt.Errorf("node %q not found in graph", imp.Identifier)
	}

	imp.result = &ImpactResult{
		Name:     node.Name,
		NodeType: node.Type,
		NodeKind: node.Kind,
	}

	switch node.Type {
	case "component":
		imp.analyzeComponent(g, node)
	case "variable":
		imp.analyzeVariable(g, node)
	case "zone":
		imp.analyzeZone(g, node)
	case "node":
		imp.analyzeNode(g, node)
	case "package":
		imp.analyzePackage(g, node)
	default:
		imp.analyzeGeneric(g, node)
	}

	return nil
}

func (imp *Impact) analyzeComponent(g *graph.PlatformGraph, node *graph.PlatformNode) {
	depEdges := graph.ComponentDependencyEdgeTypes()

	// Direct: components that depend on this one
	var directDeps []string
	for _, e := range g.EdgesTo(node.Name, depEdges...) {
		directDeps = append(directDeps, fmt.Sprintf("%s (%s)", e.From().Name, e.Type))
	}
	sort.Strings(directDeps)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Components depending on this",
		Count:    len(directDeps),
		Items:    directDeps,
	})

	// Direct: variables this parametrizes
	var vars []string
	for _, e := range g.EdgesTo(node.Name, "parametrizes") {
		vars = append(vars, e.From().Name)
	}
	sort.Strings(vars)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Variables this uses",
		Count:    len(vars),
		Items:    vars,
	})

	// Direct: zone attachment
	var zones []string
	for _, e := range g.EdgesTo(node.Name, "distributes") {
		zones = append(zones, e.From().Name)
	}
	sort.Strings(zones)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Attached to zone",
		Count:    len(zones),
		Items:    zones,
	})

	// Direct: nodes serving this component's zone
	nodeSet := make(map[string]bool)
	for _, ch := range zones {
		for _, e := range g.EdgesTo(ch, "allocates") {
			nodeSet[e.From().Name] = true
		}
	}
	var nodes []string
	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Served by nodes",
		Count:    len(nodes),
		Items:    nodes,
	})

	// Transitive: all ancestors via dependency edges
	ancestors := g.Ancestors(node.Name, 0, depEdges...)
	var transitiveComps []string
	for _, a := range ancestors {
		if a.Type == "component" {
			transitiveComps = append(transitiveComps, a.Name)
		}
	}
	sort.Strings(transitiveComps)
	imp.result.Transitive = append(imp.result.Transitive, ImpactEntry{
		Category: "Total affected components",
		Count:    len(transitiveComps),
		Items:    transitiveComps,
	})

	// Transitive: all zones affected
	transitiveZoneSet := make(map[string]bool)
	for _, comp := range transitiveComps {
		for _, e := range g.EdgesTo(comp, "distributes") {
			transitiveZoneSet[e.From().Name] = true
		}
	}
	for _, z := range zones {
		transitiveZoneSet[z] = true
	}
	var transitiveZones []string
	for z := range transitiveZoneSet {
		transitiveZones = append(transitiveZones, z)
	}
	sort.Strings(transitiveZones)
	imp.result.Transitive = append(imp.result.Transitive, ImpactEntry{
		Category: "Total affected zones",
		Count:    len(transitiveZones),
		Items:    transitiveZones,
	})
}

func (imp *Impact) analyzeVariable(g *graph.PlatformGraph, node *graph.PlatformNode) {
	// Direct: components that consume this variable
	var consumers []string
	for _, e := range g.EdgesFrom(node.Name, "parametrizes") {
		consumers = append(consumers, e.To().Name)
	}
	sort.Strings(consumers)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Components using this variable",
		Count:    len(consumers),
		Items:    consumers,
	})

	// Direct: components that define this variable
	var definers []string
	for _, e := range g.EdgesTo(node.Name, "defines") {
		definers = append(definers, e.From().Name)
	}
	sort.Strings(definers)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Defined by",
		Count:    len(definers),
		Items:    definers,
	})

	// Direct: components that override this variable
	var overriders []string
	for _, e := range g.EdgesTo(node.Name, "overrides") {
		overriders = append(overriders, e.From().Name)
	}
	sort.Strings(overriders)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Overridden by",
		Count:    len(overriders),
		Items:    overriders,
	})

	// Transitive: zones and nodes affected by consuming components
	zoneSet := make(map[string]bool)
	nodeSet := make(map[string]bool)
	for _, comp := range consumers {
		for _, e := range g.EdgesTo(comp, "distributes") {
			z := e.From().Name
			zoneSet[z] = true
			for _, ne := range g.EdgesTo(z, "allocates") {
				nodeSet[ne.From().Name] = true
			}
		}
	}

	var zoneList []string
	for z := range zoneSet {
		zoneList = append(zoneList, z)
	}
	sort.Strings(zoneList)
	imp.result.Transitive = append(imp.result.Transitive, ImpactEntry{
		Category: "Affected zones",
		Count:    len(zoneList),
		Items:    zoneList,
	})

	var nodeList []string
	for n := range nodeSet {
		nodeList = append(nodeList, n)
	}
	sort.Strings(nodeList)
	imp.result.Transitive = append(imp.result.Transitive, ImpactEntry{
		Category: "Affected nodes",
		Count:    len(nodeList),
		Items:    nodeList,
	})
}

func (imp *Impact) analyzeZone(g *graph.PlatformGraph, node *graph.PlatformNode) {
	// Direct: components attached
	var components []string
	for _, e := range g.EdgesFrom(node.Name, "distributes") {
		components = append(components, e.To().Name)
	}
	sort.Strings(components)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Components attached",
		Count:    len(components),
		Items:    components,
	})

	// Direct: nodes allocated
	var nodes []string
	for _, e := range g.EdgesTo(node.Name, "allocates") {
		nodes = append(nodes, e.From().Name)
	}
	sort.Strings(nodes)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Nodes allocated",
		Count:    len(nodes),
		Items:    nodes,
	})

	// Transitive: all components transitively depending on attached components
	depEdges := graph.ComponentDependencyEdgeTypes()
	transitiveSet := make(map[string]bool)
	for _, comp := range components {
		for _, a := range g.Ancestors(comp, 0, depEdges...) {
			if a.Type == "component" {
				transitiveSet[a.Name] = true
			}
		}
	}
	var transitive []string
	for name := range transitiveSet {
		transitive = append(transitive, name)
	}
	sort.Strings(transitive)
	imp.result.Transitive = append(imp.result.Transitive, ImpactEntry{
		Category: "Total affected components (transitive)",
		Count:    len(transitive),
		Items:    transitive,
	})
}

func (imp *Impact) analyzeNode(g *graph.PlatformGraph, node *graph.PlatformNode) {
	// Direct: zones this node serves
	var zonePaths []string
	for _, e := range g.EdgesFrom(node.Name, "allocates") {
		zonePaths = append(zonePaths, e.To().Name)
	}
	sort.Strings(zonePaths)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Zones served",
		Count:    len(zonePaths),
		Items:    zonePaths,
	})

	// Direct: zones that would have zero nodes if this node is removed
	var zeroNodePaths []string
	for _, zPath := range zonePaths {
		members := g.EdgesTo(zPath, "allocates")
		if len(members) == 1 {
			zeroNodePaths = append(zeroNodePaths, zPath)
		}
	}
	sort.Strings(zeroNodePaths)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Zones left with zero nodes if removed",
		Count:    len(zeroNodePaths),
		Items:    zeroNodePaths,
	})

	// Transitive: components that would lose infrastructure
	componentSet := make(map[string]bool)
	for _, zPath := range zeroNodePaths {
		for _, e := range g.EdgesFrom(zPath, "distributes") {
			componentSet[e.To().Name] = true
		}
	}
	var affectedComponents []string
	for comp := range componentSet {
		affectedComponents = append(affectedComponents, comp)
	}
	sort.Strings(affectedComponents)
	imp.result.Transitive = append(imp.result.Transitive, ImpactEntry{
		Category: "Components that would lose infrastructure",
		Count:    len(affectedComponents),
		Items:    affectedComponents,
	})
}

func (imp *Impact) analyzePackage(g *graph.PlatformGraph, node *graph.PlatformNode) {
	depEdges := graph.ComponentDependencyEdgeTypes()

	// Direct: components in this package
	var components []string
	for _, e := range g.EdgesFrom(node.Name, "contains") {
		if e.To().Type == "component" {
			components = append(components, e.To().Name)
		}
	}
	sort.Strings(components)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Components in package",
		Count:    len(components),
		Items:    components,
	})

	// Direct: external components that depend on components in this package
	externalDeps := make(map[string]bool)
	componentSet := make(map[string]bool)
	for _, c := range components {
		componentSet[c] = true
	}
	for _, comp := range components {
		for _, e := range g.EdgesTo(comp, depEdges...) {
			if !componentSet[e.From().Name] {
				externalDeps[fmt.Sprintf("%s depends on %s", e.From().Name, comp)] = true
			}
		}
	}
	var externalList []string
	for dep := range externalDeps {
		externalList = append(externalList, dep)
	}
	sort.Strings(externalList)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "External dependencies on this package",
		Count:    len(externalList),
		Items:    externalList,
	})

	// Transitive: all components affected by removing this package
	transitiveSet := make(map[string]bool)
	for _, comp := range components {
		for _, a := range g.Ancestors(comp, 0, depEdges...) {
			if a.Type == "component" && !componentSet[a.Name] {
				transitiveSet[a.Name] = true
			}
		}
	}
	var transitive []string
	for name := range transitiveSet {
		transitive = append(transitive, name)
	}
	sort.Strings(transitive)
	imp.result.Transitive = append(imp.result.Transitive, ImpactEntry{
		Category: "Total affected components outside package",
		Count:    len(transitive),
		Items:    transitive,
	})
}

func (imp *Impact) analyzeGeneric(g *graph.PlatformGraph, node *graph.PlatformNode) {
	// Generic: show all incoming and outgoing edges
	var incoming []string
	for _, e := range g.EdgesTo(node.Name) {
		incoming = append(incoming, fmt.Sprintf("%s (%s)", e.From().Name, e.Type))
	}
	sort.Strings(incoming)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Incoming edges",
		Count:    len(incoming),
		Items:    incoming,
	})

	var outgoing []string
	for _, e := range g.EdgesFrom(node.Name) {
		outgoing = append(outgoing, fmt.Sprintf("%s (%s)", e.To().Name, e.Type))
	}
	sort.Strings(outgoing)
	imp.result.Direct = append(imp.result.Direct, ImpactEntry{
		Category: "Outgoing edges",
		Count:    len(outgoing),
		Items:    outgoing,
	})
}

func (imp *Impact) PrintText() {
	r := imp.result

	imp.Term().Info().Printfln("Impact analysis for %s (%s/%s)", r.Name, r.NodeType, r.NodeKind)
	imp.Term().Info().Println()

	if len(r.Direct) > 0 {
		imp.Term().Info().Println("Direct impact:")
		for _, entry := range r.Direct {
			imp.Term().Printfln("  %s: %d", entry.Category, entry.Count)
			for _, item := range entry.Items {
				imp.Term().Printfln("    - %s", item)
			}
		}
	}

	if len(r.Transitive) > 0 {
		imp.Term().Info().Println()
		imp.Term().Info().Println("Transitive impact:")
		for _, entry := range r.Transitive {
			imp.Term().Printfln("  %s: %d", entry.Category, entry.Count)
			// Only show items if not too many
			if len(entry.Items) <= 20 {
				for _, item := range entry.Items {
					imp.Term().Printfln("    - %s", item)
				}
			} else {
				for _, item := range entry.Items[:10] {
					imp.Term().Printfln("    - %s", item)
				}
				imp.Term().Printfln("    ... and %d more", len(entry.Items)-10)
			}
		}
	}
}


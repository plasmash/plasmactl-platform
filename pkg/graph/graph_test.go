package graph

import (
	"encoding/json"
	"testing"
)

// Minimal graph fixture for unit tests (no binary needed).
func testGraph(t *testing.T) *PlatformGraph {
	t.Helper()
	raw := rawGraphResult{
		Nodes: []rawNode{
			{ID: "platform", Type: "platform", Kind: "platform"},
			{ID: "platform.foundation", Type: "chassis", Kind: "chassis", Layer: "foundation"},
			{ID: "platform.interaction", Type: "chassis", Kind: "chassis", Layer: "interaction"},
			{ID: "platform.cognition", Type: "chassis", Kind: "chassis", Layer: "cognition"},
			{ID: "interaction.applications.dashboards", Type: "component", Kind: "application", Layer: "interaction", Version: "abc123", Path: "/interaction/applications"},
			{ID: "interaction.services.dashboards_grafana", Type: "component", Kind: "service", Layer: "interaction"},
			{ID: "interaction.softwares.grafana", Type: "component", Kind: "software", Layer: "interaction"},
			{ID: "foundation.variables.domain", Type: "variable", Kind: "variable", Layer: "foundation"},
		},
		Edges: []rawEdge{
			{Source: "platform", Target: "platform.foundation", Type: "contains"},
			{Source: "platform", Target: "platform.interaction", Type: "contains"},
			{Source: "platform", Target: "platform.cognition", Type: "contains"},
			{Source: "interaction.applications.dashboards", Target: "platform.interaction", Type: "distributes"},
			{Source: "interaction.applications.dashboards", Target: "interaction.services.dashboards_grafana", Type: "requires"},
			{Source: "interaction.services.dashboards_grafana", Target: "interaction.softwares.grafana", Type: "requires"},
			{Source: "interaction.services.dashboards_grafana", Target: "foundation.variables.domain", Type: "parametrizes"},
		},
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	g, err := LoadFromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestNodeCount(t *testing.T) {
	g := testGraph(t)
	if got := g.NodeCount(); got != 8 {
		t.Errorf("NodeCount() = %d, want 8", got)
	}
}

func TestEdgeCount(t *testing.T) {
	g := testGraph(t)
	if got := g.EdgeCount(); got != 7 {
		t.Errorf("EdgeCount() = %d, want 7", got)
	}
}

func TestNodeLookup(t *testing.T) {
	g := testGraph(t)

	n := g.Node("interaction.applications.dashboards")
	if n == nil {
		t.Fatal("Node not found")
	}
	if n.Kind != "application" {
		t.Errorf("Kind = %q, want %q", n.Kind, "application")
	}
	if n.Layer != "interaction" {
		t.Errorf("Layer = %q, want %q", n.Layer, "interaction")
	}
	if n.Version != "abc123" {
		t.Errorf("Version = %q, want %q", n.Version, "abc123")
	}

	if g.Node("nonexistent") != nil {
		t.Error("expected nil for nonexistent node")
	}
}

func TestNodesByKind(t *testing.T) {
	g := testGraph(t)

	chassis := g.NodesByKind("chassis")
	if len(chassis) != 3 {
		t.Errorf("NodesByKind(chassis) = %d, want 3", len(chassis))
	}

	apps := g.NodesByKind("application")
	if len(apps) != 1 {
		t.Errorf("NodesByKind(application) = %d, want 1", len(apps))
	}
}

func TestNodesByType(t *testing.T) {
	g := testGraph(t)

	components := g.NodesByType("component")
	if len(components) != 3 {
		t.Errorf("NodesByType(component) = %d, want 3", len(components))
	}
}

func TestEdgesFrom(t *testing.T) {
	g := testGraph(t)

	// All edges from platform
	edges := g.EdgesFrom("platform")
	if len(edges) != 3 {
		t.Errorf("EdgesFrom(platform) = %d, want 3", len(edges))
	}

	// Filtered by type
	contains := g.EdgesFrom("platform", "contains")
	if len(contains) != 3 {
		t.Errorf("EdgesFrom(platform, contains) = %d, want 3", len(contains))
	}

	// No edges of this type
	attaches := g.EdgesFrom("platform", "distributes")
	if len(attaches) != 0 {
		t.Errorf("EdgesFrom(platform, attaches) = %d, want 0", len(attaches))
	}

	// Nonexistent node
	if edges := g.EdgesFrom("nonexistent"); edges != nil {
		t.Error("expected nil for nonexistent node")
	}
}

func TestEdgesTo(t *testing.T) {
	g := testGraph(t)

	edges := g.EdgesTo("platform.interaction")
	if len(edges) != 2 {
		t.Errorf("EdgesTo(platform.interaction) = %d, want 2", len(edges))
	}

	contains := g.EdgesTo("platform.interaction", "contains")
	if len(contains) != 1 {
		t.Errorf("EdgesTo(platform.interaction, contains) = %d, want 1", len(contains))
	}
}

func TestDescendants(t *testing.T) {
	g := testGraph(t)

	// Depth 1 from platform (contains edges only)
	desc := g.Descendants("platform", 1, "contains")
	if len(desc) != 3 {
		t.Errorf("Descendants(platform, 1, contains) = %d, want 3", len(desc))
	}

	// Unlimited depth from dashboards (all reachable)
	desc = g.Descendants("interaction.applications.dashboards", 0)
	// Should reach: platform.interaction, dashboards_grafana, grafana, domain
	if len(desc) != 4 {
		t.Errorf("Descendants(dashboards, unlimited) = %d, want 4", len(desc))
	}

	// Nonexistent node
	if desc := g.Descendants("nonexistent", 0); desc != nil {
		t.Error("expected nil for nonexistent node")
	}
}

func TestAncestors(t *testing.T) {
	g := testGraph(t)

	// Who depends on grafana software?
	anc := g.Ancestors("interaction.softwares.grafana", 0)
	// dashboards_grafana → dashboards
	if len(anc) != 2 {
		t.Errorf("Ancestors(grafana, unlimited) = %d, want 2", len(anc))
	}

	// Depth-limited ancestors
	anc = g.Ancestors("interaction.softwares.grafana", 1)
	if len(anc) != 1 {
		t.Errorf("Ancestors(grafana, 1) = %d, want 1", len(anc))
	}
}

func TestShortestPath(t *testing.T) {
	g := testGraph(t)

	p := g.ShortestPath("interaction.applications.dashboards", "interaction.softwares.grafana")
	if p == nil {
		t.Fatal("expected path, got nil")
	}
	if len(p) != 3 {
		t.Errorf("ShortestPath length = %d, want 3", len(p))
	}
	if p[0].Name != "interaction.applications.dashboards" {
		t.Errorf("path[0] = %q, want dashboards", p[0].Name)
	}
	if p[len(p)-1].Name != "interaction.softwares.grafana" {
		t.Errorf("path[-1] = %q, want grafana", p[len(p)-1].Name)
	}

	// No path
	if p := g.ShortestPath("platform.foundation", "platform"); p != nil {
		t.Errorf("expected no path from foundation to platform, got %d nodes", len(p))
	}

	// Nonexistent nodes
	if p := g.ShortestPath("nonexistent", "platform"); p != nil {
		t.Error("expected nil for nonexistent source")
	}
}

func TestLoadWithoutBinary(t *testing.T) {
	binaryPath = ""
	_, err := Load()
	if err == nil {
		t.Error("expected error when binary not registered")
	}
}

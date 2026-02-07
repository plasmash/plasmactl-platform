// Package graph provides a shared platform dependency graph for plasmactl plugins.
// It executes the embedded Rust graph binary, parses its JSON output, and exposes
// typed query methods backed by gonum's directed graph.
package graph

import (
	"encoding/json"
	"fmt"
	"os/exec"

	gonumgraph "gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/path"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/traverse"
)

// PlatformNode represents a node in the platform dependency graph.
type PlatformNode struct {
	uid     int64
	Name    string // e.g. "interaction.applications.dashboards"
	Type    string // "component", "chassis", "variable", "node", "platform", "model", "package"
	Kind    string // "application", "service", "chassis", etc.
	Layer   string // "interaction", "foundation", etc. (may be empty)
	Version string // git commit hash (may be empty)
	Path    string // filesystem path (may be empty)
}

// ID implements gonum graph.Node.
func (n *PlatformNode) ID() int64 { return n.uid }

// PlatformEdge represents a directed edge in the platform dependency graph.
type PlatformEdge struct {
	from, to *PlatformNode
	Type     string // "attaches", "contains", "parametrizes", etc.
}

// From returns the source node.
func (e *PlatformEdge) From() *PlatformNode { return e.from }

// To returns the target node.
func (e *PlatformEdge) To() *PlatformNode { return e.to }

// PlatformGraph holds the parsed platform dependency graph.
type PlatformGraph struct {
	g      *simple.DirectedGraph
	byName map[string]*PlatformNode
	byID   map[int64]*PlatformNode
	edges  map[[2]int64]*PlatformEdge
	byKind map[string][]*PlatformNode
	byType map[string][]*PlatformNode
}

// Package-level state — set by the platform plugin at init time.
var binaryPath string

// SetBinaryPath is called by the platform plugin during OnAppInit.
func SetBinaryPath(p string) { binaryPath = p }

// Load executes the graph binary and builds the gonum graph.
// The binary uses its default --compose-dir (.plasma/model/compose/merged)
// relative to the current working directory.
func Load() (*PlatformGraph, error) {
	if binaryPath == "" {
		return nil, fmt.Errorf("graph binary not registered (plasmactl-platform plugin not loaded?)")
	}
	cmd := exec.Command(binaryPath, "", "--format=json")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("graph binary failed: %w\nstderr: %s", err, exitErr.Stderr)
		}
		return nil, fmt.Errorf("graph binary failed: %w", err)
	}

	var raw rawGraphResult
	if err = json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse graph JSON: %w", err)
	}

	return buildGraph(&raw)
}

// LoadFromJSON builds the graph from pre-existing JSON bytes (e.g. for tests).
func LoadFromJSON(data []byte) (*PlatformGraph, error) {
	var raw rawGraphResult
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse graph JSON: %w", err)
	}
	return buildGraph(&raw)
}

func buildGraph(raw *rawGraphResult) (*PlatformGraph, error) {
	pg := &PlatformGraph{
		g:      simple.NewDirectedGraph(),
		byName: make(map[string]*PlatformNode, len(raw.Nodes)),
		byID:   make(map[int64]*PlatformNode, len(raw.Nodes)),
		edges:  make(map[[2]int64]*PlatformEdge, len(raw.Edges)),
		byKind: make(map[string][]*PlatformNode),
		byType: make(map[string][]*PlatformNode),
	}

	for i, rn := range raw.Nodes {
		n := &PlatformNode{
			uid:     int64(i),
			Name:    rn.ID,
			Type:    rn.Type,
			Kind:    rn.Kind,
			Layer:   rn.Layer,
			Version: rn.Version,
			Path:    rn.Path,
		}
		pg.g.AddNode(n)
		pg.byName[n.Name] = n
		pg.byID[n.uid] = n
		pg.byKind[n.Kind] = append(pg.byKind[n.Kind], n)
		pg.byType[n.Type] = append(pg.byType[n.Type], n)
	}

	for _, re := range raw.Edges {
		fromNode := pg.byName[re.Source]
		toNode := pg.byName[re.Target]
		if fromNode == nil || toNode == nil {
			continue
		}
		key := [2]int64{fromNode.uid, toNode.uid}
		if _, exists := pg.edges[key]; exists {
			continue
		}
		e := &PlatformEdge{
			from: fromNode,
			to:   toNode,
			Type: re.Type,
		}
		pg.edges[key] = e
		pg.g.SetEdge(simple.Edge{F: fromNode, T: toNode})
	}

	return pg, nil
}

// Node returns a node by name, or nil if not found.
func (g *PlatformGraph) Node(name string) *PlatformNode {
	return g.byName[name]
}

// NodesByKind returns all nodes with the given kind (e.g. "application", "chassis").
func (g *PlatformGraph) NodesByKind(kind string) []*PlatformNode {
	return g.byKind[kind]
}

// NodesByType returns all nodes with the given type (e.g. "component", "variable").
func (g *PlatformGraph) NodesByType(typ string) []*PlatformNode {
	return g.byType[typ]
}

// EdgesFrom returns edges originating from the named node, optionally filtered by edge type.
func (g *PlatformGraph) EdgesFrom(name string, edgeTypes ...string) []*PlatformEdge {
	n := g.byName[name]
	if n == nil {
		return nil
	}
	filter := makeFilter(edgeTypes)
	var result []*PlatformEdge
	it := g.g.From(n.uid)
	for it.Next() {
		key := [2]int64{n.uid, it.Node().ID()}
		if e, ok := g.edges[key]; ok {
			if filter == nil || filter[e.Type] {
				result = append(result, e)
			}
		}
	}
	return result
}

// EdgesTo returns edges pointing to the named node, optionally filtered by edge type.
func (g *PlatformGraph) EdgesTo(name string, edgeTypes ...string) []*PlatformEdge {
	n := g.byName[name]
	if n == nil {
		return nil
	}
	filter := makeFilter(edgeTypes)
	var result []*PlatformEdge
	it := g.g.To(n.uid)
	for it.Next() {
		key := [2]int64{it.Node().ID(), n.uid}
		if e, ok := g.edges[key]; ok {
			if filter == nil || filter[e.Type] {
				result = append(result, e)
			}
		}
	}
	return result
}

// Descendants returns nodes reachable from the named node via BFS, up to depth levels.
// Use depth <= 0 for unlimited. Optionally filter by edge types.
func (g *PlatformGraph) Descendants(name string, depth int, edgeTypes ...string) []*PlatformNode {
	n := g.byName[name]
	if n == nil {
		return nil
	}
	filter := makeFilter(edgeTypes)
	var result []*PlatformNode
	currentDepth := map[int64]int{n.uid: 0}

	w := traverse.BreadthFirst{
		Traverse: func(e gonumgraph.Edge) bool {
			if depth > 0 {
				d := currentDepth[e.From().ID()]
				if d >= depth {
					return false
				}
			}
			if filter != nil {
				key := [2]int64{e.From().ID(), e.To().ID()}
				pe, ok := g.edges[key]
				if !ok || !filter[pe.Type] {
					return false
				}
			}
			return true
		},
	}
	w.Walk(g.g, n, func(node gonumgraph.Node, d int) bool {
		if node.ID() != n.uid {
			if pn, ok := g.byID[node.ID()]; ok {
				result = append(result, pn)
			}
		}
		currentDepth[node.ID()] = d
		return false
	})
	return result
}

// Ancestors returns nodes that can reach the named node via reverse BFS, up to depth levels.
// Use depth <= 0 for unlimited. Optionally filter by edge types.
func (g *PlatformGraph) Ancestors(name string, depth int, edgeTypes ...string) []*PlatformNode {
	n := g.byName[name]
	if n == nil {
		return nil
	}
	filter := makeFilter(edgeTypes)
	rev := simple.NewDirectedGraph()

	// Build reverse graph (only edges matching filter)
	nodeIt := g.g.Nodes()
	for nodeIt.Next() {
		rev.AddNode(simple.Node(nodeIt.Node().ID()))
	}
	for key, e := range g.edges {
		if filter != nil && !filter[e.Type] {
			continue
		}
		rev.SetEdge(simple.Edge{
			F: simple.Node(key[1]),
			T: simple.Node(key[0]),
		})
	}

	var result []*PlatformNode
	currentDepth := map[int64]int{n.uid: 0}

	w := traverse.BreadthFirst{
		Traverse: func(e gonumgraph.Edge) bool {
			if depth > 0 {
				d := currentDepth[e.From().ID()]
				if d >= depth {
					return false
				}
			}
			return true
		},
	}
	w.Walk(rev, simple.Node(n.uid), func(node gonumgraph.Node, d int) bool {
		if node.ID() != n.uid {
			if pn, ok := g.byID[node.ID()]; ok {
				result = append(result, pn)
			}
		}
		currentDepth[node.ID()] = d
		return false
	})
	return result
}

// ShortestPath returns the shortest path between two named nodes, or nil if no path exists.
func (g *PlatformGraph) ShortestPath(from, to string) []*PlatformNode {
	fromNode := g.byName[from]
	toNode := g.byName[to]
	if fromNode == nil || toNode == nil {
		return nil
	}
	pt := path.DijkstraFrom(fromNode, g.g)
	nodes, _ := pt.To(toNode.uid)
	if len(nodes) == 0 {
		return nil
	}
	result := make([]*PlatformNode, len(nodes))
	for i, gn := range nodes {
		result[i] = g.byID[gn.ID()]
	}
	return result
}

// NodeCount returns the total number of nodes.
func (g *PlatformGraph) NodeCount() int {
	return g.g.Nodes().Len()
}

// EdgeCount returns the total number of edges.
func (g *PlatformGraph) EdgeCount() int {
	return len(g.edges)
}

// AllEdges returns all edges in the graph.
func (g *PlatformGraph) AllEdges() []*PlatformEdge {
	result := make([]*PlatformEdge, 0, len(g.edges))
	for _, e := range g.edges {
		result = append(result, e)
	}
	return result
}

// AllNodes returns all nodes in the graph.
func (g *PlatformGraph) AllNodes() []*PlatformNode {
	result := make([]*PlatformNode, 0, len(g.byName))
	for _, n := range g.byName {
		result = append(result, n)
	}
	return result
}

// dependencyEdgeTypes are edge types that represent component-to-component dependencies.
var dependencyEdgeTypes = map[string]bool{
	"orchestrates": true,
	"configures":   true,
	"computes":     true,
	"implements":   true,
	"builds":       true,
	"uses":         true,
	"depends":      true,
}

// IsDependencyEdge returns true if the edge type represents a component dependency.
func IsDependencyEdge(edgeType string) bool {
	return dependencyEdgeTypes[edgeType]
}

// FindCycles detects cycles in the subgraph formed by the given edge types.
// Returns a list of cycles, where each cycle is a list of node names.
func (g *PlatformGraph) FindCycles(edgeTypes ...string) [][]string {
	filter := makeFilter(edgeTypes)

	// Build adjacency list for the filtered subgraph
	adj := make(map[int64][]int64)
	for key, e := range g.edges {
		if filter != nil && !filter[e.Type] {
			continue
		}
		adj[key[0]] = append(adj[key[0]], key[1])
	}

	const (
		white = 0 // unvisited
		gray  = 1 // in current path
		black = 2 // fully processed
	)

	color := make(map[int64]int)
	parent := make(map[int64]int64)
	var cycles [][]string

	var dfs func(u int64)
	dfs = func(u int64) {
		color[u] = gray
		for _, v := range adj[u] {
			if color[v] == gray {
				// Back edge found — extract cycle
				cycle := []string{g.byID[v].Name}
				cur := u
				for cur != v {
					cycle = append(cycle, g.byID[cur].Name)
					cur = parent[cur]
				}
				// Reverse to get forward order
				for i, j := 0, len(cycle)-1; i < j; i, j = i+1, j-1 {
					cycle[i], cycle[j] = cycle[j], cycle[i]
				}
				cycles = append(cycles, cycle)
			} else if color[v] == white {
				parent[v] = u
				dfs(v)
			}
		}
		color[u] = black
	}

	for id := range g.byID {
		if color[id] == white {
			dfs(id)
		}
	}

	return cycles
}

// ComponentDependencyEdgeTypes returns the standard edge types for component dependencies.
func ComponentDependencyEdgeTypes() []string {
	return []string{"orchestrates", "configures", "computes", "implements", "builds", "uses", "depends"}
}

func makeFilter(edgeTypes []string) map[string]bool {
	if len(edgeTypes) == 0 {
		return nil
	}
	m := make(map[string]bool, len(edgeTypes))
	for _, t := range edgeTypes {
		m[t] = true
	}
	return m
}

// Internal JSON structs for unmarshaling the Rust binary output.
type rawGraphResult struct {
	Nodes []rawNode `json:"nodes"`
	Edges []rawEdge `json:"edges"`
}

type rawNode struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Kind    string `json:"kind"`
	Layer   string `json:"layer,omitempty"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
}

type rawEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

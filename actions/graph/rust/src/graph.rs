use std::collections::{BTreeMap, HashMap, HashSet, VecDeque};

use petgraph::graph::{DiGraph, NodeIndex};
use petgraph::visit::EdgeRef;
use serde_json::{json, Value};

use crate::types::{guess_kind, is_versioned, layer_names};

#[derive(Clone, Debug)]
pub struct Node {
    pub name: String,
    pub kind: String,
    #[allow(dead_code)]
    pub versioned: bool,
    pub version: Option<String>,
    pub path: Option<String>,
    pub attrs: HashMap<String, String>,
}

pub struct PlatformGraph {
    pub graph: DiGraph<Node, String>,
    node_indices: HashMap<String, NodeIndex>,
    edge_set: HashSet<(NodeIndex, NodeIndex, String)>,
}

impl PlatformGraph {
    pub fn new() -> Self {
        Self {
            graph: DiGraph::new(),
            node_indices: HashMap::new(),
            edge_set: HashSet::new(),
        }
    }

    pub fn add_node(&mut self, node: Node) -> NodeIndex {
        if let Some(&idx) = self.node_indices.get(&node.name) {
            let existing = &mut self.graph[idx];
            if node.version.is_some() && existing.version.is_none() {
                existing.version = node.version;
            }
            if node.path.is_some() && existing.path.is_none() {
                existing.path = node.path;
            }
            for (k, v) in node.attrs {
                existing.attrs.entry(k).or_insert(v);
            }
            return idx;
        }
        let name = node.name.clone();
        let idx = self.graph.add_node(node);
        self.node_indices.insert(name, idx);
        idx
    }

    pub fn add_edge(&mut self, source: &str, target: &str, edge_type: &str) {
        let source_idx = self.ensure_node(source);
        let target_idx = self.ensure_node(target);
        let key = (source_idx, target_idx, edge_type.to_string());
        if self.edge_set.insert(key) {
            self.graph.add_edge(source_idx, target_idx, edge_type.to_string());
        }
    }

    fn ensure_node(&mut self, name: &str) -> NodeIndex {
        if let Some(&idx) = self.node_indices.get(name) {
            return idx;
        }
        let kind = guess_kind(name);
        self.add_node(Node {
            name: name.to_string(),
            kind: kind.to_string(),
            versioned: is_versioned(kind),
            version: None,
            path: None,
            attrs: HashMap::new(),
        })
    }

    pub fn get_node(&self, name: &str) -> Option<&Node> {
        self.node_indices
            .get(name)
            .map(|&idx| &self.graph[idx])
    }

    pub fn has_node(&self, name: &str) -> bool {
        self.node_indices.contains_key(name)
    }

    pub fn node_count(&self) -> usize {
        self.graph.node_count()
    }

    pub fn edge_count(&self) -> usize {
        self.graph.edge_count()
    }

    /// Get all descendants of a node (forward dependencies).
    pub fn descendants(&self, name: &str, depth: i32) -> HashSet<String> {
        let Some(&start) = self.node_indices.get(name) else {
            return HashSet::new();
        };
        if depth == -1 {
            self.bounded_descendants(start)
        } else {
            self.limited_descendants(start, depth as usize)
        }
    }

    fn bounded_descendants(&self, start: NodeIndex) -> HashSet<String> {
        let mut result = HashSet::new();
        let mut queue = VecDeque::new();
        queue.push_back(start);
        let mut visited = HashSet::new();
        visited.insert(start);
        while let Some(idx) = queue.pop_front() {
            for succ in self.graph.neighbors(idx) {
                if visited.insert(succ) {
                    result.insert(self.graph[succ].name.clone());
                    queue.push_back(succ);
                }
            }
        }
        result
    }

    fn limited_descendants(&self, start: NodeIndex, max_depth: usize) -> HashSet<String> {
        let mut result = HashSet::new();
        let mut current_level: HashSet<NodeIndex> = [start].into();
        for _ in 0..max_depth {
            let mut next_level = HashSet::new();
            for &idx in &current_level {
                for succ in self.graph.neighbors(idx) {
                    let name = &self.graph[succ].name;
                    if result.insert(name.clone()) {
                        next_level.insert(succ);
                    }
                }
            }
            if next_level.is_empty() {
                break;
            }
            current_level = next_level;
        }
        result
    }

    /// Get all ancestors of a node (reverse dependencies).
    pub fn ancestors(&self, name: &str, depth: i32) -> HashSet<String> {
        let Some(&start) = self.node_indices.get(name) else {
            return HashSet::new();
        };
        if depth == -1 {
            self.bounded_ancestors(start)
        } else {
            self.limited_ancestors(start, depth as usize)
        }
    }

    fn bounded_ancestors(&self, start: NodeIndex) -> HashSet<String> {
        let mut result = HashSet::new();
        let mut queue = VecDeque::new();
        queue.push_back(start);
        let mut visited = HashSet::new();
        visited.insert(start);
        while let Some(idx) = queue.pop_front() {
            for pred in self
                .graph
                .neighbors_directed(idx, petgraph::Direction::Incoming)
            {
                if visited.insert(pred) {
                    result.insert(self.graph[pred].name.clone());
                    queue.push_back(pred);
                }
            }
        }
        result
    }

    fn limited_ancestors(&self, start: NodeIndex, max_depth: usize) -> HashSet<String> {
        let mut result = HashSet::new();
        let mut current_level: HashSet<NodeIndex> = [start].into();
        for _ in 0..max_depth {
            let mut next_level = HashSet::new();
            for &idx in &current_level {
                for pred in self
                    .graph
                    .neighbors_directed(idx, petgraph::Direction::Incoming)
                {
                    let name = &self.graph[pred].name;
                    if result.insert(name.clone()) {
                        next_level.insert(pred);
                    }
                }
            }
            if next_level.is_empty() {
                break;
            }
            current_level = next_level;
        }
        result
    }

    /// Get edges originating from a node: Vec<(edge_type, target_name)>.
    pub fn edges_from(&self, name: &str) -> Vec<(String, String)> {
        let Some(&idx) = self.node_indices.get(name) else {
            return Vec::new();
        };
        let mut edges = Vec::new();
        for edge in self.graph.edges(idx) {
            edges.push((edge.weight().clone(), self.graph[edge.target()].name.clone()));
        }
        edges
    }

    /// Get edges pointing to a node: Vec<(edge_type, source_name)>.
    pub fn edges_to(&self, name: &str) -> Vec<(String, String)> {
        let Some(&idx) = self.node_indices.get(name) else {
            return Vec::new();
        };
        let mut edges = Vec::new();
        for edge in self
            .graph
            .edges_directed(idx, petgraph::Direction::Incoming)
        {
            edges.push((edge.weight().clone(), self.graph[edge.source()].name.clone()));
        }
        edges
    }

    /// Export graph or subgraph as JSON value matching the result schema.
    pub fn to_dict(&self, query: Option<&str>, reverse: bool, depth: i32) -> Value {
        let node_names: HashSet<String> = if let Some(q) = query {
            let mut set = if reverse {
                self.ancestors(q, depth)
            } else {
                self.descendants(q, depth)
            };
            set.insert(q.to_string());
            set
        } else {
            self.node_indices.keys().cloned().collect()
        };

        let layers = layer_names();
        let mut nodes = Vec::new();
        let sorted_names: BTreeMap<&str, ()> =
            node_names.iter().map(|n| (n.as_str(), ())).collect();

        for (name, _) in &sorted_names {
            if let Some(node) = self.get_node(name) {
                let node_type = if is_versioned(&node.kind) {
                    "component"
                } else {
                    &node.kind
                };
                let mut entry = json!({
                    "id": node.name,
                    "type": node_type,
                    "kind": node.kind,
                });
                let parts: Vec<&str> = node.name.splitn(3, '.').collect();
                if parts.len() >= 2 && layers.contains(parts[0]) {
                    entry["layer"] = json!(parts[0]);
                }
                if let Some(ref v) = node.version {
                    entry["version"] = json!(v);
                }
                if let Some(ref p) = node.path {
                    entry["path"] = json!(p);
                }
                nodes.push(entry);
            }
        }

        // Targeted edge iteration: only visit edges of nodes in the subgraph
        let mut edges = Vec::new();
        let mut seen_edges: HashSet<(String, String, String)> = HashSet::new();
        for name in &node_names {
            if let Some(&idx) = self.node_indices.get(name.as_str()) {
                for edge in self.graph.edges(idx) {
                    let target = &self.graph[edge.target()].name;
                    if !node_names.contains(target) {
                        continue;
                    }
                    let key = (name.clone(), target.clone(), edge.weight().clone());
                    if seen_edges.insert(key) {
                        edges.push(json!({
                            "source": name,
                            "target": target,
                            "type": edge.weight(),
                        }));
                    }
                }
            }
        }

        json!({
            "query": {
                "node": query.unwrap_or(""),
                "reverse": reverse,
                "depth": depth,
            },
            "nodes": nodes,
            "edges": edges,
            "stats": {
                "total_nodes": nodes.len(),
                "total_edges": edges.len(),
            },
        })
    }

    /// Compute the subgraph node set for a query (or all nodes).
    fn subgraph_indices(&self, query: Option<&str>, reverse: bool, depth: i32) -> HashSet<NodeIndex> {
        if let Some(q) = query {
            let names = if reverse {
                let mut s = self.ancestors(q, depth);
                s.insert(q.to_string());
                s
            } else {
                let mut s = self.descendants(q, depth);
                s.insert(q.to_string());
                s
            };
            names
                .iter()
                .filter_map(|n| self.node_indices.get(n.as_str()).copied())
                .collect()
        } else {
            self.graph.node_indices().collect()
        }
    }

    /// Export graph or subgraph directly to GraphML, iterating petgraph nodes/edges.
    pub fn to_graphml(&self, query: Option<&str>, reverse: bool, depth: i32, positions: Option<&[(f64, f64)]>) -> String {
        let sub = self.subgraph_indices(query, reverse, depth);
        let layers = layer_names();

        let mut out = String::with_capacity(64 * 1024);
        out.push_str("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n\
            <graphml xmlns=\"http://graphml.graphdrawing.org/graphml\"\n\
            \x20        xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\"\n\
            \x20        xsi:schemaLocation=\"http://graphml.graphdrawing.org/graphml http://graphml.graphdrawing.org/xmlns/1.0/graphml.xsd\">\n");

        // Key declarations
        out.push_str("  <key id=\"label\" for=\"node\" attr.name=\"label\" attr.type=\"string\"/>\n");
        out.push_str("  <key id=\"kind\" for=\"node\" attr.name=\"kind\" attr.type=\"string\"/>\n");
        out.push_str("  <key id=\"type\" for=\"node\" attr.name=\"type\" attr.type=\"string\"/>\n");
        out.push_str("  <key id=\"layer\" for=\"node\" attr.name=\"layer\" attr.type=\"string\"/>\n");
        out.push_str("  <key id=\"version\" for=\"node\" attr.name=\"version\" attr.type=\"string\"/>\n");
        out.push_str("  <key id=\"x\" for=\"node\" attr.name=\"x\" attr.type=\"double\"/>\n");
        out.push_str("  <key id=\"y\" for=\"node\" attr.name=\"y\" attr.type=\"double\"/>\n");
        out.push_str("  <key id=\"r\" for=\"node\" attr.name=\"r\" attr.type=\"int\"/>\n");
        out.push_str("  <key id=\"g\" for=\"node\" attr.name=\"g\" attr.type=\"int\"/>\n");
        out.push_str("  <key id=\"b\" for=\"node\" attr.name=\"b\" attr.type=\"int\"/>\n");
        out.push_str("  <key id=\"edge_type\" for=\"edge\" attr.name=\"type\" attr.type=\"string\"/>\n");
        out.push_str("  <graph id=\"platform\" edgedefault=\"directed\">\n");

        // Nodes — sorted for deterministic output
        let mut sorted: Vec<NodeIndex> = sub.iter().copied().collect();
        sorted.sort_by(|a, b| self.graph[*a].name.cmp(&self.graph[*b].name));

        for &idx in &sorted {
            let node = &self.graph[idx];
            let ntype = if is_versioned(&node.kind) { "component" } else { &node.kind };

            out.push_str("    <node id=\"");
            xml_escape_into(&mut out, &node.name);
            out.push_str("\">\n");

            // Label: short name (last dot-segment)
            let label = node.name.rsplit('.').next().unwrap_or(&node.name);
            out.push_str("      <data key=\"label\">");
            xml_escape_into(&mut out, label);
            out.push_str("</data>\n");

            out.push_str("      <data key=\"kind\">");
            xml_escape_into(&mut out, &node.kind);
            out.push_str("</data>\n      <data key=\"type\">");
            xml_escape_into(&mut out, ntype);
            out.push_str("</data>\n");

            let parts: Vec<&str> = node.name.splitn(3, '.').collect();
            if parts.len() >= 2 && layers.contains(parts[0]) {
                out.push_str("      <data key=\"layer\">");
                out.push_str(parts[0]);
                out.push_str("</data>\n");
            }
            if let Some(ref v) = node.version {
                out.push_str("      <data key=\"version\">");
                xml_escape_into(&mut out, v);
                out.push_str("</data>\n");
            }

            // Position
            if let Some(positions) = positions {
                let (x, y) = positions[idx.index()];
                out.push_str("      <data key=\"x\">");
                out.push_str(&format!("{:.2}", x));
                out.push_str("</data>\n      <data key=\"y\">");
                out.push_str(&format!("{:.2}", y));
                out.push_str("</data>\n");
            }

            // Color by kind
            let (r, g, b) = kind_color(&node.kind);
            out.push_str("      <data key=\"r\">");
            out.push_str(&r.to_string());
            out.push_str("</data>\n      <data key=\"g\">");
            out.push_str(&g.to_string());
            out.push_str("</data>\n      <data key=\"b\">");
            out.push_str(&b.to_string());
            out.push_str("</data>\n");

            out.push_str("    </node>\n");
        }

        // Edges — iterate subgraph nodes, emit only edges within subgraph
        let mut edge_id = 0u32;
        let mut seen: HashSet<(NodeIndex, NodeIndex)> = HashSet::new();
        for &idx in &sorted {
            for edge in self.graph.edges(idx) {
                let tgt = edge.target();
                if !sub.contains(&tgt) {
                    continue;
                }
                if !seen.insert((idx, tgt)) {
                    continue;
                }
                let src_name = &self.graph[idx].name;
                let tgt_name = &self.graph[tgt].name;
                out.push_str("    <edge id=\"e");
                out.push_str(&edge_id.to_string());
                out.push_str("\" source=\"");
                xml_escape_into(&mut out, src_name);
                out.push_str("\" target=\"");
                xml_escape_into(&mut out, tgt_name);
                out.push_str("\">\n      <data key=\"edge_type\">");
                xml_escape_into(&mut out, edge.weight());
                out.push_str("</data>\n    </edge>\n");
                edge_id += 1;
            }
        }

        out.push_str("  </graph>\n</graphml>\n");
        out
    }
}

/// Node color by kind (r, g, b).
fn kind_color(kind: &str) -> (u8, u8, u8) {
    match kind {
        "node"        => (128, 128, 128), // gray
        "platform"    => ( 74, 144, 217), // blue
        "model"       => (155,  89, 182), // purple
        "package"     => (230, 126,  34), // orange
        "zone"        => (121,  85,  72), // brown
        "agent"       => ( 39, 174,  96), // green
        "application" => ( 41, 128, 185), // steel blue
        "skill"       => ( 46, 204, 113), // light green
        "service"     => ( 52, 152, 219), // light blue
        "function"    => ( 26, 188, 156), // teal
        "software"    => (142,  68, 173), // violet
        "library"     => (243, 156,  18), // gold
        "builder"     => (127, 140, 141), // dark gray
        "helper"      => (149, 165, 166), // medium gray
        "entity"      => (231,  76,  60), // red
        "metric"      => (233,  30,  99), // pink
        "variable"    => (189, 195, 199), // light gray
        _             => (149, 165, 166), // default gray
    }
}

/// Append XML-escaped text to a String buffer.
fn xml_escape_into(buf: &mut String, s: &str) {
    for c in s.chars() {
        match c {
            '&' => buf.push_str("&amp;"),
            '<' => buf.push_str("&lt;"),
            '>' => buf.push_str("&gt;"),
            '"' => buf.push_str("&quot;"),
            '\'' => buf.push_str("&apos;"),
            _ => buf.push(c),
        }
    }
}

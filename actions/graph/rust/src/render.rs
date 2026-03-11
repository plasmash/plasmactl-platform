use std::collections::{BTreeMap, HashSet};

use crate::graph::PlatformGraph;

pub struct TreeRenderer<'a> {
    graph: &'a PlatformGraph,
}

impl<'a> TreeRenderer<'a> {
    pub fn new(graph: &'a PlatformGraph) -> Self {
        Self { graph }
    }

    pub fn render(&self, query: Option<&str>, reverse: bool, depth: i32) -> String {
        match query {
            None => self.render_full_graph(),
            Some(q) => {
                let node = match self.graph.get_node(q) {
                    Some(n) => n,
                    None => return format!("Node not found: {}", q),
                };
                let mut lines = vec![format!("{} ({})", node.name, node.kind)];
                let mut visited = HashSet::new();
                if reverse {
                    self.render_ancestors(q, &mut lines, depth, "", &mut visited);
                } else {
                    self.render_descendants(q, &mut lines, depth, "", &mut visited);
                }
                lines.join("\n")
            }
        }
    }

    fn render_full_graph(&self) -> String {
        let mut nodes_by_kind: BTreeMap<&str, usize> = BTreeMap::new();
        for idx in self.graph.graph.node_indices() {
            let kind = self.graph.graph[idx].kind.as_str();
            *nodes_by_kind.entry(kind).or_insert(0) += 1;
        }

        let mut lines = vec!["Platform Graph Summary".to_string(), String::new()];
        lines.push("Nodes by kind:".to_string());
        for (kind, count) in &nodes_by_kind {
            lines.push(format!("  {}: {}", kind, count));
        }
        lines.push(String::new());
        lines.push(format!("Total nodes: {}", self.graph.node_count()));
        lines.push(format!("Total edges: {}", self.graph.edge_count()));
        lines.join("\n")
    }

    fn render_descendants(
        &self,
        name: &str,
        lines: &mut Vec<String>,
        depth: i32,
        prefix: &str,
        visited: &mut HashSet<String>,
    ) {
        if visited.contains(name) {
            return;
        }
        visited.insert(name.to_string());

        let edges = self.graph.edges_from(name);
        let mut edges_by_type: BTreeMap<&str, Vec<&str>> = BTreeMap::new();
        for (edge_type, target) in &edges {
            edges_by_type
                .entry(edge_type.as_str())
                .or_default()
                .push(target.as_str());
        }

        let type_items: Vec<(&str, Vec<&str>)> = edges_by_type.into_iter().collect();
        let n_types = type_items.len();

        for (i, (edge_type, targets)) in type_items.iter().enumerate() {
            let is_last_type = i == n_types - 1;
            let type_connector = if is_last_type { "\u{2514}\u{2500}\u{2500} " } else { "\u{251c}\u{2500}\u{2500} " };
            lines.push(format!("{}{}{}", prefix, type_connector, edge_type));
            let type_prefix = if is_last_type {
                format!("{}    ", prefix)
            } else {
                format!("{}\u{2502}   ", prefix)
            };

            let mut sorted_targets: Vec<&&str> = targets.iter().collect();
            sorted_targets.sort();
            let n_targets = sorted_targets.len();

            for (j, target) in sorted_targets.iter().enumerate() {
                let is_last = j == n_targets - 1;
                let connector = if is_last { "\u{2514}\u{2500}\u{2500} " } else { "\u{251c}\u{2500}\u{2500} " };
                let target_node = self.graph.get_node(target);
                let kind_str = target_node
                    .map(|n| format!(" ({})", n.kind))
                    .unwrap_or_default();
                let is_circular = visited.contains(**target);
                let circular_marker = if is_circular { " [circular]" } else { "" };
                lines.push(format!(
                    "{}{}{}{}{}",
                    type_prefix, connector, target, kind_str, circular_marker
                ));

                if depth != 0 && !is_circular {
                    let new_depth = if depth > 0 { depth - 1 } else { -1 };
                    let child_prefix = if is_last {
                        format!("{}    ", type_prefix)
                    } else {
                        format!("{}\u{2502}   ", type_prefix)
                    };
                    self.render_descendants(target, lines, new_depth, &child_prefix, visited);
                }
            }
        }
    }

    fn render_ancestors(
        &self,
        name: &str,
        lines: &mut Vec<String>,
        depth: i32,
        prefix: &str,
        visited: &mut HashSet<String>,
    ) {
        if visited.contains(name) {
            return;
        }
        visited.insert(name.to_string());

        let edges = self.graph.edges_to(name);
        let mut edges_by_type: BTreeMap<String, Vec<&str>> = BTreeMap::new();
        for (edge_type, source) in &edges {
            let inv = format!("{} (by)", edge_type);
            edges_by_type.entry(inv).or_default().push(source.as_str());
        }

        let type_items: Vec<(String, Vec<&str>)> = edges_by_type.into_iter().collect();
        let n_types = type_items.len();

        for (i, (edge_type, sources)) in type_items.iter().enumerate() {
            let is_last_type = i == n_types - 1;
            let type_connector = if is_last_type { "\u{2514}\u{2500}\u{2500} " } else { "\u{251c}\u{2500}\u{2500} " };
            lines.push(format!("{}{}{}", prefix, type_connector, edge_type));
            let type_prefix = if is_last_type {
                format!("{}    ", prefix)
            } else {
                format!("{}\u{2502}   ", prefix)
            };

            let mut sorted_sources: Vec<&&str> = sources.iter().collect();
            sorted_sources.sort();
            let n_sources = sorted_sources.len();

            for (j, source) in sorted_sources.iter().enumerate() {
                let is_last = j == n_sources - 1;
                let connector = if is_last { "\u{2514}\u{2500}\u{2500} " } else { "\u{251c}\u{2500}\u{2500} " };
                let source_node = self.graph.get_node(source);
                let kind_str = source_node
                    .map(|n| format!(" ({})", n.kind))
                    .unwrap_or_default();
                let is_circular = visited.contains(**source);
                let circular_marker = if is_circular { " [circular]" } else { "" };
                lines.push(format!(
                    "{}{}{}{}{}",
                    type_prefix, connector, source, kind_str, circular_marker
                ));

                if depth != 0 && !is_circular {
                    let new_depth = if depth > 0 { depth - 1 } else { -1 };
                    let child_prefix = if is_last {
                        format!("{}    ", type_prefix)
                    } else {
                        format!("{}\u{2502}   ", type_prefix)
                    };
                    self.render_ancestors(source, lines, new_depth, &child_prefix, visited);
                }
            }
        }
    }
}

pub struct MermaidRenderer<'a> {
    graph: &'a PlatformGraph,
}

impl<'a> MermaidRenderer<'a> {
    pub fn new(graph: &'a PlatformGraph) -> Self {
        Self { graph }
    }

    pub fn render(&self, query: Option<&str>, reverse: bool, depth: i32) -> String {
        let data = self.graph.to_dict(query, reverse, depth);
        let mut lines = vec!["graph LR".to_string()];
        let mut node_ids: std::collections::HashMap<String, String> =
            std::collections::HashMap::new();

        if let Some(nodes) = data["nodes"].as_array() {
            for (i, node) in nodes.iter().enumerate() {
                let id = node["id"].as_str().unwrap_or("").to_string();
                let mid = format!("n{}", i);
                node_ids.insert(id.clone(), mid.clone());
                let label = id.rsplit('.').next().unwrap_or(&id);
                let kind = node["kind"].as_str().unwrap_or("");
                let (open, close) = mermaid_shape(kind);
                lines.push(format!("  {}{}{}{}", mid, open, label, close));
            }
        }

        if let Some(edges) = data["edges"].as_array() {
            for edge in edges {
                let source = edge["source"].as_str().unwrap_or("");
                let target = edge["target"].as_str().unwrap_or("");
                let etype = edge["type"].as_str().unwrap_or("");
                if let (Some(s), Some(t)) = (node_ids.get(source), node_ids.get(target)) {
                    lines.push(format!("  {} -->|{}| {}", s, etype, t));
                }
            }
        }

        lines.join("\n")
    }
}

fn mermaid_shape(kind: &str) -> (&'static str, &'static str) {
    match kind {
        "application" => ("[[", "]]"),
        "service" => ("((", "))"),
        "software" => ("[", "]"),
        "function" => ("{{", "}}"),
        "skill" => (">", "]"),
        "agent" => ("([", "])"),
        "library" => ("[(", ")]"),
        "entity" => ("[/", "/]"),
        "zone" => ("[[", "]]"),
        "variable" => ("(", ")"),
        _ => ("[", "]"),
    }
}

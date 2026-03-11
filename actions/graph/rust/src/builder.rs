use std::collections::{HashMap, HashSet};
use std::fs;
use std::path::{Path, PathBuf};
use std::time::Instant;

use walkdir::WalkDir;

use crate::graph::{Node, PlatformGraph};
use crate::types::{
    determine_edge_type, include_role_re, is_versioned, kind_from_plural, toplevel_key_re,
    version_re,
};

/// Dirs to prune from traversal.
const PRUNE_DIRS: &[&str] = &[
    ".git",
    ".plasma",
    ".plasmactl",
    "ansible_collections",
    "dist",
    "img",
    "inst",
];

/// Task file names we care about.
const TASK_FILENAMES: &[&str] = &["dependencies.yaml", "main.yaml", "configuration.yaml"];

/// Pending data collected during walk for deferred processing.
struct DefaultsPending {
    component: String,
    var_names: Vec<String>,
}

struct DepsPending {
    source: String,
    source_kind: String,
    targets: Vec<String>,
}

struct ScanEntry {
    text: String,
    component: String,
}

struct FlowPending {
    component: String,
    trigger: String,
    output: String,
}

struct VarsRefPending {
    var_name: String,
    refs: Vec<String>,
}

pub struct GraphBuilder {
    compose_dir: PathBuf,
    pub graph: PlatformGraph,
    known_variables: HashSet<String>,
    platform_names: Vec<String>,
}

impl GraphBuilder {
    pub fn new(compose_dir: &Path) -> Self {
        Self {
            compose_dir: compose_dir.to_path_buf(),
            graph: PlatformGraph::new(),
            known_variables: HashSet::new(),
            platform_names: Vec::new(),
        }
    }

    pub fn build(mut self, debug: bool) -> PlatformGraph {
        let total_start = Instant::now();

        // 1. Discover structure
        let t = Instant::now();
        self.discover_platforms();
        if debug {
            eprintln!("  discover_platforms: {:.3}s ({} found)", t.elapsed().as_secs_f64(), self.platform_names.len());
        }

        let t = Instant::now();
        self.discover_zones();
        if debug {
            eprintln!("  discover_zones: {:.3}s", t.elapsed().as_secs_f64());
        }

        let t = Instant::now();
        self.discover_nodes();
        if debug {
            eprintln!("  discover_nodes: {:.3}s", t.elapsed().as_secs_f64());
        }

        // 2. Single-pass walk over compose-dir/src
        let t = Instant::now();
        let mut defaults_pending: Vec<DefaultsPending> = Vec::new();
        let mut deps_pending: Vec<DepsPending> = Vec::new();
        let mut scan_data: Vec<ScanEntry> = Vec::new();
        let mut flow_pending: Vec<FlowPending> = Vec::new();
        let mut vars_ref_pending: Vec<VarsRefPending> = Vec::new();
        let jinja_env = minijinja::Environment::new();
        let mut n_comp = 0u32;
        let mut n_gv = 0u32;

        let src_dir = self.compose_dir.join("src");
        let src_str = src_dir.to_string_lossy().to_string();
        let src_len = src_str.len() + 1; // +1 for trailing separator

        let toplevel_re = toplevel_key_re();
        let version_re = version_re();
        let include_re = include_role_re();

        for entry in WalkDir::new(&src_dir)
            .follow_links(true)
            .into_iter()
            .filter_entry(|e| {
                if e.file_type().is_dir() {
                    let name = e.file_name().to_string_lossy();
                    !PRUNE_DIRS.contains(&name.as_ref())
                } else {
                    true
                }
            })
        {
            let entry = match entry {
                Ok(e) => e,
                Err(_) => continue,
            };
            if !entry.file_type().is_file() {
                continue;
            }

            let path = entry.path();
            let path_str = path.to_string_lossy();
            if path_str.len() <= src_len {
                continue;
            }
            let rel = &path_str[src_len..];
            let parts: Vec<&str> = rel.split('/').collect();
            let depth = parts.len();

            // Minimum: <layer>/<something>
            if depth < 2 {
                continue;
            }

            let layer = parts[0];
            let kind_plural_or_dir = parts[1];
            let filename = parts[depth - 1];

            // --- variables/<zone>/vars.yaml ---
            // Path: src/<layer>/variables/<zone>/vars.yaml
            if depth == 4 && kind_plural_or_dir == "variables" && filename == "vars.yaml" {
                let zone_path = parts[2];
                if let Ok(content) = fs::read(path) {
                    if content.starts_with(b"$ANSIBLE_VAULT") {
                        continue;
                    }
                    let text = String::from_utf8_lossy(&content);
                    vars_ref_pending.extend(extract_var_refs(&text, &jinja_env));
                    let var_names: Vec<String> = toplevel_re
                        .captures_iter(&text)
                        .filter_map(|c| {
                            let k = c.get(1).unwrap().as_str();
                            if !k.starts_with('_') {
                                Some(k.to_string())
                            } else {
                                None
                            }
                        })
                        .collect();
                    if !var_names.is_empty() {
                        self.graph.add_node(Node {
                            name: zone_path.to_string(),
                            kind: "zone".to_string(),
                            versioned: false,
                            version: None,
                            path: None,
                            attrs: HashMap::new(),
                        });
                        for vn in &var_names {
                            self.known_variables.insert(vn.clone());
                            let mut attrs = HashMap::new();
                            attrs.insert("zone".to_string(), zone_path.to_string());
                            attrs.insert("vault".to_string(), "false".to_string());
                            attrs.insert("source".to_string(), "group_vars".to_string());
                            self.graph.add_node(Node {
                                name: vn.clone(),
                                kind: "variable".to_string(),
                                versioned: false,
                                version: None,
                                path: None,
                                attrs,
                            });
                            self.graph.add_edge(zone_path, vn, "overrides");
                        }
                        n_gv += 1;
                    }
                }
                continue;
            }

            // --- cfg/<zone>/vars.yaml or vault.yaml ---
            // Path: src/<layer>/cfg/<zone>/vars.yaml|vault.yaml
            if depth == 4
                && kind_plural_or_dir == "cfg"
                && (filename == "vars.yaml" || filename == "vault.yaml")
            {
                let zone_path = parts[2];
                let is_vault = filename == "vault.yaml";
                if let Ok(content) = fs::read(path) {
                    if content.starts_with(b"$ANSIBLE_VAULT") {
                        continue;
                    }
                    let text = String::from_utf8_lossy(&content);
                    if !is_vault {
                        vars_ref_pending.extend(extract_var_refs(&text, &jinja_env));
                    }
                    let var_names: Vec<String> = toplevel_re
                        .captures_iter(&text)
                        .filter_map(|c| {
                            let k = c.get(1).unwrap().as_str();
                            if !k.starts_with('_') {
                                Some(k.to_string())
                            } else {
                                None
                            }
                        })
                        .collect();
                    if !var_names.is_empty() {
                        self.graph.add_node(Node {
                            name: zone_path.to_string(),
                            kind: "zone".to_string(),
                            versioned: false,
                            version: None,
                            path: None,
                            attrs: HashMap::new(),
                        });
                        for vn in &var_names {
                            self.known_variables.insert(vn.clone());
                            let mut attrs = HashMap::new();
                            attrs.insert("zone".to_string(), zone_path.to_string());
                            attrs.insert("vault".to_string(), is_vault.to_string());
                            attrs.insert("source".to_string(), "group_vars".to_string());
                            self.graph.add_node(Node {
                                name: vn.clone(),
                                kind: "variable".to_string(),
                                versioned: false,
                                version: None,
                                path: None,
                                attrs,
                            });
                            self.graph.add_edge(zone_path, vn, "overrides");
                        }
                        n_gv += 1;
                    }
                }
                continue;
            }

            // --- Component paths: src/<layer>/<kind_plural>/<component>/<subdir>/... ---
            if depth < 4 {
                continue;
            }

            let kind = match kind_from_plural(kind_plural_or_dir) {
                Some(k) => k,
                None => continue,
            };

            // Strip legacy roles/ intermediate directory at position 2 if present
            let (comp_part, subdir, file_depth) = if parts.len() > 3 && parts[2] == "roles" {
                (parts[3], if parts.len() > 4 { parts[4] } else { "" }, 6usize)
            } else {
                (parts[2], parts[3], 5usize)
            };
            let component_name_str = format!("{}.{}.{}", layer, kind_plural_or_dir, comp_part);

            // meta/plasma.yaml → discover component
            if depth == file_depth && subdir == "meta" && filename == "plasma.yaml" {
                if let Ok(content) = fs::read(path) {
                    if !memchr::memmem::find(&content, b"plasma:").is_some() {
                        continue;
                    }
                    let text = String::from_utf8_lossy(&content);
                    let version = version_re
                        .captures(&text)
                        .and_then(|c| Some(c.get(1)?.as_str().trim().to_string()));
                    // Path is component dir (two levels up from meta/plasma.yaml)
                    let comp_path = path
                        .parent()
                        .and_then(|p| p.parent())
                        .map(|p| p.to_string_lossy().to_string());
                    self.graph.add_node(Node {
                        name: component_name_str.clone(),
                        kind: kind.to_string(),
                        versioned: is_versioned(kind),
                        version,
                        path: comp_path,
                        attrs: HashMap::new(),
                    });
                    n_comp += 1;
                }
                continue;
            }

            // defaults/main.yaml → collect for deferred processing
            if depth == file_depth && subdir == "defaults" && filename == "main.yaml" {
                if let Ok(content) = fs::read(path) {
                    let text = String::from_utf8_lossy(&content);
                    vars_ref_pending.extend(extract_var_refs(&text, &jinja_env));
                    let var_names: Vec<String> = toplevel_re
                        .captures_iter(&text)
                        .filter_map(|c| {
                            let k = c.get(1).unwrap().as_str();
                            if !k.starts_with('_') {
                                Some(k.to_string())
                            } else {
                                None
                            }
                        })
                        .collect();
                    if !var_names.is_empty() {
                        defaults_pending.push(DefaultsPending {
                            component: component_name_str.clone(),
                            var_names,
                        });
                    }
                }
                continue;
            }

            // tasks/{dependencies,main,configuration}.yaml
            if depth == file_depth && subdir == "tasks" && TASK_FILENAMES.contains(&filename) {
                if let Ok(content) = fs::read(path) {
                    // Check for include_role references
                    if memchr::memmem::find(&content, b"include_role").is_some() {
                        let text = String::from_utf8_lossy(&content);
                        let targets: Vec<String> = include_re
                            .captures_iter(&text)
                            .map(|c| c.get(1).unwrap().as_str().to_string())
                            .collect();
                        if !targets.is_empty() {
                            deps_pending.push(DepsPending {
                                source: component_name_str.clone(),
                                source_kind: kind.to_string(),
                                targets,
                            });
                        }

                        // Extract flow_trigger/flow_output from flow_builder tasks
                        if kind_plural_or_dir == "flows"
                            && filename == "main.yaml"
                            && memchr::memmem::find(&content, b"flow_builder").is_some()
                        {
                            if let Some(fp) =
                                extract_flow_vars(&component_name_str, &text)
                            {
                                flow_pending.push(fp);
                            }
                        }
                    }
                    // Scan task files for variable references
                    if memchr::memmem::find(&content, b"{{").is_some()
                        || memchr::memmem::find(&content, b"{%").is_some()
                    {
                        if let Ok(text) = String::from_utf8(content.clone()) {
                            scan_data.push(ScanEntry {
                                text,
                                component: component_name_str.clone(),
                            });
                        }
                    }
                }
                continue;
            }

            // Scannable files (templates, configs) in deeper subdirs
            if depth >= file_depth
                && (filename.ends_with(".j2") || filename.ends_with(".yaml"))
                && subdir != "meta"
                && subdir != "defaults"
            {
                if let Ok(content) = fs::read(path) {
                    if memchr::memmem::find(&content, b"{{").is_some()
                        || memchr::memmem::find(&content, b"{%").is_some()
                    {
                        if let Ok(text) = String::from_utf8(content) {
                            scan_data.push(ScanEntry {
                                text,
                                component: component_name_str.clone(),
                            });
                        }
                    }
                }
            }
        }

        if debug {
            eprintln!(
                "  walk_and_discover: {:.3}s (components: {}, group_vars: {}, defaults: {}, deps: {}, scannable: {})",
                t.elapsed().as_secs_f64(),
                n_comp,
                n_gv,
                defaults_pending.len(),
                deps_pending.len(),
                scan_data.len()
            );
        }

        // 3. Apply defaults
        let t = Instant::now();
        for dp in &defaults_pending {
            for vn in &dp.var_names {
                self.known_variables.insert(vn.clone());
                let mut attrs = HashMap::new();
                attrs.insert("component".to_string(), dp.component.clone());
                attrs.insert("source".to_string(), "defaults".to_string());
                self.graph.add_node(Node {
                    name: vn.clone(),
                    kind: "variable".to_string(),
                    versioned: false,
                    version: None,
                    path: None,
                    attrs,
                });
                if self.graph.has_node(&dp.component) {
                    self.graph.add_edge(&dp.component, vn, "defines");
                }
            }
        }
        if debug {
            eprintln!("  apply_defaults: {:.3}s", t.elapsed().as_secs_f64());
        }

        // 3.5. Variable-to-variable references
        let t = Instant::now();
        let n_var_refs = self.build_variable_references(&vars_ref_pending);
        if debug {
            eprintln!(
                "  build_variable_references: {:.3}s (pending: {}, edges: {})",
                t.elapsed().as_secs_f64(),
                vars_ref_pending.len(),
                n_var_refs
            );
        }

        // 4. Apply dependencies
        let t = Instant::now();
        for dp in &deps_pending {
            if !self.graph.has_node(&dp.source) {
                continue;
            }
            for target in &dp.targets {
                if !self.graph.has_node(target) {
                    let target_kind = guess_kind_from_name(target);
                    self.graph.add_node(Node {
                        name: target.clone(),
                        kind: target_kind.to_string(),
                        versioned: is_versioned(target_kind),
                        version: None,
                        path: None,
                        attrs: HashMap::new(),
                    });
                }
                let edge_type = determine_edge_type(&dp.source_kind, target);
                self.graph.add_edge(&dp.source, target, edge_type);
            }
        }
        if debug {
            eprintln!("  apply_dependencies: {:.3}s", t.elapsed().as_secs_f64());
        }

        // 5. Zone attachments
        let t = Instant::now();
        self.build_zone_attachments();
        if debug {
            eprintln!(
                "  build_zone_attachments: {:.3}s",
                t.elapsed().as_secs_f64()
            );
        }

        // 6. Variable edges — scan templates with minijinja
        let t = Instant::now();
        let known_components: HashSet<String> = self
            .graph
            .graph
            .node_indices()
            .map(|i| self.graph.graph[i].name.clone())
            .collect();
        // Filter scan_data to only known components
        let scan_data: Vec<ScanEntry> = scan_data
            .into_iter()
            .filter(|e| known_components.contains(&e.component))
            .collect();
        let n_files = scan_data.len();
        self.scan_variables(&scan_data);
        if debug {
            eprintln!(
                "  build_variable_edges: {:.3}s (files: {})",
                t.elapsed().as_secs_f64(),
                n_files
            );
        }

        // 7. Package discovery
        let t = Instant::now();
        self.discover_packages();
        if debug {
            eprintln!(
                "  discover_packages: {:.3}s",
                t.elapsed().as_secs_f64()
            );
        }

        // 8. Flow choreography edges
        let t = Instant::now();
        let n_choreo = self.build_flow_choreography(&flow_pending);
        if debug {
            eprintln!(
                "  build_flow_choreography: {:.3}s (flows: {}, edges: {})",
                t.elapsed().as_secs_f64(),
                flow_pending.len(),
                n_choreo
            );
        }

        if debug {
            eprintln!("  TOTAL: {:.3}s", total_start.elapsed().as_secs_f64());
        }

        self.graph
    }

    /// Resolve the repository root from compose_dir (.plasma/model/compose/merged → repo root).
    fn repo_root(&self) -> PathBuf {
        // compose_dir is .plasma/model/compose/merged, repo root is 4 levels up
        self.compose_dir
            .parent()
            .and_then(|p| p.parent())
            .and_then(|p| p.parent())
            .and_then(|p| p.parent())
            .map(|p| p.to_path_buf())
            .unwrap_or_else(|| self.compose_dir.clone())
    }

    /// Discover all platform instances from inst/*/platform.yaml.
    fn discover_platforms(&mut self) {
        let inst_dir = self.compose_dir.join("inst");
        if let Ok(entries) = fs::read_dir(&inst_dir) {
            for entry in entries.flatten() {
                if !entry.file_type().map(|t| t.is_dir()).unwrap_or(false) {
                    continue;
                }
                let pf = entry.path().join("platform.yaml");
                if let Ok(content) = fs::read_to_string(&pf) {
                    if let Ok(data) = serde_yaml::from_str::<serde_yaml::Value>(&content) {
                        if let Some(name) = data.get("name").and_then(|v| v.as_str()) {
                            self.platform_names.push(name.to_string());
                            self.graph.add_node(Node {
                                name: name.to_string(),
                                kind: "platform".to_string(),
                                versioned: false,
                                version: None,
                                path: Some(pf.to_string_lossy().to_string()),
                                attrs: HashMap::new(),
                            });
                        }
                    }
                }
            }
        }
        if self.platform_names.is_empty() {
            self.platform_names.push("platform".to_string());
            self.graph.add_node(Node {
                name: "platform".to_string(),
                kind: "platform".to_string(),
                versioned: false,
                version: None,
                path: None,
                attrs: HashMap::new(),
            });
        }
    }

    fn discover_zones(&mut self) {
        let zone_file = self.compose_dir.join("topology.yaml");
        let content = match fs::read_to_string(&zone_file) {
            Ok(c) => c,
            Err(_) => return,
        };
        let data: serde_yaml::Value = match serde_yaml::from_str(&content) {
            Ok(d) => d,
            Err(e) => {
                eprintln!("Warning: Failed to parse {}: {}", zone_file.display(), e);
                return;
            }
        };

        // topology.yaml has a top-level "platform" key containing the zone tree.
        // Extract it and build the tree, connecting platform instances to top-level zones.
        if let Some(platform_data) = data.get("platform") {
            self.build_top_zones(platform_data, "platform");
        }
    }

    /// Build top-level zone nodes and connect each platform instance to them.
    /// Deeper levels use add_zone_hierarchy (no platform edges needed).
    fn build_top_zones(&mut self, data: &serde_yaml::Value, prefix: &str) {
        let platform_names = self.platform_names.clone();
        match data {
            serde_yaml::Value::Mapping(map) => {
                for (key, value) in map {
                    if let Some(key_str) = key.as_str() {
                        let zone_name = format!("{}.{}", prefix, key_str);
                        self.graph.add_node(Node {
                            name: zone_name.clone(),
                            kind: "zone".to_string(),
                            versioned: false,
                            version: None,
                            path: None,
                            attrs: HashMap::new(),
                        });
                        for pname in &platform_names {
                            self.graph.add_edge(pname, &zone_name, "contains");
                        }
                        if !value.is_null() {
                            self.add_zone_hierarchy(value, &zone_name);
                        }
                    }
                }
            }
            serde_yaml::Value::Sequence(seq) => {
                for item in seq {
                    match item {
                        serde_yaml::Value::String(s) => {
                            let zone_name = format!("{}.{}", prefix, s);
                            self.graph.add_node(Node {
                                name: zone_name.clone(),
                                kind: "zone".to_string(),
                                versioned: false,
                                version: None,
                                path: None,
                                attrs: HashMap::new(),
                            });
                            for pname in &platform_names {
                                self.graph.add_edge(pname, &zone_name, "contains");
                            }
                        }
                        serde_yaml::Value::Mapping(_) => {
                            self.build_top_zones(item, prefix);
                        }
                        _ => {}
                    }
                }
            }
            _ => {}
        }
    }

    fn add_zone_hierarchy(&mut self, data: &serde_yaml::Value, parent: &str) {
        match data {
            serde_yaml::Value::Mapping(map) => {
                for (key, value) in map {
                    if let Some(key_str) = key.as_str() {
                        let zone_name = format!("{}.{}", parent, key_str);
                        self.graph.add_node(Node {
                            name: zone_name.clone(),
                            kind: "zone".to_string(),
                            versioned: false,
                            version: None,
                            path: None,
                            attrs: HashMap::new(),
                        });
                        self.graph.add_edge(parent, &zone_name, "contains");
                        if !value.is_null() {
                            self.add_zone_hierarchy(value, &zone_name);
                        }
                    }
                }
            }
            serde_yaml::Value::Sequence(seq) => {
                for item in seq {
                    match item {
                        serde_yaml::Value::String(s) => {
                            let zone_name = format!("{}.{}", parent, s);
                            self.graph.add_node(Node {
                                name: zone_name.clone(),
                                kind: "zone".to_string(),
                                versioned: false,
                                version: None,
                                path: None,
                                attrs: HashMap::new(),
                            });
                            self.graph.add_edge(parent, &zone_name, "contains");
                        }
                        serde_yaml::Value::Mapping(_) => {
                            self.add_zone_hierarchy(item, parent);
                        }
                        _ => {}
                    }
                }
            }
            _ => {}
        }
    }

    fn discover_nodes(&mut self) {
        let inst_dir = self.compose_dir.join("inst");
        if !inst_dir.exists() {
            return;
        }
        let entries = match fs::read_dir(&inst_dir) {
            Ok(e) => e,
            Err(_) => return,
        };
        for entry in entries.flatten() {
            if !entry.file_type().map(|t| t.is_dir()).unwrap_or(false) {
                continue;
            }
            // Read platform name for this instance
            let platform_name = {
                let pf = entry.path().join("platform.yaml");
                fs::read_to_string(&pf).ok().and_then(|content| {
                    serde_yaml::from_str::<serde_yaml::Value>(&content).ok().and_then(|data| {
                        data.get("name").and_then(|v| v.as_str()).map(|s| s.to_string())
                    })
                })
            };
            let nodes_dir = entry.path().join("nodes");
            if !nodes_dir.exists() {
                continue;
            }
            let node_entries = match fs::read_dir(&nodes_dir) {
                Ok(e) => e,
                Err(_) => continue,
            };
            for node_entry in node_entries.flatten() {
                let path = node_entry.path();
                if path.extension().and_then(|e| e.to_str()) != Some("yaml") {
                    continue;
                }
                self.add_node_from_yaml(&path, platform_name.as_deref());
            }
        }
    }

    fn add_node_from_yaml(&mut self, path: &Path, platform_name: Option<&str>) {
        let content = match fs::read_to_string(path) {
            Ok(c) => c,
            Err(_) => return,
        };
        let data: serde_yaml::Value = match serde_yaml::from_str(&content) {
            Ok(d) => d,
            Err(e) => {
                eprintln!("Warning: Failed to parse {}: {}", path.display(), e);
                return;
            }
        };
        let hostname = data
            .get("hostname")
            .and_then(|v| v.as_str())
            .unwrap_or_else(|| path.file_stem().unwrap().to_str().unwrap())
            .to_string();

        let mut zone_list = Vec::new();
        if let Some(seq) = data.get("zones").and_then(|v| v.as_sequence()) {
            for item in seq {
                if let Some(s) = item.as_str() {
                    zone_list.push(s.to_string());
                }
            }
        }

        let mut attrs = HashMap::new();
        attrs.insert("zones".to_string(), zone_list.join(","));

        self.graph.add_node(Node {
            name: hostname.clone(),
            kind: "node".to_string(),
            versioned: false,
            version: None,
            path: Some(path.to_string_lossy().to_string()),
            attrs,
        });

        for zone in &zone_list {
            self.graph.add_node(Node {
                name: zone.clone(),
                kind: "zone".to_string(),
                versioned: false,
                version: None,
                path: None,
                attrs: HashMap::new(),
            });
            self.graph.add_edge(&hostname, zone, "allocates");
        }

        // Node executes its platform instance
        if let Some(pname) = platform_name {
            self.graph.add_edge(&hostname, pname, "executes");
        }
    }

    fn build_zone_attachments(&mut self) {
        let src_dir = self.compose_dir.join("src");
        let entries = match fs::read_dir(&src_dir) {
            Ok(e) => e,
            Err(_) => return,
        };
        for entry in entries.flatten() {
            if !entry.file_type().map(|t| t.is_dir()).unwrap_or(false) {
                continue;
            }
            let dir_name = entry.file_name().to_string_lossy().to_string();
            let playbook = entry.path().join(format!("{}.yaml", dir_name));
            if playbook.exists() {
                self.parse_playbook(&playbook);
            }
        }
    }

    fn parse_playbook(&mut self, playbook: &Path) {
        let content = match fs::read_to_string(playbook) {
            Ok(c) => c,
            Err(_) => return,
        };
        let data: serde_yaml::Value = match serde_yaml::from_str(&content) {
            Ok(d) => d,
            Err(e) => {
                eprintln!(
                    "Warning: Failed to parse {}: {}",
                    playbook.display(),
                    e
                );
                return;
            }
        };

        let plays = match data.as_sequence() {
            Some(s) => s,
            None => return,
        };

        for play in plays {
            let map = match play.as_mapping() {
                Some(m) => m,
                None => continue,
            };
            let hosts = match map
                .get(serde_yaml::Value::String("hosts".to_string()))
                .and_then(|v| v.as_str())
            {
                Some(h) => h.to_string(),
                None => continue,
            };
            let roles = match map
                .get(serde_yaml::Value::String("roles".to_string()))
                .and_then(|v| v.as_sequence())
            {
                Some(r) => r,
                None => continue,
            };

            self.graph.add_node(Node {
                name: hosts.clone(),
                kind: "zone".to_string(),
                versioned: false,
                version: None,
                path: None,
                attrs: HashMap::new(),
            });

            for role in roles {
                let role_name = match role {
                    serde_yaml::Value::String(s) => s.clone(),
                    serde_yaml::Value::Mapping(m) => {
                        m.get(serde_yaml::Value::String("role".to_string()))
                            .or_else(|| m.get(serde_yaml::Value::String("name".to_string())))
                            .and_then(|v| v.as_str())
                            .unwrap_or("")
                            .to_string()
                    }
                    _ => continue,
                };
                if !role_name.is_empty() && self.graph.has_node(&role_name) {
                    self.graph.add_edge(&hosts, &role_name, "distributes");
                }
            }
        }
    }

    fn build_variable_references(&mut self, pending: &[VarsRefPending]) -> usize {
        let mut n_edges = 0;
        for entry in pending {
            for ref_var in &entry.refs {
                if ref_var != &entry.var_name && self.known_variables.contains(ref_var.as_str()) {
                    self.graph.add_edge(ref_var, &entry.var_name, "parametrizes");
                    n_edges += 1;
                }
            }
        }
        n_edges
    }

    fn scan_variables(&mut self, scan_data: &[ScanEntry]) {
        use rayon::prelude::*;

        let known = &self.known_variables;
        let env = minijinja::Environment::new();

        // Parallel: parse templates and find variable references
        let results: Vec<(&str, Vec<String>)> = scan_data
            .par_iter()
            .filter_map(|entry| {
                let tmpl = env.template_from_str(&entry.text).ok()?;
                let vars: Vec<String> = tmpl
                    .undeclared_variables(false)
                    .into_iter()
                    .filter(|v| known.contains(v.as_str()))
                    .collect();
                if vars.is_empty() {
                    None
                } else {
                    Some((entry.component.as_str(), vars))
                }
            })
            .collect();

        // Sequential: add edges to graph
        for (component, vars) in results {
            for var_name in vars {
                self.graph.add_edge(&var_name, component, "parametrizes");
            }
        }
    }

    fn discover_packages(&mut self) {
        // Packages are at compose_dir/../packages/
        let packages_dir = self.compose_dir.join("..").join("packages");
        let packages_dir = match packages_dir.canonicalize() {
            Ok(p) => p,
            Err(_) => return,
        };
        if !packages_dir.exists() {
            return;
        }

        // Read compose.yaml for model name (at repo root)
        let compose_file = self.repo_root().join("compose.yaml");
        let model_name = if let Ok(content) = fs::read_to_string(&compose_file) {
            if let Ok(data) = serde_yaml::from_str::<serde_yaml::Value>(&content) {
                data.get("name")
                    .and_then(|v| v.as_str())
                    .unwrap_or("model")
                    .to_string()
            } else {
                "model".to_string()
            }
        } else {
            "model".to_string()
        };

        // Add model node — version is always "head" (current filesystem state)
        self.graph.add_node(Node {
            name: model_name.clone(),
            kind: "model".to_string(),
            versioned: true,
            version: Some("head".to_string()),
            path: None,
            attrs: HashMap::new(),
        });

        // Connect each platform → model
        let platform_names = self.platform_names.clone();
        for pname in &platform_names {
            self.graph.add_edge(pname, &model_name, "materializes");
        }

        // Track which components belong to packages
        let mut component_to_package: HashMap<String, String> = HashMap::new();

        let pkg_entries = match fs::read_dir(&packages_dir) {
            Ok(e) => e,
            Err(_) => return,
        };

        for pkg_entry in pkg_entries.flatten() {
            if !pkg_entry.file_type().map(|t| t.is_dir()).unwrap_or(false) {
                continue;
            }
            let pkg_name = pkg_entry.file_name().to_string_lossy().to_string();

            // Add package node
            self.graph.add_node(Node {
                name: pkg_name.clone(),
                kind: "package".to_string(),
                versioned: true,
                version: None,
                path: Some(pkg_entry.path().to_string_lossy().to_string()),
                attrs: HashMap::new(),
            });

            // model composes package
            self.graph.add_edge(&model_name, &pkg_name, "composes");

            // Find package root: either prepare/src/ or <ref>/ (e.g. master/)
            let pkg_roots = find_package_roots(&pkg_entry.path());
            for pkg_root in &pkg_roots {
                for walk_entry in WalkDir::new(pkg_root)
                    .follow_links(true)
                    .into_iter()
                    .filter_entry(|e| {
                        if e.file_type().is_dir() {
                            let name = e.file_name().to_string_lossy();
                            !PRUNE_DIRS.contains(&name.as_ref())
                        } else {
                            true
                        }
                    })
                {
                    let walk_entry = match walk_entry {
                        Ok(e) => e,
                        Err(_) => continue,
                    };
                    if !walk_entry.file_type().is_file() {
                        continue;
                    }
                    if walk_entry.file_name().to_string_lossy() != "plasma.yaml" {
                        continue;
                    }
                    let walk_path = walk_entry.path();
                    let parent = match walk_path.parent() {
                        Some(p) => p,
                        None => continue,
                    };
                    if parent.file_name().and_then(|n| n.to_str()) != Some("meta") {
                        continue;
                    }
                    let comp_dir = match parent.parent() {
                        Some(p) => p,
                        None => continue,
                    };
                    // Extract component name from path relative to pkg_root
                    // Handles both layouts:
                    //   <layer>/<kind>/<component>/meta/plasma.yaml
                    //   <layer>/<kind>/roles/<component>/meta/plasma.yaml
                    let pkg_root_str = pkg_root.to_string_lossy();
                    let comp_dir_str = comp_dir.to_string_lossy();
                    if comp_dir_str.len() <= pkg_root_str.len() + 1 {
                        continue;
                    }
                    let rel = &comp_dir_str[pkg_root_str.len() + 1..];
                    let parts: Vec<&str> = rel.split('/').collect();
                    let comp_name = match parts.len() {
                        // <layer>/<kind>/<component>
                        3 => format!("{}.{}.{}", parts[0], parts[1], parts[2]),
                        // <layer>/<kind>/roles/<component>
                        4 if parts[2] == "roles" => {
                            format!("{}.{}.{}", parts[0], parts[1], parts[3])
                        }
                        _ => continue,
                    };
                    component_to_package.insert(comp_name, pkg_name.clone());
                }
            }
        }

        // Create package→component edges
        for (comp_name, pkg_name) in &component_to_package {
            if self.graph.has_node(comp_name) {
                self.graph.add_edge(pkg_name, comp_name, "contains");
            }
        }

        // Components in merged/src NOT in any package = domain components → model contains
        let all_components: Vec<String> = self
            .graph
            .graph
            .node_indices()
            .filter_map(|i| {
                let node = &self.graph.graph[i];
                if is_versioned(&node.kind) && !component_to_package.contains_key(&node.name) {
                    Some(node.name.clone())
                } else {
                    None
                }
            })
            .collect();
        for comp_name in &all_components {
            self.graph.add_edge(&model_name, comp_name, "contains");
        }
    }

    fn build_flow_choreography(&mut self, flow_pending: &[FlowPending]) -> usize {
        // Store trigger/output as node attributes
        for fp in flow_pending {
            if let Some(node) = self.graph.get_node(&fp.component) {
                let mut attrs = node.attrs.clone();
                attrs.insert("flow_trigger".to_string(), fp.trigger.clone());
                attrs.insert("flow_output".to_string(), fp.output.clone());
                self.graph.add_node(Node {
                    name: fp.component.clone(),
                    kind: node.kind.clone(),
                    versioned: node.versioned,
                    version: node.version.clone(),
                    path: node.path.clone(),
                    attrs,
                });
            }
        }

        // Build output→flows index: map normalized output to flow component name
        let mut output_to_flows: HashMap<String, Vec<String>> = HashMap::new();
        for fp in flow_pending {
            if !fp.output.is_empty() && fp.output != "None" {
                let normalized = normalize_flow_channel(&fp.output);
                output_to_flows
                    .entry(normalized)
                    .or_default()
                    .push(fp.component.clone());
            }
        }

        // Match triggers to outputs: if flow B's trigger matches flow A's output, A choreographs B.
        // Also check .event suffix: trigger "x.event" matches output "x" (Go flow-viz compat).
        let mut n_edges = 0usize;
        for fp in flow_pending {
            if fp.trigger.is_empty() || fp.trigger == "None" {
                continue;
            }
            let normalized_trigger = normalize_flow_channel(&fp.trigger);
            // Try direct match, then strip .event suffix from trigger
            let candidates = [
                normalized_trigger.clone(),
                normalized_trigger.strip_suffix(".event").unwrap_or("").to_string(),
            ];
            for candidate in &candidates {
                if candidate.is_empty() {
                    continue;
                }
                if let Some(producers) = output_to_flows.get(candidate) {
                    for producer in producers {
                        if producer != &fp.component {
                            self.graph.add_edge(producer, &fp.component, "choreographs");
                            n_edges += 1;
                        }
                    }
                }
            }
        }

        n_edges
    }
}

/// Find walkable roots inside a package directory.
/// Two layouts:
///   - prepare/src/  (e.g. plasma-core/prepare/src/)
///   - <ref>/        (e.g. plasma-intelligence/master/)
fn find_package_roots(pkg_dir: &Path) -> Vec<PathBuf> {
    let prepare_src = pkg_dir.join("prepare").join("src");
    if prepare_src.exists() {
        return vec![prepare_src];
    }
    // Try <ref>/ subdirectories (e.g. master/, v1.0.0/)
    let mut roots = Vec::new();
    if let Ok(entries) = fs::read_dir(pkg_dir) {
        for entry in entries.flatten() {
            if entry.file_type().map(|t| t.is_dir()).unwrap_or(false) {
                let name = entry.file_name().to_string_lossy().to_string();
                if !name.starts_with('.') {
                    roots.push(entry.path());
                }
            }
        }
    }
    roots
}

/// Extract flow_trigger and flow_output from a flow task file.
/// Looks for the include_role task with flow_builder and reads vars.
fn extract_flow_vars(component: &str, text: &str) -> Option<FlowPending> {
    let data: serde_yaml::Value = serde_yaml::from_str(text).ok()?;
    let tasks = data.as_sequence()?;
    for task in tasks {
        let map = match task.as_mapping() {
            Some(m) => m,
            None => continue,
        };
        let include_role = match map.get(&serde_yaml::Value::String("include_role".to_string())) {
            Some(v) => v,
            None => continue,
        };
        let role_name = include_role
            .get("name")
            .and_then(|v| v.as_str())
            .unwrap_or("");
        if !role_name.ends_with("flow_builder") {
            continue;
        }
        let vars = match map.get(&serde_yaml::Value::String("vars".to_string())) {
            Some(v) => v,
            None => continue,
        };
        let trigger = vars
            .get("flow_trigger")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string();
        let output = vars
            .get("flow_output")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string();
        if !trigger.is_empty() || !output.is_empty() {
            return Some(FlowPending {
                component: component.to_string(),
                trigger,
                output,
            });
        }
    }
    None
}

/// Normalize a flow channel by stripping the bus type prefix (event:, data:, lake:, schedule:).
fn normalize_flow_channel(channel: &str) -> String {
    for prefix in &["event:", "data:", "lake:", "schedule:"] {
        if channel.starts_with(prefix) {
            return channel[prefix.len()..].to_string();
        }
    }
    channel.to_string()
}

fn guess_kind_from_name(name: &str) -> &'static str {
    let parts: Vec<&str> = name.splitn(3, '.').collect();
    if parts.len() >= 2 {
        kind_from_plural(parts[1]).unwrap_or("helper")
    } else {
        "helper"
    }
}

/// Extract variable-to-variable references from a YAML vars file using minijinja.
/// Returns (var_name, referenced_var_names) pairs for values that reference other variables.
fn extract_var_refs(text: &str, env: &minijinja::Environment) -> Vec<VarsRefPending> {
    let data: serde_yaml::Value = match serde_yaml::from_str(text) {
        Ok(d) => d,
        Err(_) => return Vec::new(),
    };
    let map = match data.as_mapping() {
        Some(m) => m,
        None => return Vec::new(),
    };
    let mut result = Vec::new();
    for (key, value) in map {
        let var_name = match key.as_str() {
            Some(k) if !k.starts_with('_') => k,
            _ => continue,
        };
        let value_str = match value {
            serde_yaml::Value::String(s) => s.clone(),
            serde_yaml::Value::Null | serde_yaml::Value::Bool(_) | serde_yaml::Value::Number(_) => continue,
            _ => match serde_yaml::to_string(value) {
                Ok(s) => s,
                Err(_) => continue,
            },
        };
        let tmpl = match env.template_from_str(&value_str) {
            Ok(t) => t,
            Err(_) => continue,
        };
        let refs: Vec<String> = tmpl.undeclared_variables(false).into_iter().collect();
        if !refs.is_empty() {
            result.push(VarsRefPending {
                var_name: var_name.to_string(),
                refs,
            });
        }
    }
    result
}

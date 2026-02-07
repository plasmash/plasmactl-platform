#!/usr/bin/env python3
"""Platform graph action - query the platform dependency graph."""

import argparse
import json
import os
import re
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

import jinja2
import jinja2.meta
import rustworkx as rx
import yaml

# Use C-based YAML loader if available (much faster)
try:
    YamlLoader = yaml.CSafeLoader
except AttributeError:
    YamlLoader = yaml.SafeLoader


# Edge types from the platform graph specification
EDGE_TYPES = {
    # Main chain
    "executes",
    "materializes",
    "composes",
    "distributes",
    "orchestrates",
    "configures",
    "computes",
    "implements",
    "defines",
    # Transversal
    "overrides",
    "parametrizes",
    "consumes",
    "produces",
    "builds",
    "contains",
    "attaches",
    "memberof",
}

# Node kinds
VERSIONED_KINDS = {
    "agent",
    "application",
    "skill",
    "service",
    "function",
    "software",
    "library",
    "entity",
    "metric",
    "builder",
    "helper",
}

NON_VERSIONED_KINDS = {
    "node",
    "platform",
    "model",
    "chassis",
    "variable",
}

ALL_KINDS = VERSIONED_KINDS | NON_VERSIONED_KINDS

# Map plural directory names to singular kind
KIND_MAP = {
    "applications": "application",
    "services": "service",
    "softwares": "software",
    "agents": "agent",
    "skills": "skill",
    "functions": "function",
    "libraries": "library",
    "entities": "entity",
    "metrics": "metric",
    "builders": "builder",
    "helpers": "helper",
    "executors": "agent",  # Legacy mapping
    "flows": "agent",  # Legacy mapping
}

# Regex to find include_role references (faster than YAML parsing)
INCLUDE_ROLE_PATTERN = re.compile(r'include_role:\s*\n\s*name:\s*([a-zA-Z_][a-zA-Z0-9_.]*)')

# Regex to extract top-level YAML keys (for defaults and group_vars)
TOPLEVEL_KEY_PATTERN = re.compile(rb'^([a-zA-Z_][a-zA-Z0-9_]*):', re.MULTILINE)

# Regex to extract version from plasma.yaml
VERSION_PATTERN = re.compile(rb'^\s+version:\s*["\']?([^"\'#\n\r]+)', re.MULTILINE)

# Layer names for quick lookup
LAYER_NAMES = frozenset(("foundation", "integration", "cognition", "conversation", "interaction", "stabilization"))


def _fork_scan(scan_data: list[tuple[str, str]], known_vars: set[str], n_workers: int) -> list[tuple[str, str]]:
    """Fork workers to parse files with Jinja2 AST. Data inherited via COW — zero pickling."""
    import pickle
    n = len(scan_data)
    if n == 0:
        return []
    batch_size = max(1, (n + n_workers - 1) // n_workers)

    # Create Jinja2 env + warm up lexer BEFORE fork — children inherit via COW
    env = jinja2.Environment(extensions=['jinja2.ext.loopcontrols'])
    env.parse("")  # trigger lazy lexer compilation
    find = jinja2.meta.find_undeclared_variables

    pipes = []
    children = []
    for i in range(0, n, batch_size):
        r_fd, w_fd = os.pipe()
        pid = os.fork()
        if pid == 0:
            # Child process — env + known_vars + scan_data inherited via COW
            os.close(r_fd)
            try:
                hits = []
                for text, cn in scan_data[i:i + batch_size]:
                    try:
                        ast = env.parse(text)
                        for vn in find(ast):
                            if vn in known_vars:
                                hits.append((vn, cn))
                    except Exception:
                        pass
                data = pickle.dumps(hits)
                with os.fdopen(w_fd, 'wb') as wf:
                    wf.write(data)
            except Exception:
                os.close(w_fd)
            os._exit(0)
        else:
            os.close(w_fd)
            pipes.append(r_fd)
            children.append(pid)

    # Collect results from all children
    all_hits = []
    for r_fd in pipes:
        with os.fdopen(r_fd, 'rb') as rf:
            data = rf.read()
        if data:
            all_hits.extend(pickle.loads(data))

    # Wait for all children
    for pid in children:
        os.waitpid(pid, 0)

    return all_hits



@dataclass
class Node:
    """Graph node."""

    name: str
    kind: str
    versioned: bool = False
    version: Optional[str] = None
    path: Optional[str] = None
    attrs: dict = field(default_factory=dict)


@dataclass
class Edge:
    """Graph edge."""

    source: str
    target: str
    edge_type: str


class PlatformGraph:
    """Platform dependency graph using rustworkx."""

    def __init__(self):
        self.graph = rx.PyDiGraph()
        self._node_indices: dict[str, int] = {}
        self._nodes: dict[str, Node] = {}
        self._edge_set: set[tuple[int, int]] = set()

    def add_node(self, node: Node) -> int:
        """Add a node to the graph."""
        if node.name in self._node_indices:
            existing = self._nodes[node.name]
            if node.version and not existing.version:
                existing.version = node.version
            if node.path and not existing.path:
                existing.path = node.path
            if node.attrs:
                existing.attrs.update(node.attrs)
            return self._node_indices[node.name]

        idx = self.graph.add_node(node)
        self._node_indices[node.name] = idx
        self._nodes[node.name] = node
        return idx

    def add_edge(self, source: str, target: str, edge_type: str) -> None:
        """Add an edge to the graph. Creates missing nodes automatically."""
        if source not in self._node_indices:
            kind = self._guess_kind(source)
            self.add_node(Node(name=source, kind=kind, versioned=kind in VERSIONED_KINDS))

        if target not in self._node_indices:
            kind = self._guess_kind(target)
            self.add_node(Node(name=target, kind=kind, versioned=kind in VERSIONED_KINDS))

        source_idx = self._node_indices[source]
        target_idx = self._node_indices[target]

        edge_key = (source_idx, target_idx)
        if edge_key in self._edge_set:
            return
        self._edge_set.add(edge_key)

        self.graph.add_edge(source_idx, target_idx, edge_type)

    def _guess_kind(self, name: str) -> str:
        """Guess node kind from name."""
        if name.startswith("platform.") and "." in name[9:]:
            return "chassis"
        parts = name.split(".")
        if len(parts) >= 2:
            return KIND_MAP.get(parts[1], "variable")
        return "variable"

    def get_node(self, name: str) -> Optional[Node]:
        """Get a node by name."""
        return self._nodes.get(name)

    def get_node_idx(self, name: str) -> Optional[int]:
        """Get node index by name."""
        return self._node_indices.get(name)

    def descendants(self, name: str, depth: int = -1) -> list[str]:
        """Get all descendants of a node.

        Note: Variable nodes are treated as leaves to prevent explosion
        via 'parametrizes' edges that connect to many components.
        """
        idx = self._node_indices.get(name)
        if idx is None:
            return []

        if depth == -1:
            desc_indices = self._bounded_descendants(idx)
        else:
            desc_indices = self._limited_descendants(idx, depth)

        return [self.graph[i].name for i in desc_indices]

    def _bounded_descendants(self, start_idx: int) -> set[int]:
        """Get all descendants, treating variable nodes as leaves."""
        result = set()
        queue = [start_idx]

        while queue:
            idx = queue.pop()
            node = self.graph[idx]

            if node.kind == "variable":
                continue

            for successor in self.graph.successor_indices(idx):
                if successor not in result:
                    result.add(successor)
                    queue.append(successor)

        return result

    def ancestors(self, name: str, depth: int = -1) -> list[str]:
        """Get all ancestors of a node (reverse dependencies)."""
        idx = self._node_indices.get(name)
        if idx is None:
            return []

        if depth == -1:
            anc_indices = rx.ancestors(self.graph, idx)
        else:
            anc_indices = self._limited_ancestors(idx, depth)

        return [self.graph[i].name for i in anc_indices]

    def _limited_descendants(self, start_idx: int, max_depth: int) -> set[int]:
        """Get descendants up to a maximum depth.

        Note: Variable nodes are treated as leaves - we don't follow their
        outgoing edges since they connect to many components via 'parametrizes'.
        """
        result = set()
        current_level = {start_idx}

        for _ in range(max_depth):
            next_level = set()
            for idx in current_level:
                current_node = self.graph[idx]
                if current_node.kind == "variable":
                    continue
                for successor in self.graph.successor_indices(idx):
                    if successor not in result:
                        result.add(successor)
                        next_level.add(successor)
            current_level = next_level
            if not current_level:
                break

        return result

    def _limited_ancestors(self, start_idx: int, max_depth: int) -> set[int]:
        """Get ancestors up to a maximum depth."""
        result = set()
        current_level = {start_idx}

        for _ in range(max_depth):
            next_level = set()
            for idx in current_level:
                for predecessor in self.graph.predecessor_indices(idx):
                    if predecessor not in result:
                        result.add(predecessor)
                        next_level.add(predecessor)
            current_level = next_level
            if not current_level:
                break

        return result

    def get_edges_from(self, name: str) -> list[tuple[str, str]]:
        """Get all edges originating from a node."""
        idx = self._node_indices.get(name)
        if idx is None:
            return []

        edges = []
        for successor_idx in self.graph.successor_indices(idx):
            edge_data = self.graph.get_edge_data(idx, successor_idx)
            if edge_data is not None:
                edges.append((edge_data, self.graph[successor_idx].name))

        return edges

    def get_edges_to(self, name: str) -> list[tuple[str, str]]:
        """Get all edges pointing to a node."""
        idx = self._node_indices.get(name)
        if idx is None:
            return []

        edges = []
        for predecessor_idx in self.graph.predecessor_indices(idx):
            edge_data = self.graph.get_edge_data(predecessor_idx, idx)
            if edge_data is not None:
                edges.append((edge_data, self.graph[predecessor_idx].name))

        return edges

    def to_dict(self, query: Optional[str] = None, reverse: bool = False, depth: int = -1) -> dict:
        """Export graph or subgraph as dictionary matching result schema."""
        if query:
            if reverse:
                node_names = {query} | set(self.ancestors(query, depth))
            else:
                node_names = {query} | set(self.descendants(query, depth))
        else:
            node_names = set(self._nodes.keys())

        nodes = []
        for name in sorted(node_names):
            node = self._nodes.get(name)
            if node:
                node_data = {
                    "id": node.name,
                    "type": "component" if node.kind in VERSIONED_KINDS else node.kind,
                    "kind": node.kind,
                }
                parts = node.name.split(".")
                if len(parts) >= 2 and parts[0] in LAYER_NAMES:
                    node_data["layer"] = parts[0]
                nodes.append(node_data)

        # Targeted edge iteration: only visit edges of nodes in the subgraph
        edges = []
        seen_edges = set()
        for name in node_names:
            idx = self._node_indices.get(name)
            if idx is None:
                continue
            for successor_idx in self.graph.successor_indices(idx):
                target_node = self.graph[successor_idx]
                if target_node.name not in node_names:
                    continue
                edge_type = self.graph.get_edge_data(idx, successor_idx)
                edge_key = (name, target_node.name, edge_type)
                if edge_key not in seen_edges:
                    seen_edges.add(edge_key)
                    edges.append({
                        "source": name,
                        "target": target_node.name,
                        "type": edge_type,
                    })

        return {
            "query": {
                "node": query or "",
                "reverse": reverse,
                "depth": depth,
            },
            "nodes": nodes,
            "edges": edges,
            "stats": {
                "total_nodes": len(nodes),
                "total_edges": len(edges),
            },
        }


# Task file names we care about
_TASK_FILENAMES = frozenset(("dependencies.yaml", "main.yaml", "configuration.yaml"))

# Dirs to prune from traversal
_PRUNE_DIRS = frozenset((".git", ".plasma", ".plasmactl", "ansible_collections", "dist", "img", "inst"))


class GraphBuilder:
    """Builds the platform graph from the prepared codebase."""

    def __init__(self, prepare_dir: str):
        self.prepare_dir = Path(prepare_dir)
        self.graph = PlatformGraph()
        self._known_variables: set[str] = set()

    def build(self, debug: bool = False) -> PlatformGraph:
        """Build the complete platform graph.

        Pipeline: single-threaded os.walk discovers components/group_vars inline
        and pre-reads scannable files. Then forks workers for CPU-bound Jinja2 AST
        variable detection (find_undeclared_variables).
        """
        import time

        def timed(name: str, fn):
            start = time.time()
            fn()
            if debug:
                print(f"  {name}: {time.time() - start:.3f}s", file=sys.stderr)

        total_start = time.time()

        # 1. Discover structure (fast, small files, need full YAML)
        timed("discover_chassis", self._discover_chassis)
        timed("discover_nodes", self._discover_nodes)

        # 2. Single-pass walk: discover components + group_vars inline,
        #    collect defaults + deps + scannable for deferred processing
        walk_start = time.time()

        defaults_pending: list[tuple[str, list[str]]] = []
        deps_pending: list[tuple[str, str, list[str]]] = []
        scan_data: list[tuple[str, str]] = []  # (text_content, component_name) — pre-read

        prepare_str = str(self.prepare_dir)
        prepare_len = len(prepare_str) + 1
        add_node = self.graph.add_node
        add_edge = self.graph.add_edge
        known_vars = self._known_variables
        n_comp = 0
        n_gv = 0

        for dirpath, dirnames, filenames in os.walk(prepare_str):
            rel = dirpath[prepare_len:] if len(dirpath) > prepare_len else ""
            parts = rel.split(os.sep) if rel else []
            depth = len(parts)

            if depth == 0:
                dirnames[:] = [d for d in dirnames if d not in _PRUNE_DIRS]
                continue

            # --- group_vars / cfg/roles ---
            if depth >= 3 and (
                parts[depth - 2] == "group_vars"
                or (parts[depth - 3] == "cfg" and parts[depth - 2] == "roles")
            ):
                chassis_path = parts[-2]
                for fn in filenames:
                    is_vault = fn == "vault.yaml"
                    if not is_vault and fn != "vars.yaml":
                        continue
                    fp = os.path.join(dirpath, fn)
                    try:
                        with open(fp, 'rb') as f:
                            content = f.read()
                        if content.startswith(b'$ANSIBLE_VAULT'):
                            continue
                        keys = TOPLEVEL_KEY_PATTERN.findall(content)
                        var_names = [k.decode() for k in keys if not k.startswith(b'_')]
                        if not var_names:
                            continue
                        add_node(Node(name=chassis_path, kind="chassis", versioned=False))
                        for vn in var_names:
                            known_vars.add(vn)
                            add_node(Node(name=vn, kind="variable", versioned=False,
                                         attrs={"chassis": chassis_path, "vault": is_vault, "source": "group_vars"}))
                            add_edge(chassis_path, vn, "overrides")
                        n_gv += 1
                    except Exception:
                        pass
                continue

            # --- role-based paths: <top>/<kind_plural>/roles/<component>/<subdir>/... ---
            if depth < 5 or parts[2] != "roles":
                continue

            subdir = parts[4]
            component_name = f"{parts[0]}.{parts[1]}.{parts[3]}"

            # meta/plasma.yaml → discover component inline
            if depth == 5 and subdir == "meta":
                for fn in filenames:
                    if fn != "plasma.yaml":
                        continue
                    kind = KIND_MAP.get(parts[1])
                    if not kind:
                        continue
                    fp = os.path.join(dirpath, fn)
                    try:
                        with open(fp, 'rb') as f:
                            content = f.read()
                        if b'plasma:' not in content:
                            continue
                        version = None
                        m = VERSION_PATTERN.search(content)
                        if m:
                            version = m.group(1).decode('utf-8', errors='ignore').strip()
                        path = fp[:fp.rfind(os.sep, 0, fp.rfind(os.sep))]
                        add_node(Node(name=component_name, kind=kind,
                                      versioned=kind in VERSIONED_KINDS, version=version, path=path))
                        n_comp += 1
                    except Exception:
                        pass
                continue

            # defaults/main.yaml → collect for deferred processing
            if depth == 5 and subdir == "defaults":
                for fn in filenames:
                    if fn != "main.yaml":
                        continue
                    fp = os.path.join(dirpath, fn)
                    try:
                        with open(fp, 'rb') as f:
                            content = f.read()
                        keys = TOPLEVEL_KEY_PATTERN.findall(content)
                        var_names = [k.decode() for k in keys if not k.startswith(b'_')]
                        if var_names:
                            defaults_pending.append((component_name, var_names))
                    except Exception:
                        pass
                continue

            # tasks/{dependencies,main,configuration}.yaml → deps + variable scan
            if depth == 5 and subdir == "tasks":
                for fn in filenames:
                    if fn not in _TASK_FILENAMES:
                        continue
                    fp = os.path.join(dirpath, fn)
                    try:
                        with open(fp, 'rb') as f:
                            content = f.read()
                        if b'include_role' in content:
                            targets = INCLUDE_ROLE_PATTERN.findall(content.decode('utf-8', errors='ignore'))
                            if targets:
                                source_kind = KIND_MAP.get(parts[1], "helper")
                                deps_pending.append((component_name, source_kind, targets))
                        # Also scan task files for variable references
                        if b'{{' in content or b'{%' in content:
                            scan_data.append((content.decode('utf-8', errors='ignore'), component_name))
                    except Exception:
                        pass
                continue

            # Scannable files (templates, configs) → pre-read for scan phase
            for fn in filenames:
                if fn.endswith(".j2") or fn.endswith(".yaml"):
                    fp = os.path.join(dirpath, fn)
                    try:
                        with open(fp, 'rb') as f:
                            content = f.read()
                        if b'{{' in content or b'{%' in content:
                            scan_data.append((content.decode('utf-8', errors='ignore'), component_name))
                    except Exception:
                        pass

        if debug:
            print(f"  walk_and_discover: {time.time() - walk_start:.3f}s (components: {n_comp}, group_vars: {n_gv}, defaults: {len(defaults_pending)}, deps: {len(deps_pending)}, scannable: {len(scan_data)})", file=sys.stderr)

        # 3. Apply defaults (all components now in graph)
        def_start = time.time()
        get_node = self.graph.get_node
        for component_name, var_names in defaults_pending:
            for vn in var_names:
                known_vars.add(vn)
                add_node(Node(name=vn, kind="variable", versioned=False,
                              attrs={"component": component_name, "source": "defaults"}))
                if get_node(component_name):
                    add_edge(component_name, vn, "defines")
        if debug:
            print(f"  apply_defaults: {time.time() - def_start:.3f}s", file=sys.stderr)

        # 4. Apply dependencies
        dep_start = time.time()
        for source_name, source_kind, targets in deps_pending:
            if not get_node(source_name):
                continue
            for target_role in targets:
                if not get_node(target_role):
                    target_kind = self._guess_kind_from_name(target_role)
                    add_node(Node(name=target_role, kind=target_kind,
                                  versioned=target_kind in VERSIONED_KINDS))
                edge_type = self._determine_edge_type(source_kind, target_role)
                add_edge(source_name, target_role, edge_type)
        if debug:
            print(f"  apply_dependencies: {time.time() - dep_start:.3f}s", file=sys.stderr)

        # 5. Chassis attachments
        timed("build_chassis_attachments", self._build_chassis_attachments)

        # 6. Variable edges — scan templates with Jinja2 AST (parallelised via raw fork)
        #    Workers inherit scan_data + known_vars via COW — zero pickling of inputs.
        scan_start = time.time()
        known_components = set(self.graph._nodes.keys())
        scan_data[:] = [(t, cn) for t, cn in scan_data if cn in known_components]
        n_files = len(scan_data)
        cpus = os.cpu_count() or 4
        n_workers = min(8, cpus)
        all_hits = _fork_scan(scan_data, known_vars, n_workers)
        for vn, cn in all_hits:
            add_edge(vn, cn, "parametrizes")
        if debug:
            print(f"  build_variable_edges: {time.time() - scan_start:.3f}s (files: {n_files}, workers: {n_workers})", file=sys.stderr)
            print(f"  TOTAL: {time.time() - total_start:.3f}s", file=sys.stderr)

        return self.graph

    def _discover_chassis(self) -> None:
        """Discover chassis hierarchy from chassis.yaml."""
        chassis_file = self.prepare_dir / "chassis.yaml"
        if not chassis_file.exists():
            return

        try:
            with open(chassis_file) as f:
                data = yaml.load(f, Loader=YamlLoader)

            if not data:
                return

            self.graph.add_node(Node(name="platform", kind="platform", versioned=False))
            self._add_chassis_hierarchy(data, "platform")

        except Exception as e:
            print(f"Warning: Failed to parse {chassis_file}: {e}", file=sys.stderr)

    def _add_chassis_hierarchy(self, data: dict | list, parent: str) -> None:
        """Recursively add chassis nodes from hierarchy."""
        if isinstance(data, dict):
            for key, value in data.items():
                if key == "platform":
                    self._add_chassis_hierarchy(value, "platform")
                else:
                    chassis_name = f"{parent}.{key}"
                    self.graph.add_node(Node(name=chassis_name, kind="chassis", versioned=False))
                    self.graph.add_edge(parent, chassis_name, "contains")
                    if value:
                        self._add_chassis_hierarchy(value, chassis_name)
        elif isinstance(data, list):
            for item in data:
                if isinstance(item, str):
                    chassis_name = f"{parent}.{item}"
                    self.graph.add_node(Node(name=chassis_name, kind="chassis", versioned=False))
                    self.graph.add_edge(parent, chassis_name, "contains")
                elif isinstance(item, dict):
                    self._add_chassis_hierarchy(item, parent)

    def _discover_nodes(self) -> None:
        """Discover physical/virtual nodes from inst directory."""
        inst_dir = self.prepare_dir / "inst"
        if not inst_dir.exists():
            return

        for platform_path in inst_dir.iterdir():
            if not platform_path.is_dir():
                continue

            nodes_dir = platform_path / "nodes"
            if not nodes_dir.exists():
                continue

            for node_file in nodes_dir.glob("*.yaml"):
                self._add_node_from_yaml(node_file)

    def _add_node_from_yaml(self, node_file: Path) -> None:
        """Add a physical node from its YAML definition."""
        try:
            with open(node_file) as f:
                data = yaml.load(f, Loader=YamlLoader)

            if not data:
                return

            hostname = data.get("hostname", node_file.stem)
            chassis_list = data.get("chassis", [])

            node = Node(
                name=hostname,
                kind="node",
                versioned=False,
                path=str(node_file),
                attrs={"chassis": chassis_list},
            )
            self.graph.add_node(node)

            for chassis in chassis_list:
                chassis_node = Node(name=chassis, kind="chassis", versioned=False)
                self.graph.add_node(chassis_node)
                self.graph.add_edge(hostname, chassis, "memberof")

        except Exception as e:
            print(f"Warning: Failed to parse {node_file}: {e}", file=sys.stderr)

    def _build_chassis_attachments(self) -> None:
        """Build chassis-to-component attachments from layer playbooks."""
        for playbook in self.prepare_dir.glob("*/*.yaml"):
            if playbook.name in ("galaxy.yaml", "galaxy.yml"):
                continue
            if playbook.stem == playbook.parent.name:
                self._parse_playbook(playbook)

    def _parse_playbook(self, playbook: Path) -> None:
        """Parse a playbook to extract chassis-to-component attachments."""
        try:
            with open(playbook) as f:
                data = yaml.load(f, Loader=YamlLoader)

            if not data or not isinstance(data, list):
                return

            for play in data:
                if not isinstance(play, dict):
                    continue

                hosts = play.get("hosts", "")
                roles = play.get("roles", [])

                if not hosts or not roles:
                    continue

                chassis_node = Node(name=hosts, kind="chassis", versioned=False)
                self.graph.add_node(chassis_node)

                for role in roles:
                    if isinstance(role, str):
                        role_name = role
                    elif isinstance(role, dict):
                        role_name = role.get("role") or role.get("name", "")
                    else:
                        continue

                    if role_name and self.graph.get_node(role_name):
                        self.graph.add_edge(hosts, role_name, "attaches")

        except Exception as e:
            print(f"Warning: Failed to parse {playbook}: {e}", file=sys.stderr)


    def _guess_kind_from_name(self, name: str) -> str:
        """Guess component kind from its name."""
        parts = name.split(".")
        if len(parts) >= 2:
            return KIND_MAP.get(parts[1], "helper")
        return "helper"

    def _determine_edge_type(self, source_kind: str, target_name: str) -> str:
        """Determine edge type based on source kind and target name."""
        parts = target_name.split(".")
        if len(parts) >= 2:
            target_kind = KIND_MAP.get(parts[1], "")

            if source_kind == "agent" and target_kind == "skill":
                return "orchestrates"
            if source_kind == "application" and target_kind == "service":
                return "orchestrates"
            if source_kind == "skill" and target_kind == "function":
                return "configures"
            if source_kind == "service" and target_kind == "software":
                return "configures"
            if source_kind in ("function", "software") and target_kind == "library":
                return "computes"
            if source_kind == "library" and target_kind in ("entity", "metric"):
                return "implements"
            if target_kind == "builder":
                return "builds"
            if target_kind == "helper":
                return "uses"

        return "depends"



class TreeRenderer:
    """Renders graph as a tree view."""

    def __init__(self, graph: PlatformGraph):
        self.graph = graph

    def render(self, query: Optional[str] = None, reverse: bool = False, depth: int = -1) -> str:
        """Render graph or subgraph as tree."""
        if not query:
            return self._render_full_graph()

        node = self.graph.get_node(query)
        if not node:
            return f"Node not found: {query}"

        lines = [f"{node.name} ({node.kind})"]

        if reverse:
            self._render_ancestors(query, lines, depth, "")
        else:
            self._render_descendants(query, lines, depth, "")

        return "\n".join(lines)

    def _render_full_graph(self) -> str:
        """Render statistics for the full graph."""
        nodes_by_kind: dict[str, int] = {}
        for node in self.graph._nodes.values():
            nodes_by_kind[node.kind] = nodes_by_kind.get(node.kind, 0) + 1

        lines = ["Platform Graph Summary", ""]
        lines.append("Nodes by kind:")
        for kind, count in sorted(nodes_by_kind.items()):
            lines.append(f"  {kind}: {count}")

        lines.append(f"\nTotal nodes: {len(self.graph._nodes)}")
        lines.append(f"Total edges: {self.graph.graph.num_edges()}")

        return "\n".join(lines)

    def _render_descendants(self, name: str, lines: list[str], depth: int, prefix: str, visited: set[str] | None = None) -> None:
        """Render descendants as tree."""
        if visited is None:
            visited = set()

        if name in visited:
            return
        visited.add(name)

        edges = self.graph.get_edges_from(name)

        edges_by_type: dict[str, list[str]] = {}
        for edge_type, target in edges:
            if edge_type not in edges_by_type:
                edges_by_type[edge_type] = []
            edges_by_type[edge_type].append(target)

        type_items = list(edges_by_type.items())
        for i, (edge_type, targets) in enumerate(type_items):
            is_last_type = i == len(type_items) - 1
            type_connector = "└── " if is_last_type else "├── "

            lines.append(f"{prefix}{type_connector}{edge_type}")
            type_prefix = f"{prefix}    " if is_last_type else f"{prefix}│   "

            for j, target in enumerate(sorted(targets)):
                is_last_target = j == len(targets) - 1
                connector = "└── " if is_last_target else "├── "

                target_node = self.graph.get_node(target)
                kind_str = f" ({target_node.kind})" if target_node else ""
                is_circular = target in visited
                circular_marker = " [circular]" if is_circular else ""
                lines.append(f"{type_prefix}{connector}{target}{kind_str}{circular_marker}")

                is_variable = target_node and target_node.kind == "variable"
                if depth != 0 and not is_circular and not is_variable:
                    new_depth = depth - 1 if depth > 0 else -1
                    child_prefix = f"{type_prefix}    " if is_last_target else f"{type_prefix}│   "
                    self._render_descendants(target, lines, new_depth, child_prefix, visited)

    def _render_ancestors(self, name: str, lines: list[str], depth: int, prefix: str, visited: set[str] | None = None) -> None:
        """Render ancestors as tree."""
        if visited is None:
            visited = set()

        if name in visited:
            return
        visited.add(name)

        edges = self.graph.get_edges_to(name)

        edges_by_type: dict[str, list[str]] = {}
        for edge_type, source in edges:
            inverted_type = f"{edge_type} (by)"
            if inverted_type not in edges_by_type:
                edges_by_type[inverted_type] = []
            edges_by_type[inverted_type].append(source)

        type_items = list(edges_by_type.items())
        for i, (edge_type, sources) in enumerate(type_items):
            is_last_type = i == len(type_items) - 1
            type_connector = "└── " if is_last_type else "├── "

            lines.append(f"{prefix}{type_connector}{edge_type}")
            type_prefix = f"{prefix}    " if is_last_type else f"{prefix}│   "

            for j, source in enumerate(sorted(sources)):
                is_last_source = j == len(sources) - 1
                connector = "└── " if is_last_source else "├── "

                source_node = self.graph.get_node(source)
                kind_str = f" ({source_node.kind})" if source_node else ""
                is_circular = source in visited
                circular_marker = " [circular]" if is_circular else ""
                lines.append(f"{type_prefix}{connector}{source}{kind_str}{circular_marker}")

                if depth != 0 and not is_circular:
                    new_depth = depth - 1 if depth > 0 else -1
                    child_prefix = f"{type_prefix}    " if is_last_source else f"{type_prefix}│   "
                    self._render_ancestors(source, lines, new_depth, child_prefix, visited)


class FormatRenderer:
    """Renders graph in various output formats."""

    def __init__(self, graph: PlatformGraph):
        self.graph = graph

    def render_dot(self, query: Optional[str] = None, reverse: bool = False, depth: int = -1) -> str:
        """Render graph in DOT (Graphviz) format."""
        data = self.graph.to_dict(query, reverse, depth)

        lines = ["digraph platform {"]
        lines.append("  rankdir=LR;")
        lines.append("  node [shape=box, style=rounded];")
        lines.append("")

        kind_colors = {
            "application": "#4CAF50",
            "service": "#2196F3",
            "software": "#9C27B0",
            "function": "#FF9800",
            "skill": "#FFEB3B",
            "agent": "#F44336",
            "library": "#00BCD4",
            "entity": "#8BC34A",
            "metric": "#E91E63",
            "helper": "#607D8B",
            "builder": "#795548",
            "chassis": "#3F51B5",
            "variable": "#9E9E9E",
            "node": "#FF5722",
            "platform": "#673AB7",
        }

        for node in data["nodes"]:
            node_id = node["id"].replace(".", "_").replace("-", "_")
            label = node["id"]
            color = kind_colors.get(node["kind"], "#BDBDBD")
            lines.append(f'  {node_id} [label="{label}", fillcolor="{color}", style="filled,rounded"];')

        lines.append("")

        for edge in data["edges"]:
            source = edge["source"].replace(".", "_").replace("-", "_")
            target = edge["target"].replace(".", "_").replace("-", "_")
            edge_type = edge["type"]
            lines.append(f'  {source} -> {target} [label="{edge_type}"];')

        lines.append("}")
        return "\n".join(lines)

    def render_mermaid(self, query: Optional[str] = None, reverse: bool = False, depth: int = -1) -> str:
        """Render graph in Mermaid format."""
        data = self.graph.to_dict(query, reverse, depth)

        lines = ["graph LR"]

        kind_shapes = {
            "application": ("[[", "]]"),
            "service": ("((", "))"),
            "software": ("[", "]"),
            "function": ("{{", "}}"),
            "skill": (">", "]"),
            "agent": ("([", "])"),
            "library": ("[(", ")]"),
            "entity": ("[/", "/]"),
            "chassis": ("[[", "]]"),
            "variable": ("(", ")"),
        }

        node_ids = {}
        for i, node in enumerate(data["nodes"]):
            node_ids[node["id"]] = f"n{i}"

        for node in data["nodes"]:
            mid = node_ids[node["id"]]
            label = node["id"].split(".")[-1]
            shape = kind_shapes.get(node["kind"], ("[", "]"))
            lines.append(f"  {mid}{shape[0]}{label}{shape[1]}")

        for edge in data["edges"]:
            source = node_ids.get(edge["source"])
            target = node_ids.get(edge["target"])
            if source and target:
                edge_type = edge["type"]
                lines.append(f"  {source} -->|{edge_type}| {target}")

        return "\n".join(lines)

    def render_cytoscape(self, query: Optional[str] = None, reverse: bool = False, depth: int = -1) -> str:
        """Render graph in Cytoscape.js JSON format."""
        data = self.graph.to_dict(query, reverse, depth)

        elements = []

        for node in data["nodes"]:
            elements.append({
                "data": {
                    "id": node["id"],
                    "label": node["id"].split(".")[-1],
                    "kind": node["kind"],
                    "type": node["type"],
                    "layer": node.get("layer", ""),
                },
                "group": "nodes",
            })

        for i, edge in enumerate(data["edges"]):
            elements.append({
                "data": {
                    "id": f"e{i}",
                    "source": edge["source"],
                    "target": edge["target"],
                    "type": edge["type"],
                },
                "group": "edges",
            })

        return json.dumps({"elements": elements}, indent=2)


def main():
    parser = argparse.ArgumentParser(description="Query the platform dependency graph")
    parser.add_argument("query", nargs="?", default="", help="Node to query")
    parser.add_argument("--reverse", type=lambda x: x.lower() == "true", default=False)
    parser.add_argument("--depth", type=int, default=-1)
    parser.add_argument("--tree", type=lambda x: x.lower() == "true", default=False,
                        help="Show as tree (shorthand for --format=tree)")
    parser.add_argument("--format", default="auto",
                        choices=["auto", "tree", "dot", "mermaid", "cytoscape"],
                        help="Output format")
    parser.add_argument("--debug", type=lambda x: x.lower() == "true", default=False,
                        help="Show timing debug info")

    args = parser.parse_args()

    if args.tree:
        args.format = "tree"

    prepare_dir = os.getcwd()

    if not any(Path(prepare_dir).glob("*/*/roles")):
        print(f"Error: Current directory does not appear to be a prepare directory", file=sys.stderr)
        print("Run 'plasmactl model:prepare' first.", file=sys.stderr)
        sys.exit(1)

    builder = GraphBuilder(prepare_dir)
    graph = builder.build(debug=args.debug)

    query = args.query if args.query else None

    if args.format == "auto":
        result = graph.to_dict(query=query, reverse=args.reverse, depth=args.depth)
        print(json.dumps(result))

    elif args.format == "tree":
        renderer = TreeRenderer(graph)
        print(renderer.render(query=query, reverse=args.reverse, depth=args.depth))

    elif args.format == "dot":
        renderer = FormatRenderer(graph)
        print(renderer.render_dot(query=query, reverse=args.reverse, depth=args.depth))

    elif args.format == "mermaid":
        renderer = FormatRenderer(graph)
        print(renderer.render_mermaid(query=query, reverse=args.reverse, depth=args.depth))

    elif args.format == "cytoscape":
        renderer = FormatRenderer(graph)
        print(renderer.render_cytoscape(query=query, reverse=args.reverse, depth=args.depth))


if __name__ == "__main__":
    main()

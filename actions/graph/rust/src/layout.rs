use std::collections::HashMap;

use petgraph::graph::DiGraph;
use petgraph::visit::EdgeRef;
use rayon::prelude::*;

use crate::graph::Node;
use crate::types::layer_names;

const THETA: f64 = 1.2;
const KR: f64 = 2.0;
const KG: f64 = 1.0;
const ITERATIONS: usize = 100;
const EMPTY: u32 = u32::MAX;
const MAX_DEPTH: u32 = 40;

// ---- Barnes-Hut QuadTree ----

struct QTCell {
    x0: f64,
    y0: f64,
    x1: f64,
    y1: f64,
    cx: f64,
    cy: f64,
    mass: f64,
    children: [u32; 4],
    body: u32,
}

struct QuadTree {
    cells: Vec<QTCell>,
}

impl QuadTree {
    fn build(positions: &[(f64, f64)], masses: &[f64]) -> Self {
        let n = positions.len();
        if n == 0 {
            return Self { cells: Vec::new() };
        }
        let (mut x0, mut y0) = (f64::MAX, f64::MAX);
        let (mut x1, mut y1) = (f64::MIN, f64::MIN);
        for &(x, y) in positions {
            x0 = x0.min(x);
            y0 = y0.min(y);
            x1 = x1.max(x);
            y1 = y1.max(y);
        }
        let pad = (x1 - x0).max(y1 - y0) * 0.01 + 1.0;
        let mut qt = Self {
            cells: Vec::with_capacity(n * 4),
        };
        qt.cells.push(QTCell {
            x0: x0 - pad,
            y0: y0 - pad,
            x1: x1 + pad,
            y1: y1 + pad,
            cx: 0.0,
            cy: 0.0,
            mass: 0.0,
            children: [EMPTY; 4],
            body: EMPTY,
        });
        for i in 0..n {
            qt.insert(0, positions[i].0, positions[i].1, masses[i], i as u32, positions, masses, 0);
        }
        qt
    }

    fn insert(
        &mut self,
        cell_idx: u32,
        px: f64,
        py: f64,
        mass: f64,
        body: u32,
        positions: &[(f64, f64)],
        masses: &[f64],
        depth: u32,
    ) {
        let ci = cell_idx as usize;

        // Update center of mass
        let old_mass = self.cells[ci].mass;
        let new_mass = old_mass + mass;
        if new_mass > 0.0 {
            self.cells[ci].cx = (self.cells[ci].cx * old_mass + px * mass) / new_mass;
            self.cells[ci].cy = (self.cells[ci].cy * old_mass + py * mass) / new_mass;
        }
        self.cells[ci].mass = new_mass;

        if depth >= MAX_DEPTH {
            return;
        }

        let existing = self.cells[ci].body;
        let has_children = self.cells[ci].children[0] != EMPTY;

        if !has_children && existing == EMPTY {
            self.cells[ci].body = body;
            return;
        }

        if !has_children {
            self.subdivide(cell_idx);
            self.cells[ci].body = EMPTY;
            let (ex, ey) = positions[existing as usize];
            let em = masses[existing as usize];
            let q = self.quadrant(cell_idx, ex, ey);
            let child = self.cells[ci].children[q];
            self.insert(child, ex, ey, em, existing, positions, masses, depth + 1);
        }

        let q = self.quadrant(cell_idx, px, py);
        let child = self.cells[ci].children[q];
        self.insert(child, px, py, mass, body, positions, masses, depth + 1);
    }

    fn subdivide(&mut self, cell_idx: u32) {
        let ci = cell_idx as usize;
        let (x0, y0, x1, y1) = (
            self.cells[ci].x0,
            self.cells[ci].y0,
            self.cells[ci].x1,
            self.cells[ci].y1,
        );
        let mx = (x0 + x1) * 0.5;
        let my = (y0 + y1) * 0.5;
        let base = self.cells.len() as u32;
        let empty = |x0, y0, x1, y1| QTCell {
            x0,
            y0,
            x1,
            y1,
            cx: 0.0,
            cy: 0.0,
            mass: 0.0,
            children: [EMPTY; 4],
            body: EMPTY,
        };
        self.cells.push(empty(x0, y0, mx, my));
        self.cells.push(empty(mx, y0, x1, my));
        self.cells.push(empty(x0, my, mx, y1));
        self.cells.push(empty(mx, my, x1, y1));
        self.cells[ci].children = [base, base + 1, base + 2, base + 3];
    }

    fn quadrant(&self, cell_idx: u32, px: f64, py: f64) -> usize {
        let ci = cell_idx as usize;
        let mx = (self.cells[ci].x0 + self.cells[ci].x1) * 0.5;
        let my = (self.cells[ci].y0 + self.cells[ci].y1) * 0.5;
        if py < my {
            if px < mx { 0 } else { 1 }
        } else {
            if px < mx { 2 } else { 3 }
        }
    }

    fn repulsion_force(
        &self,
        cell_idx: u32,
        node_idx: usize,
        px: f64,
        py: f64,
        node_mass: f64,
    ) -> (f64, f64) {
        if cell_idx == EMPTY {
            return (0.0, 0.0);
        }
        let ci = cell_idx as usize;
        let cell = &self.cells[ci];
        if cell.mass == 0.0 {
            return (0.0, 0.0);
        }

        // Leaf node
        if cell.body != EMPTY && cell.children[0] == EMPTY {
            if cell.body as usize == node_idx {
                return (0.0, 0.0);
            }
            let dx = px - cell.cx;
            let dy = py - cell.cy;
            let dist_sq = dx * dx + dy * dy;
            if dist_sq < 1e-8 {
                return (0.0, 0.0);
            }
            let dist = dist_sq.sqrt();
            let f = KR * node_mass * cell.mass / dist;
            return (f * dx / dist, f * dy / dist);
        }

        // Barnes-Hut criterion
        let dx = px - cell.cx;
        let dy = py - cell.cy;
        let dist_sq = dx * dx + dy * dy;
        if dist_sq < 1e-8 {
            return (0.0, 0.0);
        }
        let dist = dist_sq.sqrt();
        let size = cell.x1 - cell.x0;
        if size / dist < THETA && cell.body == EMPTY {
            let f = KR * node_mass * cell.mass / dist;
            return (f * dx / dist, f * dy / dist);
        }

        // Recurse into children
        let mut fx = 0.0;
        let mut fy = 0.0;
        for &child in &cell.children {
            let (cfx, cfy) = self.repulsion_force(child, node_idx, px, py, node_mass);
            fx += cfx;
            fy += cfy;
        }
        (fx, fy)
    }
}

// ---- ForceAtlas2 Layout ----

pub fn compute_layout(graph: &DiGraph<Node, String>) -> Vec<(f64, f64)> {
    let n = graph.node_count();
    if n <= 1 {
        return vec![(0.0, 0.0); n];
    }

    let mut pos = initialize_positions(graph);
    let mut old_forces: Vec<(f64, f64)> = vec![(0.0, 0.0); n];

    let masses: Vec<f64> = graph
        .node_indices()
        .map(|i| graph.edges(i).count() as f64 + 1.0)
        .collect();

    let edges: Vec<(usize, usize)> = graph
        .edge_references()
        .map(|e| (e.source().index(), e.target().index()))
        .collect();

    let mut speed = 1.0;
    let mut speed_efficiency = 1.0;

    for _ in 0..ITERATIONS {
        // Build quadtree
        let qt = QuadTree::build(&pos, &masses);

        // Repulsion (parallel via rayon)
        let mut forces: Vec<(f64, f64)> = (0..n)
            .into_par_iter()
            .map(|i| qt.repulsion_force(0, i, pos[i].0, pos[i].1, masses[i]))
            .collect();

        // Attraction (LinLog)
        for &(s, t) in &edges {
            let dx = pos[t].0 - pos[s].0;
            let dy = pos[t].1 - pos[s].1;
            let dist = (dx * dx + dy * dy).sqrt().max(0.01);
            let f = dist.ln_1p() / dist;
            forces[s].0 += f * dx;
            forces[s].1 += f * dy;
            forces[t].0 -= f * dx;
            forces[t].1 -= f * dy;
        }

        // Gravity
        for i in 0..n {
            let dist = (pos[i].0.powi(2) + pos[i].1.powi(2)).sqrt().max(0.01);
            forces[i].0 -= KG * masses[i] * pos[i].0 / dist;
            forces[i].1 -= KG * masses[i] * pos[i].1 / dist;
        }

        // Adaptive speed (ForceAtlas2)
        let mut total_swing = 0.0f64;
        let mut total_traction = 0.0f64;
        for i in 0..n {
            let sdx = forces[i].0 - old_forces[i].0;
            let sdy = forces[i].1 - old_forces[i].1;
            let swing = (sdx * sdx + sdy * sdy).sqrt();
            let tdx = forces[i].0 + old_forces[i].0;
            let tdy = forces[i].1 + old_forces[i].1;
            let traction = (tdx * tdx + tdy * tdy).sqrt() * 0.5;
            total_swing += masses[i] * swing;
            total_traction += masses[i] * traction;
        }

        if total_swing > 0.0 {
            let jt = (0.05 * (total_traction / total_swing).sqrt()).clamp(0.05, 10.0);
            if total_swing > jt * total_traction {
                let target = jt * jt * total_traction / total_swing;
                if target < speed {
                    speed_efficiency *= 0.7;
                }
                speed = (speed_efficiency * target).clamp(0.01, 10.0);
            }
        }

        // Apply forces with per-node speed limit
        for i in 0..n {
            let sdx = forces[i].0 - old_forces[i].0;
            let sdy = forces[i].1 - old_forces[i].1;
            let swing = (sdx * sdx + sdy * sdy).sqrt();
            let factor = speed / (1.0 + speed * swing.sqrt());
            pos[i].0 += forces[i].0 * factor;
            pos[i].1 += forces[i].1 * factor;
        }

        old_forces = forces;
    }

    pos
}

fn initialize_positions(graph: &DiGraph<Node, String>) -> Vec<(f64, f64)> {
    let layers = layer_names();

    // X-axis: main chain (Active → Passive), matching ontology columns
    let kind_x: HashMap<&str, f64> = [
        ("node", 0.0),          // Execution
        ("platform", 1000.0),   // Materialization
        ("model", 2000.0),      // Composition
        ("package", 2500.0),    // Composition (sub)
        ("chassis", 3000.0),    // Distribution
        ("agent", 4000.0),      // Orchestration (top path)
        ("application", 4000.0),// Orchestration (bottom path)
        ("skill", 5000.0),      // Configuration (top path)
        ("service", 5000.0),    // Configuration (bottom path)
        ("function", 6000.0),   // Computation (top path)
        ("software", 6000.0),   // Computation (bottom path)
        ("builder", 6500.0),    // Build
        ("helper", 5500.0),     // Utility
        ("library", 7000.0),    // Implementation
        ("entity", 8000.0),     // Definition
        ("metric", 8000.0),     // Definition
        ("variable", 4500.0),   // Cross-cutting
    ]
    .into_iter()
    .collect();

    // Y-axis: layers (top → bottom), matching ontology rows
    let layer_y: HashMap<&str, f64> = [
        ("stabilization", 0.0),
        ("conversation", 2000.0),
        ("interaction", 3000.0),
        ("cognition", 4000.0),
        ("integration", 5000.0),
        ("foundation", 6000.0),
    ]
    .into_iter()
    .collect();

    // Separate the two parallel paths vertically within a layer
    let path_offset: HashMap<&str, f64> = [
        ("agent", -300.0),      // Top path
        ("skill", -300.0),
        ("function", -300.0),
        ("application", 300.0), // Bottom path
        ("service", 300.0),
        ("software", 300.0),
    ]
    .into_iter()
    .collect();

    graph
        .node_indices()
        .map(|idx| {
            let node = &graph[idx];
            let parts: Vec<&str> = node.name.splitn(3, '.').collect();

            let base_x = *kind_x.get(node.kind.as_str()).unwrap_or(&4500.0);
            let base_y = if !parts.is_empty() && layers.contains(parts[0]) {
                *layer_y.get(parts[0]).unwrap_or(&3000.0)
            } else {
                3000.0
            };
            let offset = *path_offset.get(node.kind.as_str()).unwrap_or(&0.0);

            // Deterministic noise from name hash
            let h = djb2(&node.name);
            let nx = ((h & 0xFF) as f64 - 128.0) * 3.0;
            let ny = (((h >> 8) & 0xFF) as f64 - 128.0) * 3.0;

            (base_x + nx, base_y + offset + ny)
        })
        .collect()
}

fn djb2(s: &str) -> u64 {
    let mut h: u64 = 5381;
    for b in s.bytes() {
        h = h.wrapping_mul(33).wrapping_add(b as u64);
    }
    h
}

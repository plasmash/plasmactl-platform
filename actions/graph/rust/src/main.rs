mod builder;
mod graph;
mod layout;
mod render;
mod types;

use std::path::PathBuf;
use std::time::Instant;

use clap::Parser;

use builder::GraphBuilder;
use render::{MermaidRenderer, TreeRenderer};

#[derive(Parser)]
#[command(name = "platform-graph", about = "Query the platform dependency graph")]
struct Cli {
    /// Node to query (component, chassis, variable, node). Empty for full graph.
    #[arg(default_value = "")]
    query: String,

    /// Show reverse dependencies
    #[arg(long, default_value = "false")]
    reverse: String,

    /// Maximum traversal depth (-1 for unlimited)
    #[arg(long, default_value = "-1")]
    depth: i32,

    /// Show as tree (shorthand for --format=tree)
    #[arg(long, default_value = "false")]
    tree: String,

    /// Output format: auto (tree), tree, json, graphml, mermaid
    #[arg(long, default_value = "auto")]
    format: String,

    /// Directory containing composed model
    #[arg(long = "compose-dir", default_value = ".plasma/model/compose/merged")]
    compose_dir: String,

    /// Show timing debug information
    #[arg(long, default_value = "false")]
    debug: String,
}

fn main() {
    let cli = Cli::parse();

    let reverse = cli.reverse == "true";
    let debug = cli.debug == "true";
    let tree = cli.tree == "true";
    let mut format = cli.format.clone();
    if tree {
        format = "tree".to_string();
    }

    let compose_dir = PathBuf::from(&cli.compose_dir);
    if !compose_dir.join("src").exists() {
        eprintln!(
            "Error: {} does not contain a src directory",
            compose_dir.display()
        );
        eprintln!("Run 'plasmactl model:compose' first.");
        std::process::exit(1);
    }

    let builder = GraphBuilder::new(&compose_dir);
    let graph = builder.build(debug);

    let query = if cli.query.is_empty() {
        None
    } else {
        Some(cli.query.as_str())
    };

    match format.as_str() {
        "auto" | "tree" => {
            let renderer = TreeRenderer::new(&graph);
            println!("{}", renderer.render(query, reverse, cli.depth));
        }
        "json" => {
            let data = graph.to_dict(query, reverse, cli.depth);
            println!("{}", serde_json::to_string_pretty(&data).unwrap());
        }
        "graphml" => {
            let t = Instant::now();
            let positions = layout::compute_layout(&graph.graph);
            if debug {
                eprintln!(
                    "  layout: {:.3}s ({} nodes, {} iterations)",
                    t.elapsed().as_secs_f64(),
                    graph.node_count(),
                    100
                );
            }
            print!("{}", graph.to_graphml(query, reverse, cli.depth, Some(&positions)));
        }
        "mermaid" => {
            let renderer = MermaidRenderer::new(&graph);
            println!("{}", renderer.render(query, reverse, cli.depth));
        }
        _ => {
            eprintln!("Unknown format: {}. Use: tree, json, graphml, mermaid", format);
            std::process::exit(1);
        }
    }
}

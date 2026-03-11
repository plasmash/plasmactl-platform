use regex::Regex;
use std::collections::HashSet;
use std::sync::OnceLock;

/// Map plural directory names to singular kind.
pub fn kind_from_plural(plural: &str) -> Option<&'static str> {
    match plural {
        "applications" => Some("application"),
        "services" => Some("service"),
        "softwares" => Some("software"),
        "agents" => Some("agent"),
        "skills" => Some("skill"),
        "functions" => Some("function"),
        "libraries" => Some("library"),
        "entities" => Some("entity"),
        "metrics" => Some("metric"),
        "builders" => Some("builder"),
        "helpers" => Some("helper"),
        "executors" => Some("agent"),
        "flows" => Some("agent"),
        _ => None,
    }
}

/// Kinds that carry a version.
pub fn is_versioned(kind: &str) -> bool {
    matches!(
        kind,
        "agent"
            | "application"
            | "skill"
            | "service"
            | "function"
            | "software"
            | "library"
            | "entity"
            | "metric"
            | "builder"
            | "helper"
    )
}

/// Layer names for quick lookup.
static LAYER_NAMES: OnceLock<HashSet<&'static str>> = OnceLock::new();

pub fn layer_names() -> &'static HashSet<&'static str> {
    LAYER_NAMES.get_or_init(|| {
        [
            "foundation",
            "integration",
            "cognition",
            "conversation",
            "interaction",
            "stabilization",
        ]
        .into_iter()
        .collect()
    })
}

// --- Compiled regex patterns ---

static VERSION_PATTERN: OnceLock<Regex> = OnceLock::new();
static TOPLEVEL_KEY_PATTERN: OnceLock<Regex> = OnceLock::new();
static INCLUDE_ROLE_PATTERN: OnceLock<Regex> = OnceLock::new();

pub fn version_re() -> &'static Regex {
    VERSION_PATTERN.get_or_init(|| {
        Regex::new(r#"(?m)^\s+version:\s*["']?([^"'#\n\r]+)"#).unwrap()
    })
}

pub fn toplevel_key_re() -> &'static Regex {
    TOPLEVEL_KEY_PATTERN.get_or_init(|| {
        Regex::new(r"(?m)^([a-zA-Z_][a-zA-Z0-9_]*):").unwrap()
    })
}

pub fn include_role_re() -> &'static Regex {
    INCLUDE_ROLE_PATTERN.get_or_init(|| {
        Regex::new(r"include_role:\s*\n\s*name:\s*([a-zA-Z_][a-zA-Z0-9_.]*)").unwrap()
    })
}

/// Determine edge type based on source kind and target name.
pub fn determine_edge_type(source_kind: &str, target_name: &str) -> &'static str {
    let parts: Vec<&str> = target_name.splitn(3, '.').collect();
    if parts.len() >= 2 {
        let target_kind = kind_from_plural(parts[1]).unwrap_or("");
        match (source_kind, target_kind) {
            ("agent", "skill") => return "orchestrates",
            ("application", "service") => return "orchestrates",
            ("skill", "function") => return "configures",
            ("service", "software") => return "configures",
            ("function" | "software", "library") => return "computes",
            ("library", "entity" | "metric") => return "implements",
            (_, "builder") => return "builds",
            (_, "helper") => return "uses",
            _ => {}
        }
    }
    "depends"
}

/// Guess node kind from its name.
pub fn guess_kind(name: &str) -> &'static str {
    if name.starts_with("platform.") && name[9..].contains('.') {
        return "zone";
    }
    let parts: Vec<&str> = name.splitn(3, '.').collect();
    if parts.len() >= 2 {
        return kind_from_plural(parts[1]).unwrap_or("variable");
    }
    "variable"
}

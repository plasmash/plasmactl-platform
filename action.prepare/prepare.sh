#!/bin/bash
set -e

# Parse arguments
CLEAN="${1:-false}"

# Define paths
COMPOSE_IMAGE_DIR="${COMPOSE_IMAGE_DIR:-.plasma/compose/image}"
PREPARE_DIR="${PREPARE_DIR:-.plasma/prepare}"

echo "==> Plasma Prepare Action"
echo "    Compose image: $COMPOSE_IMAGE_DIR"
echo "    Prepare dir: $PREPARE_DIR"
echo "    Clean: $CLEAN"

# Clean if requested
if [ "$CLEAN" = "true" ]; then
    echo "==> Cleaning existing prepare directory"
    rm -rf "$PREPARE_DIR"
fi

# Ensure prepare directory exists
mkdir -p "$PREPARE_DIR"

# Copy compose image to prepare directory (keeps structure as-is)
echo "==> Copying compose image to prepare directory"
rsync -a --delete "$COMPOSE_IMAGE_DIR/" "$PREPARE_DIR/"

# Detect layout
if [ -d "$PREPARE_DIR/src" ]; then
    echo "==> Detected modern layout (src/ directory)"
    COMPONENTS_BASE="$PREPARE_DIR/src"
    COLLECTIONS_PATH="./src"
    LAYOUT="modern"
else
    echo "==> Detected legacy layout (root directory)"
    COMPONENTS_BASE="$PREPARE_DIR"
    COLLECTIONS_PATH="."
    LAYOUT="legacy"
fi

# Create ansible.cfg with appropriate collections_path
if [ ! -f "$PREPARE_DIR/ansible.cfg" ]; then
    echo "==> Creating ansible.cfg (collections_path=$COLLECTIONS_PATH)"
    sed "s|COLLECTIONS_PATH_PLACEHOLDER|$COLLECTIONS_PATH|" \
        /action/files/ansible.cfg.template > "$PREPARE_DIR/ansible.cfg"
else
    echo "==> ansible.cfg already exists, skipping"
fi

# Create ansible_collections symlink in the right location
if [ "$LAYOUT" = "modern" ]; then
    SYMLINK_LOCATION="$COMPONENTS_BASE/ansible_collections"
else
    SYMLINK_LOCATION="$PREPARE_DIR/ansible_collections"
fi

if [ ! -L "$SYMLINK_LOCATION" ]; then
    echo "==> Creating ansible_collections symlink"
    ln -sf . "$SYMLINK_LOCATION"
else
    echo "==> ansible_collections symlink already exists, skipping"
fi

# Ensure library/ structure exists
if [ ! -d "$PREPARE_DIR/library" ]; then
    echo "==> Copying library/ structure"
    cp -r /action/files/library "$PREPARE_DIR/library"
else
    echo "==> library/ already exists, skipping"
fi

# Check for roles/ structure (backward compatibility)
# Look for pattern: {layer}/{type}/roles/
ROLES_EXISTS=false
for layer_dir in "$PREPARE_DIR"/{platform,interaction,integration,cognition,conversation,stabilization,foundation}; do
    if [ -d "$layer_dir" ]; then
        for type_dir in "$layer_dir"/*; do
            if [ -d "$type_dir/roles" ]; then
                ROLES_EXISTS=true
                echo "==> Found roles/ structure at $type_dir/roles"
                break 2
            fi
        done
    fi
done

if [ "$ROLES_EXISTS" = "false" ]; then
    echo "==> No roles/ structure detected, components are already runtime-agnostic"
    echo "    (This is expected for new-style repositories)"
fi

# Create platform symlinks in layer variables directories
echo "==> Creating platform symlinks in layer variables directories"
for layer_dir in "$COMPONENTS_BASE"/{foundation,interaction,integration,cognition,conversation,stabilization}; do
    if [ -d "$layer_dir/variables" ]; then
        if [ ! -e "$layer_dir/variables/platform" ]; then
            echo "    Creating symlink: $layer_dir/variables/platform"
            ln -sf ../../platform/variables/platform "$layer_dir/variables/platform"
        else
            echo "    Symlink already exists: $layer_dir/variables/platform"
        fi
    fi
done

# Generate galaxy.yml files for Ansible Galaxy collections
echo "==> Generating galaxy.yml files for component collections"
for layer_dir in "$COMPONENTS_BASE"/{platform,interaction,integration,cognition,conversation,stabilization,foundation}; do
    if [ -d "$layer_dir" ]; then
        layer_name=$(basename "$layer_dir")

        for type_dir in "$layer_dir"/*; do
            if [ -d "$type_dir" ] && [ "$(basename "$type_dir")" != "variables" ]; then
                type_name=$(basename "$type_dir")
                galaxy_file="$type_dir/galaxy.yml"

                if [ ! -f "$galaxy_file" ]; then
                    echo "    Creating: $galaxy_file"
                    sed -e "s/NAMESPACE_PLACEHOLDER/$layer_name/" \
                        -e "s/NAME_PLACEHOLDER/$type_name/" \
                        /action/files/galaxy.yml.template > "$galaxy_file"
                fi
            fi
        done
    fi
done

# Create Ansible module symlinks in helper plugins directories
echo "==> Creating Ansible module symlinks in helper plugins directories"
for plugins_dir in $(find "$COMPONENTS_BASE" -type d -path "*/helpers/plugins/modules" -not -path "*/.git/*"); do
    if [ -d "$plugins_dir" ]; then
        # Calculate relative path to library/modules
        # Typically: ../../../../library/modules for {layer}/helpers/plugins/modules
        # Or: ../../../../../library/modules for src/{layer}/helpers/plugins/modules
        relative_path=$(realpath --relative-to="$plugins_dir" "$PREPARE_DIR/library/modules")

        # Create symlinks for each module in library/modules
        if [ -d "$PREPARE_DIR/library/modules" ]; then
            for module_dir in "$PREPARE_DIR/library/modules"/*; do
                if [ -d "$module_dir" ]; then
                    module_name=$(basename "$module_dir")
                    module_file="$module_dir/${module_name}.py"
                    symlink_path="$plugins_dir/${module_name}.py"

                    if [ -f "$module_file" ] && [ ! -e "$symlink_path" ]; then
                        echo "    Creating symlink: $symlink_path -> $relative_path/${module_name}/${module_name}.py"
                        ln -sf "$relative_path/${module_name}/${module_name}.py" "$symlink_path"
                    fi
                fi
            done
        fi
    fi
done

echo "==> Prepare complete!"
echo "    Layout: $LAYOUT"
echo "    Components base: $COMPONENTS_BASE"
echo "    Collections path: $COLLECTIONS_PATH"
echo "    Runtime-ready directory: $PREPARE_DIR"

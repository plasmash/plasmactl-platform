# plasmactl-platform

A [Launchr](https://github.com/launchrctl/launchr) plugin for [Plasmactl](https://github.com/plasmash/plasmactl) that provides platform lifecycle management for Plasma platforms.

## Overview

`plasmactl-platform` orchestrates the complete platform deployment workflow including creation, validation, deployment, and destruction. It integrates with CI/CD systems and provides both local and remote deployment capabilities.

## Features

- **Platform Lifecycle**: Create, list, show, validate, deploy, destroy platforms
- **Multi-Step Orchestration**: Executes bump, compose, prepare, and deploy in sequence
- **CI/CD Integration**: Triggers pipelines in GitLab, GitHub Actions, and other systems
- **DNS Configuration**: Automatic DNS setup (MX, DKIM, DMARC, SPF, rDNS)
- **Environment-Aware**: Deploy to dev, staging, production environments

## Commands

### Platform Commands

#### platform:up

Full deployment workflow (bump → compose → prepare → deploy):

```bash
# Deploy a chassis section
plasmactl platform:up dev platform.interaction.observability

# Deploy a specific application
plasmactl platform:up dev interaction.applications.dashboards

# Local deployment (skip CI/CD)
plasmactl platform:up --local dev platform.interaction.observability

# Skip certain phases
plasmactl platform:up --skip-bump --skip-prepare dev platform.interaction.observability
```

Options:
- `--skip-bump`: Skip version bumping
- `--skip-prepare`: Skip prepare phase
- `--local`: Run deployment locally instead of via CI/CD
- `--clean`: Clean compose working directory
- `--clean-prepare`: Clean prepare directory
- `--debug`: Enable Ansible debug mode
- `--img`: Deploy from a Platform Image (.pi) file

#### platform:create

Create a new platform scaffold with DNS configuration:

```bash
plasmactl platform:create ski-dev \
  --metal-provider scaleway \
  --dns-provider ovh \
  --domain dev.skilld.cloud
```

Options:
- `--metal-provider`: Infrastructure provider (scaleway, hetzner, ovh, aws, gcp, azure)
- `--dns-provider`: DNS provider (ovh, cloudflare, route53)
- `--domain`: Domain name for the platform
- `--skip-dns`: Skip DNS configuration

#### platform:list

List all platforms:

```bash
plasmactl platform:list
plasmactl platform:list --format json
```

Options:
- `--format`: Output format (table, json, yaml)

#### platform:show

Show platform details:

```bash
plasmactl platform:show ski-dev
plasmactl platform:show ski-dev --format json
```

Options:
- `--format`: Output format (table, json, yaml)

#### platform:validate

Validate platform configuration:

```bash
plasmactl platform:validate ski-dev
plasmactl platform:validate ski-dev --skip-dns --skip-mail
```

Options:
- `--skip-dns`: Skip DNS validation
- `--skip-mail`: Skip mail configuration validation

#### platform:deploy

Deploy to a platform (Ansible deployment):

```bash
plasmactl platform:deploy dev platform.interaction.observability
plasmactl platform:deploy dev interaction.applications.connect --debug
```

Options:
- `--debug`: Enable Ansible debug mode
- `--check`: Dry-run mode (no changes)
- `--img`: Deploy from Platform Image
- `--prepare-dir`: Custom prepare directory

#### platform:destroy

Destroy a platform (requires confirmation):

```bash
plasmactl platform:destroy ski-dev

# With confirmation bypass (for automation)
plasmactl platform:destroy ski-dev --yes-i-am-sure
```

Options:
- `--yes-i-am-sure`: Skip confirmation prompt

## Project Structure

```
plasmactl-platform/
├── plugin.go                        # Plugin registration
├── actions/
│   ├── create/
│   │   ├── create.yaml              # Action definition
│   │   └── create.go                # Implementation
│   ├── deploy/
│   │   ├── deploy.yaml
│   │   └── deploy.go
│   ├── destroy/
│   │   ├── destroy.yaml
│   │   └── destroy.go
│   ├── list/
│   │   ├── list.yaml
│   │   └── list.go
│   ├── show/
│   │   ├── show.yaml
│   │   └── show.go
│   ├── up/
│   │   ├── up.yaml
│   │   └── up.go
│   └── validate/
│       ├── validate.yaml
│       └── validate.go
└── internal/
    ├── ci/                          # CI/CD integration
    │   └── ci.go                    # Pipeline triggering
    └── git/                         # Git operations
        └── git.go                   # Repository operations
```

## Deployment Workflow

### Full Workflow

```bash
plasmactl platform:up dev platform.interaction.observability
```

Executes:
1. **Bump**: `plasmactl component:bump`
2. **Compose**: `plasmactl model:compose`
3. **Prepare**: `plasmactl model:prepare`
4. **Deploy**: `plasmactl platform:deploy`

### End-to-End Platform Setup

```bash
# 1. Create platform with DNS
plasmactl platform:create ski-dev \
  --metal-provider scaleway \
  --dns-provider ovh \
  --domain dev.skilld.cloud

# 2. Provision nodes (via plasmactl-node)
plasmactl node:provision ski-dev -c foundation.cluster.control:GP1-L:3

# 3. Deploy
plasmactl platform:deploy ski-dev
```

## Directory Structure

Platforms are stored in `inst/`:

```
inst/
└── ski-dev/
    ├── platform.yaml      # Platform configuration
    └── nodes/             # Node definitions
        └── *.yaml
```

## CI/CD Integration

### GitLab CI

```bash
# Store credentials
plasmactl keyring:login gitlab
```

The plugin triggers GitLab CI pipelines when deploying without `--local`.

### GitHub Actions

```bash
plasmactl keyring:login github
```

## Related Commands

| Plugin | Command | Purpose |
|--------|---------|---------|
| plasmactl-node | `node:provision` | Provision infrastructure |
| plasmactl-node | `node:allocate` | Allocate nodes to chassis |
| plasmactl-model | `model:compose` | Compose packages |
| plasmactl-model | `model:prepare` | Prepare for deployment |
| plasmactl-component | `component:bump` | Bump versions |

## Documentation

- [Plasmactl](https://github.com/plasmash/plasmactl) - Main CLI tool
- [plasmactl-node](https://github.com/plasmash/plasmactl-node) - Node provisioning
- [plasmactl-model](https://github.com/plasmash/plasmactl-model) - Model composition
- [Plasma Platform](https://plasma.sh) - Platform documentation

## License

[European Union Public License 1.2 (EUPL-1.2)](LICENSE)

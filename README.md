# plasmactl-platform

A [Launchr](https://github.com/launchrctl/launchr) plugin for [Plasmactl](https://github.com/plasmash/plasmactl) that provides platform lifecycle management for Plasma platforms.

## Overview

`plasmactl-platform` orchestrates the complete platform deployment workflow including composition, version bumping, packaging, publishing, and deployment. It integrates with CI/CD systems and provides both local and remote deployment capabilities.

## Features

- **Multi-Step Orchestration**: Executes compose, bump, package, publish, and deploy in sequence
- **CI/CD Integration**: Triggers pipelines in GitLab, GitHub Actions, and other systems
- **Artifact Management**: Create and publish deployment packages
- **Release Management**: Create git tags with changelogs
- **Environment-Aware**: Deploy to dev, staging, production environments
- **Chassis-Based Deployment**: Target specific platform sections

## Commands

### platform:ship

Orchestrate deployment to an environment:

```bash
# Deploy a chassis section
plasmactl platform:ship dev platform.interaction.observability

# Deploy a specific application
plasmactl platform:ship dev interaction.applications.dashboards

# Local deployment (skip CI/CD)
plasmactl platform:ship --local dev platform.interaction.observability
```

Options:
- `--skip-bump`: Skip version bumping
- `--skip-prepare`: Skip prepare phase
- `--local`: Run deployment locally instead of via CI/CD
- `--clean`: Clean compose working directory
- `--debug`: Enable Ansible debug mode

### platform:package

Create a deployment artifact (tar.gz archive):

```bash
plasmactl platform:package
```

Creates artifact in `.plasma/platform/package/artifacts/`.

### platform:publish

Upload artifact to repository:

```bash
plasmactl platform:publish
```

Options:
- `--username`: Repository username
- `--password`: Repository password

### platform:release

Create a git tag with changelog:

```bash
plasmactl platform:release
```

## Platform-Specific Actions

The following actions must be provided by your platform package (e.g., [plasma-core](https://github.com/plasmash/pla-plasma)):

### platform:prepare

Prepare runtime environment with Ansible requirements. Provided by the platform package at `src/platform/actions/prepare/`.

### platform:deploy

Deploy Ansible resources to target cluster. Provided by the platform package at `src/platform/actions/deploy/`.

**Note**: `platform:ship` validates these actions exist at runtime:
- `platform:deploy` is **mandatory** - deployment fails if not found
- `platform:prepare` is **optional** - automatically skipped if not found

## Deployment Workflow

### Full Workflow

```bash
plasmactl platform:ship dev platform.interaction.observability
```

Executes:
1. **Compose**: `plasmactl package:compose`
2. **Bump**: `plasmactl component:bump`
3. **Prepare**: `plasmactl platform:prepare` (if available)
4. **Sync**: `plasmactl component:sync`
5. **Deploy**: `plasmactl platform:deploy`

### Chassis-Based Deployment

Deploy via chassis attachment point:

```bash
# From platform repository
plasmactl platform:ship dev platform.interaction.interop
```

**Important**: Chassis deployment applies variable overrides from `group_vars` based on the chassis attachment point.

### Direct Application Deployment

Deploy a specific application:

```bash
plasmactl platform:ship dev interaction.applications.connect
```

**Note**: Direct deployment bypasses chassis-specific variable overrides.

## Environments

### Standard Environments

- **dev**: Development environment for testing
- **staging**: Pre-production environment
- **prod**: Production environment

### Environment Configuration

Each environment has:
- Target Kubernetes cluster
- Node assignments
- Resource allocations
- Security policies
- Environment-specific variables

## CI/CD Integration

### GitLab CI (Current)

The plugin integrates with GitLab CI using Ory authentication:

```bash
# Store credentials
plasmactl keyring:set ory_client_id
plasmactl keyring:set ory_client_secret
```

### GitHub Actions (Planned)

Future GitHub Actions integration with GitHub CLI authentication.

## Local Deployment

Run deployment locally without CI/CD:

```bash
plasmactl platform:ship --local dev platform.interaction.observability
```

Useful for:
- Development and testing
- Debugging deployment issues
- Quick iterations

## Workflow Examples

### Complete Release and Deploy

```bash
# 1. Create release tag
plasmactl platform:release

# 2. Ship to dev
plasmactl platform:ship dev platform.interaction.observability

# 3. After testing, ship to prod
plasmactl platform:ship prod platform.interaction.observability
```

### Iterative Development

```bash
# Make changes, commit
git commit -m "feat: add new component"

# Quick deploy to dev
plasmactl platform:ship dev platform.interaction.observability --skip-bump
```

## Best Practices

1. **Use Chassis Deployment**: Prefer chassis-based deployment for proper variable resolution
2. **Test in Dev First**: Always deploy to dev before staging/production
3. **Monitor Pipelines**: Watch CI/CD pipeline execution for errors
4. **Incremental Updates**: Deploy small, tested changes frequently
5. **Version Control**: Ensure all changes are committed before shipping

## Documentation

- [Plasmactl](https://github.com/plasmash/plasmactl) - Main CLI tool
- [Plasma Platform](https://plasma.sh) - Platform documentation

## License

[European Union Public License 1.2 (EUPL-1.2)](LICENSE)

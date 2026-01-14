# plasmactl-ship

A [Launchr](https://github.com/launchrctl/launchr) plugin for [Plasmactl](https://github.com/plasmash/plasmactl) that orchestrates platform deployment by triggering CI/CD pipelines.

## Overview

`plasmactl-ship` automates the deployment of Plasma platforms by triggering CI/CD pipelines in various systems. It handles the complete deployment workflow including composition, version bumping, packaging, publishing, and execution.

## Features

- **Multi-Step Orchestration**: Executes compose, bump, package, publish, and deploy in sequence
- **CI/CD Integration**: Triggers pipelines in GitLab, GitHub Actions, and other systems
- **Secure Authentication**: Supports multiple auth methods (Ory, GitHub CLI, tokens)
- **Environment-Aware**: Deploy to dev, staging, production environments
- **Chassis-Based Deployment**: Target specific platform sections

## Usage

### Basic Deployment

Deploy a chassis section to an environment:

```bash
plasmactl ship dev platform.interaction.observability
```

This will:
1. Run `plasmactl compose`
2. Run `plasmactl bump --sync`
3. Run `plasmactl package`
4. Run `plasmactl publish`
5. Trigger CI/CD pipeline for deployment

### Deploy Specific Application

Deploy a single application by its MRN (Machine Resource Name):

```bash
plasmactl ship dev interaction.applications.dashboards
```

### Skip Steps

Skip specific steps in the workflow:

```bash
plasmactl ship dev platform.interaction.observability --skip-bump --skip-package
```

## Configuration

### Supported CI/CD Systems

The plugin currently supports GitLab with Ory authentication and is being extended to support:

- **GitHub Actions** - With GitHub CLI or PAT authentication
- **GitLab CI** - With Ory or token authentication
- **Jenkins** - With API token authentication
- **Custom Systems** - Via webhook integration

**Note**: Multi-backend support is under development. See [OPEN_SOURCE_PLAN.md](https://github.com/plasmash/pla-plasma/.claude/OPEN_SOURCE_PLAN.md) for details.

### Store Credentials

Store CI/CD system credentials in keyring:

```bash
# For GitLab with Ory
plasmactl keyring:set ory_client_id
plasmactl keyring:set ory_client_secret

# For GitHub
gh auth login
```

## Arguments

```
plasmactl ship <environment> <target> [options]
```

### Positional Arguments

- `<environment>`: Target environment (dev, staging, prod)
- `<target>`: Chassis section or application MRN

### Options

- `--skip-compose`: Skip composition step
- `--skip-bump`: Skip version bumping
- `--skip-package`: Skip packaging step
- `--skip-publish`: Skip artifact publishing
- `--local`: Run deployment locally instead of via CI/CD

## Deployment Workflow

### Full Workflow

```bash
plasmactl ship dev platform.interaction.observability
```

Executes:
1. **Compose**: `plasmactl compose --conflicts-verbosity --skip-not-versioned`
2. **Bump**: `plasmactl bump --sync`
3. **Package**: `plasmactl package`
4. **Publish**: `plasmactl publish`
5. **Deploy**: Triggers CI/CD pipeline with environment and target parameters

### Chassis-Based Deployment

Deploying via chassis attachment point:

```bash
# From platform repository
cd /path/to/ski-platform

# Deploy all applications attached to this chassis section
plasmactl ship dev platform.interaction.interop
```

**Important**: Chassis deployment applies variable overrides from `group_vars` based on the chassis attachment point.

### Direct Application Deployment

Deploying a specific application:

```bash
plasmactl ship dev interaction.applications.connect
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

## Chassis vs Application Deployment

### When to Use Chassis

Use chassis deployment when:
- The application has multiple attachment points with different configurations
- You need chassis-specific variable overrides
- Deploying a group of related applications

Example:
```bash
plasmactl ship dev platform.interaction.observability
```

### When to Use Direct Application

Use direct application deployment when:
- The application has a single attachment point
- No chassis-specific configuration is needed
- Deploying a standalone application

Example:
```bash
plasmactl ship dev interaction.applications.dashboards
```

## CI/CD Integration

### GitLab CI (Current)

The plugin integrates with GitLab CI using Ory authentication:

```yaml
# .gitlab-ci.yml (automatically triggered)
deploy:
  script:
    - ansible-playbook deploy.yaml -e env=$ENVIRONMENT -e target=$TARGET
  environment:
    name: $ENVIRONMENT
```

### GitHub Actions (Planned)

Future GitHub Actions integration:

```yaml
# .github/workflows/deploy.yml (triggered via workflow_dispatch)
deploy:
  steps:
    - name: Deploy Platform
      run: ansible-playbook deploy.yaml
      env:
        ENVIRONMENT: ${{ inputs.environment }}
        TARGET: ${{ inputs.target }}
```

## Local Deployment

Run deployment locally without CI/CD:

```bash
plasmactl ship --local dev platform.interaction.observability
```

This executes all steps on your local machine, useful for:
- Development and testing
- Debugging deployment issues
- Quick iterations

## Best Practices

1. **Use Chassis Deployment**: Prefer chassis-based deployment for proper variable resolution
2. **Test in Dev First**: Always deploy to dev before staging/production
3. **Monitor Pipelines**: Watch CI/CD pipeline execution for errors
4. **Incremental Updates**: Deploy small, tested changes frequently
5. **Version Control**: Ensure all changes are committed before shipping

## Error Handling

### "CI/CD pipeline trigger failed"
Check your authentication credentials:
```bash
plasmactl keyring:set ory_client_id
plasmactl keyring:set ory_client_secret
```

### "Deployment failed"
Check the CI/CD pipeline logs for specific errors.

### "Chassis not found"
Verify the chassis section exists in the layer playbook (e.g., `interaction/interaction.yaml`).

## Workflow Examples

### Complete Release and Deploy

```bash
# 1. Create release tag
plasmactl release --tag v1.2.0

# 2. Ship to dev
plasmactl ship dev platform.interaction.observability

# 3. After testing, ship to prod
plasmactl ship prod platform.interaction.observability
```

### Iterative Development

```bash
# Make changes, commit
git commit -m "feat: add new component"

# Quick deploy to dev (skip unnecessary steps)
plasmactl ship dev platform.interaction.observability --skip-bump
```

## Documentation

- [Plasmactl](https://github.com/plasmash/plasmactl) - Main CLI tool
- [Plasma Platform](https://plasma.sh) - Platform documentation
- [Open Source Plan](https://github.com/plasmash/pla-plasma/.claude/OPEN_SOURCE_PLAN.md) - Multi-backend roadmap

## License

Apache License 2.0

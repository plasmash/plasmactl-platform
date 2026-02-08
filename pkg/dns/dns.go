package dns

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// Config holds DNS provider configuration for HCL generation
type Config struct {
	Provider     string // ovh, cloudflare, route53, gandi, inwx
	APIToken     string
	APIUsername   string // Username for providers that need it (e.g., INWX)
	Domain       string // Full platform domain (e.g., dev.skilld.cloud)
	Zone         string // DNS zone (e.g., skilld.cloud)
	Subdomain    string // Derived: prefix before zone (e.g., dev)
	Region       string // Provider region (e.g., "eu" for OVH)
	WebIP        string // Web ingress failover IP (for A records)
	MailIP       string // Mail ingress failover IP (for MX, SPF)
	DKIMSelector string // DKIM selector (default: "default")
	DKIMKey      string // DKIM public key (base64)
}

// providerTemplates maps DNS provider names to their HCL templates
var providerTemplates = map[string]string{}

// RegisterTemplate registers an HCL template for a DNS provider
func RegisterTemplate(provider, tmpl string) {
	providerTemplates[provider] = tmpl
}

// Manager handles DNS Terraform operations
type Manager struct {
	workDir string
	tf      *tfexec.Terraform
}

// NewManager creates a new DNS terraform manager
func NewManager(envDir string) (*Manager, error) {
	workDir := filepath.Join(envDir, ".terraform-dns")

	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dns terraform directory: %w", err)
	}

	execPath, err := findTerraformBinary()
	if err != nil {
		return nil, err
	}

	tf, err := tfexec.NewTerraform(workDir, execPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create terraform instance: %w", err)
	}

	return &Manager{
		workDir: workDir,
		tf:      tf,
	}, nil
}

// findTerraformBinary finds tofu or terraform in PATH
func findTerraformBinary() (string, error) {
	for _, name := range []string{"tofu", "terraform"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("neither tofu nor terraform found in PATH")
}

// GenerateHCL generates DNS Terraform HCL for the given provider config
func (m *Manager) GenerateHCL(config Config) error {
	mainFile := filepath.Join(m.workDir, "main.tf")

	tmplStr, ok := providerTemplates[config.Provider]
	if !ok {
		return fmt.Errorf("no DNS terraform template registered for provider %q", config.Provider)
	}

	tmpl, err := template.New("main.tf").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("failed to parse DNS template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		return fmt.Errorf("failed to execute DNS template: %w", err)
	}

	if err := os.WriteFile(mainFile, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write DNS main.tf: %w", err)
	}

	return nil
}

// Apply runs terraform init + apply for DNS
func (m *Manager) Apply(ctx context.Context) error {
	if err := m.tf.Init(ctx, tfexec.Upgrade(true)); err != nil {
		return fmt.Errorf("terraform init failed: %w", err)
	}
	if err := m.tf.Apply(ctx); err != nil {
		return fmt.Errorf("terraform apply failed: %w", err)
	}
	return nil
}

// GetWorkDir returns the DNS terraform working directory
func (m *Manager) GetWorkDir() string {
	return m.workDir
}

// DeriveSubdomain extracts the subdomain from domain relative to zone.
// e.g., domain="dev.skilld.cloud", zone="skilld.cloud" → "dev"
func DeriveSubdomain(domain, zone string) string {
	suffix := "." + zone
	if strings.HasSuffix(domain, suffix) {
		return strings.TrimSuffix(domain, suffix)
	}
	// Domain is the zone itself
	if domain == zone {
		return ""
	}
	return domain
}

// DeriveZone extracts the zone from a domain by taking the last two segments.
// e.g., "dev.skilld.cloud" → "skilld.cloud"
func DeriveZone(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return domain
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

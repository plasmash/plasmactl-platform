package validate

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// Validate implements the platform:validate command
type Validate struct {
	Log      *launchr.Logger
	Term     *launchr.Terminal
	Name     string
	SkipDNS  bool
	SkipMail bool
}

// SetLogger sets the logger for the action
func (v *Validate) SetLogger(log *launchr.Logger) {
	v.Log = log
}

// SetTerm sets the terminal for the action
func (v *Validate) SetTerm(term *launchr.Terminal) {
	v.Term = term
}

// Execute runs the platform:validate action
func (v *Validate) Execute() error {
	instDir := filepath.Join("inst", v.Name)
	platformFile := filepath.Join(instDir, "platform.yaml")

	// Check if platform exists
	if _, err := os.Stat(platformFile); os.IsNotExist(err) {
		return fmt.Errorf("platform %q not found", v.Name)
	}

	// Read platform.yaml
	data, err := os.ReadFile(platformFile)
	if err != nil {
		return fmt.Errorf("failed to read platform.yaml: %w", err)
	}

	var platform schema.Platform
	if err := yaml.Unmarshal(data, &platform); err != nil {
		return fmt.Errorf("failed to parse platform.yaml: %w", err)
	}

	v.Term.Info().Printfln("Validating platform %q...", v.Name)
	v.Term.Info().Println()

	hasErrors := false

	// Validate basic configuration
	v.Term.Info().Println("Basic Configuration:")
	if platform.Name == "" {
		v.Term.Error().Println("  ✗ Name is missing")
		hasErrors = true
	} else {
		v.Term.Success().Printfln("  ✓ Name: %s", platform.Name)
	}

	if platform.Infrastructure.MetalProvider == "" {
		v.Term.Error().Println("  ✗ Metal provider is missing")
		hasErrors = true
	} else {
		v.Term.Success().Printfln("  ✓ Metal provider: %s", platform.Infrastructure.MetalProvider)
	}

	if platform.DNS.Domain == "" {
		v.Term.Warning().Println("  ! Domain is not configured")
	} else {
		v.Term.Success().Printfln("  ✓ Domain: %s", platform.DNS.Domain)
	}

	// Validate DNS if not skipped
	if !v.SkipDNS && platform.DNS.Domain != "" {
		v.Term.Info().Println()
		v.Term.Info().Println("DNS Records:")
		v.validateDNS(platform.DNS.Domain, &hasErrors)
	}

	// Validate mail authentication if not skipped
	if !v.SkipMail && platform.DNS.Domain != "" {
		v.Term.Info().Println()
		v.Term.Info().Println("Mail Authentication:")
		v.validateMailAuth(platform.DNS.Domain, &hasErrors)
	}

	// Check nodes directory
	nodesDir := filepath.Join(instDir, "nodes")
	nodeCount := 0
	if nodeEntries, err := os.ReadDir(nodesDir); err == nil {
		for _, nodeEntry := range nodeEntries {
			if !nodeEntry.IsDir() && filepath.Ext(nodeEntry.Name()) == ".yaml" && nodeEntry.Name() != ".gitkeep" {
				nodeCount++
			}
		}
	}

	v.Term.Info().Println()
	v.Term.Info().Println("Infrastructure:")
	if nodeCount == 0 {
		v.Term.Warning().Println("  ! No nodes provisioned")
	} else {
		v.Term.Success().Printfln("  ✓ Nodes: %d", nodeCount)
	}

	v.Term.Info().Println()
	if hasErrors {
		v.Term.Error().Println("Validation failed with errors")
		return fmt.Errorf("validation failed")
	}

	v.Term.Success().Println("Validation passed")
	return nil
}

// validateDNS checks DNS records for the domain
func (v *Validate) validateDNS(domain string, hasErrors *bool) {
	// Check MX records
	mxRecords, err := net.LookupMX(domain)
	if err != nil || len(mxRecords) == 0 {
		v.Term.Warning().Println("  ! MX records not found")
	} else {
		v.Term.Success().Printfln("  ✓ MX records: %d found", len(mxRecords))
		for _, mx := range mxRecords {
			v.Term.Info().Printfln("      %s (priority %d)", mx.Host, mx.Pref)
		}
	}

	// Check A/AAAA records
	ips, err := net.LookupIP(domain)
	if err != nil || len(ips) == 0 {
		v.Term.Warning().Println("  ! A/AAAA records not found")
	} else {
		v.Term.Success().Printfln("  ✓ A/AAAA records: %d found", len(ips))
	}
}

// validateMailAuth checks DKIM, DMARC, and SPF records
func (v *Validate) validateMailAuth(domain string, hasErrors *bool) {
	// Check SPF record
	txtRecords, _ := net.LookupTXT(domain)
	hasSPF := false
	for _, txt := range txtRecords {
		if strings.HasPrefix(txt, "v=spf1") {
			hasSPF = true
			v.Term.Success().Println("  ✓ SPF record found")
			break
		}
	}
	if !hasSPF {
		v.Term.Warning().Println("  ! SPF record not found")
	}

	// Check DMARC record
	dmarcRecords, _ := net.LookupTXT("_dmarc." + domain)
	hasDMARC := false
	for _, txt := range dmarcRecords {
		if strings.HasPrefix(txt, "v=DMARC1") {
			hasDMARC = true
			v.Term.Success().Println("  ✓ DMARC record found")
			break
		}
	}
	if !hasDMARC {
		v.Term.Warning().Println("  ! DMARC record not found")
	}

	// Check DKIM record (common selector: default)
	dkimRecords, _ := net.LookupTXT("default._domainkey." + domain)
	hasDKIM := false
	for _, txt := range dkimRecords {
		if strings.Contains(txt, "v=DKIM1") {
			hasDKIM = true
			v.Term.Success().Println("  ✓ DKIM record found (selector: default)")
			break
		}
	}
	if !hasDKIM {
		v.Term.Warning().Println("  ! DKIM record not found (selector: default)")
	}
}

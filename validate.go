package plasmactlplatform

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchrctl/launchr"
	"github.com/plasmash/plasmactl-platform/pkg/types"
	"gopkg.in/yaml.v3"
)

// platformValidate implements the platform:validate command
type platformValidate struct {
	log      *launchr.Logger
	term     *launchr.Terminal
	name     string
	skipDNS  bool
	skipMail bool
}

// SetLogger sets the logger for the action
func (a *platformValidate) SetLogger(log *launchr.Logger) {
	a.log = log
}

// SetTerm sets the terminal for the action
func (a *platformValidate) SetTerm(term *launchr.Terminal) {
	a.term = term
}

// Execute runs the platform:validate action
func (a *platformValidate) Execute() error {
	instDir := filepath.Join("inst", a.name)
	platformFile := filepath.Join(instDir, "platform.yaml")

	// Check if platform exists
	if _, err := os.Stat(platformFile); os.IsNotExist(err) {
		return fmt.Errorf("platform %q not found", a.name)
	}

	// Read platform.yaml
	data, err := os.ReadFile(platformFile)
	if err != nil {
		return fmt.Errorf("failed to read platform.yaml: %w", err)
	}

	var platform types.Platform
	if err := yaml.Unmarshal(data, &platform); err != nil {
		return fmt.Errorf("failed to parse platform.yaml: %w", err)
	}

	a.term.Info().Printfln("Validating platform %q...", a.name)
	a.term.Info().Println()

	hasErrors := false

	// Validate basic configuration
	a.term.Info().Println("Basic Configuration:")
	if platform.Name == "" {
		a.term.Error().Println("  ✗ Name is missing")
		hasErrors = true
	} else {
		a.term.Success().Printfln("  ✓ Name: %s", platform.Name)
	}

	if platform.Infrastructure.MetalProvider == "" {
		a.term.Error().Println("  ✗ Metal provider is missing")
		hasErrors = true
	} else {
		a.term.Success().Printfln("  ✓ Metal provider: %s", platform.Infrastructure.MetalProvider)
	}

	if platform.DNS.Domain == "" {
		a.term.Warning().Println("  ! Domain is not configured")
	} else {
		a.term.Success().Printfln("  ✓ Domain: %s", platform.DNS.Domain)
	}

	// Validate DNS if not skipped
	if !a.skipDNS && platform.DNS.Domain != "" {
		a.term.Info().Println()
		a.term.Info().Println("DNS Records:")
		a.validateDNS(platform.DNS.Domain, &hasErrors)
	}

	// Validate mail authentication if not skipped
	if !a.skipMail && platform.DNS.Domain != "" {
		a.term.Info().Println()
		a.term.Info().Println("Mail Authentication:")
		a.validateMailAuth(platform.DNS.Domain, &hasErrors)
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

	a.term.Info().Println()
	a.term.Info().Println("Infrastructure:")
	if nodeCount == 0 {
		a.term.Warning().Println("  ! No nodes provisioned")
	} else {
		a.term.Success().Printfln("  ✓ Nodes: %d", nodeCount)
	}

	a.term.Info().Println()
	if hasErrors {
		a.term.Error().Println("Validation failed with errors")
		return fmt.Errorf("validation failed")
	}

	a.term.Success().Println("Validation passed")
	return nil
}

// validateDNS checks DNS records for the domain
func (a *platformValidate) validateDNS(domain string, hasErrors *bool) {
	// Check MX records
	mxRecords, err := net.LookupMX(domain)
	if err != nil || len(mxRecords) == 0 {
		a.term.Warning().Println("  ! MX records not found")
	} else {
		a.term.Success().Printfln("  ✓ MX records: %d found", len(mxRecords))
		for _, mx := range mxRecords {
			a.term.Info().Printfln("      %s (priority %d)", mx.Host, mx.Pref)
		}
	}

	// Check A/AAAA records
	ips, err := net.LookupIP(domain)
	if err != nil || len(ips) == 0 {
		a.term.Warning().Println("  ! A/AAAA records not found")
	} else {
		a.term.Success().Printfln("  ✓ A/AAAA records: %d found", len(ips))
	}
}

// validateMailAuth checks DKIM, DMARC, and SPF records
func (a *platformValidate) validateMailAuth(domain string, hasErrors *bool) {
	// Check SPF record
	txtRecords, _ := net.LookupTXT(domain)
	hasSPF := false
	for _, txt := range txtRecords {
		if strings.HasPrefix(txt, "v=spf1") {
			hasSPF = true
			a.term.Success().Println("  ✓ SPF record found")
			break
		}
	}
	if !hasSPF {
		a.term.Warning().Println("  ! SPF record not found")
	}

	// Check DMARC record
	dmarcRecords, _ := net.LookupTXT("_dmarc." + domain)
	hasDMARC := false
	for _, txt := range dmarcRecords {
		if strings.HasPrefix(txt, "v=DMARC1") {
			hasDMARC = true
			a.term.Success().Println("  ✓ DMARC record found")
			break
		}
	}
	if !hasDMARC {
		a.term.Warning().Println("  ! DMARC record not found")
	}

	// Check DKIM record (common selector: default)
	dkimRecords, _ := net.LookupTXT("default._domainkey." + domain)
	hasDKIM := false
	for _, txt := range dkimRecords {
		if strings.Contains(txt, "v=DKIM1") {
			hasDKIM = true
			a.term.Success().Println("  ✓ DKIM record found (selector: default)")
			break
		}
	}
	if !hasDKIM {
		a.term.Warning().Println("  ! DKIM record not found (selector: default)")
	}
}

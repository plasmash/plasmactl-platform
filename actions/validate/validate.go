package validate

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"gopkg.in/yaml.v3"
)

// ValidationCheck represents a single validation check result
type ValidationCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "pass", "fail", "warning"
	Value  string `json:"value,omitempty"`
}

// ValidateResult is the structured output for platform:validate
type ValidateResult struct {
	Name       string            `json:"name"`
	Valid      bool              `json:"valid"`
	Checks     []ValidationCheck `json:"checks"`
	NodeCount  int               `json:"node_count"`
	HasErrors  bool              `json:"has_errors"`
	HasWarnings bool             `json:"has_warnings"`
}

// Validate implements the platform:validate command
type Validate struct {
	action.WithLogger
	action.WithTerm

	Name     string
	SkipDNS  bool
	SkipMail bool

	result *ValidateResult
}

// Result returns the structured result for JSON output
func (v *Validate) Result() any {
	return v.result
}

// Execute runs the platform:validate action
func (v *Validate) Execute() error {
	platformDir := filepath.Join("platforms", v.Name)
	platformFile := filepath.Join(platformDir, "platform.yaml")

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

	v.Term().Info().Printfln("Validating platform %q...", v.Name)
	v.Term().Info().Println()

	// Initialize result
	v.result = &ValidateResult{
		Name:   v.Name,
		Checks: []ValidationCheck{},
	}

	hasErrors := false
	hasWarnings := false

	// Validate basic configuration
	v.Term().Info().Println("Basic Configuration:")
	if platform.Name == "" {
		v.Term().Error().Println("  ✗ Name is missing")
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "name", Status: "fail"})
		hasErrors = true
	} else {
		v.Term().Success().Printfln("  ✓ Name: %s", platform.Name)
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "name", Status: "pass", Value: platform.Name})
	}

	if platform.Infrastructure.MetalProvider == "" {
		v.Term().Error().Println("  ✗ Metal provider is missing")
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "metal_provider", Status: "fail"})
		hasErrors = true
	} else {
		v.Term().Success().Printfln("  ✓ Metal provider: %s", platform.Infrastructure.MetalProvider)
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "metal_provider", Status: "pass", Value: platform.Infrastructure.MetalProvider})
	}

	if platform.DNS.Domain == "" {
		v.Term().Warning().Println("  ! Domain is not configured")
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "domain", Status: "warning"})
		hasWarnings = true
	} else {
		v.Term().Success().Printfln("  ✓ Domain: %s", platform.DNS.Domain)
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "domain", Status: "pass", Value: platform.DNS.Domain})
	}

	// Validate network configuration
	v.Term().Info().Println()
	v.Term().Info().Println("Network Configuration:")
	if platform.Networking.PrivateNetwork == "" {
		v.Term().Warning().Println("  ! Private network CIDR not configured")
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "private_network", Status: "warning"})
		hasWarnings = true
	} else if !isValidCIDR(platform.Networking.PrivateNetwork) {
		v.Term().Error().Printfln("  ✗ Invalid private network CIDR: %s", platform.Networking.PrivateNetwork)
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "private_network", Status: "fail", Value: platform.Networking.PrivateNetwork})
		hasErrors = true
	} else {
		v.Term().Success().Printfln("  ✓ Private network: %s", platform.Networking.PrivateNetwork)
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "private_network", Status: "pass", Value: platform.Networking.PrivateNetwork})
	}

	if platform.Networking.PrivateVIPNetwork != "" {
		if !isValidCIDR(platform.Networking.PrivateVIPNetwork) {
			v.Term().Error().Printfln("  ✗ Invalid VIP network CIDR: %s", platform.Networking.PrivateVIPNetwork)
			v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "vip_network", Status: "fail", Value: platform.Networking.PrivateVIPNetwork})
			hasErrors = true
		} else {
			v.Term().Success().Printfln("  ✓ VIP network: %s", platform.Networking.PrivateVIPNetwork)
			v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "vip_network", Status: "pass", Value: platform.Networking.PrivateVIPNetwork})
		}
	}

	// Validate API configuration
	v.Term().Info().Println()
	v.Term().Info().Println("API Configuration:")
	if platform.Infrastructure.MetalProvider != "manual" {
		if platform.Infrastructure.API.Token == "" {
			v.Term().Warning().Println("  ! API token not configured")
			v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "api_token", Status: "warning"})
			hasWarnings = true
		} else if strings.Contains(platform.Infrastructure.API.Token, "{{ .keyring.") {
			v.Term().Success().Println("  ✓ API token configured (keyring reference)")
			v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "api_token", Status: "pass", Value: "keyring"})
		} else {
			v.Term().Warning().Println("  ! API token is hardcoded (consider using keyring)")
			v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "api_token", Status: "warning", Value: "hardcoded"})
			hasWarnings = true
		}
	} else {
		v.Term().Info().Println("  - Skipped (manual provider)")
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "api_token", Status: "pass", Value: "manual"})
	}

	// Validate pool configuration
	v.Term().Info().Println()
	v.Term().Info().Println("Pool Configuration:")
	if len(platform.Pools) == 0 {
		v.Term().Warning().Println("  ! No pools defined")
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "pools", Status: "warning", Value: "0"})
		hasWarnings = true
	} else {
		totalNodes := 0
		for name, pool := range platform.Pools {
			if pool.Count <= 0 {
				v.Term().Error().Printfln("  ✗ Invalid count for pool %s: %d", name, pool.Count)
				v.result.Checks = append(v.result.Checks, ValidationCheck{
					Name:   fmt.Sprintf("pool_%s", name),
					Status: "fail",
					Value:  fmt.Sprintf("%s: invalid count %d", pool.Machine, pool.Count),
				})
				hasErrors = true
			} else if pool.Machine == "" {
				v.Term().Error().Printfln("  ✗ No machine specified for pool %s", name)
				v.result.Checks = append(v.result.Checks, ValidationCheck{
					Name:   fmt.Sprintf("pool_%s", name),
					Status: "fail",
					Value:  "no machine specified",
				})
				hasErrors = true
			} else if len(pool.Zones) == 0 {
				v.Term().Error().Printfln("  ✗ No zones for pool %s", name)
				v.result.Checks = append(v.result.Checks, ValidationCheck{
					Name:   fmt.Sprintf("pool_%s", name),
					Status: "fail",
					Value:  "no zones",
				})
				hasErrors = true
			} else {
				totalNodes += pool.Count
			}
		}
		if totalNodes > 0 {
			v.Term().Success().Printfln("  ✓ Pools: %d pools, %d total nodes", len(platform.Pools), totalNodes)
			v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "pools", Status: "pass", Value: fmt.Sprintf("%d pools", len(platform.Pools))})
		}
	}

	// Validate DNS if not skipped
	if !v.SkipDNS && platform.DNS.Domain != "" {
		v.Term().Info().Println()
		v.Term().Info().Println("DNS Records:")
		v.validateDNS(platform.DNS.Domain, &hasErrors)
	}

	// Validate mail authentication if not skipped
	if !v.SkipMail && platform.DNS.Domain != "" {
		v.Term().Info().Println()
		v.Term().Info().Println("Mail Authentication:")
		v.validateMailAuth(platform.DNS.Domain, &hasErrors)
	}

	// Check nodes directory
	nodesDir := filepath.Join(platformDir, "nodes")
	nodeCount := 0
	if nodeEntries, err := os.ReadDir(nodesDir); err == nil {
		for _, nodeEntry := range nodeEntries {
			if !nodeEntry.IsDir() && filepath.Ext(nodeEntry.Name()) == ".yaml" && nodeEntry.Name() != ".gitkeep" {
				nodeCount++
			}
		}
	}

	v.Term().Info().Println()
	v.Term().Info().Println("Infrastructure:")
	if nodeCount == 0 {
		v.Term().Warning().Println("  ! No nodes provisioned")
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "nodes", Status: "warning", Value: "0"})
		hasWarnings = true
	} else {
		v.Term().Success().Printfln("  ✓ Nodes: %d", nodeCount)
		v.result.Checks = append(v.result.Checks, ValidationCheck{Name: "nodes", Status: "pass", Value: fmt.Sprintf("%d", nodeCount)})
	}

	// Finalize result
	v.result.NodeCount = nodeCount
	v.result.HasErrors = hasErrors
	v.result.HasWarnings = hasWarnings
	v.result.Valid = !hasErrors

	v.Term().Info().Println()
	if hasErrors {
		v.Term().Error().Println("Validation failed with errors")
		return fmt.Errorf("validation failed")
	}

	v.Term().Success().Println("Validation passed")
	return nil
}

// validateDNS checks DNS records for the domain
func (v *Validate) validateDNS(domain string, hasErrors *bool) {
	// Check MX records
	mxRecords, err := net.LookupMX(domain)
	if err != nil || len(mxRecords) == 0 {
		v.Term().Warning().Println("  ! MX records not found")
	} else {
		v.Term().Success().Printfln("  ✓ MX records: %d found", len(mxRecords))
		for _, mx := range mxRecords {
			v.Term().Info().Printfln("      %s (priority %d)", mx.Host, mx.Pref)
		}
	}

	// Check A/AAAA records
	ips, err := net.LookupIP(domain)
	if err != nil || len(ips) == 0 {
		v.Term().Warning().Println("  ! A/AAAA records not found")
	} else {
		v.Term().Success().Printfln("  ✓ A/AAAA records: %d found", len(ips))
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
			v.Term().Success().Println("  ✓ SPF record found")
			break
		}
	}
	if !hasSPF {
		v.Term().Warning().Println("  ! SPF record not found")
	}

	// Check DMARC record
	dmarcRecords, _ := net.LookupTXT("_dmarc." + domain)
	hasDMARC := false
	for _, txt := range dmarcRecords {
		if strings.HasPrefix(txt, "v=DMARC1") {
			hasDMARC = true
			v.Term().Success().Println("  ✓ DMARC record found")
			break
		}
	}
	if !hasDMARC {
		v.Term().Warning().Println("  ! DMARC record not found")
	}

	// Check DKIM record (common selector: default)
	dkimRecords, _ := net.LookupTXT("default._domainkey." + domain)
	hasDKIM := false
	for _, txt := range dkimRecords {
		if strings.Contains(txt, "v=DKIM1") {
			hasDKIM = true
			v.Term().Success().Println("  ✓ DKIM record found (selector: default)")
			break
		}
	}
	if !hasDKIM {
		v.Term().Warning().Println("  ! DKIM record not found (selector: default)")
	}
}

// isValidCIDR checks if a string is a valid CIDR notation
func isValidCIDR(cidr string) bool {
	_, _, err := net.ParseCIDR(cidr)
	return err == nil
}

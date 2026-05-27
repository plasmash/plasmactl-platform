// Package provisioner orchestrates infrastructure lifecycle via OpenTofu.
//
// Contract: TF state is transient. Node YAMLs (platforms/<name>/nodes/*.yaml)
// and platform.yaml are the sole source of truth. Every operation rebuilds
// state from provider APIs via import blocks; nothing persists between
// invocations. Callers should `defer Manager.Close()` to enforce this.
package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/hashicorp/terraform-exec/tfexec"
	pkgtofu "github.com/plasmash/plasmactl-platform/pkg/tofu"
)

// planFileName is the workdir-relative name of the saved binary plan file.
// tfexec runs tofu with cmd.Dir = workDir, so any path passed through tfexec
// options (Out, ShowPlanFile, ShowPlanFileRaw, DirOrPlan) is interpreted
// relative to that workdir — pass the bare filename, not Manager.planPath().
// Go-side file operations (Stat/Chmod/Remove) run from the launch CWD and
// must use Manager.planPath() (workdir-joined) instead.
const planFileName = "plan.tfplan"

// Manager handles infrastructure provisioning via OpenTofu.
type Manager struct {
	workDir     string
	tf          *tfexec.Terraform
	dryRun      bool
	autoApprove bool
}

// ServerOutput represents a provisioned server from OpenTofu output.
type ServerOutput struct {
	Hostname   string `json:"hostname"`
	PublicIP   string `json:"public_ip"`
	FailoverIP string `json:"failover_ip"`
	PrivateIP  string `json:"private_ip"`
	PrivateMAC string `json:"private_mac"`
	ServerID   string `json:"server_id"`
	Zone       string `json:"zone"`
	Region     string `json:"region"`
	Machine    string `json:"machine"`
	Pool       string `json:"pool"`
	Provider   string `json:"provider"`
}

// PoolSpec is a flat provisioning view of a node pool derived from platform.yaml.
// It drives per-pool resource generation in the HCL templates.
type PoolSpec struct {
	Name    string   // pool name (used for resource naming and hostnames)
	Zones   []string // zones co-located on these nodes
	Machine string   // hardware identifier (provider-specific)
	Count   int      // number of nodes
}

// ExistingNode represents a previously provisioned node for TF import.
// Derived from node YAML files — enables stateless provisioning where
// node files are the single source of truth.
type ExistingNode struct {
	Pool     string // pool name (matched from zones)
	Index    int    // 0-based index within pool
	ImportID string // provider-specific import ID (provider_metadata.server_id)
	Hostname string // friendly name from node yaml; empty until plasmactl-node populates it
}

// ProviderConfig holds provider-agnostic configuration for HCL generation
type ProviderConfig struct {
	EnvName       string
	Pools         []PoolSpec
	Provider      string
	APIToken      string
	Zone          string
	Region        string
	ProjectID     string
	Image         string
	SSHKeyID      string
	ExistingNodes []ExistingNode
	// ImportOnly constrains HCL generation to existing resources only:
	// each pool's Count is reduced to the number of existing nodes with
	// matching Pool. TF imports adopt them; no new resources are created.
	ImportOnly bool

	// NEW — populated by plasmactl-node when using the 2-field schema.
	// When non-empty for a given provider, templates branch to the new shape.
	OVHClientID       string
	OVHClientSecret   string
	OVHEndpoint       string // "ovh-eu" | "ovh-ca" | "ovh-us"
	ScalewayAccessKey string
	ScalewaySecretKey string
	// ScalewayProjectID reuses the existing ProjectID field.
}

// providerTemplates maps provider names to their HCL templates
var providerTemplates = map[string]string{}

// templateFuncMap returns the Go text/template funcMap used by all provider
// templates. Centralized here so GenerateHCL and renderOnly stay in sync
// when new helpers are added.
func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		"seq": func(start, count int) []int {
			s := make([]int, count)
			for i := range s {
				s[i] = start + i
			}
			return s
		},
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"replace": func(old, new, s string) string {
			return strings.ReplaceAll(s, old, new)
		},
		"splitList": func(sep, s string) []string {
			return strings.Split(s, sep)
		},
		"last": func(s []string) string {
			if len(s) == 0 {
				return ""
			}
			return s[len(s)-1]
		},
		// countExisting returns the number of ExistingNodes whose Pool == name.
		// Used by template_ovh.go to compute fresh-create count per pool.
		"countExisting": func(pool string, existing []ExistingNode) int {
			n := 0
			for _, e := range existing {
				if e.Pool == pool {
					n++
				}
			}
			return n
		},
		// poolMachine returns pool.Machine for the pool whose Name matches.
		// Used by template_ovh.go to look up Machine for an adopted output entry
		// (which carries Pool but not Machine on ExistingNode).
		"poolMachine": func(pool string, pools []PoolSpec) string {
			for _, p := range pools {
				if p.Name == pool {
					return p.Machine
				}
			}
			return ""
		},
	}
}

// RegisterTemplate registers an HCL template for a provider
func RegisterTemplate(provider, tmpl string) {
	providerTemplates[provider] = tmpl
}

// constrainPoolsToExisting returns a pool list where each pool's Count equals
// the number of ExistingNodes with matching Pool. TF will declare exactly that
// many resources, all covered by import blocks — no new resources are created.
// Pools with no existing nodes are dropped entirely.
func constrainPoolsToExisting(pools []PoolSpec, existing []ExistingNode) []PoolSpec {
	countByPool := make(map[string]int)
	for _, e := range existing {
		countByPool[e.Pool]++
	}
	result := make([]PoolSpec, 0, len(pools))
	for _, p := range pools {
		n := countByPool[p.Name]
		if n == 0 {
			continue
		}
		p.Count = n
		result = append(result, p)
	}
	return result
}

// NewManager creates a new provisioner manager.
// Working directory is .plasma/node/provision/<envName>/.
// All generated files (HCL, state, provider cache) are ephemeral.
func NewManager(envName string, dryRun, autoApprove bool) (*Manager, error) {
	workDir := filepath.Join(".plasma", "node", "provision", envName)

	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create provision directory: %w", err)
	}

	execPath, err := pkgtofu.FindBinary()
	if err != nil {
		return nil, err
	}

	tf, err := tfexec.NewTerraform(workDir, execPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create tofu instance: %w", err)
	}

	return &Manager{
		workDir:     workDir,
		tf:          tf,
		dryRun:      dryRun,
		autoApprove: autoApprove,
	}, nil
}

// removeStateFiles deletes TF state artifacts from the working directory.
// The state is recomputed from provider APIs + import blocks on every run,
// so persistence across invocations is neither needed nor desired.
func (m *Manager) removeStateFiles() error {
	for _, name := range []string{"terraform.tfstate", "terraform.tfstate.backup", planFileName} {
		path := filepath.Join(m.workDir, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", name, err)
		}
	}
	if err := removeTFVarsFile(m.workDir); err != nil {
		return fmt.Errorf("failed to remove tfvars: %w", err)
	}
	return nil
}

// CleanState wipes any stale TF state before HCL generation so that import
// blocks are the sole source of truth for existing resources.
func (m *Manager) CleanState() error {
	return m.removeStateFiles()
}

// Close removes transient TF state after operations complete. Call via
// `defer` immediately after NewManager to guarantee no state survives an
// invocation, even on partial failure. Safe to call multiple times.
func (m *Manager) Close() error {
	return m.removeStateFiles()
}

// GenerateHCL generates HCL for the configured provider
func (m *Manager) GenerateHCL(config ProviderConfig) error {
	mainFile := filepath.Join(m.workDir, "main.tf")

	tmplStr, ok := providerTemplates[config.Provider]
	if !ok {
		return fmt.Errorf("no HCL template registered for provider %q", config.Provider)
	}

	if config.ImportOnly {
		config.Pools = constrainPoolsToExisting(config.Pools, config.ExistingNodes)
	}

	funcMap := templateFuncMap()

	tmpl, err := template.New("main.tf").Funcs(funcMap).Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	if err := os.WriteFile(mainFile, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write main.tf: %w", err)
	}

	// Write per-run credentials to an auto-loaded tfvars file (0600).
	// OpenTofu picks up *.auto.tfvars.json automatically — no CLI flag needed.
	// Overrides the placeholder defaults baked into the HCL template so secrets
	// don't linger in main.tf longer than this run.
	if _, err := writeTFVarsFile(m.workDir, buildTFVars(config)); err != nil {
		return fmt.Errorf("failed to write tfvars: %w", err)
	}

	return nil
}

// Init initializes OpenTofu
func (m *Manager) Init(ctx context.Context) error {
	return m.tf.Init(ctx, tfexec.Upgrade(true))
}

// Plan runs tofu plan and saves the binary plan to <workDir>/plan.tfplan.
// The plan file is consumed by PlanText/PlanSummary/ApplyPlan so the operator
// approves and we apply the exact same plan (closes any plan/apply TOCTOU).
// Plan files can contain credential references through resource attributes;
// the file is chmod'd to 0600 immediately. If chmod fails the plan file is
// removed and the call fails — never expose a 0644 plan with secrets.
func (m *Manager) Plan(ctx context.Context) (bool, error) {
	hasChanges, err := m.tf.Plan(ctx, tfexec.Out(planFileName))
	if err != nil {
		return false, err
	}
	if cerr := os.Chmod(m.planPath(), 0600); cerr != nil {
		_ = os.Remove(m.planPath())
		return false, fmt.Errorf("failed to secure plan file: %w", cerr)
	}
	return hasChanges, nil
}

// PlanText returns the human-readable text of the saved plan file
// (equivalent to `tofu show -no-color plan.tfplan`). It is best-effort UX:
// callers that fail to read it should log a warning and continue, since the
// load-bearing call for safety decisions is PlanSummary.
func (m *Manager) PlanText(ctx context.Context) (string, error) {
	if err := m.ensurePlanExists("PlanText"); err != nil {
		return "", err
	}
	return m.tf.ShowPlanFileRaw(ctx, planFileName)
}

// Apply runs tofu apply
func (m *Manager) Apply(ctx context.Context) error {
	return m.tf.Apply(ctx)
}

// ApplyPlan applies the saved plan file rather than re-planning. Requires
// Plan(ctx) to have been called. node:join uses this to ensure the user
// approves and applies exactly the same plan (no implicit re-plan TOCTOU).
// Apply (without saved plan) remains available for node:provision/destroy.
func (m *Manager) ApplyPlan(ctx context.Context) error {
	if err := m.ensurePlanExists("ApplyPlan"); err != nil {
		return err
	}
	return m.tf.Apply(ctx, tfexec.DirOrPlan(planFileName))
}

// Destroy runs tofu destroy
func (m *Manager) Destroy(ctx context.Context) error {
	return m.tf.Destroy(ctx)
}

// GetOutputs retrieves provisioning outputs
func (m *Manager) GetOutputs(ctx context.Context) ([]ServerOutput, error) {
	outputs, err := m.tf.Output(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get outputs: %w", err)
	}

	serversOutput, ok := outputs["servers"]
	if !ok {
		return nil, nil
	}

	var servers map[string]ServerOutput
	if err := json.Unmarshal(serversOutput.Value, &servers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal servers output: %w", err)
	}

	var result []ServerOutput
	for _, s := range servers {
		result = append(result, s)
	}

	return result, nil
}

// GetWorkDir returns the provisioning working directory
func (m *Manager) GetWorkDir() string {
	return m.workDir
}

// planPath returns the launch-CWD-relative path to the saved plan file.
// Use this for Go-side file operations (os.Stat/Chmod/Remove). Do NOT pass
// it to tfexec calls — tfexec runs tofu with the workDir as CWD, so paths
// in tfexec options must be workdir-relative (use planFileName instead).
func (m *Manager) planPath() string {
	return filepath.Join(m.workDir, planFileName)
}

// ensurePlanExists returns an error if the saved plan file is missing.
// Methods that read the saved plan (PlanText, PlanSummary, ApplyPlan) call
// this first to surface a clear "Plan(ctx) wasn't called" message instead of
// whatever low-level error tofu would raise.
func (m *Manager) ensurePlanExists(methodName string) error {
	if _, err := os.Stat(m.planPath()); os.IsNotExist(err) {
		return fmt.Errorf("provisioner: plan file missing — Plan(ctx) must be called before %s: %w", methodName, err)
	}
	return nil
}

// renderOnly renders a registered template to a string without writing to disk.
// Test-only helper: keeps template tests independent of filesystem/tfexec setup.
func renderOnly(provider string, cfg ProviderConfig) (string, error) {
	tmplStr, ok := providerTemplates[provider]
	if !ok {
		return "", fmt.Errorf("no template for %q", provider)
	}
	funcMap := templateFuncMap()
	t, err := template.New("test").Funcs(funcMap).Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, cfg); err != nil {
		return "", err
	}
	return buf.String(), nil
}

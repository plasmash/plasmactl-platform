package provisioner

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderOVH(t *testing.T, cfg ProviderConfig) string {
	t.Helper()
	cfg.Provider = "ovh"
	cfg.EnvName = "testenv"
	cfg.Pools = []PoolSpec{{Name: "control", Zones: []string{"foundation"}, Machine: "advance-1", Count: 1}}
	out, err := renderOnly("ovh", cfg)
	require.NoError(t, err)
	return out
}

func TestOVHTemplate_NewShape(t *testing.T) {
	out := renderOVH(t, ProviderConfig{
		OVHClientID:     "CID",
		OVHClientSecret: "CSEC",
		OVHEndpoint:     "ovh-eu",
		Region:          "ca",
		Zone:            "bhs",
		Image:           "debian12_64",
	})
	assert.Contains(t, out, `endpoint      = var.ovh_endpoint`)
	assert.Contains(t, out, `client_id     = var.ovh_client_id`)
	assert.Contains(t, out, `client_secret = var.ovh_client_secret`)
	assert.Contains(t, out, `variable "ovh_client_id"`)
	assert.Contains(t, out, `sensitive   = true`)
	// No legacy access_token block in new shape.
	assert.NotContains(t, out, "access_token = var.api_token")
	// Physical DC stays in resource config, independent of endpoint.
	assert.Contains(t, out, `value = "bhs"`)
	assert.Contains(t, out, `value = "ca"`)
}

func TestOVHTemplate_LegacyShape(t *testing.T) {
	out := renderOVH(t, ProviderConfig{
		APIToken: "legacy-bearer-token",
		Region:   "eu",
		Zone:     "rbx",
		Image:    "debian12_64",
	})
	assert.Contains(t, out, `access_token = var.api_token`)
	assert.Contains(t, out, `endpoint     = "ovh-eu"`)
	assert.Contains(t, out, `default     = "legacy-bearer-token"`)
	assert.NotContains(t, out, `var.ovh_client_id`)
}

func TestOVHTemplate_DisplayNameIgnoreChanges(t *testing.T) {
	out := renderOVH(t, ProviderConfig{
		OVHClientID:     "CID",
		OVHClientSecret: "CSEC",
		OVHEndpoint:     "ovh-eu",
		Region:          "ca",
		Zone:            "bhs",
		Image:           "debian12_64",
	})
	// Resource block must declare lifecycle.ignore_changes for display_name
	// so that node:join (import) does not push our auto-name back to OVH,
	// and node:provision (create) is unaffected (ignore_changes only applies
	// to updates, not creates).
	assert.Contains(t, out, "lifecycle {")
	assert.Contains(t, out, "ignore_changes = [display_name]")
}

func TestOVHTemplate_OutputServerIDUsesName(t *testing.T) {
	out := renderOVH(t, ProviderConfig{
		OVHClientID:     "CID",
		OVHClientSecret: "CSEC",
		OVHEndpoint:     "ovh-eu",
		Region:          "ca",
		Zone:            "bhs",
		Image:           "debian12_64",
	})
	// Output keying must use the resource's canonical .name (e.g. ns1234567.ip-...)
	// so that plasmactl-node can match TF outputs against --server-id (which is
	// also the canonical service name). The numeric .server_id (e.g. 499820) is
	// not what the operator addresses the server with.
	assert.Contains(t, out, "server_id   = ovh_dedicated_server.")
	assert.Contains(t, out, ".name\n")
	assert.NotContains(t, out, "tostring(ovh_dedicated_server.")
}

// --- Split-resource adoption tests (2026-04-29 design) ---

// renderOVHCustom renders the OVH template with a fully custom config,
// bypassing the fixed-pool defaults of renderOVH. Used by adoption tests
// that need specific pool/existing combinations.
func renderOVHCustom(t *testing.T, cfg ProviderConfig) string {
	t.Helper()
	cfg.Provider = "ovh"
	if cfg.EnvName == "" {
		cfg.EnvName = "example-plat"
	}
	out, err := renderOnly("ovh", cfg)
	require.NoError(t, err)
	return out
}

func TestRenderOVH_PureAdoption(t *testing.T) {
	// node:join scenario: 3 BHS servers being adopted into a single 'control'
	// pool. Pool.Count is set to 3 to match the post-constrain expectation
	// (renderOnly does not apply constrainPoolsToExisting).
	out := renderOVHCustom(t, ProviderConfig{
		OVHClientID:     "CID",
		OVHClientSecret: "CSEC",
		OVHEndpoint:     "ovh-eu",
		Region:          "ca",
		Zone:            "bhs",
		Image:           "debian12_64",
		Pools: []PoolSpec{
			{Name: "control", Zones: []string{"foundation"}, Machine: "advance-1", Count: 3},
		},
		ImportOnly: true,
		ExistingNodes: []ExistingNode{
			{Pool: "control", Index: 0, ImportID: "ns1234567.ip-192-0-2.net"},
			{Pool: "control", Index: 1, ImportID: "ns1234568.ip-198-51-100.net"},
			{Pool: "control", Index: 2, ImportID: "ns1234569.ip-198-51-100.net"},
		},
	})

	// 3 import blocks targeting the existing-resource addresses.
	assert.Contains(t, out, `to = ovh_dedicated_server.example_plat_control_existing_0`)
	assert.Contains(t, out, `to = ovh_dedicated_server.example_plat_control_existing_1`)
	assert.Contains(t, out, `to = ovh_dedicated_server.example_plat_control_existing_2`)
	assert.Contains(t, out, `id = "ns1234567.ip-192-0-2.net"`)
	assert.Contains(t, out, `id = "ns1234568.ip-198-51-100.net"`)
	assert.Contains(t, out, `id = "ns1234569.ip-198-51-100.net"`)

	// 3 minimal adopted resources.
	assert.Contains(t, out, `resource "ovh_dedicated_server" "example_plat_control_existing_0"`)
	assert.Contains(t, out, `resource "ovh_dedicated_server" "example_plat_control_existing_1"`)
	assert.Contains(t, out, `resource "ovh_dedicated_server" "example_plat_control_existing_2"`)

	// Each of the 3 adopted blocks must carry both directives — not just one of them.
	assert.Equal(t, 3, strings.Count(out, "prevent_install_on_import = true"),
		"each of the 3 adopted blocks must have prevent_install_on_import = true")
	assert.Equal(t, 3, strings.Count(out, "ignore_changes = all"),
		"each of the 3 adopted blocks must have ignore_changes = all")

	// service_name set to ImportID on each adopted resource.
	assert.Contains(t, out, `service_name              = "ns1234567.ip-192-0-2.net"`)

	// No fresh-create blocks: with Count=3 and 3 existing, freshCount=0.
	assert.NotContains(t, out, `resource "ovh_dedicated_server" "example_plat_control_0"`)
	assert.NotContains(t, out, `resource "ovh_dedicated_server" "example_plat_control_1"`)
	assert.NotContains(t, out, `resource "ovh_dedicated_server" "example_plat_control_2"`)

	// Output map contains adopted entries keyed by ImportID.
	assert.Contains(t, out, `"ns1234567.ip-192-0-2.net" = {`)
	assert.Contains(t, out, `"ns1234568.ip-198-51-100.net" = {`)
	assert.Contains(t, out, `"ns1234569.ip-198-51-100.net" = {`)
}

func TestRenderOVH_PureCreate(t *testing.T) {
	// node:provision pure-create scenario (regression check for existing path):
	// 2 fresh slots in 'control' pool, no ExistingNodes. Same shape as today's
	// template — full order block per resource.
	out := renderOVHCustom(t, ProviderConfig{
		OVHClientID:     "CID",
		OVHClientSecret: "CSEC",
		OVHEndpoint:     "ovh-eu",
		Region:          "ca",
		Zone:            "bhs",
		Image:           "debian12_64",
		Pools: []PoolSpec{
			{Name: "control", Zones: []string{"foundation"}, Machine: "advance-1", Count: 2},
		},
		// ImportOnly: false (default)
		// ExistingNodes: nil (default)
	})

	// No import blocks.
	assert.NotContains(t, out, "import {",
		"pure-create scenario must not emit any import blocks")
	// No adopted resources.
	assert.NotContains(t, out, `_existing_`)
	// 2 fresh-create resources.
	assert.Contains(t, out, `resource "ovh_dedicated_server" "example_plat_control_0"`)
	assert.Contains(t, out, `resource "ovh_dedicated_server" "example_plat_control_1"`)
	// Full order block on fresh resources.
	assert.Contains(t, out, `ovh_subsidiary = data.ovh_me.account.ovh_subsidiary`)
	assert.Contains(t, out, `plan_code    = "advance-1"`)
	assert.Contains(t, out, `value = "bhs"`)
	// Output map keyed by auto-name.
	assert.Contains(t, out, `"example-plat-control-001" = {`)
	assert.Contains(t, out, `"example-plat-control-002" = {`)
}

func TestRenderOVH_MixedReconcile(t *testing.T) {
	// node:provision mixed scenario: 1 already-adopted node + 2 fresh slots
	// in pool count=3. Template emits 1 adopted block + 2 fresh-create blocks.
	out := renderOVHCustom(t, ProviderConfig{
		OVHClientID:     "CID",
		OVHClientSecret: "CSEC",
		OVHEndpoint:     "ovh-eu",
		Region:          "ca",
		Zone:            "bhs",
		Image:           "debian12_64",
		Pools: []PoolSpec{
			{Name: "control", Zones: []string{"foundation"}, Machine: "advance-1", Count: 3},
		},
		// ImportOnly: false — mixed mode keeps Pool.Count untouched
		ExistingNodes: []ExistingNode{
			{Pool: "control", Index: 0, ImportID: "ns1234567.ip-192-0-2.net"},
		},
	})

	// 1 adopted, 2 fresh.
	assert.Contains(t, out, `resource "ovh_dedicated_server" "example_plat_control_existing_0"`)
	assert.Contains(t, out, `resource "ovh_dedicated_server" "example_plat_control_0"`)
	assert.Contains(t, out, `resource "ovh_dedicated_server" "example_plat_control_1"`)
	// No third fresh resource (freshCount = 3 - 1 = 2).
	assert.NotContains(t, out, `resource "ovh_dedicated_server" "example_plat_control_2"`)

	// Output map: 1 adopted + 2 fresh.
	assert.Contains(t, out, `"ns1234567.ip-192-0-2.net" = {`)
	assert.Contains(t, out, `"example-plat-control-001" = {`)
	assert.Contains(t, out, `"example-plat-control-002" = {`)
}

func TestRenderOVH_AdoptedHasNoOrderBlock(t *testing.T) {
	// Adoption is incompatible with the order block: OVH provider Read cannot
	// repopulate ovh_subsidiary / plan / customizations from the API, so any
	// HCL declaring them on an imported resource diffs as ForceNew → terminate.
	// This test guards against regression by extracting just the adopted-block
	// section of the rendered template and asserting the offending strings are
	// absent.
	out := renderOVHCustom(t, ProviderConfig{
		OVHClientID:     "CID",
		OVHClientSecret: "CSEC",
		OVHEndpoint:     "ovh-eu",
		Region:          "ca",
		Zone:            "bhs",
		Image:           "debian12_64",
		Pools: []PoolSpec{
			{Name: "control", Zones: []string{"foundation"}, Machine: "advance-1", Count: 1},
		},
		ImportOnly: true,
		ExistingNodes: []ExistingNode{
			{Pool: "control", Index: 0, ImportID: "ns1234567.ip-192-0-2.net"},
		},
	})

	// Section markers come from the template (see template_ovh.go).
	const adoptedStart = "# === Adopted resources"
	const freshStart = "# === Fresh-create resources"
	const outputsStart = "# === Outputs"

	adoptedStartIdx := strings.Index(out, adoptedStart)
	require.NotEqual(t, -1, adoptedStartIdx, "adopted-resources section marker missing")

	// Adopted section ends at the next `# ===` marker. Anchor on a leading
	// newline so the search cannot match a substring inside the header itself.
	adoptedEndRel := strings.Index(out[adoptedStartIdx+len(adoptedStart):], "\n# === ")
	require.NotEqual(t, -1, adoptedEndRel, "no section marker after adopted block")
	adoptedSection := out[adoptedStartIdx : adoptedStartIdx+len(adoptedStart)+adoptedEndRel]

	// None of the order-block strings may appear inside the adopted section.
	assert.NotContains(t, adoptedSection, "ovh_subsidiary",
		"adopted section must not declare ovh_subsidiary (OVH Read cannot populate it; forces ForceNew)")
	assert.NotContains(t, adoptedSection, "plan = [",
		"adopted section must not declare plan block")
	assert.NotContains(t, adoptedSection, "customizations",
		"adopted section must not declare customizations block")
	osAttrRe := regexp.MustCompile(`(?m)^\s*os\s+=`)
	assert.False(t, osAttrRe.MatchString(adoptedSection),
		"adopted section must not declare os attribute (any whitespace style)")

	// Sanity: the fresh and outputs section markers exist (i.e., the template
	// rendered all three sections).
	assert.Contains(t, out, freshStart)
	assert.Contains(t, out, outputsStart)
}

func TestRenderOVH_OutputMapKeyDistinct(t *testing.T) {
	// In a mixed-mode render, adopted entries (keyed by canonical OVH name)
	// and fresh entries (keyed by auto-name) must have non-colliding keys.
	out := renderOVHCustom(t, ProviderConfig{
		OVHClientID:     "CID",
		OVHClientSecret: "CSEC",
		OVHEndpoint:     "ovh-eu",
		Region:          "ca",
		Zone:            "bhs",
		Image:           "debian12_64",
		Pools: []PoolSpec{
			{Name: "control", Zones: []string{"foundation"}, Machine: "advance-1", Count: 2},
		},
		ExistingNodes: []ExistingNode{
			{Pool: "control", Index: 0, ImportID: "ns1234567.ip-192-0-2.net"},
		},
	})

	// Adopted key (canonical OVH name).
	assert.Contains(t, out, `"ns1234567.ip-192-0-2.net" = {`)
	// Fresh key (auto-name format).
	assert.Contains(t, out, `"example-plat-control-001" = {`)
	// Canonical-name format never collides with auto-name format (different
	// shape entirely): canonical has dots, auto has dashes only.
	assert.NotContains(t, out, `"ns1234567.ip-192-0-2.net" = ovh_dedicated_server.example_plat_control_0.`)
}

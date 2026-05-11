package provisioner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderSCW(t *testing.T, cfg ProviderConfig) string {
	t.Helper()
	cfg.Provider = "scaleway"
	cfg.EnvName = "testenv"
	cfg.Pools = []PoolSpec{{Name: "control", Zones: []string{"foundation"}, Machine: "EM-A410R-NVME", Count: 1}}
	out, err := renderOnly("scaleway", cfg)
	require.NoError(t, err)
	return out
}

func TestScalewayTemplate_NewShape(t *testing.T) {
	out := renderSCW(t, ProviderConfig{
		ScalewayAccessKey: "AK",
		ScalewaySecretKey: "SK",
		ProjectID:         "project-uuid",
		Zone:              "fr-par-2",
		Image:             "debian12_64",
	})
	assert.Contains(t, out, `access_key = var.scaleway_access_key`)
	assert.Contains(t, out, `secret_key = var.scaleway_secret_key`)
	assert.Contains(t, out, `project_id = var.scaleway_project_id`)
	assert.Contains(t, out, `variable "scaleway_access_key"`)
	assert.Contains(t, out, `sensitive   = true`)
	// Legacy single-var form must NOT appear in new-shape output.
	assert.NotContains(t, out, `secret_key = var.api_token`)
}

func TestScalewayTemplate_LegacyShape(t *testing.T) {
	out := renderSCW(t, ProviderConfig{
		APIToken: "legacy-secret-only",
		Zone:     "fr-par-2",
	})
	assert.Contains(t, out, `secret_key = var.api_token`)
	assert.Contains(t, out, `default     = "legacy-secret-only"`)
	assert.NotContains(t, out, `var.scaleway_access_key`)
}

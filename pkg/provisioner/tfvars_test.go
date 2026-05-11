package provisioner

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTFVars_NewOVH(t *testing.T) {
	v := buildTFVars(ProviderConfig{
		Provider:        "ovh",
		OVHClientID:     "CID",
		OVHClientSecret: "CSEC",
		OVHEndpoint:     "ovh-eu",
	})
	assert.Equal(t, "CID", v["ovh_client_id"])
	assert.Equal(t, "CSEC", v["ovh_client_secret"])
	assert.Equal(t, "ovh-eu", v["ovh_endpoint"])
	_, hasApi := v["api_token"]
	assert.False(t, hasApi)
}

func TestBuildTFVars_LegacyOVH(t *testing.T) {
	v := buildTFVars(ProviderConfig{Provider: "ovh", APIToken: "T"})
	assert.Equal(t, "T", v["api_token"])
	_, hasNew := v["ovh_client_id"]
	assert.False(t, hasNew)
}

func TestBuildTFVars_NewScaleway(t *testing.T) {
	v := buildTFVars(ProviderConfig{
		Provider:          "scaleway",
		ScalewayAccessKey: "AK",
		ScalewaySecretKey: "SK",
		ProjectID:         "PID",
	})
	assert.Equal(t, "AK", v["scaleway_access_key"])
	assert.Equal(t, "SK", v["scaleway_secret_key"])
	assert.Equal(t, "PID", v["scaleway_project_id"])
}

func TestBuildTFVars_OtherProviderToken(t *testing.T) {
	v := buildTFVars(ProviderConfig{Provider: "hetzner", APIToken: "HZ"})
	assert.Equal(t, "HZ", v["api_token"])
}

func TestWriteTFVarsFile_Mode0600(t *testing.T) {
	dir := t.TempDir()
	path, err := writeTFVarsFile(dir, map[string]string{"k": "v"})
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var got map[string]string
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "v", got["k"])
}

func TestWriteTFVarsFile_EmptyClearsExisting(t *testing.T) {
	dir := t.TempDir()
	// First, write a file.
	_, err := writeTFVarsFile(dir, map[string]string{"k": "v"})
	require.NoError(t, err)
	// Now write empty — should remove the file.
	_, err = writeTFVarsFile(dir, map[string]string{})
	require.NoError(t, err)
	_, err = os.Stat(dir + "/" + tfVarsFile)
	assert.True(t, os.IsNotExist(err))
}

func TestWriteTFVarsFile_StalePermsResetTo0600(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/" + tfVarsFile
	// Pre-create a file with looser perms.
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))
	// Re-write via writeTFVarsFile — should end up at 0600 regardless.
	_, err := writeTFVarsFile(dir, map[string]string{"k": "v"})
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestRemoveTFVarsFile_Idempotent(t *testing.T) {
	dir := t.TempDir()
	// Remove from empty dir — no error.
	require.NoError(t, removeTFVarsFile(dir))
	// Write then remove.
	_, err := writeTFVarsFile(dir, map[string]string{"k": "v"})
	require.NoError(t, err)
	require.NoError(t, removeTFVarsFile(dir))
	// Remove again — still no error.
	require.NoError(t, removeTFVarsFile(dir))
}

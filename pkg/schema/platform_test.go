package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestAPIConfig_OldShape_OVH(t *testing.T) {
	var cfg Platform
	require.NoError(t, yaml.Unmarshal([]byte(`
name: p
infrastructure:
  metal_provider: ovh
  api:
    token: "{{ .keyring.ovh_api_token }}"
`), &cfg))
	assert.Equal(t, "{{ .keyring.ovh_api_token }}", cfg.Infrastructure.API.Token)
	assert.Empty(t, cfg.Infrastructure.API.ClientID)
	assert.Empty(t, cfg.Infrastructure.API.ClientSecret)
}

func TestAPIConfig_NewShape_OVH(t *testing.T) {
	var cfg Platform
	require.NoError(t, yaml.Unmarshal([]byte(`
name: p
infrastructure:
  metal_provider: ovh
  api:
    client_id: "{{ .keyring.ovh_client_id }}"
    client_secret: "{{ .keyring.ovh_client_secret }}"
`), &cfg))
	assert.Equal(t, "{{ .keyring.ovh_client_id }}", cfg.Infrastructure.API.ClientID)
	assert.Equal(t, "{{ .keyring.ovh_client_secret }}", cfg.Infrastructure.API.ClientSecret)
	assert.Empty(t, cfg.Infrastructure.API.Token)
}

func TestAPIConfig_NewShape_Scaleway(t *testing.T) {
	var cfg Platform
	require.NoError(t, yaml.Unmarshal([]byte(`
name: p
infrastructure:
  metal_provider: scaleway
  api:
    access_key: "AK"
    secret_key: "SK"
`), &cfg))
	assert.Equal(t, "AK", cfg.Infrastructure.API.AccessKey)
	assert.Equal(t, "SK", cfg.Infrastructure.API.SecretKey)
}

func TestAPIConfig_JSONOmitsCredentials(t *testing.T) {
	cfg := APIConfig{
		URI:          "https://api",
		Token:        "TOK",
		ClientID:     "CID",
		ClientSecret: "CSEC",
		AccessKey:    "AK",
		SecretKey:    "SK",
	}
	b, err := json.Marshal(cfg)
	require.NoError(t, err)
	s := string(b)
	assert.Contains(t, s, `"uri":"https://api"`)
	assert.NotContains(t, s, "TOK")
	assert.NotContains(t, s, "CID")
	assert.NotContains(t, s, "CSEC")
	assert.NotContains(t, s, "AK")
	assert.NotContains(t, s, "SK")
}

func TestPlatformYAML_ParsesPrivateVLANID(t *testing.T) {
	var p Platform
	require.NoError(t, yaml.Unmarshal([]byte(`name: example-plat
infrastructure:
  metal_provider: ovh
  private_vlan_id: 7
`), &p))
	assert.Equal(t, 7, p.Infrastructure.PrivateVLANID)
}

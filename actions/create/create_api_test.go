package create

import (
	"strings"
	"testing"

	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func marshalPlatform(t *testing.T, p *schema.Platform) string {
	t.Helper()
	b, err := yaml.Marshal(p)
	require.NoError(t, err)
	return string(b)
}

func TestScaffoldPlatform_OVH_NewShape(t *testing.T) {
	p, err := ScaffoldPlatform(ScaffoldOptions{
		Name:          "test-ovh",
		MetalProvider: "ovh",
		DNSProvider:   "ovh",
		Domain:        "example.test",
	})
	require.NoError(t, err)

	y := marshalPlatform(t, p)
	assert.Contains(t, y, `client_id: '{{ .keyring.ovh_client_id }}'`)
	assert.Contains(t, y, `client_secret: '{{ .keyring.ovh_client_secret }}'`)
	assert.NotContains(t, y, "ovh_api_token")
}

func TestScaffoldPlatform_Scaleway_NewShape(t *testing.T) {
	p, err := ScaffoldPlatform(ScaffoldOptions{
		Name:          "test-scw",
		MetalProvider: "scaleway",
		DNSProvider:   "cloudflare",
		Domain:        "example.test",
	})
	require.NoError(t, err)

	y := marshalPlatform(t, p)
	assert.Contains(t, y, `access_key: '{{ .keyring.scaleway_access_key }}'`)
	assert.Contains(t, y, `secret_key: '{{ .keyring.scaleway_secret_key }}'`)
	assert.NotContains(t, y, "scaleway_api_token")
}

func TestScaffoldPlatform_Hetzner_UnchangedLegacy(t *testing.T) {
	p, err := ScaffoldPlatform(ScaffoldOptions{
		Name:          "test-hz",
		MetalProvider: "hetzner",
		DNSProvider:   "cloudflare",
		Domain:        "example.test",
	})
	require.NoError(t, err)

	y := marshalPlatform(t, p)
	assert.Contains(t, y, `token: '{{ .keyring.hetzner_api_token }}'`)
	assert.True(t, strings.Contains(y, "hetzner_api_token"))
}

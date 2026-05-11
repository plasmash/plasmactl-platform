package dns

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderDNSOVH(t *testing.T, cfg Config) string {
	t.Helper()
	tmpl, err := template.New("t").Parse(ovhDNSTemplate)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, tmpl.Execute(&buf, cfg))
	return buf.String()
}

func TestDNSOVHTemplate_NewShape(t *testing.T) {
	out := renderDNSOVH(t, Config{
		OVHClientID:     "CID",
		OVHClientSecret: "CSEC",
		OVHEndpoint:     "ovh-eu",
		Zone:            "example.test",
		Domain:          "example.test",
	})
	assert.Contains(t, out, `client_id     = var.ovh_client_id`)
	assert.Contains(t, out, `client_secret = var.ovh_client_secret`)
	assert.Contains(t, out, `endpoint      = var.ovh_endpoint`)
	assert.NotContains(t, out, "access_token = var.api_token")
}

func TestDNSOVHTemplate_LegacyShape(t *testing.T) {
	out := renderDNSOVH(t, Config{
		APIToken: "T",
		Region:   "eu",
		Zone:     "example.test",
		Domain:   "example.test",
	})
	assert.Contains(t, out, `access_token = var.api_token`)
	assert.NotContains(t, out, "var.ovh_client_id")
}

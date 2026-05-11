package validate

import (
	"testing"

	"github.com/plasmash/plasmactl-platform/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestShapeOfAPICreds(t *testing.T) {
	tests := []struct {
		name string
		api  schema.APIConfig
		want string
	}{
		{"empty", schema.APIConfig{}, "empty"},
		{"legacy", schema.APIConfig{Token: "{{ .keyring.x }}"}, "legacy"},
		{"ovh-new", schema.APIConfig{ClientID: "a", ClientSecret: "b"}, "ovh-new"},
		{"ovh-partial-id", schema.APIConfig{ClientID: "a"}, "ovh-partial"},
		{"ovh-partial-secret", schema.APIConfig{ClientSecret: "b"}, "ovh-partial"},
		{"scw-new", schema.APIConfig{AccessKey: "a", SecretKey: "b"}, "scw-new"},
		{"scw-partial-ak", schema.APIConfig{AccessKey: "a"}, "scw-partial"},
		{"scw-partial-sk", schema.APIConfig{SecretKey: "b"}, "scw-partial"},
		{"mixed-token-ovh", schema.APIConfig{Token: "t", ClientID: "a", ClientSecret: "b"}, "mixed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ShapeOfAPICreds(tc.api))
		})
	}
}

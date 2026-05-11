package provisioner

import (
	"encoding/json"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rcFromJSON builds a *tfjson.ResourceChange from a JSON literal. The struct's
// nested fields (Change.Actions, Change.Before, Change.After) are populated by
// the JSON unmarshaler from the same shape tofu emits in `show -json` output.
func rcFromJSON(t *testing.T, raw string) *tfjson.ResourceChange {
	t.Helper()
	var rc tfjson.ResourceChange
	require.NoError(t, json.Unmarshal([]byte(raw), &rc))
	return &rc
}

func TestPlanSummary_NoOp(t *testing.T) {
	rc := rcFromJSON(t, `{
		"address": "ovh_dedicated_server.example_plat_control_0",
		"type": "ovh_dedicated_server",
		"name": "example_plat_control_0",
		"change": {"actions": ["no-op"], "before": {}, "after": {}}
	}`)
	s := classifyResourceChanges([]*tfjson.ResourceChange{rc})
	assert.Equal(t, 0, s.Adds)
	assert.Equal(t, 0, s.Changes)
	assert.Equal(t, 0, s.Destroys)
	assert.Equal(t, 0, s.Replaces)
	assert.Empty(t, s.DestructiveUpdates)
	assert.Empty(t, s.Deletes)
	assert.Empty(t, s.Replacements)
}

func TestPlanSummary_DisplayNameOnlyUpdate_NotDestructive(t *testing.T) {
	rc := rcFromJSON(t, `{
		"address": "ovh_dedicated_server.example_plat_control_0",
		"type": "ovh_dedicated_server",
		"name": "example_plat_control_0",
		"change": {
			"actions": ["update"],
			"before": {"display_name": "old", "os": "debian12_64"},
			"after":  {"display_name": "new", "os": "debian12_64"}
		}
	}`)
	s := classifyResourceChanges([]*tfjson.ResourceChange{rc})
	assert.Equal(t, 1, s.Changes)
	assert.Empty(t, s.DestructiveUpdates, "display_name-only changes are shielded by ignore_changes and not destructive")
}

func TestPlanSummary_OSUpdate_IsDestructive(t *testing.T) {
	rc := rcFromJSON(t, `{
		"address": "ovh_dedicated_server.example_plat_control_0",
		"type": "ovh_dedicated_server",
		"name": "example_plat_control_0",
		"change": {
			"actions": ["update"],
			"before": {"os": "none_64"},
			"after":  {"os": "debian12_64"}
		}
	}`)
	s := classifyResourceChanges([]*tfjson.ResourceChange{rc})
	assert.Equal(t, 1, s.Changes)
	require.Len(t, s.DestructiveUpdates, 1)
	assert.Equal(t, "ovh_dedicated_server.example_plat_control_0", s.DestructiveUpdates[0].Address)
	assert.Equal(t, []string{"os"}, s.DestructiveUpdates[0].ChangedAttrs)
}

func TestPlanSummary_CustomizationsUpdate_IsDestructive(t *testing.T) {
	rc := rcFromJSON(t, `{
		"address": "ovh_dedicated_server.example_plat_control_0",
		"type": "ovh_dedicated_server",
		"name": "example_plat_control_0",
		"change": {
			"actions": ["update"],
			"before": {"customizations": {"hostname": "old", "post_installation_script_link": ""}},
			"after":  {"customizations": {"hostname": "new", "post_installation_script_link": "https://x"}}
		}
	}`)
	s := classifyResourceChanges([]*tfjson.ResourceChange{rc})
	require.Len(t, s.DestructiveUpdates, 1)
	assert.Contains(t, s.DestructiveUpdates[0].ChangedAttrs, "customizations")
}

func TestPlanSummary_BootIDUpdate_IsDestructive(t *testing.T) {
	rc := rcFromJSON(t, `{
		"address": "ovh_dedicated_server.example_plat_control_0",
		"type": "ovh_dedicated_server",
		"name": "example_plat_control_0",
		"change": {
			"actions": ["update"],
			"before": {"boot_id": 1},
			"after":  {"boot_id": 2}
		}
	}`)
	s := classifyResourceChanges([]*tfjson.ResourceChange{rc})
	require.Len(t, s.DestructiveUpdates, 1)
	assert.Equal(t, []string{"boot_id"}, s.DestructiveUpdates[0].ChangedAttrs)
}

func TestPlanSummary_Delete(t *testing.T) {
	rc := rcFromJSON(t, `{
		"address": "ovh_dedicated_server.example_plat_control_0",
		"type": "ovh_dedicated_server",
		"name": "example_plat_control_0",
		"change": {"actions": ["delete"], "before": {"os": "debian12_64"}, "after": null}
	}`)
	s := classifyResourceChanges([]*tfjson.ResourceChange{rc})
	assert.Equal(t, 1, s.Destroys)
	require.Len(t, s.Deletes, 1)
	assert.Equal(t, "ovh_dedicated_server.example_plat_control_0", s.Deletes[0].Address)
	assert.Empty(t, s.DestructiveUpdates, "deletes must not double-count as destructive updates")
}

func TestPlanSummary_Replace(t *testing.T) {
	rc := rcFromJSON(t, `{
		"address": "ovh_dedicated_server.example_plat_control_0",
		"type": "ovh_dedicated_server",
		"name": "example_plat_control_0",
		"change": {"actions": ["delete", "create"], "before": {"os": "debian12_64"}, "after": {"os": "debian12_64"}}
	}`)
	s := classifyResourceChanges([]*tfjson.ResourceChange{rc})
	assert.Equal(t, 1, s.Replaces)
	require.Len(t, s.Replacements, 1)
	assert.Equal(t, "ovh_dedicated_server.example_plat_control_0", s.Replacements[0].Address)
}

func TestPlanSummary_NonOVHResource_UpdateNotDestructive(t *testing.T) {
	rc := rcFromJSON(t, `{
		"address": "random_id.example",
		"type": "random_id",
		"name": "example",
		"change": {
			"actions": ["update"],
			"before": {"os": "old"},
			"after":  {"os": "new"}
		}
	}`)
	s := classifyResourceChanges([]*tfjson.ResourceChange{rc})
	assert.Equal(t, 1, s.Changes, "still counts in Changes")
	assert.Empty(t, s.DestructiveUpdates, "non-OVH resource types default to non-destructive")
}

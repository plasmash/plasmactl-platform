package provisioner

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
)

// ResourceImpact identifies a single resource change in a plan summary.
type ResourceImpact struct {
	Address      string   // e.g. "ovh_dedicated_server.example_plat_control_0"
	Type         string   // e.g. "ovh_dedicated_server"
	Name         string   // e.g. "example_plat_control_0"
	ChangedAttrs []string // for updates: top-level attribute paths that differ
}

// PlanSummary is a classified view of a TF plan suitable for action-level
// confirmation policy. node:join uses it to decide between auto-proceed,
// typed-yes prompt, and hard-refuse.
type PlanSummary struct {
	Adds, Changes, Destroys, Replaces int
	DestructiveUpdates                []ResourceImpact // updates that mutate provider-side state
	Deletes                           []ResourceImpact
	Replacements                      []ResourceImpact
}

// ovhDestructivePrefixes are matched against top-level attribute keys of the
// resource Change's Before/After diff. The match is by-prefix so that nested
// blocks (e.g. customizations.partition_scheme_name) are caught when only the
// parent block is reported as changed in Before/After.
//
// When a second provider needs destructive-attribute shielding, lift this into
// a per-type registry keyed on tfjson.ResourceChange.Type.
var ovhDestructivePrefixes = []string{"os", "boot_id", "customizations"}

// PlanSummary returns a classified summary of the saved plan file. Requires
// Plan(ctx) to have been called.
func (m *Manager) PlanSummary(ctx context.Context) (*PlanSummary, error) {
	if err := m.ensurePlanExists("PlanSummary"); err != nil {
		return nil, err
	}
	plan, err := m.tf.ShowPlanFile(ctx, planFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w", err)
	}
	return classifyResourceChanges(plan.ResourceChanges), nil
}

// classifyResourceChanges walks a list of tfjson resource changes and produces
// a PlanSummary. Pure function, fully unit-testable.
func classifyResourceChanges(rcs []*tfjson.ResourceChange) *PlanSummary {
	s := &PlanSummary{}
	for _, rc := range rcs {
		if rc == nil || rc.Change == nil {
			continue
		}
		actions := rc.Change.Actions
		switch {
		case actions.NoOp():
			// not counted
		case actions.Create() && !actions.Replace():
			s.Adds++
		case actions.Delete() && !actions.Replace():
			s.Destroys++
			s.Deletes = append(s.Deletes, resourceImpact(rc, nil))
		case actions.Replace():
			// covers both ["delete","create"] and ["create","delete"]
			s.Replaces++
			s.Replacements = append(s.Replacements, resourceImpact(rc, nil))
		case actions.Update():
			s.Changes++
			changed := changedTopLevelKeys(rc.Change.Before, rc.Change.After)
			if rc.Type == "ovh_dedicated_server" && hasDestructivePrefix(changed, ovhDestructivePrefixes) {
				s.DestructiveUpdates = append(s.DestructiveUpdates, resourceImpact(rc, changed))
			}
		}
	}
	return s
}

func resourceImpact(rc *tfjson.ResourceChange, changedAttrs []string) ResourceImpact {
	return ResourceImpact{
		Address:      rc.Address,
		Type:         rc.Type,
		Name:         rc.Name,
		ChangedAttrs: changedAttrs,
	}
}

// changedTopLevelKeys returns the set of top-level keys whose values differ
// between before and after. Both arguments are typically map[string]interface{}
// from parsed JSON; nil inputs produce an empty result.
func changedTopLevelKeys(before, after interface{}) []string {
	bMap, _ := before.(map[string]interface{})
	aMap, _ := after.(map[string]interface{})
	keys := make(map[string]struct{})
	for k := range bMap {
		keys[k] = struct{}{}
	}
	for k := range aMap {
		keys[k] = struct{}{}
	}
	var changed []string
	for k := range keys {
		if !reflect.DeepEqual(bMap[k], aMap[k]) {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}

// hasDestructivePrefix returns true if any key in changed starts with any of
// the destructive prefixes. Prefix match (not exact) so that nested attribute
// paths like "customizations.hostname" match the "customizations" prefix.
func hasDestructivePrefix(changed, prefixes []string) bool {
	for _, k := range changed {
		for _, p := range prefixes {
			if k == p || strings.HasPrefix(k, p+".") {
				return true
			}
		}
	}
	return false
}

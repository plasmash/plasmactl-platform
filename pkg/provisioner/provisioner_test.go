package provisioner

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestManager_PlanPath(t *testing.T) {
	m := &Manager{workDir: filepath.Join(".plasma", "node", "provision", "example-plat")}
	assert.Equal(t, filepath.Join(".plasma", "node", "provision", "example-plat", "plan.tfplan"), m.planPath())
}

func TestManager_PlanText_MissingFile(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{workDir: dir}
	_, err := m.PlanText(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plan file missing")
	assert.Contains(t, err.Error(), "Plan(ctx)")
}

func TestManager_ApplyPlan_MissingFile(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{workDir: dir}
	err := m.ApplyPlan(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plan file missing")
	assert.Contains(t, err.Error(), "Plan(ctx)")
}

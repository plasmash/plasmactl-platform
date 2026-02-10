// Package tofu provides a shared OpenTofu binary path for plasmactl plugins.
// The binary path is resolved once at startup (by the platform plugin) and
// made available to all consumers via SetBinaryPath / FindBinary.
package tofu

import (
	"fmt"
	"os/exec"
)

// DefaultVersion is the pinned OpenTofu version downloaded on first use.
const DefaultVersion = "1.9.0"

// Package-level state — set by the platform plugin at init time.
var binaryPath string

// SetBinaryPath is called by the platform plugin during OnAppInit.
func SetBinaryPath(p string) { binaryPath = p }

// BinaryPath returns the registered tofu binary path (empty if not resolved).
func BinaryPath() string { return binaryPath }

// FindBinary returns the tofu binary path. It checks in order:
//  1. The path registered via SetBinaryPath (cached download or system install)
//  2. exec.LookPath("tofu") as a last-resort fallback
//
// Returns an error if tofu cannot be found anywhere.
func FindBinary() (string, error) {
	if binaryPath != "" {
		return binaryPath, nil
	}
	p, err := exec.LookPath("tofu")
	if err == nil {
		return p, nil
	}
	return "", fmt.Errorf("tofu binary not found (install OpenTofu or run a plasmactl command to auto-download)")
}

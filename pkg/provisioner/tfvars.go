package provisioner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// tfVarsFile is the per-run file OpenTofu auto-loads.
// OpenTofu reads any *.auto.tfvars.json in the working dir on Init/Plan/Apply.
const tfVarsFile = "credentials.auto.tfvars.json"

// buildTFVars maps a ProviderConfig into the TF variable names expected by
// the generated HCL. Only non-empty fields are included; empty fields let
// the HCL template defaults take over (preserves backward compatibility
// with platforms that still use the deprecated single-token credential form).
func buildTFVars(cfg ProviderConfig) map[string]string {
	v := map[string]string{}
	switch cfg.Provider {
	case "ovh":
		if cfg.OVHClientID != "" {
			v["ovh_client_id"] = cfg.OVHClientID
			v["ovh_client_secret"] = cfg.OVHClientSecret
			v["ovh_endpoint"] = cfg.OVHEndpoint
		} else if cfg.APIToken != "" {
			v["api_token"] = cfg.APIToken
		}
	case "scaleway":
		if cfg.ScalewayAccessKey != "" {
			v["scaleway_access_key"] = cfg.ScalewayAccessKey
			v["scaleway_secret_key"] = cfg.ScalewaySecretKey
			if cfg.ProjectID != "" {
				v["scaleway_project_id"] = cfg.ProjectID
			}
		} else if cfg.APIToken != "" {
			v["api_token"] = cfg.APIToken
		}
	default:
		if cfg.APIToken != "" {
			v["api_token"] = cfg.APIToken
		}
	}
	return v
}

// writeTFVarsFile materializes vars to workDir/<tfVarsFile> with 0600.
// Returns the absolute path of the file (or where it would be).
// If vars is empty, removes any pre-existing file and returns successfully —
// useful when GenerateHCL is called for a provider that doesn't need vars.
func writeTFVarsFile(workDir string, vars map[string]string) (string, error) {
	path := filepath.Join(workDir, tfVarsFile)
	if len(vars) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return path, fmt.Errorf("tfvars: remove stale %s: %w", path, err)
		}
		return path, nil
	}
	b, err := json.Marshal(vars)
	if err != nil {
		return "", fmt.Errorf("tfvars: marshal: %w", err)
	}
	// Defense: WriteFile only applies perms on creation. A stale file with
	// looser perms would otherwise survive the rewrite. Remove first.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("tfvars: clear stale %s: %w", path, err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", fmt.Errorf("tfvars: write %s: %w", path, err)
	}
	return path, nil
}

// removeTFVarsFile deletes workDir/<tfVarsFile> if present.
// Idempotent: missing-file is not an error.
func removeTFVarsFile(workDir string) error {
	path := filepath.Join(workDir, tfVarsFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

package plasmactlplatform

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
	"gopkg.in/yaml.v3"
)

// platformDeploy implements the platform:deploy command
type platformDeploy struct {
	log     *launchr.Logger
	term    *launchr.Terminal
	keyring keyring.Keyring

	environment string
	tags        string
	img         string
	debug       bool
	check       bool
	password    string
	logs        bool
	prepareDir  string

	originalDir  string
	extractedDir string
}

// SetLogger sets the logger for the action
func (a *platformDeploy) SetLogger(log *launchr.Logger) {
	a.log = log
}

// SetTerm sets the terminal for the action
func (a *platformDeploy) SetTerm(term *launchr.Terminal) {
	a.term = term
}

// Execute runs the platform:deploy action
func (a *platformDeploy) Execute() error {
	var err error
	a.originalDir, err = os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Extract Platform Image if provided
	if a.img != "" {
		if err := a.extractImage(); err != nil {
			return err
		}
		defer a.cleanup()
	}

	// Determine working directory
	workDir := a.prepareDir
	if a.extractedDir != "" {
		workDir = a.extractedDir
	}
	if workDir == "" {
		return fmt.Errorf("no working directory specified (use --prepare-dir or --img)")
	}

	// Change to working directory
	if err := os.Chdir(workDir); err != nil {
		return fmt.Errorf("failed to change to prepare directory %s: %w", workDir, err)
	}
	defer os.Chdir(a.originalDir)

	// Check if hosts cache exists
	if !a.cacheExists() {
		a.term.Warning().Println("Inventory cache does not exist, skipping deployment")
		return nil
	}

	a.term.Info().Printfln("Deploying %s to %s...", a.tags, a.environment)

	// Build ansible-playbook command
	args := a.buildAnsibleArgs()

	// Set up environment
	env := a.buildEnvironment()

	// Create askpass script for vault password
	askpassScript, err := a.createAskpassScript()
	if err != nil {
		return err
	}
	defer os.Remove(askpassScript)

	// Run ansible-playbook
	return a.runAnsiblePlaybook(args, env, askpassScript)
}

// extractImage extracts a Platform Image (.pm) file
func (a *platformDeploy) extractImage() error {
	imgPath := a.img
	if !filepath.IsAbs(imgPath) {
		imgPath = filepath.Join(a.originalDir, imgPath)
	}

	if _, err := os.Stat(imgPath); os.IsNotExist(err) {
		return fmt.Errorf("platform image not found: %s", imgPath)
	}

	// Create extraction directory
	a.extractedDir = ".deploy"
	if err := os.RemoveAll(a.extractedDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clean extraction directory: %w", err)
	}
	if err := os.MkdirAll(a.extractedDir, 0755); err != nil {
		return fmt.Errorf("failed to create extraction directory: %w", err)
	}

	a.term.Info().Printfln("Extracting Platform Image: %s", imgPath)

	// Open the tar.gz file
	file, err := os.Open(imgPath)
	if err != nil {
		return fmt.Errorf("failed to open platform image: %w", err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		target := filepath.Join(a.extractedDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			f.Close()
		case tar.TypeSymlink:
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}
		}
	}

	a.term.Info().Printfln("Platform Image extracted to %s/", a.extractedDir)
	return nil
}

// cleanup removes extracted files
func (a *platformDeploy) cleanup() {
	if a.extractedDir != "" {
		os.Chdir(a.originalDir)
		a.term.Info().Printfln("Cleaning up %s/", a.extractedDir)
		os.RemoveAll(a.extractedDir)
	}
}

// cacheExists checks if the inventory cache file exists
func (a *platformDeploy) cacheExists() bool {
	configPath := fmt.Sprintf("library/inventories/platform_nodes/configuration/%s.yaml", a.environment)

	data, err := os.ReadFile(configPath)
	if err != nil {
		a.log.Warn("Failed to read inventory configuration", "path", configPath, "error", err)
		return false
	}

	var config struct {
		SourceInventory struct {
			CachePath string `yaml:"cache_path"`
		} `yaml:"source_inventory"`
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		a.log.Warn("Failed to parse inventory configuration", "error", err)
		return false
	}

	cachePath := filepath.Join(config.SourceInventory.CachePath, "ansible-online_net.cache")
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		return false
	}

	return true
}

// buildAnsibleArgs builds the ansible-playbook command arguments
func (a *platformDeploy) buildAnsibleArgs() []string {
	args := []string{
		"platform/platform.yaml",
		"--tags", a.tags,
		"--extra-vars", fmt.Sprintf("machine_target_config=%s", a.environment),
	}

	if a.debug {
		args = append(args, "-vvv")
	}

	if a.check {
		args = append(args, "--check")
	}

	return args
}

// buildEnvironment builds the environment variables for ansible-playbook
func (a *platformDeploy) buildEnvironment() []string {
	env := os.Environ()

	// Set ANSIBLE_CONFIG if not already set
	hasAnsibleConfig := false
	for _, e := range env {
		if strings.HasPrefix(e, "ANSIBLE_CONFIG=") {
			hasAnsibleConfig = true
			break
		}
	}
	if !hasAnsibleConfig {
		env = append(env, "ANSIBLE_CONFIG=./ansible.cfg")
	}

	// Set up OpenTelemetry environment if configured
	otelEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otelEndpoint != "" {
		// Override env attribute with actual deployment target
		attrs := os.Getenv("OTEL_RESOURCE_ATTRIBUTES")
		attrsMap := make(map[string]string)

		if attrs != "" {
			for _, attr := range strings.Split(attrs, ",") {
				if parts := strings.SplitN(attr, "=", 2); len(parts) == 2 {
					attrsMap[parts[0]] = parts[1]
				}
			}
		}
		attrsMap["env"] = a.environment

		var newAttrs []string
		for k, v := range attrsMap {
			newAttrs = append(newAttrs, fmt.Sprintf("%s=%s", k, v))
		}
		env = append(env, fmt.Sprintf("OTEL_RESOURCE_ATTRIBUTES=%s", strings.Join(newAttrs, ",")))
		env = append(env, "OTEL_EXPORTER_OTLP_TIMEOUT=30000")
		env = append(env, "OTEL_EXPORTER_OTLP_COMPRESSION=gzip")
	}

	return env
}

// createAskpassScript creates a script for SSH_ASKPASS that reads password from env var
// This avoids writing the actual password to disk - only a script that echoes an env var
func (a *platformDeploy) createAskpassScript() (string, error) {
	tmpFile, err := os.CreateTemp("", "askpass-*.sh")
	if err != nil {
		return "", fmt.Errorf("failed to create askpass script: %w", err)
	}

	// Script reads password from environment variable, not from file
	// The actual password is passed via PLASMA_VAULT_PASS env var at runtime
	script := "#!/bin/sh\necho \"$PLASMA_VAULT_PASS\"\n"
	if _, err := tmpFile.WriteString(script); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write askpass script: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpFile.Name(), 0700); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to chmod askpass script: %w", err)
	}

	return tmpFile.Name(), nil
}

// runAnsiblePlaybook executes ansible-playbook
func (a *platformDeploy) runAnsiblePlaybook(args, env []string, askpassScript string) error {
	cmd := exec.Command("ansible-playbook", args...)
	cmd.Env = append(env,
		fmt.Sprintf("SSH_ASKPASS=%s", askpassScript),
		"SSH_ASKPASS_REQUIRE=force",
		fmt.Sprintf("ANSIBLE_VAULT_PASSWORD_FILE=%s", askpassScript),
		// Pass password via env var - the script echoes this, password never written to disk
		fmt.Sprintf("PLASMA_VAULT_PASS=%s", a.password),
	)

	// Set up output
	if a.logs {
		logFile, err := os.Create("deploy.log")
		if err != nil {
			return fmt.Errorf("failed to create log file: %w", err)
		}
		defer logFile.Close()

		// Tee output to both stdout/stderr and log file
		cmd.Stdout = io.MultiWriter(os.Stdout, logFile)
		cmd.Stderr = io.MultiWriter(os.Stderr, logFile)
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	cmd.Stdin = os.Stdin

	a.term.Info().Printfln("Running: ansible-playbook %s", strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("ansible-playbook failed with exit code %d", exitErr.ExitCode())
		}
		return fmt.Errorf("failed to run ansible-playbook: %w", err)
	}

	a.term.Success().Println("Deployment completed successfully")
	return nil
}

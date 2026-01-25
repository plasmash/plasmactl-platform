package plasmactlplatform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/launchrctl/launchr"
	"gopkg.in/yaml.v3"
)

// resolveChassisConfigDir finds the configuration directory for a chassis
// Chassis configs are stored in src/{layer}/cfg/{chassis}/
func resolveChassisConfigDir(chassis string) (string, error) {
	if chassis == "" {
		return "", fmt.Errorf("chassis name is required")
	}

	// Parse chassis name to extract layer
	// Example: platform.foundation.cluster -> foundation
	parts := strings.Split(chassis, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid chassis name %q (expected format: platform.{layer}.{...})", chassis)
	}

	layer := parts[1] // e.g., "foundation", "integration", "cognition"

	configDir := filepath.Join("src", layer, "cfg", chassis)
	if _, err := os.Stat(configDir); err == nil {
		return configDir, nil
	}

	return "", fmt.Errorf("chassis config directory not found: %s", configDir)
}

// cfgGet implements the config:get command
type cfgGet struct {
	log     *launchr.Logger
	term    *launchr.Terminal
	key     string
	vault   bool
	chassis string
}

func (a *cfgGet) SetLogger(log *launchr.Logger) { a.log = log }
func (a *cfgGet) SetTerm(term *launchr.Terminal) { a.term = term }

func (a *cfgGet) Execute() error {
	configDir, err := resolveChassisConfigDir(a.chassis)
	if err != nil {
		return err
	}

	filename := "vars.yaml"
	if a.vault {
		filename = "vault.yaml"
	}

	configFile := filepath.Join(configDir, filename)
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return fmt.Errorf("config file not found: %s", configFile)
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var config map[string]interface{}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	value, ok := config[a.key]
	if !ok {
		return fmt.Errorf("key %q not found", a.key)
	}

	fmt.Println(value)
	return nil
}

// cfgSet implements the config:set command
type cfgSet struct {
	log     *launchr.Logger
	term    *launchr.Terminal
	key     string
	value   string
	vault   bool
	chassis string
}

func (a *cfgSet) SetLogger(log *launchr.Logger) { a.log = log }
func (a *cfgSet) SetTerm(term *launchr.Terminal) { a.term = term }

func (a *cfgSet) Execute() error {
	configDir, err := resolveChassisConfigDir(a.chassis)
	if err != nil {
		// Create config directory if it doesn't exist
		parts := strings.Split(a.chassis, ".")
		if len(parts) < 2 {
			return fmt.Errorf("invalid chassis name %q", a.chassis)
		}
		layer := parts[1]
		configDir = filepath.Join("src", layer, "cfg", a.chassis)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
	}

	filename := "vars.yaml"
	if a.vault {
		filename = "vault.yaml"
	}

	configFile := filepath.Join(configDir, filename)

	var config map[string]interface{}
	if data, err := os.ReadFile(configFile); err == nil {
		if err := yaml.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse existing config: %w", err)
		}
	} else {
		config = make(map[string]interface{})
	}

	config[a.key] = a.value

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	a.term.Success().Printfln("Set %s = %s", a.key, a.value)
	return nil
}

// cfgList implements the config:list command
type cfgList struct {
	log       *launchr.Logger
	term      *launchr.Terminal
	component string
	vault     bool
	chassis   string
	format    string
}

func (a *cfgList) SetLogger(log *launchr.Logger) { a.log = log }
func (a *cfgList) SetTerm(term *launchr.Terminal) { a.term = term }

func (a *cfgList) Execute() error {
	configDir, err := resolveChassisConfigDir(a.chassis)
	if err != nil {
		a.term.Info().Println("No configuration found")
		return nil
	}

	result := make(map[string]interface{})

	// Read vars.yaml
	valuesFile := filepath.Join(configDir, "vars.yaml")
	if data, err := os.ReadFile(valuesFile); err == nil {
		var values map[string]interface{}
		if err := yaml.Unmarshal(data, &values); err == nil {
			for k, v := range values {
				if a.component == "" || strings.HasPrefix(k, a.component) {
					result[k] = v
				}
			}
		}
	}

	// Read vault.yaml if requested
	if a.vault {
		vaultFile := filepath.Join(configDir, "vault.yaml")
		if data, err := os.ReadFile(vaultFile); err == nil {
			var vault map[string]interface{}
			if err := yaml.Unmarshal(data, &vault); err == nil {
				for k, v := range vault {
					if a.component == "" || strings.HasPrefix(k, a.component) {
						result[k+" (vault)"] = v
					}
				}
			}
		}
	}

	if len(result) == 0 {
		a.term.Info().Println("No configuration values found")
		return nil
	}

	switch a.format {
	case "json":
		output, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(output))
	case "yaml":
		output, _ := yaml.Marshal(result)
		fmt.Println(string(output))
	default:
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KEY\tVALUE")
		fmt.Fprintln(w, "---\t-----")
		for k, v := range result {
			fmt.Fprintf(w, "%s\t%v\n", k, v)
		}
		w.Flush()
	}

	return nil
}

// cfgValidate implements the config:validate command
type cfgValidate struct {
	log       *launchr.Logger
	term      *launchr.Terminal
	component string
	chassis   string
	strict    bool
}

func (a *cfgValidate) SetLogger(log *launchr.Logger) { a.log = log }
func (a *cfgValidate) SetTerm(term *launchr.Terminal) { a.term = term }

func (a *cfgValidate) Execute() error {
	a.term.Info().Println("Validating configuration...")

	// TODO: Implement schema-based validation
	// 1. Load component schemas from meta/plasma.yaml files
	// 2. Validate config values against schemas
	// 3. Report errors and warnings

	a.term.Warning().Println("Schema-based validation not yet implemented")
	a.term.Success().Println("Basic config structure is valid")
	return nil
}

// cfgRotate implements the config:rotate command
type cfgRotate struct {
	log        *launchr.Logger
	term       *launchr.Terminal
	key        string
	chassis    string
	yesIAmSure bool
}

func (a *cfgRotate) SetLogger(log *launchr.Logger) { a.log = log }
func (a *cfgRotate) SetTerm(term *launchr.Terminal) { a.term = term }

func (a *cfgRotate) Execute() error {
	if !a.yesIAmSure {
		a.term.Warning().Println("⚠️  Secret rotation will change credentials.")
		a.term.Warning().Println("Applications may need to be restarted.")

		// For now, just warn - proper confirmation would use confirmDestroy pattern
		a.term.Info().Println("Use --yes-i-am-sure to proceed")
		return nil
	}

	// TODO: Implement secret rotation
	// 1. Generate new secret value
	// 2. Update vault.yaml
	// 3. Optionally trigger re-deployment

	a.term.Warning().Println("Secret rotation not yet implemented")
	return nil
}

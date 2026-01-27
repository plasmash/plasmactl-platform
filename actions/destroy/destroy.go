package destroy

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
)

// Destroy implements the platform:destroy command
type Destroy struct {
	Log     *launchr.Logger
	Term    *launchr.Terminal
	Keyring keyring.Keyring

	Name       string
	YesIAmSure bool
	KeepDNS    bool
}

// SetLogger sets the logger for the action
func (d *Destroy) SetLogger(log *launchr.Logger) {
	d.Log = log
}

// SetTerm sets the terminal for the action
func (d *Destroy) SetTerm(term *launchr.Terminal) {
	d.Term = term
}

// Execute runs the platform:destroy action
func (d *Destroy) Execute() error {
	instDir := filepath.Join("inst", d.Name)

	// Check if platform exists
	if _, err := os.Stat(instDir); os.IsNotExist(err) {
		return fmt.Errorf("platform %q not found", d.Name)
	}

	// Confirm destruction
	if !d.YesIAmSure {
		confirmed, err := confirmDestroy(d.Term, "platform", d.Name)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	d.Term.Info().Printfln("Destroying platform %q...", d.Name)

	// TODO: Destroy DNS records if not --keep-dns
	if !d.KeepDNS {
		d.Term.Info().Println("  Removing DNS records...")
		// DNS removal via Terraform would go here
		d.Term.Warning().Println("  DNS removal not yet implemented")
	}

	// TODO: Destroy nodes via Terraform
	// This should invoke node:destroy for each node
	nodesDir := filepath.Join(instDir, "nodes")
	if nodeEntries, err := os.ReadDir(nodesDir); err == nil {
		for _, nodeEntry := range nodeEntries {
			if !nodeEntry.IsDir() && filepath.Ext(nodeEntry.Name()) == ".yaml" && nodeEntry.Name() != ".gitkeep" {
				nodeName := nodeEntry.Name()[:len(nodeEntry.Name())-5]
				d.Term.Info().Printfln("  Would destroy node: %s", nodeName)
				// node destruction via Terraform would go here
			}
		}
	}

	// Remove the environment directory
	d.Term.Info().Println("  Removing platform directory...")
	if err := os.RemoveAll(instDir); err != nil {
		return fmt.Errorf("failed to remove platform directory: %w", err)
	}

	d.Term.Success().Printfln("Platform %q destroyed", d.Name)
	return nil
}

// confirmDestroy prompts user to type the resource name to confirm destruction
func confirmDestroy(term *launchr.Terminal, resourceType, resourceName string) (bool, error) {
	term.Warning().Printfln("⚠️  This will PERMANENTLY destroy %s '%s'.", resourceType, resourceName)
	term.Warning().Printf("Type '%s' to confirm: ", resourceName)

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input != resourceName {
		term.Error().Println("Confirmation failed. Aborting.")
		return false, nil
	}

	return true, nil
}

package plasmactlplatform

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
)

// destroyPlatformAction implements the platform:destroy command
type destroyPlatformAction struct {
	log     *launchr.Logger
	term    *launchr.Terminal
	keyring keyring.Keyring

	name        string
	yesIAmSure  bool
	keepDNS     bool
}

// SetLogger sets the logger for the action
func (a *destroyPlatformAction) SetLogger(log *launchr.Logger) {
	a.log = log
}

// SetTerm sets the terminal for the action
func (a *destroyPlatformAction) SetTerm(term *launchr.Terminal) {
	a.term = term
}

// Execute runs the platform:destroy action
func (a *destroyPlatformAction) Execute() error {
	envDir := filepath.Join("inst", a.name)

	// Check if platform exists
	if _, err := os.Stat(envDir); os.IsNotExist(err) {
		return fmt.Errorf("platform %q not found", a.name)
	}

	// Confirm destruction
	if !a.yesIAmSure {
		confirmed, err := confirmDestroy(a.term, "platform", a.name)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	a.term.Info().Printfln("Destroying platform %q...", a.name)

	// TODO: Destroy DNS records if not --keep-dns
	if !a.keepDNS {
		a.term.Info().Println("  Removing DNS records...")
		// DNS removal via Terraform would go here
		a.term.Warning().Println("  DNS removal not yet implemented")
	}

	// TODO: Destroy nodes via Terraform
	// This should invoke node:destroy for each node
	nodesDir := filepath.Join(envDir, "nodes")
	if nodeEntries, err := os.ReadDir(nodesDir); err == nil {
		for _, nodeEntry := range nodeEntries {
			if !nodeEntry.IsDir() && filepath.Ext(nodeEntry.Name()) == ".yaml" && nodeEntry.Name() != ".gitkeep" {
				nodeName := nodeEntry.Name()[:len(nodeEntry.Name())-5]
				a.term.Info().Printfln("  Would destroy node: %s", nodeName)
				// node destruction via Terraform would go here
			}
		}
	}

	// Remove the environment directory
	a.term.Info().Println("  Removing platform directory...")
	if err := os.RemoveAll(envDir); err != nil {
		return fmt.Errorf("failed to remove platform directory: %w", err)
	}

	a.term.Success().Printfln("Platform %q destroyed", a.name)
	return nil
}

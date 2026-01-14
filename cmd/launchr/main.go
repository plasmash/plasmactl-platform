// Package executes Launchr application.
package main

import (
	"github.com/launchrctl/launchr"

	_ "github.com/plasmash/plasmactl-platform"
)

func main() {
	launchr.RunAndExit()
}

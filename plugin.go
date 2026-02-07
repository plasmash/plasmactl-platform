// Package platform implements a launchr plugin for platform lifecycle management
package platform

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
	"github.com/launchrctl/launchr/pkg/action"

	"github.com/plasmash/plasmactl-platform/actions/check"
	"github.com/plasmash/plasmactl-platform/actions/create"
	"github.com/plasmash/plasmactl-platform/actions/deploy"
	"github.com/plasmash/plasmactl-platform/actions/destroy"
	"github.com/plasmash/plasmactl-platform/actions/impact"
	"github.com/plasmash/plasmactl-platform/actions/list"
	"github.com/plasmash/plasmactl-platform/actions/show"
	"github.com/plasmash/plasmactl-platform/actions/up"
	"github.com/plasmash/plasmactl-platform/actions/validate"
	pkggraph "github.com/plasmash/plasmactl-platform/pkg/graph"
)

//go:embed actions/*/*.yaml
var actionYamlFS embed.FS

func init() {
	launchr.RegisterPlugin(&Plugin{})
}

// Plugin is [launchr.Plugin] providing platform lifecycle actions.
type Plugin struct {
	k   keyring.Keyring
	m   action.Manager
	app launchr.App
}

// PluginInfo implements [launchr.Plugin] interface.
func (p *Plugin) PluginInfo() launchr.PluginInfo {
	return launchr.PluginInfo{
		Weight: 1337,
	}
}

// OnAppInit implements [launchr.Plugin] interface.
func (p *Plugin) OnAppInit(app launchr.App) error {
	app.GetService(&p.k)
	app.GetService(&p.m)
	p.app = app

	// Extract the graph binary from embedded FS early so it's available
	// to all plugins (not just platform:* actions). DiscoverActions uses
	// lazy evaluation and may not run for non-platform commands.
	if err := p.initGraphBinary(); err != nil {
		launchr.Log().Warn("failed to extract graph binary", "err", err)
	}
	return nil
}

// initGraphBinary extracts the embedded Rust graph binary to a cache directory
// and registers it with the graph package so any plugin can call graph.Load().
// Uses a fixed path to avoid accumulating temp directories across runs.
func (p *Plugin) initGraphBinary() error {
	if len(graphBinaryData) == 0 {
		return fmt.Errorf("no graph binary embedded for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	dir := filepath.Join(cacheDir, "plasmactl", "graph")
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := "graph"
	if runtime.GOOS == "windows" {
		name = "graph.exe"
	}
	binPath := filepath.Join(dir, name)
	if err = os.WriteFile(binPath, graphBinaryData, 0o755); err != nil {
		return err
	}
	pkggraph.SetBinaryPath(binPath)
	return nil
}

// DiscoverActions implements [launchr.ActionDiscoveryPlugin] interface.
func (p *Plugin) DiscoverActions(_ context.Context) ([]*action.Action, error) {
	var actions []*action.Action

	// platform:up action (full workflow: compose → prepare → deploy)
	upYaml, _ := actionYamlFS.ReadFile("actions/up/up.yaml")
	upAction := action.NewFromYAML("platform:up", upYaml)
	upAction.SetRuntime(action.NewFnRuntime(func(ctx context.Context, a *action.Action) error {
		input := a.Input()
		env := input.Arg("environment").(string)
		tags := input.Arg("tags").(string)
		v := launchr.Version()
		options := up.UpOptions{
			Bin:                v.Name,
			Img:                input.Opt("img").(string),
			Last:               input.Opt("last").(bool),
			SkipBump:           input.Opt("skip-bump").(bool),
			SkipPrepare:        input.Opt("skip-prepare").(bool),
			CI:                 input.Opt("ci").(bool),
			Local:              input.Opt("local").(bool),
			Clean:              input.Opt("clean").(bool),
			CleanPrepare:       input.Opt("clean-prepare").(bool),
			Debug:              input.Opt("debug").(bool),
			ConflictsVerbosity: input.Opt("conflicts-verbosity").(bool),
			GitlabDomain:       input.Opt("gitlab-domain").(string),
			Streams:            a.Input().Streams(),
			Persistent:         a.Input().GroupFlags(p.m.GetPersistentFlags().GetName()),
		}

		u := up.NewUp(a, p.k, p.m)
		return u.Run(ctx, env, tags, options)
	}))
	actions = append(actions, upAction)

	// platform:create action
	createYaml, _ := actionYamlFS.ReadFile("actions/create/create.yaml")
	createAction := action.NewFromYAML("platform:create", createYaml)
	createAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		c := &create.Create{
			Keyring:       p.k,
			Name:          input.Arg("name").(string),
			MetalProvider: input.Opt("metal-provider").(string),
			DNSProvider:   input.Opt("dns-provider").(string),
			Domain:        input.Opt("domain").(string),
			Zone:          input.Opt("zone").(string),
			Region:        input.Opt("region").(string),
			ProjectID:     input.Opt("project-id").(string),
			Image:         input.Opt("image").(string),
			SSHKeyID:      input.Opt("ssh-key-id").(string),
			SkipDNS:       input.Opt("skip-dns").(bool),
		}
		c.SetLogger(log)
		c.SetTerm(term)
		return c.Execute()
	}))
	actions = append(actions, createAction)

	// platform:list action
	listYaml, _ := actionYamlFS.ReadFile("actions/list/list.yaml")
	listAction := action.NewFromYAML("platform:list", listYaml)
	listAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		l := &list.List{
			Format: input.Opt("output").(string),
		}
		l.SetLogger(log)
		l.SetTerm(term)
		return l.Execute()
	}))
	actions = append(actions, listAction)

	// platform:show action
	showYaml, _ := actionYamlFS.ReadFile("actions/show/show.yaml")
	showAction := action.NewFromYAML("platform:show", showYaml)
	showAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		s := &show.Show{
			Name:   input.Arg("name").(string),
			Format: input.Opt("output").(string),
		}
		s.SetLogger(log)
		s.SetTerm(term)
		return s.Execute()
	}))
	actions = append(actions, showAction)

	// platform:validate action
	validateYaml, _ := actionYamlFS.ReadFile("actions/validate/validate.yaml")
	validateAction := action.NewFromYAML("platform:validate", validateYaml)
	validateAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		v := &validate.Validate{
			Name:     input.Arg("name").(string),
			SkipDNS:  input.Opt("skip-dns").(bool),
			SkipMail: input.Opt("skip-mail").(bool),
		}
		v.SetLogger(log)
		v.SetTerm(term)
		return v.Execute()
	}))
	actions = append(actions, validateAction)

	// platform:destroy action
	destroyYaml, _ := actionYamlFS.ReadFile("actions/destroy/destroy.yaml")
	destroyAction := action.NewFromYAML("platform:destroy", destroyYaml)
	destroyAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		d := &destroy.Destroy{
			Keyring:    p.k,
			Name:       input.Arg("name").(string),
			YesIAmSure: input.Opt("yes-i-am-sure").(bool),
			KeepDNS:    input.Opt("keep-dns").(bool),
		}
		d.SetLogger(log)
		d.SetTerm(term)
		return d.Execute()
	}))
	actions = append(actions, destroyAction)

	// platform:deploy action
	deployYaml, _ := actionYamlFS.ReadFile("actions/deploy/deploy.yaml")
	deployAction := action.NewFromYAML("platform:deploy", deployYaml)
	deployAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		d := &deploy.Deploy{
			Keyring:     p.k,
			Environment: input.Arg("environment").(string),
			Tags:        input.Arg("tags").(string),
			Img:         input.Opt("img").(string),
			Debug:       input.Opt("debug").(bool),
			Check:       input.Opt("check").(bool),
			Password:    input.Opt("password").(string),
			Logs:        input.Opt("logs").(bool),
			PrepareDir:  input.Opt("prepare-dir").(string),
		}
		d.SetLogger(log)
		d.SetTerm(term)
		return d.Execute()
	}))
	actions = append(actions, deployAction)

	// platform:check action
	checkYaml, _ := actionYamlFS.ReadFile("actions/check/check.yaml")
	checkAction := action.NewFromYAML("platform:check", checkYaml)
	checkAction.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLoggerTerm(a)
		ch := &check.Check{
			Section: input.Opt("section").(string),
		}
		ch.SetLogger(log)
		ch.SetTerm(term)
		err := ch.Execute()
		// Structured (--json): return result only (total_errors is in the result).
		// Otherwise: print and return error.
		jsonOutput, _ := input.GetFlagInGroup(p.m.GetPersistentFlags().Name(), "json").(bool)
		if jsonOutput {
			return ch.Result(), nil
		}
		ch.PrintText()
		return nil, err
	}))
	actions = append(actions, checkAction)

	// platform:impact action
	impactYaml, _ := actionYamlFS.ReadFile("actions/impact/impact.yaml")
	impactAction := action.NewFromYAML("platform:impact", impactYaml)
	impactAction.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLoggerTerm(a)
		imp := &impact.Impact{
			Name: input.Arg("name").(string),
		}
		imp.SetLogger(log)
		imp.SetTerm(term)
		err := imp.Execute()
		// Structured (--json): return result only. Otherwise: print and return nil.
		jsonOutput, _ := input.GetFlagInGroup(p.m.GetPersistentFlags().Name(), "json").(bool)
		if jsonOutput {
			return imp.Result(), err
		}
		imp.PrintText()
		return nil, err
	}))
	actions = append(actions, impactAction)

	// Note: platform:prepare is NOT embedded here.
	// It must be provided by plasmactl-model plugin.
	// platform:up validates its existence at runtime.

	// platform:graph action
	graphYaml, _ := actionYamlFS.ReadFile("actions/graph/action.yaml")
	graphAction := action.NewFromYAML("platform:graph", graphYaml)
	graphAction.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		query := input.Arg("query").(string)
		reverse := input.Opt("reverse").(bool)
		depth := input.Opt("depth").(int)
		tree := input.Opt("tree").(bool)
		format := input.Opt("format").(string)
		composeDir := input.Opt("compose-dir").(string)
		debug := input.Opt("debug").(bool)

		// Detect framework --json flag for structured output
		jsonOutput, _ := input.GetFlagInGroup(p.m.GetPersistentFlags().Name(), "json").(bool)
		if jsonOutput {
			format = "json"
		}

		args := []string{query,
			fmt.Sprintf("--reverse=%t", reverse),
			fmt.Sprintf("--depth=%d", depth),
			fmt.Sprintf("--tree=%t", tree),
			fmt.Sprintf("--format=%s", format),
			fmt.Sprintf("--compose-dir=%s", composeDir),
			fmt.Sprintf("--debug=%t", debug),
		}

		binPath := pkggraph.BinaryPath()
		if binPath == "" {
			return nil, fmt.Errorf("graph binary not available")
		}

		cmd := exec.Command(binPath, args...)
		cmd.Stderr = a.Input().Streams().Err()

		if jsonOutput {
			// Capture output and return structured result for --json/web
			out, err := cmd.Output()
			if err != nil {
				return nil, fmt.Errorf("graph binary failed: %w", err)
			}
			var result any
			if err = json.Unmarshal(out, &result); err != nil {
				return nil, fmt.Errorf("failed to parse graph output: %w", err)
			}
			return result, nil
		}

		// Pipe directly to stdout for CLI display, return nil (outputText skips nil)
		cmd.Stdout = a.Input().Streams().Out()
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("graph binary failed: %w", err)
		}
		return nil, nil
	}))
	actions = append(actions, graphAction)

	return actions, nil
}

// getLoggerTerm extracts logger and terminal from action runtime
func getLoggerTerm(a *action.Action) (*launchr.Logger, *launchr.Terminal) {
	log := launchr.Log()
	if rt, ok := a.Runtime().(action.RuntimeLoggerAware); ok {
		log = rt.LogWith()
	}

	term := launchr.Term()
	if rt, ok := a.Runtime().(action.RuntimeTermAware); ok {
		term = rt.Term()
	}

	return log, term
}

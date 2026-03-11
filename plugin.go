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
	"github.com/opentofu/tofudl"

	"github.com/plasmash/plasmactl-platform/actions/check"
	"github.com/plasmash/plasmactl-platform/actions/create"
	"github.com/plasmash/plasmactl-platform/actions/deploy"
	"github.com/plasmash/plasmactl-platform/actions/destroy"
	"github.com/plasmash/plasmactl-platform/actions/impact"
	"github.com/plasmash/plasmactl-platform/actions/list"
	"github.com/plasmash/plasmactl-platform/actions/show"
	"github.com/plasmash/plasmactl-platform/actions/size"
	"github.com/plasmash/plasmactl-platform/actions/up"
	"github.com/plasmash/plasmactl-platform/actions/validate"
	pkggraph "github.com/plasmash/plasmactl-platform/pkg/graph"
	pkgtofu "github.com/plasmash/plasmactl-platform/pkg/tofu"
)

//go:embed actions/*/*.yaml
var actionYamlFS embed.FS

//go:embed actions/graph/component.js
var graphComponentJS []byte

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

	// Resolve the OpenTofu binary (system PATH → cache → download).
	if err := p.initTofuBinary(); err != nil {
		launchr.Log().Debug("tofu binary not available", "err", err)
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

// initTofuBinary resolves the OpenTofu binary path using a three-tier strategy:
//  1. System PATH — honor user's existing installation
//  2. Cache hit — previously downloaded binary at ~/.cache/plasmactl/tofu/<version>/tofu
//  3. Download — fetch via tofudl and cache for future runs
func (p *Plugin) initTofuBinary() error {
	// 1. System PATH — nothing to do, FindBinary() will pick it up via LookPath.
	if path, err := exec.LookPath("tofu"); err == nil {
		pkgtofu.SetBinaryPath(path)
		return nil
	}

	// 2. Check cache.
	version := pkgtofu.DefaultVersion
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	dir := filepath.Join(cacheDir, "plasmactl", "tofu", version)
	name := "tofu"
	if runtime.GOOS == "windows" {
		name = "tofu.exe"
	}
	binPath := filepath.Join(dir, name)

	if _, err = os.Stat(binPath); err == nil {
		pkgtofu.SetBinaryPath(binPath)
		return nil
	}

	// 3. Download via tofudl.
	launchr.Log().Debug("downloading OpenTofu", "version", version)
	dl, err := tofudl.New()
	if err != nil {
		return fmt.Errorf("failed to create tofudl downloader: %w", err)
	}
	binary, err := dl.Download(context.Background(), tofudl.DownloadOptVersion(tofudl.Version(version)))
	if err != nil {
		return fmt.Errorf("failed to download tofu %s: %w", version, err)
	}
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create tofu cache dir: %w", err)
	}
	if err = os.WriteFile(binPath, binary, 0o755); err != nil {
		return fmt.Errorf("failed to write tofu binary: %w", err)
	}
	pkgtofu.SetBinaryPath(binPath)
	launchr.Log().Debug("tofu downloaded", "path", binPath)
	return nil
}

// ActionComponents implements web server's ActionComponentProvider interface.
// Returns pre-compiled JS components keyed by action ID.
func (p *Plugin) ActionComponents() map[string][]byte {
	m := make(map[string][]byte)
	if len(graphComponentJS) > 0 {
		m["platform:graph"] = graphComponentJS
	}
	return m
}

// DiscoverActions implements [launchr.ActionDiscoveryPlugin] interface.
func (p *Plugin) DiscoverActions(_ context.Context) ([]*action.Action, error) {
	var actions []*action.Action

	// platform:up action (full workflow: compose → prepare → deploy)
	upYaml, _ := actionYamlFS.ReadFile("actions/up/up.yaml")
	upAction := action.NewFromYAML("platform:up", upYaml)
	upAction.SetRuntime(action.NewFnRuntimeWithResult(func(ctx context.Context, a *action.Action) (any, error) {
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
		err := u.Run(ctx, env, tags, options)
		return u.Result(), err
	}))
	actions = append(actions, upAction)

	// platform:create action
	createYaml, _ := actionYamlFS.ReadFile("actions/create/create.yaml")
	createAction := action.NewFromYAML("platform:create", createYaml)
	createAction.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
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
		err := c.Execute()
		return c.Result(), err
	}))
	actions = append(actions, createAction)

	// platform:list action
	listYaml, _ := actionYamlFS.ReadFile("actions/list/list.yaml")
	listAction := action.NewFromYAML("platform:list", listYaml)
	listAction.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		log, term := getLoggerTerm(a)
		l := &list.List{}
		l.SetLogger(log)
		l.SetTerm(term)
		err := l.Execute()
		return l.Result(), err
	}))
	actions = append(actions, listAction)

	// platform:show action
	showYaml, _ := actionYamlFS.ReadFile("actions/show/show.yaml")
	showAction := action.NewFromYAML("platform:show", showYaml)
	showAction.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLoggerTerm(a)
		s := &show.Show{
			Name: input.Arg("name").(string),
		}
		s.SetLogger(log)
		s.SetTerm(term)
		err := s.Execute()
		return s.Result(), err
	}))
	actions = append(actions, showAction)

	// platform:validate action
	validateYaml, _ := actionYamlFS.ReadFile("actions/validate/validate.yaml")
	validateAction := action.NewFromYAML("platform:validate", validateYaml)
	validateAction.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLoggerTerm(a)
		v := &validate.Validate{
			Name:     input.Arg("name").(string),
			SkipDNS:  input.Opt("skip-dns").(bool),
			SkipMail: input.Opt("skip-mail").(bool),
		}
		v.SetLogger(log)
		v.SetTerm(term)
		err := v.Execute()
		return v.Result(), err
	}))
	actions = append(actions, validateAction)

	// platform:size action
	sizeYaml, _ := actionYamlFS.ReadFile("actions/size/size.yaml")
	sizeAction := action.NewFromYAML("platform:size", sizeYaml)
	sizeAction.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
		input := a.Input()
		log, term := getLoggerTerm(a)
		sz := &size.Size{
			Name:    input.Arg("name").(string),
			Suggest: input.Opt("suggest").(bool),
			Add:     input.Opt("add").(string),
			Remove:  input.Opt("remove").(string),
			Zones:   action.InputOptSlice[string](input, "zone"),
			Machine: input.Opt("machine").(string),
			Count:   input.Opt("count").(int),
		}
		sz.SetLogger(log)
		sz.SetTerm(term)
		err := sz.Execute()
		return sz.Result(), err
	}))
	actions = append(actions, sizeAction)

	// platform:destroy action
	destroyYaml, _ := actionYamlFS.ReadFile("actions/destroy/destroy.yaml")
	destroyAction := action.NewFromYAML("platform:destroy", destroyYaml)
	destroyAction.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
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
		err := d.Execute()
		return d.Result(), err
	}))
	actions = append(actions, destroyAction)

	// platform:deploy action
	deployYaml, _ := actionYamlFS.ReadFile("actions/deploy/deploy.yaml")
	deployAction := action.NewFromYAML("platform:deploy", deployYaml)
	deployAction.SetRuntime(action.NewFnRuntimeWithResult(func(_ context.Context, a *action.Action) (any, error) {
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
		err := d.Execute()
		return d.Result(), err
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
		var depth int
		switch v := input.Opt("depth").(type) {
		case int:
			depth = v
		case float64:
			depth = int(v)
		}
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

		if format == "json" {
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

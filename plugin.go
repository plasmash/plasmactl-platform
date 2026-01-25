// Package plasmactlplatform implements a launchr plugin for platform lifecycle management
package plasmactlplatform

import (
	"context"
	_ "embed"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
	"github.com/launchrctl/launchr/pkg/action"
)

//go:embed action.up.yaml
var actionUpYaml []byte

//go:embed action.create.yaml
var actionCreateYaml []byte

//go:embed action.list.yaml
var actionListYaml []byte

//go:embed action.show.yaml
var actionShowYaml []byte

//go:embed action.validate.yaml
var actionValidateYaml []byte

//go:embed action.destroy.yaml
var actionDestroyYaml []byte

//go:embed action.config.get.yaml
var actionConfigGetYaml []byte

//go:embed action.config.set.yaml
var actionConfigSetYaml []byte

//go:embed action.config.list.yaml
var actionConfigListYaml []byte

//go:embed action.config.validate.yaml
var actionConfigValidateYaml []byte

//go:embed action.config.rotate.yaml
var actionConfigRotateYaml []byte

//go:embed action.deploy.yaml
var actionDeployYaml []byte

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
	return nil
}

// DiscoverActions implements [launchr.ActionDiscoveryPlugin] interface.
func (p *Plugin) DiscoverActions(_ context.Context) ([]*action.Action, error) {
	var actions []*action.Action

	// platform:up action (full workflow: compose → prepare → deploy)
	upAction := action.NewFromYAML("platform:up", actionUpYaml)
	upAction.SetRuntime(action.NewFnRuntime(func(ctx context.Context, a *action.Action) error {
		input := a.Input()
		env := input.Arg("environment").(string)
		tags := input.Arg("tags").(string)
		v := launchr.Version()
		options := shipOptions{
			bin:                v.Name,
			img:                input.Opt("img").(string),
			last:               input.Opt("last").(bool),
			skipBump:           input.Opt("skip-bump").(bool),
			skipPrepare:        input.Opt("skip-prepare").(bool),
			ci:                 input.Opt("ci").(bool),
			local:              input.Opt("local").(bool),
			clean:              input.Opt("clean").(bool),
			cleanPrepare:       input.Opt("clean-prepare").(bool),
			debug:              input.Opt("debug").(bool),
			conflictsVerbosity: input.Opt("conflicts-verbosity").(bool),
			gitlabDomain:       input.Opt("gitlab-domain").(string),
			streams:            a.Input().Streams(),
			persistent:         a.Input().GroupFlags(p.m.GetPersistentFlags().GetName()),
		}

		ship := newShipAction(a, p.k, p.m)
		return ship.run(ctx, env, tags, options)
	}))
	actions = append(actions, upAction)

	// platform:create action
	createAction := action.NewFromYAML("platform:create", actionCreateYaml)
	createAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		create := &platformCreate{
			keyring:       p.k,
			name:          input.Arg("name").(string),
			metalProvider: input.Opt("metal-provider").(string),
			dnsProvider:   input.Opt("dns-provider").(string),
			domain:        input.Opt("domain").(string),
			skipDNS:       input.Opt("skip-dns").(bool),
		}
		create.SetLogger(log)
		create.SetTerm(term)
		return create.Execute()
	}))
	actions = append(actions, createAction)

	// platform:list action
	listAction := action.NewFromYAML("platform:list", actionListYaml)
	listAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		list := &platformList{
			format: input.Opt("format").(string),
		}
		list.SetLogger(log)
		list.SetTerm(term)
		return list.Execute()
	}))
	actions = append(actions, listAction)

	// platform:show action
	showAction := action.NewFromYAML("platform:show", actionShowYaml)
	showAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		show := &platformShow{
			name:   input.Arg("name").(string),
			format: input.Opt("format").(string),
		}
		show.SetLogger(log)
		show.SetTerm(term)
		return show.Execute()
	}))
	actions = append(actions, showAction)

	// platform:validate action
	validateAction := action.NewFromYAML("platform:validate", actionValidateYaml)
	validateAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		validate := &platformValidate{
			name:     input.Arg("name").(string),
			skipDNS:  input.Opt("skip-dns").(bool),
			skipMail: input.Opt("skip-mail").(bool),
		}
		validate.SetLogger(log)
		validate.SetTerm(term)
		return validate.Execute()
	}))
	actions = append(actions, validateAction)

	// platform:destroy action
	destroyAction := action.NewFromYAML("platform:destroy", actionDestroyYaml)
	destroyAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		destroy := &destroyPlatformAction{
			keyring:    p.k,
			name:       input.Arg("name").(string),
			yesIAmSure: input.Opt("yes-i-am-sure").(bool),
			keepDNS:    input.Opt("keep-dns").(bool),
		}
		destroy.SetLogger(log)
		destroy.SetTerm(term)
		return destroy.Execute()
	}))
	actions = append(actions, destroyAction)

	// config:get action
	configGetAction := action.NewFromYAML("config:get", actionConfigGetYaml)
	configGetAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		get := &cfgGet{
			key:      input.Arg("key").(string),
			vault:    input.Opt("vault").(bool),
			platform: input.Opt("platform").(string),
		}
		get.SetLogger(log)
		get.SetTerm(term)
		return get.Execute()
	}))
	actions = append(actions, configGetAction)

	// config:set action
	configSetAction := action.NewFromYAML("config:set", actionConfigSetYaml)
	configSetAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		set := &cfgSet{
			key:      input.Arg("key").(string),
			value:    input.Arg("value").(string),
			vault:    input.Opt("vault").(bool),
			platform: input.Opt("platform").(string),
		}
		set.SetLogger(log)
		set.SetTerm(term)
		return set.Execute()
	}))
	actions = append(actions, configSetAction)

	// config:list action
	configListAction := action.NewFromYAML("config:list", actionConfigListYaml)
	configListAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		// Handle optional argument
		component := ""
		if c := input.Arg("component"); c != nil {
			component = c.(string)
		}
		list := &cfgList{
			component: component,
			vault:     input.Opt("vault").(bool),
			platform:  input.Opt("platform").(string),
			format:    input.Opt("format").(string),
		}
		list.SetLogger(log)
		list.SetTerm(term)
		return list.Execute()
	}))
	actions = append(actions, configListAction)

	// config:validate action
	configValidateAction := action.NewFromYAML("config:validate", actionConfigValidateYaml)
	configValidateAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		// Handle optional argument
		component := ""
		if c := input.Arg("component"); c != nil {
			component = c.(string)
		}
		validate := &cfgValidate{
			component: component,
			platform:  input.Opt("platform").(string),
			strict:    input.Opt("strict").(bool),
		}
		validate.SetLogger(log)
		validate.SetTerm(term)
		return validate.Execute()
	}))
	actions = append(actions, configValidateAction)

	// config:rotate action
	configRotateAction := action.NewFromYAML("config:rotate", actionConfigRotateYaml)
	configRotateAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		// Handle optional argument
		key := ""
		if k := input.Arg("key"); k != nil {
			key = k.(string)
		}
		rotate := &cfgRotate{
			key:        key,
			platform:   input.Opt("platform").(string),
			yesIAmSure: input.Opt("yes-i-am-sure").(bool),
		}
		rotate.SetLogger(log)
		rotate.SetTerm(term)
		return rotate.Execute()
	}))
	actions = append(actions, configRotateAction)

	// platform:deploy action
	deployAction := action.NewFromYAML("platform:deploy", actionDeployYaml)
	deployAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		log, term := getLoggerTerm(a)
		deploy := &platformDeploy{
			keyring:     p.k,
			environment: input.Arg("environment").(string),
			tags:        input.Arg("tags").(string),
			img:         input.Opt("img").(string),
			debug:       input.Opt("debug").(bool),
			check:       input.Opt("check").(bool),
			password:    input.Opt("password").(string),
			logs:        input.Opt("logs").(bool),
			prepareDir:  input.Opt("prepare-dir").(string),
		}
		deploy.SetLogger(log)
		deploy.SetTerm(term)
		return deploy.Execute()
	}))
	actions = append(actions, deployAction)

	// Note: platform:prepare is NOT embedded here.
	// It must be provided by plasmactl-model plugin.
	// platform:ship validates its existence at runtime.

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

// Package plasmactlplatform implements a launchr plugin for platform lifecycle management
package plasmactlplatform

import (
	"context"
	"embed"
	"io/fs"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
	"github.com/launchrctl/launchr/pkg/action"
)

//go:embed action.ship.yaml
var actionShipYaml []byte

//go:embed action.package.yaml
var actionPackageYaml []byte

//go:embed action.publish.yaml
var actionPublishYaml []byte

//go:embed action.release
var actionReleaseFS embed.FS

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

	// platform:ship action (orchestrates CI/local builds)
	shipAction := action.NewFromYAML("platform:ship", actionShipYaml)
	shipAction.SetRuntime(action.NewFnRuntime(func(ctx context.Context, a *action.Action) error {
		input := a.Input()
		env := input.Arg("environment").(string)
		tags := input.Arg("tags").(string)
		v := launchr.Version()
		options := shipOptions{
			bin:                v.Name,
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
	actions = append(actions, shipAction)

	// platform:package action (creates tar.gz archive)
	packageAction := action.NewFromYAML("platform:package", actionPackageYaml)
	packageAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, _ *action.Action) error {
		return createArtifact()
	}))
	actions = append(actions, packageAction)

	// platform:publish action (uploads artifact to repository)
	publishAction := action.NewFromYAML("platform:publish", actionPublishYaml)
	publishAction.SetRuntime(action.NewFnRuntime(func(_ context.Context, a *action.Action) error {
		input := a.Input()
		username := input.Opt("username").(string)
		password := input.Opt("password").(string)
		return publishArtifact(username, password, p.k)
	}))
	actions = append(actions, publishAction)

	// platform:release action (creates git tags with changelog)
	releaseSubFS, err := fs.Sub(actionReleaseFS, "action.release")
	if err != nil {
		return nil, err
	}
	releaseAction, err := action.NewYAMLFromFS("platform:release", releaseSubFS)
	if err != nil {
		return nil, err
	}
	actions = append(actions, releaseAction)

	// Note: platform:prepare and platform:deploy are NOT embedded here.
	// They must be provided by the platform package (e.g., plasma-core).
	// platform:ship validates their existence at runtime.

	return actions, nil
}

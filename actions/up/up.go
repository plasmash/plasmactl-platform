package up

import (
	"context"
	"errors"
	"fmt"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-platform/internal/ci"
	"github.com/plasmash/plasmactl-platform/internal/git"
)

// UpOptions holds options for the platform:up command
type UpOptions struct {
	Bin                string
	Img                string
	Last               bool
	SkipBump           bool
	SkipPrepare        bool
	CI                 bool
	Local              bool
	Clean              bool
	CleanPrepare       bool
	Debug              bool
	ConflictsVerbosity bool
	GitlabDomain       string
	Streams            launchr.Streams
	Persistent         action.InputParams
}

// Up implements the platform:up command
type Up struct {
	action.WithLogger
	action.WithTerm

	K  keyring.Keyring
	M  action.Manager
	G  *git.GitUp
	CI *ci.ContinuousIntegration
}

// NewUp creates a new Up instance
func NewUp(a *action.Action, k keyring.Keyring, m action.Manager) *Up {
	log := launchr.Log()
	if rt, ok := a.Runtime().(action.RuntimeLoggerAware); ok {
		log = rt.LogWith()
	}

	term := launchr.Term()
	if rt, ok := a.Runtime().(action.RuntimeTermAware); ok {
		term = rt.Term()
	}

	u := &Up{K: k, M: m}
	u.SetLogger(log)
	u.SetTerm(term)

	u.G = &git.GitUp{WithLogger: u.WithLogger, WithTerm: u.WithTerm}
	u.CI = &ci.ContinuousIntegration{WithLogger: u.WithLogger, WithTerm: u.WithTerm}
	return u
}

// Run executes the platform:up workflow
func (u *Up) Run(ctx context.Context, environment, tags string, options UpOptions) error {
	if options.CI {
		u.Term().Info().Println("--ci option is deprecated: builds are now done by default in CI")
	}

	// Deploy from Platform Image - skip compose/sync/bump/prepare
	if options.Img != "" {
		u.Term().Info().Printfln("Deploying from Platform Image: %s", options.Img)

		err := u.executeAction(ctx, "platform:deploy", action.InputParams{
			"environment": environment,
			"tags":        tags,
		}, action.InputParams{
			"img":   options.Img,
			"debug": options.Debug,
		}, options.Persistent, options.Streams)
		if err != nil {
			return fmt.Errorf("deploy error: %w", err)
		}
		return nil
	}

	u.Log().Info("arguments", "environment", environment, "tags", tags)

	ansibleDebug := options.Debug
	if ansibleDebug {
		u.Term().Info().Printfln("Ansible debug mode: %t", ansibleDebug)
	}

	var username, password string

	// Commit unversioned changes if any
	err := u.G.CommitChangesIfAny()
	if err != nil {
		return fmt.Errorf("commit error: %w", err)
	}

	// Execute bump
	if !options.SkipBump {
		err = u.executeAction(ctx, "component:bump", nil, action.InputParams{
			"last": options.Last,
		},
			options.Persistent, options.Streams)
		if err != nil {
			return fmt.Errorf("bump error: %w", err)
		}
	} else {
		u.Term().Info().Println("--skip-bump option detected: Skipping bump execution")
	}
	u.Term().Printf("\n")

	if options.Local {
		u.Term().Info().Println("Starting local build")

		// Commands executed sequentially: compose → prepare → sync → deploy
		err = u.executeAction(ctx, "model:compose", nil, action.InputParams{
			"skip-not-versioned":  true,
			"conflicts-verbosity": options.ConflictsVerbosity,
			"clean":               options.Clean,
		}, options.Persistent, options.Streams)
		if err != nil {
			return fmt.Errorf("compose error: %w", err)
		}

		u.Term().Println()
		if !options.SkipPrepare {
			err = u.executeAction(ctx, "model:prepare", nil, action.InputParams{
				"clean": options.CleanPrepare,
			}, options.Persistent, options.Streams)
			if err != nil {
				return fmt.Errorf("prepare error: %w", err)
			}
			u.Term().Println()
		} else {
			u.Term().Info().Println("--skip-prepare option detected: Skipping prepare execution")
		}

		err = u.executeAction(ctx, "component:sync", nil, nil, options.Persistent, options.Streams)
		if err != nil {
			return fmt.Errorf("sync error: %w", err)
		}

		err = u.executeAction(ctx, "platform:deploy", action.InputParams{
			"environment": environment,
			"tags":        tags,
		}, action.InputParams{
			"debug": options.Debug,
		}, options.Persistent, options.Streams)
		if err != nil {
			return fmt.Errorf("deploy error: %w", err)
		}

	} else {
		u.Term().Info().Println("Starting CI build (now default behavior)")

		// Push branch if it does not exist on remote
		if err := u.G.PushBranchIfNotRemote(); err != nil {
			return err
		}

		// Push any un-pushed commits
		if err := u.G.PushCommitsIfAny(); err != nil {
			return err
		}

		gitlabDomain := options.GitlabDomain
		if gitlabDomain == "" {
			return fmt.Errorf("gitlab-domain is empty: pass it as option or local config")
		}
		u.Term().Info().Printfln("Getting user credentials for %s from keyring", gitlabDomain)
		c, save, err := u.getCredentials(gitlabDomain, username, password)
		if err != nil {
			return err
		}
		u.Term().Printfln("URL: %s", c.URL)
		u.Term().Printfln("Username: %s", c.Username)

		username = c.Username
		password = c.Password

		// Get Gitlab OAuth token
		gitlabAccessToken, err := u.CI.GetOAuthTokens(gitlabDomain, username, password)
		if err != nil {
			return fmt.Errorf("failed to get OAuth token: %w", err)
		}

		// Save gitlab credentials to keyring once API requests are successful
		if save {
			err = u.K.Save()
			u.Log().Debug("saving user credentials to keyring", "url", gitlabDomain)
			if err != nil {
				u.Log().Error("error during saving keyring file", "error", err)
			}
		}

		// Get branch name
		branchName, err := u.CI.GetBranchName()
		if err != nil {
			return fmt.Errorf("failed to get branch name: %w", err)
		}

		// Get repo name
		repoName, err := u.CI.GetRepoName()
		if err != nil {
			return fmt.Errorf("failed to get repo name: %w", err)
		}

		// Get project ID
		projectID, err := u.CI.GetProjectID(gitlabDomain, gitlabAccessToken, repoName)
		if err != nil {
			return fmt.Errorf("failed to get ID of project %q: %w", repoName, err)
		}

		// Trigger pipeline
		pipelineID, err := u.CI.TriggerPipeline(gitlabDomain, gitlabAccessToken, projectID, branchName, environment, tags, ansibleDebug)
		if err != nil {
			return fmt.Errorf("failed to trigger pipeline: %w", err)
		}

		// Get all jobs in the pipeline
		jobs, err := u.CI.GetJobsInPipeline(gitlabDomain, gitlabAccessToken, projectID, pipelineID)
		if err != nil {
			return fmt.Errorf("failed to retrieve jobs in pipeline: %w", err)
		}

		// Find the target job ID
		var targetJobID int
		for _, job := range jobs {
			if job.Name == ci.TargetJobName {
				targetJobID = job.ID
				break
			}
		}
		if targetJobID == 0 {
			return fmt.Errorf("no %s job found in pipeline", ci.TargetJobName)
		}

		// Trigger the manual job
		err = u.CI.TriggerManualJob(gitlabDomain, gitlabAccessToken, projectID, targetJobID, pipelineID)
		if err != nil {
			return fmt.Errorf("failed to trigger manual job: %w", err)
		}
	}
	return nil
}

func (u *Up) executeAction(ctx context.Context, id string, args, opts, persistent action.InputParams, streams launchr.Streams) error {
	a, ok := u.M.Get(id)
	if !ok {
		return fmt.Errorf("action %q was not found", id)
	}

	persistentKey := u.M.GetPersistentFlags().GetName()
	input := action.NewInput(a, args, opts, streams)
	for k, v := range persistent {
		input.SetFlagInGroup(persistentKey, k, v)
	}

	err := u.M.ValidateInput(a, input)
	if err != nil {
		return fmt.Errorf("failed to validate input for action %q: %w", id, err)
	}

	err = a.SetInput(input)
	if err != nil {
		return fmt.Errorf("failed to set input for action %q: %w", id, err)
	}

	u.M.Decorate(a)
	err = a.Execute(ctx)
	if err != nil {
		return fmt.Errorf("error executing action %q: %w", id, err)
	}
	return nil
}

func (u *Up) getCredentials(url, username, password string) (keyring.CredentialsItem, bool, error) {
	ci, err := u.K.GetForURL(url)
	save := false
	if err != nil {
		if errors.Is(err, keyring.ErrEmptyPass) {
			return ci, false, err
		} else if !errors.Is(err, keyring.ErrNotFound) {
			u.Log().Error("error", "error", err)
			return ci, false, errors.New("the keyring is malformed or wrong passphrase provided")
		}
		ci = keyring.CredentialsItem{}
		ci.URL = url
		ci.Username = username
		ci.Password = password
		if ci.Username == "" || ci.Password == "" {
			if ci.URL != "" {
				u.Term().Info().Printfln("Please add login and password for %s", ci.URL)
			}
			err = keyring.RequestCredentialsFromTty(&ci)
			if err != nil {
				return ci, false, err
			}
		}

		err = u.K.AddItem(ci)
		if err != nil {
			return ci, false, err
		}

		save = true
	}

	return ci, save, nil
}

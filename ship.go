package plasmactlplatform

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/launchrctl/keyring"
	"github.com/launchrctl/launchr"
	"github.com/launchrctl/launchr/pkg/action"
)

type shipOptions struct {
	bin                string
	last               bool
	skipBump           bool
	skipPrepare        bool
	ci                 bool
	local              bool
	clean              bool
	cleanPrepare       bool
	debug              bool
	conflictsVerbosity bool
	gitlabDomain       string
	streams            launchr.Streams
	persistent         action.InputParams
}

type shipAction struct {
	action.WithLogger
	action.WithTerm

	k  keyring.Keyring
	m  action.Manager
	g  *gitShip
	ci *continuousIntegration
}

func newShipAction(a *action.Action, k keyring.Keyring, m action.Manager) *shipAction {
	log := launchr.Log()
	if rt, ok := a.Runtime().(action.RuntimeLoggerAware); ok {
		log = rt.LogWith()
	}

	term := launchr.Term()
	if rt, ok := a.Runtime().(action.RuntimeTermAware); ok {
		term = rt.Term()
	}

	ship := &shipAction{k: k, m: m}
	ship.SetLogger(log)
	ship.SetTerm(term)

	ship.g = &gitShip{WithLogger: ship.WithLogger, WithTerm: ship.WithTerm}
	ship.ci = &continuousIntegration{WithLogger: ship.WithLogger, WithTerm: ship.WithTerm}
	return ship
}

func (sa *shipAction) run(ctx context.Context, environment, tags string, options shipOptions) error {
	if options.ci {
		sa.Term().Info().Println("--ci option is deprecated: builds are now done by default in CI")
	}

	sa.Log().Info("arguments", "environment", environment, "tags", tags)

	ansibleDebug := options.debug
	if ansibleDebug {
		sa.Term().Info().Printfln("Ansible debug mode: %t", ansibleDebug)
	}

	var username, password string

	// Commit unversioned changes if any
	err := sa.g.commitChangesIfAny()
	if err != nil {
		return fmt.Errorf("commit error: %w", err)
	}

	// Execute bump
	if !options.skipBump {
		err = sa.executeAction(ctx, "component:bump", nil, action.InputParams{
			"last": options.last,
		},
			options.persistent, options.streams)
		if err != nil {
			return fmt.Errorf("bump error: %w", err)
		}
	} else {
		sa.Term().Info().Println("--skip-bump option detected: Skipping bump execution")
	}
	sa.Term().Printf("\n")

	if options.local {
		sa.Term().Info().Println("Starting local build")

		// Commands executed sequentially: compose → prepare → sync → deploy
		err = sa.executeAction(ctx, "package:compose", nil, action.InputParams{
			"skip-not-versioned":  true,
			"conflicts-verbosity": options.conflictsVerbosity,
			"clean":               options.clean,
		}, options.persistent, options.streams)
		if err != nil {
			return fmt.Errorf("compose error: %w", err)
		}

		sa.Term().Println()
		if !options.skipPrepare {
			err = sa.executeAction(ctx, "platform:prepare", nil, action.InputParams{
				"clean": options.cleanPrepare,
			}, options.persistent, options.streams)
			if err != nil {
				return fmt.Errorf("prepare error: %w", err)
			}
			sa.Term().Println()
		} else {
			sa.Term().Info().Println("--skip-prepare option detected: Skipping prepare execution")
		}

		err = sa.executeAction(ctx, "component:sync", nil, nil, options.persistent, options.streams)
		if err != nil {
			return fmt.Errorf("sync error: %w", err)
		}

		err = sa.executeAction(ctx, "platform:deploy", action.InputParams{
			"environment": environment,
			"tags":        tags,
		}, action.InputParams{
			"debug": options.debug,
		}, options.persistent, options.streams)
		if err != nil {
			return fmt.Errorf("deploy error: %w", err)
		}

	} else {
		sa.Term().Info().Println("Starting CI build (now default behavior)")

		// Push branch if it does not exist on remote
		if err := sa.g.pushBranchIfNotRemote(); err != nil {
			return err
		}

		// Push any un-pushed commits
		if err := sa.g.pushCommitsIfAny(); err != nil {
			return err
		}

		gitlabDomain := options.gitlabDomain
		if gitlabDomain == "" {
			return fmt.Errorf("gitlab-domain is empty: pass it as option or local config")
		}
		sa.Term().Info().Printfln("Getting user credentials for %s from keyring", gitlabDomain)
		ci, save, err := sa.getCredentials(gitlabDomain, username, password)
		if err != nil {
			return err
		}
		sa.Term().Printfln("URL: %s", ci.URL)
		sa.Term().Printfln("Username: %s", ci.Username)

		username = ci.Username
		password = ci.Password

		// Get Gitlab OAuth token
		gitlabAccessToken, err := sa.ci.getOAuthTokens(gitlabDomain, username, password)
		if err != nil {
			return fmt.Errorf("failed to get OAuth token: %w", err)
		}

		// Save gitlab credentials to keyring once API requests are successful
		if save {
			err = sa.k.Save()
			sa.Log().Debug("saving user credentials to keyring", "url", gitlabDomain)
			if err != nil {
				sa.Log().Error("error during saving keyring file", "error", err)
			}
		}

		// Get branch name
		branchName, err := sa.ci.getBranchName()
		if err != nil {
			return fmt.Errorf("failed to get branch name: %w", err)
		}

		// Get repo name
		repoName, err := sa.ci.getRepoName()
		if err != nil {
			return fmt.Errorf("failed to get repo name: %w", err)
		}

		// Get project ID
		projectID, err := sa.ci.getProjectID(gitlabDomain, gitlabAccessToken, repoName)
		if err != nil {
			return fmt.Errorf("failed to get ID of project %q: %w", repoName, err)
		}

		// Trigger pipeline
		pipelineID, err := sa.ci.triggerPipeline(gitlabDomain, gitlabAccessToken, projectID, branchName, environment, tags, ansibleDebug)
		if err != nil {
			return fmt.Errorf("failed to trigger pipeline: %w", err)
		}

		// Get all jobs in the pipeline
		jobs, err := sa.ci.getJobsInPipeline(gitlabDomain, gitlabAccessToken, projectID, pipelineID)
		if err != nil {
			return fmt.Errorf("failed to retrieve jobs in pipeline: %w", err)
		}

		// Find the target job ID
		var targetJobID int
		for _, job := range jobs {
			if job.Name == targetJobName {
				targetJobID = job.ID
				break
			}
		}
		if targetJobID == 0 {
			return fmt.Errorf("no %s job found in pipeline", targetJobName)
		}

		// Trigger the manual job
		err = sa.ci.triggerManualJob(gitlabDomain, gitlabAccessToken, projectID, targetJobID, pipelineID)
		if err != nil {
			return fmt.Errorf("failed to trigger manual job: %w", err)
		}
	}
	return nil
}

func (sa *shipAction) executeAction(ctx context.Context, id string, args, opts, persistent action.InputParams, streams launchr.Streams) error {
	a, ok := sa.m.Get(id)
	if !ok {
		return fmt.Errorf("action %q was not found", id)
	}

	persistentKey := sa.m.GetPersistentFlags().GetName()
	input := action.NewInput(a, args, opts, streams)
	for k, v := range persistent {
		input.SetFlagInGroup(persistentKey, k, v)
	}

	err := sa.m.ValidateInput(a, input)
	if err != nil {
		return fmt.Errorf("failed to validate input for action %q: %w", id, err)
	}

	err = a.SetInput(input)
	if err != nil {
		return fmt.Errorf("failed to set input for action %q: %w", id, err)
	}

	sa.m.Decorate(a)
	err = a.Execute(ctx)
	if err != nil {
		return fmt.Errorf("error executing action %q: %w", id, err)
	}
	return nil
}

func (sa *shipAction) getCredentials(url, username, password string) (keyring.CredentialsItem, bool, error) {
	ci, err := sa.k.GetForURL(url)
	save := false
	if err != nil {
		if errors.Is(err, keyring.ErrEmptyPass) {
			return ci, false, err
		} else if !errors.Is(err, keyring.ErrNotFound) {
			sa.Log().Error("error", "error", err)
			return ci, false, errors.New("the keyring is malformed or wrong passphrase provided")
		}
		ci = keyring.CredentialsItem{}
		ci.URL = url
		ci.Username = username
		ci.Password = password
		if ci.Username == "" || ci.Password == "" {
			if ci.URL != "" {
				sa.Term().Info().Printfln("Please add login and password for %s", ci.URL)
			}
			err = keyring.RequestCredentialsFromTty(&ci)
			if err != nil {
				return ci, false, err
			}
		}

		err = sa.k.AddItem(ci)
		if err != nil {
			return ci, false, err
		}

		save = true
	}

	return ci, save, nil
}

func isURLAccessible(url string, code *int) bool {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}

	defer resp.Body.Close()
	*code = resp.StatusCode
	return resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices
}

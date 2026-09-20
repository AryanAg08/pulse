package pulse

import "time"

// CollectSignals gathers everything Pulse knows right now. It mutates state to
// advance focus tracking, so callers must persist state even on a silent cycle.
func CollectSignals(cfg Config, state *State, now time.Time) Signals {
	repos := CollectRepos(cfg.RepoRoots, cfg.User.GithubLogin, cfg.RepoScanDepth)

	gh := CollectGithub(cfg.User.GithubLogin)
	var errs []string
	if !gh.OK && gh.Err != "" {
		errs = append(errs, "github: "+gh.Err)
	}

	focus := UpdateFocus(repos, state, now)

	return Signals{
		At:             now.UnixMilli(),
		Repos:          repos,
		PRs:            gh.PRs,
		ReviewRequests: gh.ReviewRequests,
		FocusMinutes:   focus.Minutes,
		FocusRepo:      focus.Repo,
		GithubOK:       gh.OK,
		Errors:         errs,
	}
}

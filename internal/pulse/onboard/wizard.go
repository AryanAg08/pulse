package onboard

import (
	"pulse/internal/pulse"
)

// answers holds every response, separately from the config it eventually
// builds. Keeping them apart is what makes going back cheap: a revisited
// question shows what you said last time, and nothing has to be un-applied
// from a half-built config.
type answers struct {
	login     string
	roots     string
	dayStart  string
	dayEnd    string
	workDays  []int
	standup   bool
	standupAt string
	standupDs []int
	gym       bool
	gymAt     string
	gymDays   []int
	posture   bool
	postFrom  string
	postUntil string
	postEvery int
	postDays  []int
	chatty    int
	focusMin  string
	useAI     bool
	anthropic bool
	aiModel   string
	aiURL     string
	custom    []pulse.Routine
	// addCustom is the answer to "add another routine?", re-asked after each.
	addCustom bool
}

// step is one question. skip lets a step drop out of the plan when an earlier
// answer makes it irrelevant, and the driver honours that in both directions,
// so going back from "how often" lands on "until" rather than on a question
// that is no longer being asked.
type step struct {
	id string
	// header marks a step that only prints a section title. It asks nothing,
	// so going back must step over it — landing on a header would bounce
	// straight forward again and make Escape look broken.
	header bool
	skip   func(*answers) bool
	ask    func(*asker, *answers) error
}

// run walks the plan, moving forward on an answer and backward on errBack.
// Escape at the first question simply re-asks it: there is nowhere earlier to
// go, and quitting setup because of a stray keypress would be hostile.
func runSteps(a *asker, ans *answers, steps []step) {
	for i := 0; i < len(steps); {
		s := steps[i]
		if s.skip != nil && s.skip(ans) {
			i++
			continue
		}
		if err := s.ask(a, ans); err == errBack {
			i = prevAsked(steps, ans, i)
			continue
		}
		i++
	}
}

// prevAsked finds the nearest earlier step that is actually being asked.
func prevAsked(steps []step, ans *answers, from int) int {
	for j := from - 1; j >= 0; j-- {
		if steps[j].header {
			continue
		}
		if steps[j].skip == nil || !steps[j].skip(ans) {
			return j
		}
	}
	// Nothing earlier to return to: re-ask the first real question rather than
	// abandoning setup over a stray keypress.
	for j := 0; j < len(steps); j++ {
		if !steps[j].header {
			return j
		}
	}
	return 0
}

// toConfig folds the answers into the config. Doing this once at the end,
// rather than as each question is answered, means a revisited answer cannot
// leave a stale value behind.
func (ans *answers) toConfig(base pulse.Config) pulse.Config {
	cfg := base
	cfg.User.GithubLogin = ans.login
	if r := splitList(ans.roots); len(r) > 0 {
		cfg.RepoRoots = r
	}
	// Quiet hours are the complement of the working day.
	cfg.Quiet = pulse.QuietHours{Start: ans.dayEnd, End: ans.dayStart}

	var routines []pulse.Routine
	if ans.standup {
		routines = append(routines, pulse.Routine{
			Name: "Stand-up", At: ans.standupAt, Days: ans.standupDs,
			Note: "what you shipped yesterday",
		})
	}
	if ans.gym {
		routines = append(routines, pulse.Routine{
			Name: "Gym", At: ans.gymAt, Days: ans.gymDays,
		})
	}
	if ans.posture {
		routines = append(routines, pulse.Routine{
			Name: "Posture check", At: ans.postFrom, Until: ans.postUntil,
			Every: ans.postEvery, Days: ans.postDays,
			// A posture nudge to an empty chair is pure noise.
			RequireActive: true,
		})
	}
	routines = append(routines, ans.custom...)
	cfg.Routines = routines

	switch ans.chatty {
	case 0:
		cfg.MaxNudgesPerDay, cfg.MinMinutesBetweenNudge = 3, 90
	case 2:
		cfg.MaxNudgesPerDay, cfg.MinMinutesBetweenNudge = 10, 20
	default:
		cfg.MaxNudgesPerDay, cfg.MinMinutesBetweenNudge = 6, 45
	}
	cfg.Focus.BreakAfterMinutes = atoiOr(ans.focusMin, base.Focus.BreakAfterMinutes)

	cfg.Phrasing.UseLLM = ans.useAI
	if ans.useAI {
		if ans.anthropic {
			cfg.Phrasing.Provider = "anthropic"
		} else {
			cfg.Phrasing.Provider = "api"
			cfg.Phrasing.APIURL = ans.aiURL
		}
		cfg.Phrasing.ModelName = ans.aiModel
	}
	return cfg
}

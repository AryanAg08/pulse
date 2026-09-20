package onboard

import (
	"fmt"

	"pulse/internal/pulse"
	"pulse/internal/pulse/ui"
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

// routines assembles the declared routines. The section recap and the final
// config both call this, so what you are shown cannot drift from what is saved.
func (ans *answers) routines() []pulse.Routine {
	var out []pulse.Routine
	if ans.standup {
		days := ans.standupDs
		if days == nil {
			days = ans.workDays
		}
		out = append(out, pulse.Routine{
			Name: "Stand-up", At: ans.standupAt, Days: days,
			Note: "what you shipped yesterday",
		})
	}
	if ans.gym {
		out = append(out, pulse.Routine{Name: "Gym", At: ans.gymAt, Days: ans.gymDays})
	}
	if ans.posture {
		days := ans.postDays
		if days == nil {
			days = ans.workDays
		}
		out = append(out, pulse.Routine{
			Name: "Posture check", At: ans.postFrom, Until: ans.postUntil,
			Every: ans.postEvery, Days: days,
			// A posture nudge to an empty chair is pure noise.
			RequireActive: true,
		})
	}
	return append(out, ans.custom...)
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

	cfg.Routines = ans.routines()

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

// recap is an output-only step that echoes back what a section captured.
//
// It is marked as a header so Escape steps over it: a recap asks nothing, and
// landing on one would bounce straight forward. Because it re-renders whenever
// the driver passes it, correcting an answer and moving on shows the corrected
// value rather than a stale one.
func recap(id string, render func(*answers) []string) step {
	return step{
		id:     id,
		header: true,
		ask: func(a *asker, ans *answers) error {
			lines := render(ans)
			if len(lines) == 0 {
				return nil
			}
			a.say("")
			for _, l := range lines {
				a.say("  %s %s", ui.Symbol("ok"), l)
			}
			return nil
		},
	}
}

// field formats one recap row.
func field(label, value string) string {
	return ui.Pad(ui.Grey(label), 16) + value
}

// describeRoutine is the one-line form used in both the section recap and the
// final summary.
func describeRoutine(r pulse.Routine) string {
	when := r.At
	if r.Every > 0 && r.Until != "" {
		when = fmt.Sprintf("%s–%s every %dm", r.At, r.Until, r.Every)
	}
	if len(r.Days) > 0 {
		when += "  " + daysLabel(r.Days)
	} else {
		when += "  daily"
	}
	if r.RequireActive {
		when += ui.Grey("  (only when active)")
	}
	return field(r.Name, when)
}

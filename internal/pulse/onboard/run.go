package onboard

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"pulse/internal/pulse"
	"pulse/internal/pulse/ui"
)

// Run asks the setup questions and returns the resulting config.
//
// base carries what could be detected without asking — GitHub login, repo
// roots — so the questionnaire covers only what detection cannot know: when
// you work, when you want to be left alone, and what you want reminding of.
func Run(in io.Reader, out io.Writer, base pulse.Config) pulse.Config {
	a := newAsker(in, out)
	cfg := base

	a.say("")
	a.say("%s", ui.Bold("Let's set up Pulse."))
	a.say("%s", ui.Grey("  Enter accepts the default in brackets. Everything is editable later"))
	a.say("%s", ui.Grey("  in "+pulse.ConfigPath()+", or with `pulse init --force`."))
	a.say("")

	// --- identity and code location, confirming detection rather than asking cold
	a.say("%s", ui.Cyan("▌ you"))
	cfg.User.GithubLogin = a.line("GitHub username", base.User.GithubLogin)
	if roots := a.line("Where do you keep your code", strings.Join(base.RepoRoots, ", ")); roots != "" {
		cfg.RepoRoots = splitList(roots)
	}
	a.say("")

	// --- the working day, which drives quiet hours
	a.say("%s", ui.Cyan("▌ your day"))
	a.say("%s", ui.Grey("  Pulse stays silent outside these hours."))
	dayStart := a.timeOfDay("When does your day start", "09:00")
	dayEnd := a.timeOfDay("When do you want to stop being interrupted", "22:30")
	// Quiet hours are the complement of the working day: the window Pulse must
	// not speak in runs from the evening cutoff to the morning start.
	cfg.Quiet = pulse.QuietHours{Start: dayEnd, End: dayStart}
	workDays := a.days("Which days do you work", "weekdays")
	a.say("")

	// --- routines
	a.say("%s", ui.Cyan("▌ routines"))
	a.say("%s", ui.Grey("  Things Pulse should remind you of, whatever your code is doing."))
	var routines []pulse.Routine

	if a.yesNo("Daily stand-up?", true) {
		routines = append(routines, pulse.Routine{
			Name: "Stand-up",
			At:   a.timeOfDay("  what time", "10:00"),
			Days: a.days("  which days", daysLabel(workDays)),
			Note: "what you shipped yesterday",
		})
	}
	if a.yesNo("Gym or exercise?", true) {
		routines = append(routines, pulse.Routine{
			Name: "Gym",
			At:   a.timeOfDay("  what time", "19:00"),
			Days: a.days("  which days", "mon,wed,fri"),
		})
	}
	if a.yesNo("Posture and stand-up-from-the-desk reminders?", true) {
		routines = append(routines, pulse.Routine{
			Name: "Posture check",
			At:   a.timeOfDay("  what time", "15:00"),
			Days: workDays,
			// Only worth saying if you are actually at the keyboard.
			RequireActive: true,
		})
	}
	for a.yesNo("Add another routine?", false) {
		name := a.line("  what is it", "")
		if name == "" {
			break
		}
		routines = append(routines, pulse.Routine{
			Name: name,
			At:   a.timeOfDay("  what time", "18:00"),
			Days: a.days("  which days", "daily"),
		})
	}
	cfg.Routines = routines
	a.say("")

	// --- the noise ceiling, the single most consequential answer here
	a.say("%s", ui.Cyan("▌ how much should it talk"))
	a.say("%s", ui.Grey("  Most cycles say nothing. This is the ceiling, not the target."))
	switch strings.ToLower(a.line("quiet / normal / chatty", "normal")) {
	case "quiet", "q", "1":
		cfg.MaxNudgesPerDay, cfg.MinMinutesBetweenNudge = 3, 90
	case "chatty", "c", "3":
		cfg.MaxNudgesPerDay, cfg.MinMinutesBetweenNudge = 10, 20
	default:
		cfg.MaxNudgesPerDay, cfg.MinMinutesBetweenNudge = 6, 45
	}
	cfg.Focus.BreakAfterMinutes = atoiOr(
		a.line("Nudge you to stand after how many minutes of unbroken work",
			strconv.Itoa(base.Focus.BreakAfterMinutes)), base.Focus.BreakAfterMinutes)
	a.say("")

	// --- optional AI wording
	a.say("%s", ui.Cyan("▌ wording"))
	a.say("%s", ui.Grey("  Optional. Without it Pulse uses fixed wording and works offline."))
	if a.yesNo("Use pulse-ai to word the notifications?", false) {
		cfg.Phrasing.UseLLM = true
		if a.yesNo("  Anthropic? (no = any OpenAI-compatible endpoint)", true) {
			cfg.Phrasing.Provider = "anthropic"
			cfg.Phrasing.ModelName = a.line("  model", "claude-opus-5")
		} else {
			cfg.Phrasing.Provider = "api"
			cfg.Phrasing.APIURL = a.line("  api url", "")
			cfg.Phrasing.ModelName = a.line("  model id", "")
		}
		// The key is deliberately not asked for: an answer typed here lands in
		// a file on disk, and the environment is the safer home for it.
		a.say("  %s", ui.Grey("set the key in your shell, not here:"))
		a.say("  %s", ui.Grey("  export PULSE_API_KEY=…"))
		a.say("  %s", ui.Grey("launchd does not inherit your shell, so for the background agent:"))
		a.say("  %s", ui.Grey("  launchctl setenv PULSE_API_KEY …"))
	} else {
		cfg.Phrasing.UseLLM = false
	}
	a.say("")

	return cfg
}

// Summary is the recap printed after the questionnaire, so a wrong answer is
// visible before the first nudge rather than after it.
func Summary(cfg pulse.Config) []string {
	rows := []string{
		ui.Pad(ui.Grey("quiet hours"), 18) + cfg.Quiet.Start + " – " + cfg.Quiet.End,
		ui.Pad(ui.Grey("ceiling"), 18) + fmt.Sprintf("at most %d a day, %dm apart",
			cfg.MaxNudgesPerDay, cfg.MinMinutesBetweenNudge),
		ui.Pad(ui.Grey("focus break"), 18) + fmt.Sprintf("after %dm", cfg.Focus.BreakAfterMinutes),
	}
	for _, r := range cfg.Routines {
		when := r.At
		if len(r.Days) > 0 {
			when += "  " + daysLabel(r.Days)
		} else {
			when += "  daily"
		}
		if r.RequireActive {
			when += ui.Grey("  (only when active)")
		}
		rows = append(rows, ui.Pad(ui.Grey(r.Name), 18)+when)
	}
	return rows
}

func daysLabel(days []int) string {
	if len(days) == 0 {
		return "daily"
	}
	if len(days) == 5 && days[0] == 1 && days[4] == 5 {
		return "weekdays"
	}
	names := []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
	var out []string
	for _, d := range days {
		if d >= 0 && d < 7 {
			out = append(out, names[d])
		}
	}
	return strings.Join(out, ",")
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
		return n
	}
	return def
}

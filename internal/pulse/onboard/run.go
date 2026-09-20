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
	return RunOpts(in, out, base, false)
}

// weekdayItems is Monday-first, which is how people describe a working week,
// even though the stored values are Sunday-indexed to match time.Weekday.
var weekdayItems = []Item{
	{Label: "Monday", Value: 1}, {Label: "Tuesday", Value: 2},
	{Label: "Wednesday", Value: 3}, {Label: "Thursday", Value: 4},
	{Label: "Friday", Value: 5}, {Label: "Saturday", Value: 6},
	{Label: "Sunday", Value: 0},
}

// indicesToDays converts picker rows into the weekday numbers the config uses.
func indicesToDays(idx []int) []int {
	var out []int
	for _, i := range idx {
		if i >= 0 && i < len(weekdayItems) {
			out = append(out, weekdayItems[i].Value.(int))
		}
	}
	if len(out) == 7 {
		// Every day is spelled as no restriction.
		return nil
	}
	return out
}

func daysToIndices(days []int) []int {
	var out []int
	for i, it := range weekdayItems {
		for _, d := range days {
			if it.Value.(int) == d {
				out = append(out, i)
			}
		}
	}
	return out
}

var weekdayIdx = []int{0, 1, 2, 3, 4} // Mon–Fri rows

// RunOpts is Run with the picker path forced on, for a real terminal.
func RunOpts(in io.Reader, out io.Writer, base pulse.Config, interactive bool) pulse.Config {
	a := newAsker(in, out, interactive)
	ans := &answers{
		login:    base.User.GithubLogin,
		roots:    strings.Join(base.RepoRoots, ", "),
		dayStart: "09:00", dayEnd: "22:30",
		workDays: []int{1, 2, 3, 4, 5},
		standup:  true, standupAt: "10:00",
		gym: true, gymAt: "19:00", gymDays: []int{1, 3, 5},
		posture: true, postFrom: "10:00", postUntil: "18:00", postEvery: 30,
		chatty: 1, focusMin: strconv.Itoa(base.Focus.BreakAfterMinutes),
		anthropic: true, aiModel: "claude-opus-5",
	}

	a.say("")
	a.say("%s", ui.Bold("Let's set up Pulse."))
	a.say("%s", ui.Grey("  Enter accepts the default in brackets. Escape goes back a question."))
	a.say("%s", ui.Grey("  Everything is editable later in "+pulse.ConfigPath()+"."))
	a.say("")

	runSteps(a, ans, plan())

	cfg := ans.toConfig(base)
	a.say("")
	return cfg
}

// plan is the ordered list of questions. Steps that depend on an earlier
// answer declare it with skip, so the driver walks past them in both
// directions and Escape never lands on a question that is not being asked.
func plan() []step {
	notStandup := func(a *answers) bool { return !a.standup }
	notGym := func(a *answers) bool { return !a.gym }
	notPosture := func(a *answers) bool { return !a.posture }
	notAI := func(a *answers) bool { return !a.useAI }

	return []step{
		{id: "section-you", header: true, ask: func(a *asker, _ *answers) error {
			a.say("%s", ui.Cyan("▌ you"))
			return nil
		}},
		{id: "login", ask: func(a *asker, ans *answers) error {
			v, err := a.ask("GitHub username", ans.login, "")
			ans.login = v
			return err
		}},
		{id: "roots", ask: func(a *asker, ans *answers) error {
			v, err := a.ask("Where do you keep your code", ans.roots, "comma-separated")
			ans.roots = v
			return err
		}},

		recap("recap-you", func(ans *answers) []string {
			return []string{
				field("github", ans.login),
				field("code in", ans.roots),
			}
		}),

		{id: "section-day", header: true, ask: func(a *asker, _ *answers) error {
			a.say("")
			a.say("%s", ui.Cyan("▌ your day"))
			a.say("%s", ui.Grey("  Pulse stays silent outside these hours."))
			return nil
		}},
		{id: "day-start", ask: func(a *asker, ans *answers) error {
			v, err := a.timeOfDay("When does your day start", ans.dayStart)
			ans.dayStart = v
			return err
		}},
		{id: "day-end", ask: func(a *asker, ans *answers) error {
			v, err := a.timeOfDay("When do you want to stop being interrupted", ans.dayEnd)
			ans.dayEnd = v
			return err
		}},
		{id: "work-days", ask: func(a *asker, ans *answers) error {
			idx, err := a.pickMany("Which days do you work?", weekdayItems,
				daysToIndices(ans.workDays), daysLabel(ans.workDays))
			if err == nil {
				ans.workDays = indicesToDays(idx)
			}
			return err
		}},

		recap("recap-day", func(ans *answers) []string {
			return []string{
				field("working day", ans.dayStart+" – "+ans.dayEnd+"  "+daysLabel(ans.workDays)),
				// Stating the derived value matters: nobody was asked for it.
				field("silent", ans.dayEnd+" – "+ans.dayStart+ui.Grey("  (derived)")),
			}
		}),

		{id: "section-routines", header: true, ask: func(a *asker, _ *answers) error {
			a.say("")
			a.say("%s", ui.Cyan("▌ routines"))
			a.say("%s", ui.Grey("  Things Pulse should remind you of, whatever your code is doing."))
			return nil
		}},
		{id: "standup", ask: func(a *asker, ans *answers) error {
			v, err := a.yesNo("Daily stand-up?", ans.standup)
			ans.standup = v
			return err
		}},
		{id: "standup-at", skip: notStandup, ask: func(a *asker, ans *answers) error {
			v, err := a.timeOfDay("  what time", ans.standupAt)
			ans.standupAt = v
			return err
		}},
		{id: "standup-days", skip: notStandup, ask: func(a *asker, ans *answers) error {
			pre := ans.standupDs
			if pre == nil {
				pre = ans.workDays
			}
			idx, err := a.pickMany("  which days?", weekdayItems, daysToIndices(pre), daysLabel(pre))
			if err == nil {
				ans.standupDs = indicesToDays(idx)
			}
			return err
		}},

		{id: "gym", ask: func(a *asker, ans *answers) error {
			v, err := a.yesNo("Gym or exercise?", ans.gym)
			ans.gym = v
			return err
		}},
		{id: "gym-at", skip: notGym, ask: func(a *asker, ans *answers) error {
			v, err := a.timeOfDay("  what time", ans.gymAt)
			ans.gymAt = v
			return err
		}},
		{id: "gym-days", skip: notGym, ask: func(a *asker, ans *answers) error {
			idx, err := a.pickMany("  which days?", weekdayItems,
				daysToIndices(ans.gymDays), daysLabel(ans.gymDays))
			if err == nil {
				ans.gymDays = indicesToDays(idx)
			}
			return err
		}},

		{id: "posture", ask: func(a *asker, ans *answers) error {
			v, err := a.yesNo("Posture and stand-up-from-the-desk reminders?", ans.posture)
			ans.posture = v
			return err
		}},
		{id: "posture-from", skip: notPosture, ask: func(a *asker, ans *answers) error {
			v, err := a.timeOfDay("  from", ans.postFrom)
			ans.postFrom = v
			return err
		}},
		{id: "posture-until", skip: notPosture, ask: func(a *asker, ans *answers) error {
			v, err := a.timeOfDay("  until", ans.postUntil)
			ans.postUntil = v
			return err
		}},
		{id: "posture-every", skip: notPosture, ask: func(a *asker, ans *answers) error {
			v, err := a.interval("  how often?", ans.postEvery)
			ans.postEvery = v
			return err
		}},
		{id: "posture-days", skip: notPosture, ask: func(a *asker, ans *answers) error {
			pre := ans.postDays
			if pre == nil {
				pre = ans.workDays
			}
			idx, err := a.pickMany("  which days?", weekdayItems, daysToIndices(pre), daysLabel(pre))
			if err == nil {
				ans.postDays = indicesToDays(idx)
			}
			return err
		}},

		{id: "custom", ask: func(a *asker, ans *answers) error {
			return askCustomRoutines(a, ans)
		}},

		recap("recap-routines", func(ans *answers) []string {
			rs := ans.routines()
			if len(rs) == 0 {
				return []string{ui.Grey("no routines — Pulse will only nudge about your code")}
			}
			out := make([]string, 0, len(rs))
			for _, r := range rs {
				out = append(out, describeRoutine(r))
			}
			return out
		}),

		{id: "section-volume", header: true, ask: func(a *asker, _ *answers) error {
			a.say("")
			a.say("%s", ui.Cyan("▌ how much should it talk"))
			a.say("%s", ui.Grey("  Most cycles say nothing. This is the ceiling, not the target."))
			return nil
		}},
		{id: "chatty", ask: func(a *asker, ans *answers) error {
			v, err := a.pickOne("How much should it talk?", []Item{
				{Label: "Quiet", Hint: "at most 3 a day, 90m apart"},
				{Label: "Normal", Hint: "at most 6 a day, 45m apart"},
				{Label: "Chatty", Hint: "at most 10 a day, 20m apart"},
			}, ans.chatty)
			ans.chatty = v
			return err
		}},
		{id: "focus", ask: func(a *asker, ans *answers) error {
			v, err := a.ask("Nudge you to stand after how many minutes of unbroken work",
				ans.focusMin, "minutes")
			ans.focusMin = v
			return err
		}},

		recap("recap-volume", func(ans *answers) []string {
			perDay, gap := 6, 45
			switch ans.chatty {
			case 0:
				perDay, gap = 3, 90
			case 2:
				perDay, gap = 10, 20
			}
			return []string{
				field("ceiling", fmt.Sprintf("at most %d a day, %dm apart", perDay, gap)),
				field("focus break", "after "+ans.focusMin+"m"),
			}
		}),

		{id: "section-wording", header: true, ask: func(a *asker, _ *answers) error {
			a.say("")
			a.say("%s", ui.Cyan("▌ wording"))
			a.say("%s", ui.Grey("  Optional. Without it Pulse uses fixed wording and works offline."))
			return nil
		}},
		{id: "use-ai", ask: func(a *asker, ans *answers) error {
			v, err := a.yesNo("Use pulse-ai to word the notifications?", ans.useAI)
			ans.useAI = v
			return err
		}},
		{id: "ai-anthropic", skip: notAI, ask: func(a *asker, ans *answers) error {
			v, err := a.yesNo("  Anthropic? (no = any OpenAI-compatible endpoint)", ans.anthropic)
			ans.anthropic = v
			return err
		}},
		{id: "ai-url", skip: func(a *answers) bool { return !a.useAI || a.anthropic },
			ask: func(a *asker, ans *answers) error {
				v, err := a.ask("  api url", ans.aiURL, "https://…/v1")
				ans.aiURL = v
				return err
			}},
		{id: "ai-model", skip: notAI, ask: func(a *asker, ans *answers) error {
			v, err := a.ask("  model id", ans.aiModel, "")
			ans.aiModel = v
			return err
		}},
		{id: "ai-key-note", header: true, skip: notAI, ask: func(a *asker, _ *answers) error {
			// The key is deliberately never asked for: an answer typed here
			// lands in a file on disk, and the environment is safer.
			a.say("  %s", ui.Grey("set the key in your shell, not here:"))
			a.say("  %s", ui.Grey("  export PULSE_API_KEY=…"))
			a.say("  %s", ui.Grey("launchd does not inherit your shell, so for the background agent:"))
			a.say("  %s", ui.Grey("  launchctl setenv PULSE_API_KEY …"))
			return nil
		}},
		recap("recap-wording", func(ans *answers) []string {
			if !ans.useAI {
				return []string{ui.Grey("fixed wording — no model, works offline")}
			}
			where := "pulse-ai via your own endpoint"
			if ans.anthropic {
				where = "pulse-ai via Anthropic"
			}
			return []string{field("wording", where)}
		}),
	}
}

// askCustomRoutines is a loop rather than a step, because the number of extra
// routines is not known in advance. Escape inside it abandons the routine being
// added and returns to the loop's own question.
func askCustomRoutines(a *asker, ans *answers) error {
	for {
		more, err := a.yesNo("Add another routine?", false)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
		name, err := a.ask("  what is it", "", "")
		if err != nil || name == "" {
			continue
		}
		r := pulse.Routine{Name: name}
		if r.At, err = a.timeOfDay("  what time", "18:00"); err != nil {
			continue
		}
		repeat, err := a.yesNo("  repeat it through the day?", false)
		if err != nil {
			continue
		}
		if repeat {
			if r.Until, err = a.timeOfDay("  until", "18:00"); err != nil {
				continue
			}
			if r.Every, err = a.interval("  how often?", 30); err != nil {
				continue
			}
		}
		idx, err := a.pickMany("  which days?", weekdayItems, nil, "daily")
		if err != nil {
			continue
		}
		r.Days = indicesToDays(idx)
		ans.custom = append(ans.custom, r)
	}
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
		rows = append(rows, describeRoutine(r))
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

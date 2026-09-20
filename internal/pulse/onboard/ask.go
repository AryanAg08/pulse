// Package onboard is the interactive first-run questionnaire.
//
// It reads from an io.Reader rather than os.Stdin so the whole flow can be
// driven by a test, and every question has a default that Enter accepts — the
// fastest correct path through setup should be holding Enter.
package onboard

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"pulse/internal/pulse/ui"
)

type asker struct {
	in  *bufio.Scanner
	out io.Writer
	// interactive is false when there is no terminal to drive a picker — tests
	// and piped input take the text path instead.
	interactive bool
	// aborted is set when input ends early (piped input, Ctrl-D). Every
	// subsequent question then returns its default rather than blocking.
	aborted bool
}

func newAsker(in io.Reader, out io.Writer, interactive bool) *asker {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	return &asker{in: sc, out: out, interactive: interactive}
}

// pickOne shows a single-select list, falling back to a numbered text prompt
// when there is no terminal. defIdx is preselected and returned on abort.
func (a *asker) pickOne(title string, items []Item, defIdx int) int {
	if a.interactive && !a.aborted {
		p := newPicker(title, items, false, nil)
		p.cursor = defIdx
		if chosen, err := runPicker(p); err == nil && len(chosen) > 0 {
			return chosen[0]
		}
		return defIdx
	}

	a.say("  %s", ui.Bold(title))
	for i, it := range items {
		hint := ""
		if it.Hint != "" {
			hint = ui.Grey("  " + it.Hint)
		}
		a.say("    %s %s%s", ui.Grey(fmt.Sprintf("%d)", i+1)), it.Label, hint)
	}
	for {
		raw := a.line("choose", strconv.Itoa(defIdx+1))
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err == nil && n >= 1 && n <= len(items) {
			return n - 1
		}
		if a.aborted {
			return defIdx
		}
		a.say("  %s", ui.Grey(fmt.Sprintf("enter a number between 1 and %d", len(items))))
	}
}

// pickMany shows a multi-select list, falling back to the forgiving text
// parser when there is no terminal.
//
// Both paths return ROW INDICES into items, never the items' values. The text
// parser yields weekday numbers, so it is converted back — otherwise the
// caller's index-to-value mapping runs twice and Tuesday silently becomes
// Wednesday.
func (a *asker) pickMany(title string, items []Item, preselected []int, textDefault string) []int {
	if a.interactive && !a.aborted {
		if chosen, err := runPicker(newPicker(title, items, true, preselected)); err == nil {
			return chosen
		}
		return preselected
	}
	return a.valuesToIndices(items, a.days(title, textDefault))
}

// valuesToIndices maps weekday numbers back onto their rows. An empty result
// from the parser means "every day", which is every row.
func (a *asker) valuesToIndices(items []Item, values []int) []int {
	if len(values) == 0 {
		all := make([]int, len(items))
		for i := range items {
			all[i] = i
		}
		return all
	}
	var out []int
	for i, it := range items {
		for _, v := range values {
			if n, ok := it.Value.(int); ok && n == v {
				out = append(out, i)
			}
		}
	}
	return out
}

func (a *asker) say(format string, args ...any) {
	fmt.Fprintf(a.out, format+"\n", args...)
}

// line prompts and reads one answer, returning def when the user just hits
// Enter or input has ended.
func (a *asker) line(prompt, def string) string {
	if a.aborted {
		return def
	}
	hint := ""
	if def != "" {
		hint = ui.Grey(" [" + def + "]")
	}
	fmt.Fprintf(a.out, "  %s%s %s ", prompt, hint, ui.Cyan("›"))
	if !a.in.Scan() {
		a.aborted = true
		fmt.Fprintln(a.out)
		return def
	}
	if v := strings.TrimSpace(a.in.Text()); v != "" {
		return v
	}
	return def
}

func (a *asker) yesNo(prompt string, def bool) bool {
	d := "y/N"
	if def {
		d = "Y/n"
	}
	for {
		switch strings.ToLower(a.line(prompt, d)) {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		case d:
			return def
		default:
			if a.aborted {
				return def
			}
			a.say("  %s", ui.Grey("please answer y or n"))
		}
	}
}

// timeOfDay keeps asking until the answer parses as HH:MM. A malformed time
// silently disables a routine, so this is worth being strict about.
func (a *asker) timeOfDay(prompt, def string) string {
	for {
		v := normaliseTime(a.line(prompt, def))
		if v != "" {
			return v
		}
		if a.aborted {
			return def
		}
		a.say("  %s", ui.Grey("use 24-hour HH:MM, for example 19:00"))
	}
}

// normaliseTime accepts "9:00", "09:00", "0900", "9" and returns "09:00",
// or "" if it cannot be read as a time.
func normaliseTime(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimSuffix(s, "hrs")
	s = strings.ReplaceAll(s, ".", ":")

	var h, m int
	var err error
	switch {
	case strings.Contains(s, ":"):
		parts := strings.SplitN(s, ":", 2)
		if h, err = strconv.Atoi(strings.TrimSpace(parts[0])); err != nil {
			return ""
		}
		if m, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
			return ""
		}
	case len(s) == 4:
		if h, err = strconv.Atoi(s[:2]); err != nil {
			return ""
		}
		if m, err = strconv.Atoi(s[2:]); err != nil {
			return ""
		}
	default:
		if h, err = strconv.Atoi(s); err != nil {
			return ""
		}
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", h, m)
}

var dayAliases = map[string]int{
	"sun": 0, "sunday": 0,
	"mon": 1, "monday": 1,
	"tue": 2, "tues": 2, "tuesday": 2,
	"wed": 3, "weds": 3, "wednesday": 3,
	"thu": 4, "thur": 4, "thurs": 4, "thursday": 4,
	"fri": 5, "friday": 5,
	"sat": 6, "saturday": 6,
}

// parseDays reads "weekdays", "daily", "weekends", or a list like
// "mon,wed,fri". Returns nil for every day, which is how the config spells it.
func parseDays(s string) ([]int, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "daily", "everyday", "every day", "all":
		return nil, true
	case "weekdays", "weekday":
		return []int{1, 2, 3, 4, 5}, true
	case "weekends", "weekend":
		return []int{0, 6}, true
	}

	seen := map[int]bool{}
	var out []int
	for _, raw := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '/'
	}) {
		d, ok := dayAliases[strings.TrimSpace(raw)]
		if !ok {
			return nil, false
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// days is the text path: forgiving parsing of "weekdays", "tue,thu", etc.
// intervalChoices are the common answers; the last one opens a free-text
// prompt so an unusual cadence is still one keystroke away.
var intervalChoices = []int{10, 15, 30, 45, 60}

// interval asks how often a repeating routine should fire, in minutes.
func (a *asker) interval(prompt string, def int) int {
	items := make([]Item, 0, len(intervalChoices)+1)
	defIdx := 0
	for i, m := range intervalChoices {
		items = append(items, Item{Label: fmt.Sprintf("every %d minutes", m), Value: m})
		if m == def {
			defIdx = i
		}
	}
	items = append(items, Item{Label: "custom…", Hint: "type your own"})

	choice := a.pickOne(prompt, items, defIdx)
	if choice < len(intervalChoices) {
		return intervalChoices[choice]
	}
	for {
		v := strings.TrimSpace(strings.TrimSuffix(
			strings.ToLower(a.line("    minutes between reminders", strconv.Itoa(def))), "m"))
		n, err := strconv.Atoi(v)
		if err == nil && n >= 1 && n <= 24*60 {
			return n
		}
		if a.aborted {
			return def
		}
		a.say("  %s", ui.Grey("enter a whole number of minutes, 1 to 1440"))
	}
}

// days is the text path: forgiving parsing of "weekdays", "tue,thu", etc.
func (a *asker) days(prompt, def string) []int {
	for {
		d, ok := parseDays(a.line(prompt, def))
		if ok {
			return d
		}
		if a.aborted {
			d, _ = parseDays(def)
			return d
		}
		a.say("  %s", ui.Grey("try: weekdays · daily · weekends · mon,wed,fri"))
	}
}

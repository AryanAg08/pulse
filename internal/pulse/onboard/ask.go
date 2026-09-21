// Package onboard is the interactive first-run questionnaire.
//
// It reads from an io.Reader rather than os.Stdin so the whole flow can be
// driven by a test, and every question has a default that Enter accepts — the
// fastest correct path through setup should be holding Enter.
package onboard

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pulse/internal/pulse/ui"
)

// errBack is returned by every prompt when the user pressed Escape. It is a
// sentinel rather than a bool on each signature so the step driver can treat
// "go back" uniformly.
var errBack = errors.New("back")

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
func (a *asker) pickOne(title string, items []Item, defIdx int) (int, error) {
	if a.interactive && !a.aborted {
		p := newPicker(title, items, false, nil)
		p.cursor = defIdx
		chosen, back, err := runPicker(p)
		if back {
			return defIdx, errBack
		}
		if err == nil && len(chosen) > 0 {
			return chosen[0], nil
		}
		return defIdx, nil
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
		raw, err := a.ask("choose", strconv.Itoa(defIdx+1), "")
		if err != nil {
			return defIdx, err
		}
		n, convErr := strconv.Atoi(strings.TrimSpace(raw))
		if convErr == nil && n >= 1 && n <= len(items) {
			return n - 1, nil
		}
		if a.aborted {
			return defIdx, nil
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
func (a *asker) pickMany(title string, items []Item, preselected []int, textDefault string) ([]int, error) {
	if a.interactive && !a.aborted {
		chosen, back, err := runPicker(newPicker(title, items, true, preselected))
		if back {
			return preselected, errBack
		}
		if err == nil {
			return chosen, nil
		}
		return preselected, nil
	}
	days, err := a.days(title, textDefault)
	if err != nil {
		return preselected, err
	}
	return a.valuesToIndices(items, days), nil
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

// ask prompts for text and can report errBack. In a terminal it runs the raw
// mode input; otherwise it falls back to the line reader, where a lone "b"
// means back since Escape cannot be seen in a buffered read.
func (a *asker) ask(prompt, def, hint string) (string, error) {
	if a.interactive && !a.aborted {
		final, err := tea.NewProgram(newTextInput(prompt, def, hint)).Run()
		if err != nil {
			return def, nil
		}
		t := final.(textInput)
		if t.back {
			return "", errBack
		}
		return unquote(t.answer()), nil
	}
	v := a.line(prompt, def)
	if strings.EqualFold(strings.TrimSpace(v), "b") {
		return "", errBack
	}
	return unquote(v), nil
}

// unquote strips wrapping quotes and whitespace. Values are routinely pasted
// straight out of a YAML file, brackets and all, and a URL carrying literal
// quote characters fails later with an error that names neither the field nor
// the cause.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	for len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			s = strings.TrimSpace(s[1 : len(s)-1])
			continue
		}
		break
	}
	return s
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

func (a *asker) yesNo(prompt string, def bool) (bool, error) {
	d := "y/N"
	if def {
		d = "Y/n"
	}
	for {
		v, err := a.ask(prompt, d, "")
		if err != nil {
			return def, err
		}
		switch strings.ToLower(v) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		case d:
			return def, nil
		default:
			if a.aborted {
				return def, nil
			}
			a.say("  %s", ui.Grey("please answer y or n"))
		}
	}
}

// timeOfDay keeps asking until the answer parses as HH:MM. A malformed time
// silently disables a routine, so this is worth being strict about.
func (a *asker) timeOfDay(prompt, def string) (string, error) {
	for {
		raw, err := a.ask(prompt, def, "24-hour, e.g. 09:00 · 9 · 0900 · 19.30")
		if err != nil {
			return def, err
		}
		if v := normaliseTime(raw); v != "" {
			return v, nil
		}
		if a.aborted {
			return def, nil
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
func (a *asker) interval(prompt string, def int) (int, error) {
	items := make([]Item, 0, len(intervalChoices)+1)
	defIdx := 0
	for i, m := range intervalChoices {
		items = append(items, Item{Label: fmt.Sprintf("every %d minutes", m), Value: m})
		if m == def {
			defIdx = i
		}
	}
	items = append(items, Item{Label: "custom…", Hint: "type your own"})

	choice, err := a.pickOne(prompt, items, defIdx)
	if err != nil {
		return def, err
	}
	if choice < len(intervalChoices) {
		return intervalChoices[choice], nil
	}
	for {
		raw, err := a.ask("    minutes between reminders", strconv.Itoa(def), "1 to 1440")
		if err != nil {
			return def, err
		}
		v := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(raw), "m"))
		n, convErr := strconv.Atoi(v)
		if convErr == nil && n >= 1 && n <= 24*60 {
			return n, nil
		}
		if a.aborted {
			return def, nil
		}
		a.say("  %s", ui.Grey("enter a whole number of minutes, 1 to 1440"))
	}
}

// days is the text path: forgiving parsing of "weekdays", "tue,thu", etc.
func (a *asker) days(prompt, def string) ([]int, error) {
	for {
		raw, err := a.ask(prompt, def, "weekdays · daily · weekends · mon,wed,fri")
		if err != nil {
			return nil, err
		}
		d, ok := parseDays(raw)
		if ok {
			return d, nil
		}
		if a.aborted {
			d, _ = parseDays(def)
			return d, nil
		}
		a.say("  %s", ui.Grey("try: weekdays · daily · weekends · mon,wed,fri"))
	}
}

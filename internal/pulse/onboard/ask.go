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
	// aborted is set when input ends early (piped input, Ctrl-D). Every
	// subsequent question then returns its default rather than blocking.
	aborted bool
}

func newAsker(in io.Reader, out io.Writer) *asker {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	return &asker{in: sc, out: out}
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

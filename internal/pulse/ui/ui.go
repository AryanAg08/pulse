// Package ui renders Pulse's terminal output.
//
// Colour is opt-out in three ways, because this binary's most common caller is
// launchd writing to a plain file: it is disabled when stdout is not a
// terminal, when NO_COLOR is set (https://no-color.org), and when TERM=dumb.
// Every helper degrades to readable plain text, never to stray escape codes.
package ui

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

var enabled = detect()

func detect() bool {
	// NO_COLOR is checked first: an explicit opt-out must beat an inherited
	// force-on, which some tooling sets globally.
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	// Conventional force-on, so output can be piped to `less -R` or captured
	// for a screen recording without losing colour.
	if os.Getenv("CLICOLOR_FORCE") != "" || os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Enabled reports whether styling is active.
func Enabled() bool { return enabled }

// SetEnabled overrides detection, for tests and for a --no-color flag.
func SetEnabled(v bool) { enabled = v }

const (
	reset     = "\x1b[0m"
	codeBold  = "\x1b[1m"
	codeDim   = "\x1b[2m"
	codeRed   = "\x1b[31m"
	codeGreen = "\x1b[32m"
	codeAmber = "\x1b[33m"
	codeBlue  = "\x1b[34m"
	codeCyan  = "\x1b[36m"
	codeGrey  = "\x1b[90m"
)

func wrap(code, s string) string {
	if !enabled || s == "" {
		return s
	}
	return code + s + reset
}

func Bold(s string) string  { return wrap(codeBold, s) }
func Dim(s string) string   { return wrap(codeDim, s) }
func Grey(s string) string  { return wrap(codeGrey, s) }
func Red(s string) string   { return wrap(codeRed, s) }
func Green(s string) string { return wrap(codeGreen, s) }
func Amber(s string) string { return wrap(codeAmber, s) }
func Cyan(s string) string  { return wrap(codeCyan, s) }
func Blue(s string) string  { return wrap(codeBlue, s) }

// Width returns the printable width, ignoring escape sequences, so styled text
// can still be padded into columns.
func Width(s string) int {
	n, inEscape := 0, false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			n++
		}
	}
	return n
}

// Pad right-pads to a printable width of n.
func Pad(s string, n int) string {
	if d := n - Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// Header prints a section title with a leading accent bar.
func Header(title string) string {
	if !enabled {
		return title
	}
	return Cyan("▌") + " " + Bold(title)
}

// KV renders an aligned "  key   value" line. The key column is fixed so
// successive calls line up without the caller counting spaces.
func KV(key, value string) string {
	return "  " + Grey(Pad(key, 14)) + value
}

// Symbols degrade to ASCII when styling is off, so a log file stays greppable.
func Symbol(name string) string {
	if !enabled {
		switch name {
		case "ok":
			return "+"
		case "warn":
			return "!"
		case "crit":
			return "x"
		case "quiet":
			return "-"
		case "arrow":
			return "->"
		}
		return "*"
	}
	switch name {
	case "ok":
		return Green("●")
	case "warn":
		return Amber("●")
	case "crit":
		return Red("●")
	case "quiet":
		return Grey("○")
	case "arrow":
		return Grey("→")
	}
	return Grey("•")
}

// Severity maps a candidate priority onto a colour, so the eye can rank a list
// without reading the numbers.
func Severity(priority int) func(string) string {
	switch {
	case priority >= 80:
		return Red
	case priority >= 65:
		return Amber
	case priority >= 50:
		return Cyan
	default:
		return Grey
	}
}

func SeveritySymbol(priority int) string {
	switch {
	case priority >= 80:
		return Symbol("crit")
	case priority >= 65:
		return Symbol("warn")
	case priority >= 50:
		return Symbol("ok")
	default:
		return Symbol("quiet")
	}
}

// Bar renders a proportional meter. Block glyphs on a terminal, ASCII in a log
// file, since the launchd log should stay plain.
func Bar(fraction float64, width int, colour func(string) string) string {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	filled := int(fraction*float64(width) + 0.5)
	if !enabled {
		return "[" + strings.Repeat("=", filled) + strings.Repeat(" ", width-filled) + "]"
	}
	return colour(strings.Repeat("█", filled)) + Grey(strings.Repeat("░", width-filled))
}

// Rule draws a horizontal divider.
func Rule(width int) string {
	if !enabled {
		return strings.Repeat("-", width)
	}
	return Grey(strings.Repeat("─", width))
}

// Box frames lines, used for the one verdict that matters.
func Box(title string, lines []string) string {
	inner := Width(title) + 2
	for _, l := range lines {
		if w := Width(l); w > inner {
			inner = w
		}
	}
	inner += 2

	var b strings.Builder
	b.WriteString(Grey("╭─ ") + Bold(title) + " " + Grey(strings.Repeat("─", max(0, inner-Width(title)-3))) + Grey("╮") + "\n")
	for _, l := range lines {
		b.WriteString(Grey("│ ") + Pad(l, inner-1) + Grey("│") + "\n")
	}
	b.WriteString(Grey("╰" + strings.Repeat("─", inner) + "╯"))
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Truncate shortens to n printable runes with an ellipsis, so a long PR title
// cannot wrap and break a table.
func Truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// Printf-style convenience wrappers keep call sites terse.
func Linef(format string, a ...any) string { return fmt.Sprintf(format, a...) }

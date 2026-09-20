package onboard

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pulse/internal/pulse/ui"
)

// Item is one row of a picker.
type Item struct {
	Label string
	Hint  string
	// Value is what the caller gets back; Label is only what is shown.
	Value any
}

// picker is a one-question list: single-select on Enter, or multi-select with
// space when Multi is set. It is a Bubble Tea model so the same code drives
// both the live terminal and the tests.
type picker struct {
	title    string
	items    []Item
	cursor   int
	selected map[int]bool
	multi    bool
	done     bool
	quit     bool
}

func newPicker(title string, items []Item, multi bool, preselected []int) picker {
	sel := map[int]bool{}
	for _, i := range preselected {
		if i >= 0 && i < len(items) {
			sel[i] = true
		}
	}
	return picker{title: title, items: items, selected: sel, multi: multi}
}

func (p picker) Init() tea.Cmd { return nil }

func (p picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	switch key.String() {
	case "ctrl+c", "esc":
		p.quit, p.done = true, true
		return p, tea.Quit
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "j":
		if p.cursor < len(p.items)-1 {
			p.cursor++
		}
	case " ":
		if p.multi {
			p.selected[p.cursor] = !p.selected[p.cursor]
		}
	case "a":
		if p.multi {
			// Toggling all is the fastest route to "every day" or "none".
			all := len(p.selected) < len(p.items)
			for i := range p.items {
				p.selected[i] = all
			}
		}
	case "enter":
		if !p.multi {
			p.selected = map[int]bool{p.cursor: true}
		}
		p.done = true
		return p, tea.Quit
	}
	return p, nil
}

func (p picker) View() string {
	if p.done {
		return ""
	}
	var b strings.Builder
	b.WriteString("  " + ui.Bold(p.title) + "\n")
	for i, it := range p.items {
		cursor, box := "  ", ""
		if p.multi {
			box = ui.Grey("[ ] ")
			if p.selected[i] {
				box = ui.Green("[x] ")
			}
		}
		label := it.Label
		if i == p.cursor {
			cursor = ui.Cyan("› ")
			label = ui.Cyan(label)
		}
		hint := ""
		if it.Hint != "" {
			hint = ui.Grey("  " + it.Hint)
		}
		b.WriteString("  " + cursor + box + label + hint + "\n")
	}
	keys := "↑↓ move   enter confirm"
	if p.multi {
		keys = "↑↓ move   space toggle   a all   enter confirm"
	}
	b.WriteString("  " + ui.Grey(keys) + "\n")
	return b.String()
}

func (p picker) chosen() []int {
	var out []int
	for i := range p.items {
		if p.selected[i] {
			out = append(out, i)
		}
	}
	return out
}

// runPicker shows the picker and returns the chosen indices. It is only called
// when a terminal is present; see asker.pick for the fallback.
func runPicker(p picker) ([]int, error) {
	final, err := tea.NewProgram(p).Run()
	if err != nil {
		return nil, err
	}
	done := final.(picker)
	if done.quit {
		return nil, fmt.Errorf("cancelled")
	}
	return done.chosen(), nil
}

package onboard

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pulse/internal/pulse/ui"
)

// textInput is a one-line prompt that can also report "go back".
//
// A line-buffered read cannot see Escape — the key never arrives until Enter
// does — so text questions run in raw mode like the pickers. That is the only
// way Escape can mean the same thing at every question.
type textInput struct {
	prompt string
	hint   string
	value  []rune
	def    string
	// invalid is a validation message shown under the prompt, cleared on edit.
	invalid string
	back    bool
	done    bool
}

func newTextInput(prompt, def, hint string) textInput {
	return textInput{prompt: prompt, def: def, hint: hint}
}

func (t textInput) Init() tea.Cmd { return nil }

func (t textInput) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return t, nil
	}
	switch key.Type {
	case tea.KeyEsc:
		t.back, t.done = true, true
		return t, tea.Quit
	case tea.KeyCtrlC:
		t.done = true
		return t, tea.Quit
	case tea.KeyEnter:
		t.done = true
		return t, tea.Quit
	case tea.KeyBackspace:
		if len(t.value) > 0 {
			t.value = t.value[:len(t.value)-1]
		}
		t.invalid = ""
	case tea.KeyCtrlU:
		t.value, t.invalid = nil, ""
	case tea.KeySpace:
		t.value = append(t.value, ' ')
		t.invalid = ""
	case tea.KeyRunes:
		t.value = append(t.value, key.Runes...)
		t.invalid = ""
	}
	return t, nil
}

func (t textInput) View() string {
	if t.done {
		return ""
	}
	var b strings.Builder
	b.WriteString("  " + t.prompt)
	if t.def != "" {
		b.WriteString(ui.Grey(" [" + t.def + "]"))
	}
	b.WriteString(" " + ui.Cyan("› ") + string(t.value) + ui.Grey("▏") + "\n")
	if t.invalid != "" {
		b.WriteString("  " + ui.Amber(t.invalid) + "\n")
	}
	if t.hint != "" {
		b.WriteString("  " + ui.Grey(t.hint) + "\n")
	}
	b.WriteString("  " + ui.Grey("enter confirm   esc back") + "\n")
	return b.String()
}

// answer returns the typed value, or the default when nothing was typed.
func (t textInput) answer() string {
	if v := strings.TrimSpace(string(t.value)); v != "" {
		return v
	}
	return t.def
}

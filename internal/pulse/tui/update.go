package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"pulse/internal/pulse"
)

// visibleRows is how many list rows fit, leaving room for the header, the
// column titles, and the footer.
func (m Model) visibleRows() int {
	n := m.height - 7
	if n < 3 {
		return 3
	}
	return n
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case detailMsg:
		m.loading = false
		if msg.err != nil {
			// Staying on the detail view with an error beats bouncing the user
			// back to the list with no explanation.
			m.err = msg.err.Error()
			return m, nil
		}
		m.detail = msg.detail
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""

	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	}

	switch m.view {
	case viewRepos:
		return m.keyRepos(msg)
	case viewPRs:
		return m.keyPRs(msg)
	case viewDetail:
		return m.keyDetail(msg)
	}
	return m, nil
}

func (m Model) keyRepos(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.repoIdx = clamp(m.repoIdx-1, 0, len(m.repos)-1)
	case "down", "j":
		m.repoIdx = clamp(m.repoIdx+1, 0, len(m.repos)-1)
	case "home", "g":
		m.repoIdx = 0
	case "end", "G":
		m.repoIdx = len(m.repos) - 1
	case "pgup":
		m.repoIdx = clamp(m.repoIdx-m.visibleRows(), 0, len(m.repos)-1)
	case "pgdown":
		m.repoIdx = clamp(m.repoIdx+m.visibleRows(), 0, len(m.repos)-1)
	case "enter", "right", "l":
		if len(m.repos) == 0 {
			return m, nil
		}
		if len(m.allPRs()) == 0 {
			m.status = "no open pull requests in this repository"
			return m, nil
		}
		m.view, m.prIdx, m.scroll = viewPRs, 0, 0
		return m, nil
	}
	m.scroll = scrollFor(m.repoIdx, m.scroll, m.visibleRows())
	return m, nil
}

func (m Model) keyPRs(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	prs := m.allPRs()
	switch msg.String() {
	case "up", "k":
		m.prIdx = clamp(m.prIdx-1, 0, len(prs)-1)
	case "down", "j":
		m.prIdx = clamp(m.prIdx+1, 0, len(prs)-1)
	case "home", "g":
		m.prIdx = 0
	case "end", "G":
		m.prIdx = len(prs) - 1
	case "esc", "left", "h":
		m.view, m.scroll = viewRepos, scrollFor(m.repoIdx, 0, m.visibleRows())
		return m, nil
	case "o":
		if pr, ok := m.currentPR(); ok {
			pulse.OpenAction(pr.URL)
			m.status = "opened " + pr.URL
		}
		return m, nil
	case "enter", "right", "l":
		pr, ok := m.currentPR()
		if !ok {
			return m, nil
		}
		m.view, m.detailY, m.err = viewDetail, 0, ""
		m.detail = pulse.PRDetail{}
		m.loading = true
		return m, fetchDetail(pr.Repo, pr.Number)
	}
	m.scroll = scrollFor(m.prIdx, m.scroll, m.visibleRows())
	return m, nil
}

func (m Model) keyDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "left", "h":
		m.view, m.err = viewPRs, ""
		return m, nil
	case "up", "k":
		m.detailY = clamp(m.detailY-1, 0, 1<<30)
	case "down", "j":
		m.detailY++
	case "pgup":
		m.detailY = clamp(m.detailY-m.visibleRows(), 0, 1<<30)
	case "pgdown":
		m.detailY += m.visibleRows()
	case "home", "g":
		m.detailY = 0
	case "o":
		if pr, ok := m.currentPR(); ok {
			pulse.OpenAction(pr.URL)
			m.status = "opened " + pr.URL
		}
	case "r":
		if pr, ok := m.currentPR(); ok {
			m.loading, m.err = true, ""
			return m, fetchDetail(pr.Repo, pr.Number)
		}
	}
	return m, nil
}

// scrollFor keeps the cursor inside the visible window with the minimum
// movement, so the list does not jump around as you step through it.
func scrollFor(cursor, scroll, rows int) int {
	if cursor < scroll {
		return cursor
	}
	if cursor >= scroll+rows {
		return cursor - rows + 1
	}
	return scroll
}

package tui

import (
	"fmt"
	"strings"

	"pulse/internal/pulse/ui"
)

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	switch m.tab {
	case tabMetrics:
		return m.viewMetrics()
	case tabConfig:
		return m.viewConfig()
	}
	switch m.view {
	case viewPRs:
		return m.viewPRList()
	case viewDetail:
		return m.viewDetail()
	default:
		return m.viewRepoList()
	}
}

// chrome wraps a body with the title bar and the key hints, so every view has
// the same frame and the same number of lines.
func (m Model) chrome(title, crumbs, body, keys string) string {
	var b strings.Builder
	b.WriteString(m.tabBar() + "\n\n")
	// A title is the drill-down context inside a tab; the tab bar already
	// names the section, so the top level passes an empty one.
	if title != "" {
		b.WriteString(ui.Cyan("▌") + " " + ui.Bold(title))
		if crumbs != "" {
			b.WriteString("  " + ui.Grey(crumbs))
		}
		b.WriteString("\n\n")
	}
	b.WriteString(body)
	b.WriteString("\n")
	switch {
	case m.err != "":
		b.WriteString("  " + ui.Red("error: "+m.err) + "\n")
	case m.status != "":
		b.WriteString("  " + ui.Grey(m.status) + "\n")
	default:
		b.WriteString("\n")
	}
	b.WriteString("  " + ui.Grey(keys))
	return b.String()
}

// diffstat renders "+120 -8", the number everyone actually scans for.
func diffstat(add, del int) string {
	return ui.Green(fmt.Sprintf("+%d", add)) + " " + ui.Red(fmt.Sprintf("-%d", del))
}

func checkMark(checks string) string {
	switch checks {
	case "passing":
		return ui.Green("✓")
	case "failing":
		return ui.Red("✗")
	case "pending":
		return ui.Amber("•")
	default:
		return ui.Grey("·")
	}
}

// The list grid: two spaces, then a two-cell cursor gutter, then columns.
// Header and rows must use the same lead or every column drifts.
const (
	noCursor = "  "
	lead     = "    "
)

func (m Model) viewRepoList() string {
	var b strings.Builder
	b.WriteString(lead + ui.Grey(
		ui.Pad("repository", 30)+ui.Pad("branch", 22)+
			ui.Pad("PRs", 6)+ui.Pad("review", 8)+"dirty") + "\n")

	rows := m.visibleRows()
	end := clamp(m.scroll+rows, 0, len(m.repos))
	for i := m.scroll; i < end; i++ {
		r := m.repos[i]

		label := r.Name
		if r.Remote != "" {
			short := r.Remote[strings.LastIndex(r.Remote, "/")+1:]
			if !strings.EqualFold(short, r.Name) {
				label += " →" + short
			}
		}
		if r.Clones > 1 {
			label += fmt.Sprintf(" ×%d", r.Clones)
		}

		prs, rev, dirty := ui.Grey("·"), ui.Grey("·"), ui.Grey("·")
		if n := len(r.PRs); n > 0 {
			prs = fmt.Sprintf("%d", n)
			red := 0
			for _, pr := range r.PRs {
				if pr.Checks == "failing" {
					red++
				}
			}
			if red > 0 {
				prs += " " + ui.Red(fmt.Sprintf("%d✗", red))
			}
		}
		if n := len(r.Reviews); n > 0 {
			rev = ui.Amber(fmt.Sprintf("%d", n))
		}
		if r.Dirty > 0 {
			dirty = ui.Amber(fmt.Sprintf("%d", r.Dirty))
		}

		line := ui.Pad(ui.Truncate(label, 29), 30)
		cursor := noCursor
		if i == m.repoIdx {
			cursor, line = ui.Cyan("▸ "), ui.Cyan(line)
		}
		b.WriteString("  " + cursor + line +
			ui.Pad(ui.Grey(ui.Truncate(r.Branch, 21)), 22) +
			ui.Pad(prs, 6) + ui.Pad(rev, 8) + dirty + "\n")
	}
	for i := end - m.scroll; i < rows; i++ {
		b.WriteString("\n")
	}

	crumbs := fmt.Sprintf("%d repositories", len(m.repos))
	if len(m.repos) > rows {
		crumbs += fmt.Sprintf("  ·  %d-%d", m.scroll+1, end)
	}
	return m.chrome("repositories", crumbs, b.String(),
		"↑↓ move   → open   q quit")
}

func (m Model) viewPRList() string {
	repo := m.repos[m.repoIdx]
	prs := m.allPRs()
	owned := len(repo.PRs)

	var b strings.Builder
	b.WriteString(lead + ui.Grey(
		ui.Pad("#", 8)+ui.Pad("title", 46)+
			ui.Pad("diff", 16)+ui.Pad("files", 7)+"age") + "\n")

	rows := m.visibleRows()
	end := clamp(m.scroll+rows, 0, len(prs))
	for i := m.scroll; i < end; i++ {
		pr := prs[i]

		title := ui.Truncate(pr.Title, 44)
		if i >= owned {
			// Reviews owed are somebody else's PR; mark them so the list is
			// not silently mixing two different kinds of obligation.
			title = ui.Amber("review ") + ui.Truncate(pr.Title, 37)
		}

		cursor := noCursor
		if i == m.prIdx {
			cursor = ui.Cyan("▸ ")
		}
		// checkMark is one printable rune, so this column is 8 wide either way.
		num := checkMark(pr.Checks) + " " + ui.Pad(fmt.Sprintf("%d", pr.Number), 6)

		b.WriteString("  " + cursor +
			ui.Pad(num, 8) +
			ui.Pad(title, 46) +
			ui.Pad(diffstat(pr.Additions, pr.Deletions), 16) +
			ui.Pad(ui.Grey(fmt.Sprintf("%d", pr.ChangedFiles)), 7) +
			ui.Grey(age(pr.StaleHours)) + "\n")
	}
	for i := end - m.scroll; i < rows; i++ {
		b.WriteString("\n")
	}

	return m.chrome(repo.Name, fmt.Sprintf("%d open  ·  %d review owed", owned, len(repo.Reviews)),
		b.String(), "↑↓ move   → detail   o open in browser   ← back   q quit")
}

func (m Model) viewDetail() string {
	pr, ok := m.currentPR()
	if !ok {
		return m.chrome("detail", "", "  no pull request selected", "← back   q quit")
	}

	var b strings.Builder
	b.WriteString("  " + ui.Bold(ui.Truncate(pr.Title, m.width-4)) + "\n\n")
	b.WriteString("  " + ui.Grey("author  ") + pr.Author + "\n")
	b.WriteString("  " + ui.Grey("branch  ") + pr.HeadRef + ui.Grey(" → ") + pr.BaseRef + "\n")
	b.WriteString("  " + ui.Grey("diff    ") + diffstat(pr.Additions, pr.Deletions) +
		ui.Grey(fmt.Sprintf("  across %d files", pr.ChangedFiles)) + "\n")
	b.WriteString("  " + ui.Grey("checks  ") + checkMark(pr.Checks) + " " + pr.Checks + "\n")
	b.WriteString("  " + ui.Grey("idle    ") + age(pr.StaleHours) + "\n")
	b.WriteString("  " + ui.Grey("url     ") + ui.Grey(pr.URL) + "\n\n")

	if m.loading {
		b.WriteString("  " + ui.Grey("loading description…") + "\n")
		return m.chrome(fmt.Sprintf("#%d", pr.Number), pr.Repo, b.String(), "← back   q quit")
	}

	// Body and files are one scrollable region, so a long description does not
	// push the file list permanently out of reach.
	var scrollable []string
	if strings.TrimSpace(m.detail.Body) == "" {
		scrollable = append(scrollable, ui.Grey("  (no description)"))
	} else {
		scrollable = append(scrollable, ui.Grey("  ── description "+strings.Repeat("─", clamp(m.width-20, 0, 60))))
		for _, line := range wrap(m.detail.Body, clamp(m.width-6, 20, 100)) {
			scrollable = append(scrollable, "  "+line)
		}
	}
	if len(m.detail.Files) > 0 {
		scrollable = append(scrollable, "",
			ui.Grey(fmt.Sprintf("  ── files (%d) ", len(m.detail.Files))+strings.Repeat("─", clamp(m.width-24, 0, 55))))
		for _, f := range m.detail.Files {
			scrollable = append(scrollable, "  "+
				ui.Pad(diffstat(f.Additions, f.Deletions), 18)+
				ui.Truncate(f.Path, clamp(m.width-26, 20, 80)))
		}
	}

	window := m.height - 14
	if window < 3 {
		window = 3
	}
	maxScroll := clamp(len(scrollable)-window, 0, 1<<30)
	y := clamp(m.detailY, 0, maxScroll)
	for i := y; i < clamp(y+window, 0, len(scrollable)); i++ {
		b.WriteString(scrollable[i] + "\n")
	}

	crumbs := pr.Repo
	if maxScroll > 0 {
		crumbs += fmt.Sprintf("  ·  %d%%", int(float64(y)/float64(maxScroll)*100))
	}
	return m.chrome(fmt.Sprintf("#%d", pr.Number), crumbs, b.String(),
		"↑↓ scroll   o open in browser   r reload   ← back   q quit")
}

func age(hours float64) string {
	switch {
	case hours < 1:
		return "just now"
	case hours < 24:
		return fmt.Sprintf("%dh", int(hours))
	default:
		return fmt.Sprintf("%dd", int(hours/24))
	}
}

// wrap breaks text to width, preserving blank lines so paragraphs and markdown
// lists in a PR description stay readable.
func wrap(text string, width int) []string {
	var out []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		if line == "" {
			out = append(out, "")
			continue
		}
		for len(line) > width {
			cut := strings.LastIndex(line[:width], " ")
			if cut <= 0 {
				cut = width
			}
			out = append(out, line[:cut])
			line = strings.TrimLeft(line[cut:], " ")
		}
		out = append(out, line)
	}
	return out
}

package tui

import (
	"fmt"
	"strings"
	"time"

	"pulse/internal/pulse"
	"pulse/internal/pulse/llm"
	"pulse/internal/pulse/ui"
)

// tabBar renders the section switcher. The active tab is underlined rather
// than merely coloured, so it stays legible without colour.
func (m Model) tabBar() string {
	var parts []string
	for i, name := range tabNames {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if tab(i) == m.tab {
			parts = append(parts, ui.Cyan(ui.Bold(label)))
		} else {
			parts = append(parts, ui.Grey(label))
		}
	}
	bar := strings.Join(parts, ui.Grey("│"))

	// Underline the active tab's span so the selection survives a mono theme.
	var underline strings.Builder
	for i, name := range tabNames {
		seg := len(name) + 4
		if i > 0 {
			underline.WriteString(" ")
		}
		if tab(i) == m.tab {
			underline.WriteString(ui.Cyan(strings.Repeat("─", seg)))
		} else {
			underline.WriteString(strings.Repeat(" ", seg))
		}
	}
	return bar + "\n" + underline.String()
}

// pane renders a titled block of rows, the layout unit both dashboards share.
func pane(title string, rows []string) string {
	var b strings.Builder
	b.WriteString("  " + ui.Grey("── "+title+" ") + ui.Grey(strings.Repeat("─", clamp(58-len(title), 0, 58))) + "\n")
	for _, r := range rows {
		b.WriteString("  " + r + "\n")
	}
	return b.String()
}

func meter(label string, fraction float64, right string, colour func(string) string) string {
	return ui.Pad(ui.Grey(label), 16) + ui.Bar(fraction, 24, colour) + "  " + right
}

// historyWindow is how many history rows the cursor can page through. The
// metrics body is windowed as a whole, so this only needs to be a sane paging
// step, not an exact viewport height.
func (m Model) historyWindow() int {
	return clamp(m.bodyBudget()/3, 2, 12)
}

// window trims lines to the body budget, scrolling so that keepVisible (an
// index into lines, or -1) stays on screen. Returning a slice rather than
// truncating means the caller cannot accidentally overflow the terminal.
func (m Model) window(lines []string, offset, keepVisible int) []string {
	return m.windowTo(lines, offset, keepVisible, m.bodyBudget())
}

// windowTo is window with an explicit budget, for views that spend lines on a
// title or a fixed header before the scrollable part begins.
func (m Model) windowTo(lines []string, offset, keepVisible, budget int) []string {
	if budget < 1 {
		budget = 1
	}
	if len(lines) <= budget {
		return lines
	}
	maxOffset := len(lines) - budget
	if keepVisible >= 0 {
		// Scroll the minimum needed to bring the cursor into view.
		if keepVisible < offset {
			offset = keepVisible
		}
		if keepVisible >= offset+budget {
			offset = keepVisible - budget + 1
		}
	}
	offset = clamp(offset, 0, maxOffset)
	return lines[offset : offset+budget]
}

func (m Model) viewMetrics() string {
	var lines []string
	add := func(block string) {
		for _, l := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
			lines = append(lines, l)
		}
	}
	blank := func() { lines = append(lines, "") }

	// --- the experiment
	engaged := ui.Red
	switch {
	case m.metrics.EngagedRate >= 0.4:
		engaged = ui.Green
	case m.metrics.EngagedRate >= 0.25:
		engaged = ui.Amber
	}
	dayFrac := float64(m.metrics.DaysInstalled) / 14

	add(pane("the 14-day experiment", []string{
		meter("day", dayFrac, ui.Grey(fmt.Sprintf("%d of 14", m.metrics.DaysInstalled)), ui.Cyan),
		meter("engaged", m.metrics.EngagedRate,
			engaged(fmt.Sprintf("%3.0f%%", m.metrics.EngagedRate*100))+
				ui.Grey("   ack or dismiss · 40% is the bar"), engaged),
		meter("acked", m.metrics.AckRate,
			ui.Grey(fmt.Sprintf("%3.0f%%", m.metrics.AckRate*100)), ui.Green),
		meter("ignored", m.metrics.IgnoredRate,
			ui.Grey(fmt.Sprintf("%3.0f%%", m.metrics.IgnoredRate*100)), ui.Grey),
	}))
	blank()

	// --- noise budget, the ceiling that decides whether this survives
	today := 0
	start := pulse.StartOfToday(timeNow())
	for _, n := range m.nudges {
		if n.SentAt >= start {
			today++
		}
	}
	budget := float64(today) / float64(max1(m.cfg.MaxNudgesPerDay))
	quiet := ui.Green("no")
	if m.quietNow {
		quiet = ui.Amber("yes")
	}
	add(pane("today's noise budget", []string{
		meter("nudges", budget,
			ui.Grey(fmt.Sprintf("%d of %d  ·  min %dm apart",
				today, m.cfg.MaxNudgesPerDay, m.cfg.MinMinutesBetweenNudge)), ui.Cyan),
		ui.Pad(ui.Grey("quiet hours"), 16) + quiet +
			ui.Grey(fmt.Sprintf("   %s–%s", m.cfg.Quiet.Start, m.cfg.Quiet.End)),
		ui.Pad(ui.Grey("lifetime"), 16) +
			fmt.Sprintf("%d sent", m.metrics.TotalNudges) +
			ui.Grey(fmt.Sprintf("  ·  %.1f per active day", m.metrics.NudgesPerActiveDay)),
	}))
	blank()

	// --- per kind, so a single bad rule is visible rather than averaged away
	if len(m.metrics.ByKind) > 0 {
		rows := []string{ui.Grey(ui.Pad("kind", 20) + ui.Pad("sent", 7) +
			ui.Pad("ack", 6) + ui.Pad("dismiss", 9) + "ignored")}
		for _, k := range m.metrics.ByKind {
			rows = append(rows, ui.Pad(string(k.Kind), 20)+
				ui.Pad(fmt.Sprintf("%d", k.Sent), 7)+
				ui.Pad(ui.Green(fmt.Sprintf("%d", k.Ack)), 6)+
				ui.Pad(ui.Amber(fmt.Sprintf("%d", k.Dismiss)), 9)+
				ui.Grey(fmt.Sprintf("%d", k.Ignored)))
		}
		add(pane("by kind", rows))
		blank()
	}

	// --- the log, newest first, with a cursor
	hist := m.historyNewestFirst()
	log := make([]string, 0, len(hist))
	for i, n := range hist {
		cursor := noCursor
		if i == m.logIdx {
			cursor = ui.Cyan("▸ ")
		}
		text := ui.Truncate(n.Text, 50)
		if i == m.logIdx {
			text = ui.Cyan(text)
		}
		log = append(log, cursor+
			ui.Grey(time.UnixMilli(n.SentAt).Format("02 Jan 15:04"))+"  "+
			ui.Pad(text, 51)+responseMark(n))
	}
	if len(log) == 0 {
		log = []string{ui.Grey("nothing sent yet")}
	}

	// The whole body is windowed below, so the history renders in full and the
	// cursor row is what the viewport is anchored to.
	cursorLine := -1
	add(pane(fmt.Sprintf("history (%d)", len(hist)), nil))
	for i, row := range log {
		if i == m.logIdx {
			cursorLine = len(lines)
		}
		lines = append(lines, "  "+row)
	}

	// The detail for the hovered row is pinned below the scrolling region
	// rather than appended to it. Inside the window it would scroll out of
	// view exactly when the cursor moved, which is when it is wanted.
	var pinned []string
	if n, ok := m.hoveredNudge(); ok {
		pinned = append([]string{""}, strings.Split(
			strings.TrimRight(pane("selected", m.hoveredDetail(n)), "\n"), "\n")...)
	}

	listBudget := m.bodyBudget() - len(pinned)
	if listBudget < 4 {
		// Too short to show both; the list is the more useful half.
		pinned, listBudget = nil, m.bodyBudget()
	}

	body := append(m.windowTo(lines, m.logScroll, cursorLine, listBudget), pinned...)
	return m.chrome("", "", strings.Join(body, "\n"), "↑↓ hover   o open   tab switch   q quit")
}

func responseMark(n pulse.Nudge) string {
	if n.Response == nil {
		return ui.Grey("○ ignored")
	}
	switch *n.Response {
	case "ack":
		return ui.Green("● acked")
	case "dismiss":
		return ui.Amber("● dismissed")
	default:
		return ui.Grey("○ " + *n.Response)
	}
}

// hoveredDetail is the full record behind a history row: the untruncated text,
// how it was worded, and how long it took you to answer.
func (m Model) hoveredDetail(n pulse.Nudge) []string {
	rows := []string{ui.Pad(ui.Grey("text"), 14) + ui.Truncate(n.Text, clamp(m.width-22, 20, 90))}

	// Attribution is by product name; which model wrote it is not shown.
	tag := ui.Grey("template") + ui.Grey("   fixed wording, no model involved")
	if n.PhrasedBy == "llm" {
		tag = ui.Cyan(llm.DisplayName) + ui.Grey("   worded by the model")
	}
	rows = append(rows,
		ui.Pad(ui.Grey("kind"), 14)+string(n.Kind),
		ui.Pad(ui.Grey("phrased by"), 14)+tag,
		ui.Pad(ui.Grey("sent"), 14)+time.UnixMilli(n.SentAt).Format("Mon 02 Jan 15:04:05"),
		ui.Pad(ui.Grey("id"), 14)+ui.Grey(shortID(n.ID)),
	)

	answered := ui.Grey("no response") + ui.Grey("   counts as ignored")
	if n.Response != nil {
		answered = *n.Response
		if n.RespondedAt != nil {
			took := time.UnixMilli(*n.RespondedAt).Sub(time.UnixMilli(n.SentAt))
			answered += ui.Grey(fmt.Sprintf("   after %s", took.Round(time.Second)))
		}
	}
	rows = append(rows, ui.Pad(ui.Grey("response"), 14)+answered)

	if n.Action != "" {
		rows = append(rows, ui.Pad(ui.Grey("opens"), 14)+ui.Grey(ui.Truncate(n.Action, clamp(m.width-22, 20, 90))))
	}
	return rows
}

func shortID(id string) string {
	if len(id) > 6 {
		return id[:6]
	}
	return id
}

func (m Model) viewConfig() string {
	var lines []string
	add := func(block string) {
		for _, l := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
			lines = append(lines, l)
		}
	}
	blank := func() { lines = append(lines, "") }

	// The reason belongs on its own line: inlined, it pushes the pane well past
	// the terminal width and wraps into the next row.
	phrasing, phrasingWhy := ui.Green("● ")+m.phrasingName, ""
	if m.phrasingErr != "" {
		phrasing = ui.Amber("● ") + "falling back to templates"
		phrasingWhy = m.phrasingErr
	} else if m.phrasingName == "" {
		phrasing = ui.Grey("○ templates (useLLM is off)")
	}

	add(pane("source", []string{
		ui.Pad(ui.Grey("config"), 18) + m.configPath,
		ui.Pad(ui.Grey("state"), 18) + ui.Grey(pulse.Home()),
		ui.Pad(ui.Grey("background"), 18) + backgroundState(),
	}))
	blank()

	phrasingRows := []string{
		ui.Pad(ui.Grey("provider"), 18) + phrasing,
	}
	if phrasingWhy != "" {
		phrasingRows = append(phrasingRows, ui.Pad("", 18)+ui.Grey(ui.Truncate(phrasingWhy, 56)))
	}
	phrasingRows = append(phrasingRows,
		// The model id is deliberately not shown; it is in the config file for
		// anyone who needs it, and this view gets screen-shared.
		ui.Pad(ui.Grey("model"), 18)+ui.Grey(configuredOrNot(m.cfg.Phrasing.ResolvedModel())),
		ui.Pad(ui.Grey("endpoint"), 18)+ui.Grey(hostOnly(m.cfg.Phrasing.APIURL)),
		// The key itself is never displayed, only whether one resolved.
		ui.Pad(ui.Grey("credential"), 18)+credentialState(m.phrasingErr),
	)
	add(pane("phrasing", phrasingRows))
	blank()

	add(pane("discovery", []string{
		ui.Pad(ui.Grey("repo roots"), 18) + ui.Truncate(strings.Join(m.cfg.RepoRoots, ", "), 54),
		ui.Pad(ui.Grey("scan depth"), 18) + fmt.Sprintf("%d", m.scanDepth),
		ui.Pad(ui.Grey("found"), 18) + fmt.Sprintf("%s  ·  %s  ·  %s owed",
			plural(m.repoTotal, "repo", "repos"),
			plural(m.prTotal, "open PR", "open PRs"),
			plural(m.reviewTotal, "review", "reviews")),
	}))
	blank()

	t := m.cfg.Thresholds
	// setting renders "label  value  why", with the value column padded so the
	// explanations form a readable second column instead of ragged text.
	setting := func(label, value, why string) string {
		return ui.Pad(ui.Grey(label), 18) + ui.Pad(value, 11) + ui.Grey(why)
	}
	add(pane("thresholds", []string{
		setting("stale PR", fmt.Sprintf("%dh", t.StalePRHours), "a PR of yours with no review"),
		setting("review debt", fmt.Sprintf("%dh", t.ReviewDebtHours), "someone waiting on you"),
		setting("abandoned", fmt.Sprintf("%dd", t.AbandonedAfterDays), "past this a PR is dead, not stale"),
		setting("max per kind", fmt.Sprintf("%d", t.MaxPerKind), "caps a backlog becoming a firehose"),
		setting("uncommitted", fmt.Sprintf("%d lines", t.UncommittedLines), "diff worth a checkpoint"),
		setting("focus break", fmt.Sprintf("%dm", m.cfg.Focus.BreakAfterMinutes), "unbroken work before a nudge"),
		setting("daily cap", fmt.Sprintf("%d", m.cfg.MaxNudgesPerDay), "hard ceiling on interruptions"),
		setting("min gap", fmt.Sprintf("%dm", m.cfg.MinMinutesBetweenNudge), "between any two nudges"),
	}))
	blank()

	var routines []string
	for _, r := range m.cfg.Routines {
		extra := ""
		if len(r.Days) > 0 {
			extra = ui.Grey("  " + dayNames(r.Days))
		}
		if r.RequireActive {
			extra += ui.Grey("  (only when active)")
		}
		routines = append(routines, ui.Pad(ui.Grey(r.At), 18)+r.Name+extra)
	}
	if len(routines) == 0 {
		routines = []string{ui.Grey("none declared")}
	}
	add(pane("routines", routines))
	blank()

	muted := ui.Grey("nothing muted")
	if len(m.cfg.MutedKinds) > 0 {
		var names []string
		for _, k := range m.cfg.MutedKinds {
			names = append(names, string(k))
		}
		muted = ui.Amber(strings.Join(names, ", "))
	}
	add(pane("muted", []string{muted}))

	body := strings.Join(m.window(lines, m.cfgScroll, -1), "\n")
	return m.chrome("", "", body, "↑↓ scroll   e open config in editor   tab switch   q quit")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// configuredOrNot reports whether a model is set without naming it.
func configuredOrNot(model string) string {
	if model == "" {
		return "not set"
	}
	return "configured"
}

// hostOnly keeps the endpoint recognisable without exposing a path that may
// encode a tenant or deployment name.
func hostOnly(raw string) string {
	if raw == "" {
		return "—"
	}
	rest := raw
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func valueOr(v, fallback string) string {
	if v == "" {
		return ui.Grey(fallback)
	}
	return v
}

func credentialState(phrasingErr string) string {
	if strings.Contains(phrasingErr, "API key") {
		return ui.Grey("not set — export PULSE_API_KEY")
	}
	return ui.Green("resolved")
}

func backgroundState() string {
	if pulse.AgentInstalled() {
		return ui.Green("● ") + "launchd agent installed"
	}
	return ui.Grey("○ not installed — run `pulse daemon`")
}

func dayNames(days []int) string {
	names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	var out []string
	for _, d := range days {
		if d >= 0 && d < 7 {
			out = append(out, names[d])
		}
	}
	return strings.Join(out, " ")
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

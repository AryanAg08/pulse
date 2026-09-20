package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"pulse/internal/pulse"
	"pulse/internal/pulse/ui"
)

func init() { ui.SetEnabled(false) } // assert on text, not escape codes

func pr(repo string, num int, mut func(*pulse.PRSignal)) pulse.PRSignal {
	p := pulse.PRSignal{
		Repo: repo, Number: num, Title: "a change", URL: "https://x/" + repo,
		Additions: 120, Deletions: 8, ChangedFiles: 3,
		BaseRef: "main", HeadRef: "feat/x", Checks: "passing",
		Author: "someone", StaleHours: 30,
	}
	if mut != nil {
		mut(&p)
	}
	return p
}

func fixture() Model {
	s := pulse.Signals{
		Repos: []pulse.RepoSignal{
			{Name: "quiet", Remote: "org/quiet", Branch: "main"},
			{Name: "api", Remote: "org/api", Branch: "main", DirtyLines: 40},
		},
		PRs: []pulse.PRSignal{
			pr("org/api", 1, nil),
			pr("org/api", 2, func(p *pulse.PRSignal) { p.Checks = "failing" }),
			// A PR whose repository is not cloned locally.
			pr("org/remote-only", 7, nil),
		},
		ReviewRequests: []pulse.PRSignal{
			pr("org/api", 9, func(p *pulse.PRSignal) { p.Author = "teammate" }),
		},
	}
	m := New(pulse.DefaultConfig(), s)
	m.width, m.height = 120, 30
	return m
}

func key(m Model, k string) Model {
	var msg tea.KeyMsg
	switch k {
	case "enter", "esc", "up", "down", "left", "right":
		msg = tea.KeyMsg{Type: map[string]tea.KeyType{
			"enter": tea.KeyEnter, "esc": tea.KeyEsc, "up": tea.KeyUp,
			"down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		}[k]}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestReposSortBusiestFirst(t *testing.T) {
	m := fixture()
	if m.repos[0].Name != "api" {
		t.Fatalf("the repo with PRs should lead, got %q", m.repos[0].Name)
	}
}

func TestRepoWithNoLocalCloneStillListed(t *testing.T) {
	// It is part of your surface whether or not it is on this disk.
	m := fixture()
	var found bool
	for _, r := range m.repos {
		if strings.Contains(r.Name, "remote-only") {
			found = true
		}
	}
	if !found {
		t.Fatal("a PR with no local clone must still appear as a repository")
	}
}

func TestRepoListRendersCountsAndDiffIsNotShownYet(t *testing.T) {
	v := fixture().View()
	for _, want := range []string{"repositories", "api", "branch", "PRs", "review", "dirty"} {
		if !strings.Contains(v, want) {
			t.Errorf("repo list missing %q\n%s", want, v)
		}
	}
}

func TestDrillIntoPRsShowsDiffstat(t *testing.T) {
	m := key(fixture(), "enter") // into "api"
	if m.view != viewPRs {
		t.Fatalf("enter should open the PR list, got view %d", m.view)
	}
	v := m.View()
	for _, want := range []string{"+120", "-8", "#", "diff", "files"} {
		if !strings.Contains(v, want) {
			t.Errorf("PR list missing %q\n%s", want, v)
		}
	}
}

func TestReviewsOwedAreLabelledSeparately(t *testing.T) {
	// Mixing your PRs with PRs awaiting your review without a label would
	// conflate two different obligations.
	v := key(fixture(), "enter").View()
	if !strings.Contains(v, "review") {
		t.Fatalf("a review owed should be marked in the list\n%s", v)
	}
}

func TestEnterOnEmptyRepoExplainsInsteadOfOpeningBlankList(t *testing.T) {
	m := fixture()
	m.repoIdx = len(m.repos) - 1 // the quiet repo, sorted last
	m = key(m, "enter")
	if m.view != viewRepos {
		t.Fatal("should stay on the repo list")
	}
	if !strings.Contains(m.status, "no open pull requests") {
		t.Fatalf("want an explanation, got status %q", m.status)
	}
}

func TestBackNavigationReturnsToRepos(t *testing.T) {
	m := key(key(fixture(), "enter"), "esc")
	if m.view != viewRepos {
		t.Fatalf("esc should go back, got view %d", m.view)
	}
}

func TestDetailShowsMetadataAndLoadsBody(t *testing.T) {
	m := key(key(fixture(), "enter"), "enter")
	if m.view != viewDetail {
		t.Fatalf("expected the detail view, got %d", m.view)
	}
	if !m.loading {
		t.Error("opening a PR should start the description fetch")
	}
	v := m.View()
	for _, want := range []string{"author", "branch", "feat/x", "main", "diff", "+120", "loading"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail missing %q\n%s", want, v)
		}
	}

	// Arrival of the fetched description.
	next, _ := m.Update(detailMsg{detail: pulse.PRDetail{
		Body:   "Adds the thing.\n\n- one\n- two",
		Files:  []pulse.PRFile{{Path: "internal/a.go", Additions: 100, Deletions: 8}},
		Loaded: true,
	}})
	m = next.(Model)
	if m.loading {
		t.Error("loading should clear once the detail arrives")
	}
	v = m.View()
	for _, want := range []string{"Adds the thing.", "description", "files", "internal/a.go"} {
		if !strings.Contains(v, want) {
			t.Errorf("loaded detail missing %q\n%s", want, v)
		}
	}
}

func TestDetailFetchErrorIsShownNotSwallowed(t *testing.T) {
	m := key(key(fixture(), "enter"), "enter")
	next, _ := m.Update(detailMsg{err: errString("rate limited")})
	m = next.(Model)
	if m.view != viewDetail {
		t.Fatal("an error should not bounce the user out of the view")
	}
	if !strings.Contains(m.View(), "rate limited") {
		t.Fatalf("the error must be visible\n%s", m.View())
	}
}

func TestEmptyDescriptionSaysSo(t *testing.T) {
	m := key(key(fixture(), "enter"), "enter")
	next, _ := m.Update(detailMsg{detail: pulse.PRDetail{Loaded: true}})
	if !strings.Contains(next.(Model).View(), "no description") {
		t.Fatal("an empty body should be stated, not left blank")
	}
}

func TestCursorStaysInBoundsAtListEdges(t *testing.T) {
	m := fixture()
	for range 20 {
		m = key(m, "up")
	}
	if m.repoIdx != 0 {
		t.Fatalf("cursor should stop at the top, got %d", m.repoIdx)
	}
	for range 50 {
		m = key(m, "down")
	}
	if m.repoIdx != len(m.repos)-1 {
		t.Fatalf("cursor should stop at the bottom, got %d", m.repoIdx)
	}
}

func TestScrollKeepsCursorVisible(t *testing.T) {
	if got := scrollFor(0, 0, 5); got != 0 {
		t.Errorf("no scroll needed at the top, got %d", got)
	}
	if got := scrollFor(7, 0, 5); got != 3 {
		t.Errorf("cursor below the window should scroll to show it, got %d", got)
	}
	if got := scrollFor(1, 4, 5); got != 1 {
		t.Errorf("cursor above the window should scroll up, got %d", got)
	}
}

func TestWrapPreservesBlankLines(t *testing.T) {
	got := wrap("para one\n\npara two", 40)
	if len(got) != 3 || got[1] != "" {
		t.Fatalf("paragraph breaks must survive wrapping: %q", got)
	}
}

func TestWrapBreaksLongLinesAtWidth(t *testing.T) {
	for _, line := range wrap(strings.Repeat("word ", 60), 30) {
		if len(line) > 30 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
}

func TestQuitSetsQuitting(t *testing.T) {
	if !key(fixture(), "q").quitting {
		t.Fatal("q should quit")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// --- tabs ---

func TestTabCyclesAndWraps(t *testing.T) {
	m := fixture()
	if m.tab != tabRepos {
		t.Fatal("should open on repositories")
	}
	m = key(m, "tab")
	if m.tab != tabMetrics {
		t.Fatalf("tab should advance to metrics, got %d", m.tab)
	}
	m = key(key(m, "tab"), "tab")
	if m.tab != tabRepos {
		t.Fatalf("tab should wrap back to repositories, got %d", m.tab)
	}
}

func TestNumberKeysJumpToTab(t *testing.T) {
	m := key(fixture(), "3")
	if m.tab != tabConfig {
		t.Fatalf("3 should select config, got %d", m.tab)
	}
	if m = key(m, "1"); m.tab != tabRepos {
		t.Fatalf("1 should select repositories, got %d", m.tab)
	}
}

func TestTabSwitchPreservesDrillDownPosition(t *testing.T) {
	// Leaving and returning should not silently reset where you were.
	m := key(fixture(), "enter") // into the PR list
	m = key(key(m, "2"), "1")    // to metrics and back
	if m.view != viewPRs {
		t.Fatalf("should return to the PR list, got view %d", m.view)
	}
}

func TestMetricsTabRendersTheExperiment(t *testing.T) {
	m := key(fixture(), "2")
	v := m.View()
	for _, want := range []string{"14-day experiment", "engaged", "noise budget", "quiet hours", "history"} {
		if !strings.Contains(v, want) {
			t.Errorf("metrics tab missing %q\n%s", want, v)
		}
	}
}

func TestConfigTabShowsResolvedSettingsAndNeverTheKey(t *testing.T) {
	m := fixture()
	m.cfg.Phrasing.APIKey = "sk-secret-value"
	m.tab = tabConfig
	v := m.View()
	for _, want := range []string{"source", "phrasing", "discovery", "thresholds", "routines", "muted"} {
		if !strings.Contains(v, want) {
			t.Errorf("config tab missing %q\n%s", want, v)
		}
	}
	if strings.Contains(v, "sk-secret-value") {
		t.Fatal("the config tab must never render the credential itself")
	}
}

func TestConfigTabShowsThresholdValues(t *testing.T) {
	m := fixture()
	m.cfg.Thresholds.AbandonedAfterDays = 14
	m.tab = tabConfig
	if !strings.Contains(m.View(), "14d") {
		t.Fatal("thresholds should show their actual values")
	}
}

func TestTabBarMarksActiveWithoutColour(t *testing.T) {
	// Colour is off in these tests, so the underline is the only signal.
	m := key(fixture(), "2")
	if !strings.Contains(m.tabBar(), "─") {
		t.Fatal("the active tab must be marked by more than colour")
	}
}

// --- history hover and attribution ---

func withHistory(m Model, ns ...pulse.Nudge) Model {
	m.nudges = ns
	m.metrics = pulse.ComputeMetrics(pulse.State{InstalledAt: 0}, ns, timeNow())
	m.tab = tabMetrics
	return m
}

func nudgeAt(text, phrasedBy string, response *string) pulse.Nudge {
	return pulse.Nudge{
		ID: "abcdef0123", Kind: pulse.KindStalePR, Text: text,
		Action: "https://github.com/o/r/pull/1",
		SentAt: timeNow().UnixMilli(), PhrasedBy: phrasedBy, Response: response,
	}
}

func TestHistoryCursorMovesAndStaysInBounds(t *testing.T) {
	m := withHistory(fixture(),
		nudgeAt("first", "template", nil),
		nudgeAt("second", "llm", nil),
	)
	// Newest first, so the cursor starts on "second".
	if n, _ := m.hoveredNudge(); n.Text != "second" {
		t.Fatalf("history is newest-first, got %q", n.Text)
	}
	m = key(m, "down")
	if n, _ := m.hoveredNudge(); n.Text != "first" {
		t.Fatalf("down should move to the older nudge, got %q", n.Text)
	}
	for range 10 {
		m = key(m, "down")
	}
	if m.logIdx != 1 {
		t.Fatalf("cursor must stop at the last row, got %d", m.logIdx)
	}
	for range 10 {
		m = key(m, "up")
	}
	if m.logIdx != 0 {
		t.Fatalf("cursor must stop at the first row, got %d", m.logIdx)
	}
}

func TestHoverShowsFullTextAndResponseTiming(t *testing.T) {
	ack := "ack"
	n := nudgeAt("a nudge whose text is long enough to be truncated in the list itself", "template", &ack)
	at := n.SentAt + 90_000
	n.RespondedAt = &at

	v := withHistory(fixture(), n).View()
	for _, want := range []string{"selected", "a nudge whose text is long enough", "stale_pr", "after 1m30s", "opens"} {
		if !strings.Contains(v, want) {
			t.Errorf("hover detail missing %q\n%s", want, v)
		}
	}
}

func TestUnansweredNudgeSaysItCountsAsIgnored(t *testing.T) {
	v := withHistory(fixture(), nudgeAt("x", "template", nil)).View()
	if !strings.Contains(v, "counts as ignored") {
		t.Fatal("silence is the failure signal and should be named as such")
	}
}

func TestAIPhrasedNudgeIsTaggedPulseAI(t *testing.T) {
	v := withHistory(fixture(), nudgeAt("worded by a model", "llm", nil)).View()
	if !strings.Contains(v, "pulse-ai") {
		t.Fatalf("an AI-worded nudge must carry the pulse-ai tag\n%s", v)
	}
}

func TestTemplateNudgeIsNotTaggedPulseAI(t *testing.T) {
	v := withHistory(fixture(), nudgeAt("fixed wording", "template", nil)).View()
	if strings.Contains(v, "pulse-ai") {
		t.Fatal("a template nudge must not claim AI attribution")
	}
}

func TestNoViewEverRevealsTheModelName(t *testing.T) {
	// The dashboard is the sort of thing that gets screen-shared. Which model
	// is behind pulse-ai is an implementation detail.
	const secret = "openai/gpt-5.6-luna"
	m := fixture()
	m.cfg.Phrasing.Provider = "api"
	m.cfg.Phrasing.ModelName = secret
	m.cfg.Phrasing.APIURL = "https://openrouter.ai/api/v1/deployments/acme-tenant"
	m = withHistory(m, nudgeAt("x", "llm", nil))

	for _, tb := range []tab{tabRepos, tabMetrics, tabConfig} {
		m.tab = tb
		v := m.View()
		if strings.Contains(v, secret) {
			t.Errorf("tab %d leaked the model name\n%s", tb, v)
		}
		if strings.Contains(v, "acme-tenant") {
			t.Errorf("tab %d leaked the endpoint path\n%s", tb, v)
		}
	}
}

func TestConfigTabStillConfirmsAModelIsSet(t *testing.T) {
	// Hiding the name must not make it impossible to tell configured from not.
	m := fixture()
	m.cfg.Phrasing.ModelName = "some/model"
	m.tab = tabConfig
	if !strings.Contains(m.View(), "configured") {
		t.Fatal("the config tab should confirm a model is set without naming it")
	}
}

func TestOpenOnNudgeWithNoActionExplains(t *testing.T) {
	n := nudgeAt("x", "template", nil)
	n.Action = ""
	m := key(withHistory(fixture(), n), "o")
	if !strings.Contains(m.status, "nothing to open") {
		t.Fatalf("want an explanation, got %q", m.status)
	}
}

func TestEmptyHistoryDoesNotPanic(t *testing.T) {
	m := withHistory(fixture())
	if !strings.Contains(m.View(), "nothing sent yet") {
		t.Fatal("an empty history should say so")
	}
	key(m, "down") // must not panic
}

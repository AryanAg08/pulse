// Package tui is the interactive browser: repositories, their pull requests,
// and the detail of one pull request.
//
// It is read-only by design. Pulse's job is to decide what deserves an
// interruption; this is for the moment you want the whole picture instead.
package tui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pulse/internal/pulse"
)

// notCloned marks a repository that has open PRs but no local working copy.
const notCloned = "—"

type view int

const (
	viewRepos view = iota
	viewPRs
	viewDetail
)

// tab is a top-level section. Tabs are peers; the repo tab has its own
// drill-down levels underneath, tracked separately by view.
type tab int

const (
	tabRepos tab = iota
	tabMetrics
	tabConfig
)

var tabNames = []string{"repositories", "metrics", "config"}

// repoRow is one repository, with its pull requests already attached so the
// PR view needs no further lookup.
type repoRow struct {
	Name    string
	Remote  string
	Branch  string
	Dirty   int
	Clones  int
	PRs     []pulse.PRSignal
	Reviews []pulse.PRSignal
}

// Model is the Bubble Tea model for the browser.
type Model struct {
	cfg pulse.Config

	repos    []repoRow
	repoIdx  int
	prIdx    int
	scroll   int // first visible row in the current list
	detailY  int // scroll offset within the detail body
	view     view
	width    int
	height   int
	loading  bool
	err      string
	status   string
	detail   pulse.PRDetail
	quitting bool

	// Tab state.
	tab       tab
	metrics   pulse.Metrics
	nudges    []pulse.Nudge
	logScroll int
	cfgScroll int

	// Resolved once at open, so the config tab reports what is actually in
	// effect rather than what the file says.
	configPath   string
	phrasingName string
	phrasingErr  string
	scanDepth    int
	repoTotal    int
	prTotal      int
	reviewTotal  int
	quietNow     bool
}

// New builds the model from an already-collected snapshot, so opening the
// browser costs no extra git or API work.
func New(cfg pulse.Config, s pulse.Signals) Model {
	byRemote := map[string][]pulse.PRSignal{}
	reviewsByRemote := map[string][]pulse.PRSignal{}
	for _, pr := range s.PRs {
		byRemote[strings.ToLower(pr.Repo)] = append(byRemote[strings.ToLower(pr.Repo)], pr)
	}
	for _, pr := range s.ReviewRequests {
		reviewsByRemote[strings.ToLower(pr.Repo)] = append(reviewsByRemote[strings.ToLower(pr.Repo)], pr)
	}

	cloneCount := map[string]int{}
	for _, r := range s.Repos {
		if r.Remote != "" {
			cloneCount[strings.ToLower(r.Remote)]++
		}
	}

	rows := make([]repoRow, 0, len(s.Repos))
	seenOrphan := map[string]bool{}
	for _, r := range s.Repos {
		key := strings.ToLower(r.Remote)
		rows = append(rows, repoRow{
			Name: r.Name, Remote: r.Remote, Branch: r.Branch,
			Dirty: r.DirtyLines, Clones: cloneCount[key],
			PRs: byRemote[key], Reviews: reviewsByRemote[key],
		})
		seenOrphan[key] = true
	}

	// Repositories with open PRs but no local clone still belong in the list —
	// they are part of your surface whether or not they are on this disk.
	for key, prs := range byRemote {
		if seenOrphan[key] {
			continue
		}
		seenOrphan[key] = true
		full := prs[0].Repo
		rows = append(rows, repoRow{
			// Display the bare repository name; the owner adds nothing here
			// and would render as a redundant "org/x →x".
			Name: full[strings.LastIndex(full, "/")+1:], Remote: full,
			Branch: notCloned, PRs: prs, Reviews: reviewsByRemote[key],
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if len(a.PRs) != len(b.PRs) {
			return len(a.PRs) > len(b.PRs)
		}
		if len(a.Reviews) != len(b.Reviews) {
			return len(a.Reviews) > len(b.Reviews)
		}
		if a.Dirty != b.Dirty {
			return a.Dirty > b.Dirty
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})

	m := Model{cfg: cfg, repos: rows, view: viewRepos, width: 100, height: 30}

	m.nudges = pulse.ReadNudges()
	m.metrics = pulse.ComputeMetrics(pulse.LoadState(), m.nudges, timeNow())
	m.configPath = pulse.ConfigPath()
	m.scanDepth = cfg.RepoScanDepth
	m.repoTotal, m.prTotal, m.reviewTotal = len(s.Repos), len(s.PRs), len(s.ReviewRequests)
	m.quietNow = pulse.InQuietHours(cfg, timeNow())
	if name, err := pulse.PhraseProvider(cfg); err != nil {
		m.phrasingErr = err.Error()
	} else {
		m.phrasingName = name
	}
	return m
}

func (m Model) Init() tea.Cmd { return nil }

// allPRs is the current repo's own PRs followed by reviews owed, which is the
// order the PR list renders in.
func (m Model) allPRs() []pulse.PRSignal {
	if m.repoIdx >= len(m.repos) {
		return nil
	}
	r := m.repos[m.repoIdx]
	return append(append([]pulse.PRSignal{}, r.PRs...), r.Reviews...)
}

func (m Model) currentPR() (pulse.PRSignal, bool) {
	prs := m.allPRs()
	if m.prIdx < 0 || m.prIdx >= len(prs) {
		return pulse.PRSignal{}, false
	}
	return prs[m.prIdx], true
}

type detailMsg struct {
	detail pulse.PRDetail
	err    error
}

func fetchDetail(repo string, number int) tea.Cmd {
	return func() tea.Msg {
		d, err := pulse.FetchPRDetail(repo, number)
		return detailMsg{detail: d, err: err}
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

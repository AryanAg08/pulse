package tui

import (
	"os"
	"testing"
	"time"

	"pulse/internal/pulse"
)

// TestBuildsFromLiveSignals is a smoke test against the real machine, skipped
// unless PULSE_LIVE=1. It catches integration breakage that fixtures cannot.
func TestBuildsFromLiveSignals(t *testing.T) {
	if os.Getenv("PULSE_LIVE") == "" {
		t.Skip("set PULSE_LIVE=1 to run against real git and GitHub data")
	}
	cfg, err := pulse.LoadConfig()
	if err != nil {
		t.Skipf("no config: %v", err)
	}
	state := pulse.LoadState()
	s := pulse.CollectSignals(cfg, &state, time.Now())
	pulse.CanonicalizeRepos(s.Repos)

	m := New(cfg, s)
	m.width, m.height = 120, 40
	if len(m.repos) == 0 {
		t.Fatal("expected at least one repository")
	}
	if v := m.View(); len(v) == 0 {
		t.Fatal("empty render")
	}
	t.Logf("%d repositories, top = %q with %d PRs", len(m.repos), m.repos[0].Name, len(m.repos[0].PRs))
}

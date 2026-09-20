package tui

import (
	"strings"
	"testing"

	"pulse/internal/pulse"
	"pulse/internal/pulse/ui"
)

// TestEveryViewFitsTheTerminal guards the failure this caught: a view one line
// too tall scrolls the alt-screen, and the first thing pushed off the top is
// the tab bar — so the other tabs become invisible and unreachable-looking.
func TestEveryViewFitsTheTerminal(t *testing.T) {
	ui.SetEnabled(false)

	ack := "ack"
	many := make([]pulse.Nudge, 30)
	for i := range many {
		many[i] = pulse.Nudge{
			ID: "abcdef01", Kind: pulse.KindStalePR, Text: "a nudge",
			SentAt: timeNow().UnixMilli(), PhrasedBy: "llm", Response: &ack,
		}
	}

	for _, height := range []int{14, 20, 24, 30, 40, 60} {
		for _, tb := range []tab{tabRepos, tabMetrics, tabConfig} {
			for _, vw := range []view{viewRepos, viewPRs, viewDetail} {
				m := withHistory(fixture(), many...)
				m.tab, m.view = tb, vw
				m.width, m.height = 100, height
				if vw != viewRepos {
					m.prIdx = 0
				}

				got := strings.Count(m.View(), "\n") + 1
				if got > height {
					t.Errorf("tab=%d view=%d height=%d rendered %d lines (%d too many)",
						tb, vw, height, got, got-height)
				}
			}
		}
	}
}

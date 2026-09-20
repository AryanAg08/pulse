package tui

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pulse/internal/pulse"
)

// Run opens the browser. It collects one snapshot up front rather than polling,
// because the underlying data changes on the order of minutes and a redraw loop
// would add cost for no benefit; `r` on a detail refetches that pull request.
func Run(cfg pulse.Config) error {
	// Bubble Tea needs a real terminal. Without this check it fails deep inside
	// the renderer with a message that says nothing about the actual problem.
	if fi, err := os.Stdout.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("`pulse browse` needs an interactive terminal; try `pulse metrics` when piping output")
	}

	state := pulse.LoadState()
	// A copy: browsing must not advance the focus streak the daemon tracks.
	local := state
	s := pulse.CollectSignals(cfg, &local, timeNow())

	// Follow renames and transfers so a stale origin still matches its PRs.
	pulse.CanonicalizeRepos(s.Repos)

	if !s.GithubOK {
		fmt.Println("github unavailable — showing local repositories only")
	}

	p := tea.NewProgram(New(cfg, s), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// timeNow is a variable so tests can pin the clock.
var timeNow = time.Now

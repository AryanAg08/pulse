package pulse

import "time"

const (
	// activeWindow: a file touched this recently means the user is at the keyboard.
	activeWindow = 6 * time.Minute
	// BreakGap: no activity for this long ends the streak. A bathroom break
	// shouldn't reset it, but an hour away should.
	BreakGap = 20 * time.Minute
)

type FocusResult struct {
	Minutes int
	Repo    string
}

// UpdateFocus derives a focus streak from edit recency across cycles. It mutates
// state, so the caller must persist it — the streak only exists in the record.
func UpdateFocus(repos []RepoSignal, state *State, now time.Time) FocusResult {
	nowMs := now.UnixMilli()

	// The most recently edited repo inside the active window, if any.
	var best *RepoSignal
	for i := range repos {
		r := &repos[i]
		if r.MsSinceLastEdit < 0 || r.MsSinceLastEdit >= activeWindow.Milliseconds() {
			continue
		}
		if best == nil || r.MsSinceLastEdit < best.MsSinceLastEdit {
			best = r
		}
	}

	if best == nil {
		idleFor := int64(1<<62 - 1)
		if state.LastActivityAt != nil {
			idleFor = nowMs - *state.LastActivityAt
		}
		if idleFor > BreakGap.Milliseconds() {
			state.FocusStartedAt = nil
			return FocusResult{}
		}
		// Inside the grace gap: hold the streak, but don't grow it.
		if state.FocusStartedAt != nil && state.LastActivityAt != nil {
			return FocusResult{Minutes: int((*state.LastActivityAt - *state.FocusStartedAt) / 60000)}
		}
		return FocusResult{}
	}

	gap := int64(1<<62 - 1)
	if state.LastActivityAt != nil {
		gap = nowMs - *state.LastActivityAt
	}
	if state.FocusStartedAt == nil || gap > BreakGap.Milliseconds() {
		start := nowMs
		state.FocusStartedAt = &start
	}
	state.LastActivityAt = &nowMs

	return FocusResult{
		Minutes: int((nowMs - *state.FocusStartedAt) / 60000),
		Repo:    best.Name,
	}
}

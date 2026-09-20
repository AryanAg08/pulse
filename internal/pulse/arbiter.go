package pulse

import (
	"fmt"
	"sort"
	"time"
)

// renudgeCooldown: once a fact is nudged, stay silent about it this long even if
// it persists.
const renudgeCooldown = 20 * time.Hour

type Suppressed struct {
	Candidate Candidate
	Reason    string
}

type Decision struct {
	Winner *Candidate
	// Reason explains why nothing was sent, or why this one won.
	Reason     string
	Suppressed []Suppressed
}

// Arbitrate picks at most one nudge per cycle. Every filter here exists to
// protect the one number that decides whether this product lives: whether the
// user still has notifications on two weeks from now.
//
// today is passed in rather than read from the store, keeping this a pure
// decision that can be tested without touching the filesystem.
//
// ignoreGates is preview mode only; a preview can never deliver.
func Arbitrate(cfg Config, candidates []Candidate, state State, today []Nudge, now time.Time, ignoreGates bool) Decision {
	var suppressed []Suppressed
	nowMs := now.UnixMilli()

	if !ignoreGates && state.MutedUntil != nil && nowMs < *state.MutedUntil {
		until := time.UnixMilli(*state.MutedUntil).Format("2 Jan 15:04")
		return Decision{Reason: "muted until " + until}
	}
	if !ignoreGates && InQuietHours(cfg, now) {
		return Decision{Reason: "quiet hours"}
	}
	if !ignoreGates && len(today) >= cfg.MaxNudgesPerDay {
		return Decision{Reason: fmt.Sprintf("daily cap reached (%d)", cfg.MaxNudgesPerDay)}
	}

	var lastSent int64
	for _, n := range today {
		if n.SentAt > lastSent {
			lastSent = n.SentAt
		}
	}
	if !ignoreGates && lastSent > 0 {
		mins := int((nowMs - lastSent) / 60000)
		if mins < cfg.MinMinutesBetweenNudge {
			return Decision{Reason: fmt.Sprintf("only %dm since last nudge (min %dm)",
				mins, cfg.MinMinutesBetweenNudge)}
		}
	}

	var eligible []Candidate
	for _, c := range candidates {
		if containsKind(cfg.MutedKinds, c.Kind) {
			suppressed = append(suppressed, Suppressed{c, fmt.Sprintf("kind %q is muted", c.Kind)})
			continue
		}
		if seen, ok := state.LastSeenKeys[c.DedupeKey]; ok && !ignoreGates {
			if nowMs-seen < renudgeCooldown.Milliseconds() {
				suppressed = append(suppressed, Suppressed{c, "already nudged about this"})
				continue
			}
		}
		eligible = append(eligible, c)
	}

	if len(eligible) == 0 {
		reason := "nothing worth saying"
		if len(candidates) > 0 {
			reason = "all candidates suppressed"
		}
		return Decision{Reason: reason, Suppressed: suppressed}
	}

	sort.SliceStable(eligible, func(i, j int) bool { return eligible[i].Priority > eligible[j].Priority })
	winner := eligible[0]
	for _, c := range eligible[1:] {
		suppressed = append(suppressed, Suppressed{c, "lower priority this cycle"})
	}

	return Decision{
		Winner:     &winner,
		Reason:     fmt.Sprintf("top candidate (%s, priority %d)", winner.Kind, winner.Priority),
		Suppressed: suppressed,
	}
}

func containsKind(xs []NudgeKind, v NudgeKind) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

package pulse

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type CycleResult struct {
	Signals    Signals
	Candidates []Candidate
	Decision   Decision
	Sent       *Nudge
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// A collision here only risks an ambiguous `pulse ack` prefix.
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b)
}

// RunCycle performs one observe -> decide -> speak cycle.
// dry does everything but deliver; preview (dry only) additionally ignores
// quiet hours and rate limits so the decision can be inspected on demand.
func RunCycle(cfg Config, dry, preview bool) (CycleResult, error) {
	now := time.Now()
	state := LoadState()

	signals := CollectSignals(cfg, &state, now)
	candidates := GenerateCandidates(cfg, signals, now)
	today := NudgesSince(StartOfToday(now))
	decision := Arbitrate(cfg, candidates, state, today, now, preview && dry)

	result := CycleResult{Signals: signals, Candidates: candidates, Decision: decision}

	if decision.Winner == nil || dry {
		// Focus tracking advances even on a silent cycle.
		return result, SaveState(state)
	}

	recent := today
	if len(recent) > 5 {
		recent = recent[len(recent)-5:]
	}
	phrased := Phrase(cfg, *decision.Winner, recent)

	nudge := Nudge{
		ID:        newID(),
		Kind:      decision.Winner.Kind,
		DedupeKey: decision.Winner.DedupeKey,
		Text:      phrased.Text,
		Action:    decision.Winner.Action,
		SentAt:    time.Now().UnixMilli(),
		PhrasedBy: phrased.By,
	}

	NotifyMacOS(nudge)
	if err := AppendNudge(nudge); err != nil {
		return result, err
	}
	state.LastSeenKeys[nudge.DedupeKey] = nudge.SentAt
	result.Sent = &nudge

	return result, SaveState(state)
}

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
	// Phrased is set only by a --phrase dry run: the wording the provider
	// returned, for a nudge that was deliberately not delivered.
	Phrased   string
	PhrasedBy string
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
	return RunCycleOpts(cfg, dry, preview, false)
}

// RunCycleOpts adds phraseDry: word the winner through the provider without
// delivering or logging it. This is the only way to see what the model would
// actually say without spending one of the day's nudges on finding out.
func RunCycleOpts(cfg Config, dry, preview, phraseDry bool) (CycleResult, error) {
	now := time.Now()
	state := LoadState()

	signals := CollectSignals(cfg, &state, now)
	candidates := GenerateCandidates(cfg, signals, now)
	today := NudgesSince(StartOfToday(now))
	decision := Arbitrate(cfg, candidates, state, today, now, preview && dry)

	result := CycleResult{Signals: signals, Candidates: candidates, Decision: decision}

	if decision.Winner == nil || dry {
		if decision.Winner != nil && phraseDry {
			recent := today
			if len(recent) > 5 {
				recent = recent[len(recent)-5:]
			}
			p := Phrase(cfg, *decision.Winner, recent)
			result.Phrased, result.PhrasedBy = p.Text, p.By
		}
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

package pulse

import (
	"math"
	"time"
)

const dayMs = int64(86_400_000)

type KindStat struct {
	Kind    NudgeKind
	Sent    int
	Ack     int
	Dismiss int
	Ignored int
}

type Metrics struct {
	DaysInstalled      int
	TotalNudges        int
	NudgesPerActiveDay float64
	// EngagedRate counts ack plus dismiss: a dismiss means the user read it and
	// made a decision, which is engagement. Only silence is failure.
	EngagedRate float64
	AckRate     float64
	IgnoredRate float64
	ByKind      []KindStat
	Verdict     string
}

func rate(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return math.Round(float64(n)/float64(d)*100) / 100
}

// ComputeMetrics is the falsification test, computed from the log: after two
// weeks, is the user still accepting interruptions? Everything else in Pulse is
// in service of this number.
func ComputeMetrics(state State, nudges []Nudge, now time.Time) Metrics {
	days := int((now.UnixMilli() - state.InstalledAt) / dayMs)
	if days < 1 {
		days = 1
	}

	activeDays := map[string]bool{}
	var ack, dismiss, ignored int
	kindOrder := []NudgeKind{}
	byKind := map[NudgeKind]*KindStat{}

	for _, n := range nudges {
		activeDays[time.UnixMilli(n.SentAt).Format("2006-01-02")] = true
		if _, ok := byKind[n.Kind]; !ok {
			byKind[n.Kind] = &KindStat{Kind: n.Kind}
			kindOrder = append(kindOrder, n.Kind)
		}
		k := byKind[n.Kind]
		k.Sent++
		switch {
		case n.Response == nil:
			ignored++
			k.Ignored++
		case *n.Response == "ack":
			ack++
			k.Ack++
		case *n.Response == "dismiss":
			dismiss++
			k.Dismiss++
		}
	}

	nActive := len(activeDays)
	if nActive == 0 {
		nActive = 1
	}
	total := len(nudges)
	muted := state.MutedUntil != nil && now.UnixMilli() < *state.MutedUntil

	var verdict string
	switch {
	case days < 14:
		verdict = "Day " + itoa(days) + " of 14. Too early to call."
	case muted:
		verdict = "Muted at day 14. That is the falsification — the nudges were not worth the interruption."
	case rate(ack+dismiss, total) >= 0.4:
		verdict = "Survived 14 days with real engagement. The wedge holds; build the next layer."
	default:
		verdict = "Not muted, but mostly ignored. Notifications are being tolerated, not valued — fix relevance before adding features."
	}

	stats := make([]KindStat, 0, len(kindOrder))
	for _, k := range kindOrder {
		stats = append(stats, *byKind[k])
	}

	return Metrics{
		DaysInstalled:      days,
		TotalNudges:        total,
		NudgesPerActiveDay: math.Round(float64(total)/float64(nActive)*10) / 10,
		EngagedRate:        rate(ack+dismiss, total),
		AckRate:            rate(ack, total),
		IgnoredRate:        rate(ignored, total),
		ByKind:             stats,
		Verdict:            verdict,
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

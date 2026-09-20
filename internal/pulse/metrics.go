package pulse

import (
	"math"
	"sort"
	"strings"
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

// RepoStat is one row of the repository breakdown: what is on disk joined to
// what GitHub knows about it.
type RepoStat struct {
	Name   string
	Remote string // "owner/name", empty when origin is absent or unrecognised
	Branch string
	// Local working-tree state.
	DirtyLines    int
	DirtyFiles    int
	AheadOfRemote int
	// GitHub state for this repo.
	OpenPRs     int
	RedPRs      int
	StalePRs    int
	ReviewsOwed int
	// Tracked is false when the repo has no usable origin, so its zeroed
	// GitHub columns read as "unknown" rather than "none".
	Tracked bool
}

// RepoBreakdown joins discovered repos to open PRs. Matching is on the parsed
// remote, never the directory name — a clone is frequently named differently
// from its repository.
//
// orphans counts PRs whose repository has no local clone, so the total is
// visibly accounted for rather than quietly dropped.
type RepoBreakdown struct {
	Repos   []RepoStat
	Orphans map[string]int
	// Totals across every open PR, cloned or not.
	TotalPRs     int
	TotalRed     int
	TotalReviews int
}

func BuildRepoBreakdown(s Signals, stalePRHours int) RepoBreakdown {
	byRemote := map[string]*RepoStat{}
	out := RepoBreakdown{Orphans: map[string]int{}}

	stats := make([]RepoStat, 0, len(s.Repos))
	for _, r := range s.Repos {
		stats = append(stats, RepoStat{
			Name:          r.Name,
			Remote:        r.Remote,
			Branch:        r.Branch,
			DirtyLines:    r.DirtyLines,
			DirtyFiles:    r.DirtyFiles,
			AheadOfRemote: r.AheadOfRemote,
			Tracked:       r.Remote != "",
		})
	}
	// Index after the slice is final, so pointers stay valid.
	for i := range stats {
		if stats[i].Remote != "" {
			byRemote[strings.ToLower(stats[i].Remote)] = &stats[i]
		}
	}

	attribute := func(pr PRSignal, review bool) {
		key := strings.ToLower(pr.Repo)
		stat, ok := byRemote[key]
		if !ok {
			out.Orphans[pr.Repo]++
			return
		}
		if review {
			stat.ReviewsOwed++
			return
		}
		stat.OpenPRs++
		if pr.Checks == "failing" {
			stat.RedPRs++
		}
		if pr.StaleHours >= float64(stalePRHours) {
			stat.StalePRs++
		}
	}

	for _, pr := range s.PRs {
		out.TotalPRs++
		if pr.Checks == "failing" {
			out.TotalRed++
		}
		attribute(pr, false)
	}
	for _, pr := range s.ReviewRequests {
		out.TotalReviews++
		attribute(pr, true)
	}

	// Busiest first, then dirtiest, then alphabetical — so the rows that need
	// attention are at the top rather than wherever the filesystem put them.
	sort.SliceStable(stats, func(i, j int) bool {
		a, b := stats[i], stats[j]
		if a.OpenPRs != b.OpenPRs {
			return a.OpenPRs > b.OpenPRs
		}
		if a.DirtyLines != b.DirtyLines {
			return a.DirtyLines > b.DirtyLines
		}
		return a.Name < b.Name
	})
	out.Repos = stats
	return out
}

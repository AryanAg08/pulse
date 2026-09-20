package pulse

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const hourMs = int64(3_600_000)

// routineGrace is how late a routine can still fire. Past this it's noise.
const routineGrace = 15 * time.Minute

func dayStamp(t time.Time) string {
	return fmt.Sprintf("%d-%d-%d", t.Year(), int(t.Month()), t.Day())
}

func shortRepo(nameWithOwner string) string {
	if i := strings.LastIndex(nameWithOwner, "/"); i >= 0 {
		return nameWithOwner[i+1:]
	}
	return nameWithOwner
}

// capKind keeps the top n candidates of one kind and drops the rest. A 14-PR
// backlog must not become 14 nudges — the arbiter would drip them out one a day
// for two weeks, which is precisely how an app gets muted.
func capKind(list []Candidate, n int) []Candidate {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Priority > list[j].Priority })
	if len(list) > n {
		return list[:n]
	}
	return list
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GenerateCandidates turns signals into every nudge that could be sent. It does
// not decide what gets sent — that is the arbiter's job, deliberately separated
// so the noise ceiling lives in exactly one place.
func GenerateCandidates(cfg Config, s Signals, now time.Time) []Candidate {
	var out []Candidate

	var mine []PRSignal
	for _, p := range s.PRs {
		if !p.IsDraft {
			mine = append(mine, p)
		}
	}

	abandonedHours := float64(cfg.Thresholds.AbandonedAfterDays * 24)
	// abandoned: dead, not stale. Silence beats a nudge about a PR from last year.
	abandoned := func(p PRSignal) bool { return p.StaleHours > abandonedHours }

	// --- Red CI on your own PR. Highest value: it blocks you and you may not know.
	var ciFailed []Candidate
	for _, pr := range mine {
		if pr.Checks != "failing" || abandoned(pr) {
			continue
		}
		ciFailed = append(ciFailed, Candidate{
			Kind:      KindCIFailed,
			DedupeKey: fmt.Sprintf("ci:%s#%d:%s", pr.Repo, pr.Number, pr.UpdatedAt),
			// A PR you touched an hour ago matters more than one from last week.
			Priority: 90 - minInt(20, int(pr.StaleHours/12)),
			Text:     fmt.Sprintf("CI is red on %s#%d — %q.", shortRepo(pr.Repo), pr.Number, pr.Title),
			Facts: map[string]any{
				"repo": pr.Repo, "number": pr.Number, "title": pr.Title,
				"staleHours": int(pr.StaleHours),
			},
			Action: pr.URL,
		})
	}
	out = append(out, capKind(ciFailed, cfg.Thresholds.MaxPerKind)...)

	// --- Someone is blocked on you. The nudge a teammate wishes they could send.
	var reviewDebt []Candidate
	for _, pr := range s.ReviewRequests {
		if pr.StaleHours < float64(cfg.Thresholds.ReviewDebtHours) || abandoned(pr) {
			continue
		}
		// Reviewing your own PR is not a debt to anyone.
		if cfg.User.GithubLogin != "" && pr.Author == cfg.User.GithubLogin {
			continue
		}
		reviewDebt = append(reviewDebt, Candidate{
			Kind:      KindReviewDebt,
			DedupeKey: fmt.Sprintf("review:%s#%d", pr.Repo, pr.Number),
			Priority:  80,
			Text: fmt.Sprintf("%s has been waiting %dh on your review of %s#%d.",
				pr.Author, int(pr.StaleHours), shortRepo(pr.Repo), pr.Number),
			Facts: map[string]any{
				"repo": pr.Repo, "number": pr.Number, "title": pr.Title,
				"author": pr.Author, "waitingHours": int(pr.StaleHours),
			},
			Action: pr.URL,
		})
	}
	out = append(out, capKind(reviewDebt, cfg.Thresholds.MaxPerKind)...)

	// --- Your own PR has gone quiet and still needs a human.
	var stalePRs []Candidate
	for _, pr := range mine {
		if pr.StaleHours < float64(cfg.Thresholds.StalePRHours) || abandoned(pr) {
			continue
		}
		if pr.ReviewDecision == "APPROVED" {
			continue
		}
		stalePRs = append(stalePRs, Candidate{
			Kind:      KindStalePR,
			DedupeKey: fmt.Sprintf("stale:%s#%d:%dd", pr.Repo, pr.Number, int(pr.StaleHours/24)),
			// Decays toward the abandonment cutoff: the longer it sits, the less
			// a ping will help, until the rule stops firing entirely.
			Priority: 70 - minInt(18, int(pr.StaleHours/24)*2),
			Text: fmt.Sprintf("%s#%d has sat for %dh with no review. Worth a ping.",
				shortRepo(pr.Repo), pr.Number, int(pr.StaleHours)),
			Facts: map[string]any{
				"repo": pr.Repo, "number": pr.Number, "title": pr.Title,
				"staleHours": int(pr.StaleHours), "reviewDecision": pr.ReviewDecision,
			},
			Action: pr.URL,
		})
	}
	out = append(out, capKind(stalePRs, cfg.Thresholds.MaxPerKind)...)

	// --- Routines the user declared. Time-bound, so they rank above ambient nudges.
	minsNow := now.Hour()*60 + now.Minute()
	for _, r := range cfg.Routines {
		if len(r.Days) > 0 && !containsInt(r.Days, int(now.Weekday())) {
			continue
		}
		due := parseHHMM(r.At)
		if due < 0 {
			continue
		}
		late := minsNow - due
		if late < 0 || late > int(routineGrace.Minutes()) {
			continue
		}
		if r.RequireActive && s.FocusMinutes == 0 {
			continue
		}
		text := fmt.Sprintf("%s, now.", r.Name)
		if r.Note != "" {
			text = fmt.Sprintf("%s — %s", r.Name, r.Note)
		}
		out = append(out, Candidate{
			Kind:      KindRoutine,
			DedupeKey: fmt.Sprintf("routine:%s:%s", r.Name, dayStamp(now)),
			Priority:  85,
			Text:      text,
			Facts: map[string]any{
				"routine": r.Name, "scheduledAt": r.At,
				"note": r.Note, "focusMinutes": s.FocusMinutes,
			},
		})
	}

	// --- Long unbroken focus. The posture nudge, but earned by evidence.
	if s.FocusMinutes >= cfg.Focus.BreakAfterMinutes {
		// Bucket by the break interval so one long session nudges once per interval.
		bucket := s.FocusMinutes / cfg.Focus.BreakAfterMinutes
		where := s.FocusRepo
		if where == "" {
			where = "one repo"
		}
		out = append(out, Candidate{
			Kind:      KindLongFocus,
			DedupeKey: fmt.Sprintf("focus:%s:%d", dayStamp(now), bucket),
			Priority:  60,
			Text: fmt.Sprintf("%d minutes straight in %s. Stand up, look at something far away.",
				s.FocusMinutes, where),
			Facts: map[string]any{"focusMinutes": s.FocusMinutes, "repo": s.FocusRepo},
		})
	}

	// --- A big diff with no commit behind it is work you could lose.
	for _, repo := range s.Repos {
		stale := repo.MsSinceLastCommit < 0 || repo.MsSinceLastCommit > 3*hourMs
		if repo.DirtyLines < cfg.Thresholds.UncommittedLines || !stale {
			continue
		}
		var hoursSince any
		if repo.MsSinceLastCommit >= 0 {
			hoursSince = int(repo.MsSinceLastCommit / hourMs)
		}
		out = append(out, Candidate{
			Kind:      KindUncommittedWork,
			DedupeKey: fmt.Sprintf("dirty:%s:%s", repo.Name, dayStamp(now)),
			Priority:  50,
			Text: fmt.Sprintf("%d uncommitted lines across %d files in %s (%s). Checkpoint it.",
				repo.DirtyLines, repo.DirtyFiles, repo.Name, repo.Branch),
			Facts: map[string]any{
				"repo": repo.Name, "branch": repo.Branch,
				"dirtyLines": repo.DirtyLines, "dirtyFiles": repo.DirtyFiles,
				"hoursSinceCommit": hoursSince,
			},
		})
	}

	return out
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

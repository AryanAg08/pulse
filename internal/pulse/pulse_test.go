package pulse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testCfg() Config {
	c := DefaultConfig()
	c.User = UserConfig{GithubLogin: "me", Timezone: "Asia/Kolkata"}
	return c
}

func testPR(mut func(*PRSignal)) PRSignal {
	p := PRSignal{
		Repo: "org/app", Number: 1, Title: "t", URL: "u",
		StaleHours: 48, Checks: "none", Author: "someone",
	}
	if mut != nil {
		mut(&p)
	}
	return p
}

func testRepo(mut func(*RepoSignal)) RepoSignal {
	r := RepoSignal{
		Name: "app", Path: "/app", Branch: "main",
		MsSinceLastCommit: 5 * hourMs, MsSinceLastEdit: 1000,
		DirtyFiles: 3, DirtyLines: 10,
	}
	if mut != nil {
		mut(&r)
	}
	return r
}

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func countKind(cs []Candidate, k NudgeKind) int {
	n := 0
	for _, c := range cs {
		if c.Kind == k {
			n++
		}
	}
	return n
}

func TestAbandonedPRProducesNoNudge(t *testing.T) {
	dead := testPR(func(p *PRSignal) { p.StaleHours = 24 * 400; p.Checks = "failing" })
	got := GenerateCandidates(testCfg(), Signals{PRs: []PRSignal{dead}}, at("2026-09-21T14:00:00"))
	if len(got) != 0 {
		t.Fatalf("a year-old PR is dead, not stale: got %d candidates", len(got))
	}
}

func TestStalePRPriorityDecaysWithAge(t *testing.T) {
	fresh := GenerateCandidates(testCfg(),
		Signals{PRs: []PRSignal{testPR(func(p *PRSignal) { p.StaleHours = 25 })}}, at("2026-09-21T14:00:00"))
	old := GenerateCandidates(testCfg(),
		Signals{PRs: []PRSignal{testPR(func(p *PRSignal) { p.StaleHours = 24 * 10 })}}, at("2026-09-21T14:00:00"))
	if fresh[0].Priority <= old[0].Priority {
		t.Fatalf("a ping helps less the longer it has sat: fresh=%d old=%d", fresh[0].Priority, old[0].Priority)
	}
}

func TestBacklogIsCappedPerKind(t *testing.T) {
	var many []PRSignal
	for i := range 12 {
		many = append(many, testPR(func(p *PRSignal) { p.Number = i; p.StaleHours = float64(30 + i) }))
	}
	got := GenerateCandidates(testCfg(), Signals{PRs: many}, at("2026-09-21T14:00:00"))
	if len(got) != DefaultConfig().Thresholds.MaxPerKind {
		t.Fatalf("want %d candidates, got %d", DefaultConfig().Thresholds.MaxPerKind, len(got))
	}
}

func TestOwnPRIsNotReviewDebt(t *testing.T) {
	mine := testPR(func(p *PRSignal) { p.Author = "me"; p.StaleHours = 20 })
	got := GenerateCandidates(testCfg(), Signals{ReviewRequests: []PRSignal{mine}}, at("2026-09-21T14:00:00"))
	if n := countKind(got, KindReviewDebt); n != 0 {
		t.Fatalf("reviewing your own PR is not a debt: got %d", n)
	}
}

func TestTeammateWaitingIsReviewDebt(t *testing.T) {
	theirs := testPR(func(p *PRSignal) { p.Author = "dev2"; p.StaleHours = 20 })
	got := GenerateCandidates(testCfg(), Signals{ReviewRequests: []PRSignal{theirs}}, at("2026-09-21T14:00:00"))
	if n := countKind(got, KindReviewDebt); n != 1 {
		t.Fatalf("want 1 review_debt, got %d", n)
	}
}

func TestRoutineFiresInsideGraceWindowOnly(t *testing.T) {
	c := testCfg()
	c.Routines = []Routine{{Name: "Gym", At: "19:00"}}
	if got := GenerateCandidates(c, Signals{}, at("2026-09-21T19:05:00")); len(got) != 1 {
		t.Fatalf("on time should fire: got %d", len(got))
	}
	if got := GenerateCandidates(c, Signals{}, at("2026-09-21T19:45:00")); len(got) != 0 {
		t.Fatalf("a 45-min-late reminder is noise: got %d", len(got))
	}
}

func TestRoutineRespectsWeekdays(t *testing.T) {
	c := testCfg()
	c.Routines = []Routine{{Name: "Standup", At: "10:00", Days: []int{1}}}
	sunday := at("2026-09-20T10:05:00") // 2026-09-20 is a Sunday
	if sunday.Weekday() != time.Sunday {
		t.Fatalf("fixture drift: expected a Sunday, got %v", sunday.Weekday())
	}
	if got := GenerateCandidates(c, Signals{}, sunday); len(got) != 0 {
		t.Fatalf("Monday-only routine fired on Sunday: got %d", len(got))
	}
}

func TestLongFocusNudgesOncePerInterval(t *testing.T) {
	c := testCfg()
	first := GenerateCandidates(c, Signals{FocusMinutes: 95, FocusRepo: "app"}, at("2026-09-21T14:00:00"))
	same := GenerateCandidates(c, Signals{FocusMinutes: 120, FocusRepo: "app"}, at("2026-09-21T14:00:00"))
	next := GenerateCandidates(c, Signals{FocusMinutes: 200, FocusRepo: "app"}, at("2026-09-21T14:00:00"))

	if got := GenerateCandidates(c, Signals{FocusMinutes: 80}, at("2026-09-21T14:00:00")); len(got) != 0 {
		t.Fatalf("below the threshold, silence: got %d", len(got))
	}
	if first[0].DedupeKey != same[0].DedupeKey {
		t.Fatal("same interval should be the same fact")
	}
	if first[0].DedupeKey == next[0].DedupeKey {
		t.Fatal("next interval should be a new fact")
	}
}

func TestQuietHoursHandleOvernightWrap(t *testing.T) {
	c := testCfg()
	c.Quiet = QuietHours{Start: "22:30", End: "08:00"}
	for _, tc := range []struct {
		when string
		want bool
	}{
		{"2026-09-21T23:30:00", true},
		{"2026-09-21T03:00:00", true},
		{"2026-09-21T14:00:00", false},
	} {
		if got := InQuietHours(c, at(tc.when)); got != tc.want {
			t.Errorf("InQuietHours(%s) = %v, want %v", tc.when, got, tc.want)
		}
	}
}

func TestArbiterSendsOnlyHighestPriority(t *testing.T) {
	s := Signals{
		PRs:            []PRSignal{testPR(func(p *PRSignal) { p.Number = 1; p.StaleHours = 30; p.Checks = "failing" })},
		ReviewRequests: []PRSignal{testPR(func(p *PRSignal) { p.Number = 2; p.Author = "dev2"; p.StaleHours = 20 })},
	}
	cands := GenerateCandidates(testCfg(), s, at("2026-09-21T14:00:00"))
	if len(cands) < 2 {
		t.Fatalf("fixture should produce multiple candidates, got %d", len(cands))
	}
	d := Arbitrate(testCfg(), cands, LoadStateEmpty(), nil, at("2026-09-21T14:00:00"), false)
	if d.Winner == nil || d.Winner.Kind != KindCIFailed {
		t.Fatalf("a red build outranks a review queue, got %+v", d.Winner)
	}
	if len(d.Suppressed) != len(cands)-1 {
		t.Fatalf("want %d suppressed, got %d", len(cands)-1, len(d.Suppressed))
	}
}

func TestAlreadyNudgedFactStaysQuiet(t *testing.T) {
	now := at("2026-09-21T14:00:00")
	cands := GenerateCandidates(testCfg(),
		Signals{PRs: []PRSignal{testPR(func(p *PRSignal) { p.StaleHours = 30 })}}, now)
	st := LoadStateEmpty()
	st.LastSeenKeys[cands[0].DedupeKey] = now.UnixMilli()

	if d := Arbitrate(testCfg(), cands, st, nil, now, false); d.Winner != nil {
		t.Fatalf("expected silence, got %q", d.Winner.Text)
	}
}

func TestQuietHoursSilenceEverything(t *testing.T) {
	now := at("2026-09-21T23:00:00")
	cands := GenerateCandidates(testCfg(),
		Signals{PRs: []PRSignal{testPR(func(p *PRSignal) { p.StaleHours = 30 })}}, now)
	d := Arbitrate(testCfg(), cands, LoadStateEmpty(), nil, now, false)
	if d.Winner != nil {
		t.Fatalf("expected silence, got %q", d.Winner.Text)
	}
	if d.Reason != "quiet hours" {
		t.Fatalf("want quiet hours, got %q", d.Reason)
	}
}

func TestDailyCapSilencesFurtherNudges(t *testing.T) {
	now := at("2026-09-21T14:00:00")
	cfg := testCfg()
	cands := GenerateCandidates(cfg, Signals{PRs: []PRSignal{testPR(func(p *PRSignal) { p.StaleHours = 30 })}}, now)

	var sent []Nudge
	for range cfg.MaxNudgesPerDay {
		sent = append(sent, Nudge{Kind: KindStalePR, DedupeKey: "other", SentAt: now.UnixMilli()})
	}
	d := Arbitrate(cfg, cands, LoadStateEmpty(), sent, now, false)
	if d.Winner != nil {
		t.Fatalf("expected silence at the cap, got %q", d.Winner.Text)
	}
}

func TestFocusStreakSurvivesShortBreakNotLong(t *testing.T) {
	// Sampled at the 10-minute default cadence, the way the daemon actually runs.
	st := LoadStateEmpty()
	t0 := at("2026-09-21T10:00:00")
	var res FocusResult
	for i := range 5 {
		res = UpdateFocus([]RepoSignal{testRepo(nil)}, &st, t0.Add(time.Duration(i)*10*time.Minute))
	}
	if res.Minutes < 40 {
		t.Fatalf("typing straight through keeps the streak: got %dm", res.Minutes)
	}

	st2 := LoadStateEmpty()
	UpdateFocus([]RepoSignal{testRepo(nil)}, &st2, t0)
	away := testRepo(func(r *RepoSignal) { r.MsSinceLastEdit = 99 * 60_000 })
	UpdateFocus([]RepoSignal{away}, &st2, t0.Add(time.Hour))
	after := UpdateFocus([]RepoSignal{testRepo(nil)}, &st2, t0.Add(61*time.Minute))
	if after.Minutes >= 5 {
		t.Fatalf("an hour away resets the streak: got %dm", after.Minutes)
	}
}

func TestUncommittedWorkNeedsBigAndColdDiff(t *testing.T) {
	now := at("2026-09-21T14:00:00")
	big := testRepo(func(r *RepoSignal) { r.DirtyLines = 900; r.MsSinceLastCommit = 6 * hourMs })
	warm := testRepo(func(r *RepoSignal) { r.DirtyLines = 900; r.MsSinceLastCommit = 10 * 60_000 })
	small := testRepo(func(r *RepoSignal) { r.DirtyLines = 12; r.MsSinceLastCommit = 6 * hourMs })

	if got := GenerateCandidates(testCfg(), Signals{Repos: []RepoSignal{big}}, now); len(got) != 1 {
		t.Fatalf("big and cold should nudge: got %d", len(got))
	}
	if got := GenerateCandidates(testCfg(), Signals{Repos: []RepoSignal{warm}}, now); len(got) != 0 {
		t.Fatalf("you just committed: got %d", len(got))
	}
	if got := GenerateCandidates(testCfg(), Signals{Repos: []RepoSignal{small}}, now); len(got) != 0 {
		t.Fatalf("a tiny diff is not worth an interruption: got %d", len(got))
	}
}

func TestDay14VerdictIsTheFalsificationTest(t *testing.T) {
	now := time.Now()
	ack := "ack"
	nudge := func(resp *string) Nudge { return Nudge{Kind: KindStalePR, SentAt: now.UnixMilli(), Response: resp} }

	day15 := State{InstalledAt: now.Add(-15 * 24 * time.Hour).UnixMilli(), LastSeenKeys: map[string]int64{}}

	mutedUntil := now.Add(time.Hour).UnixMilli()
	mutedState := day15
	mutedState.MutedUntil = &mutedUntil
	if v := ComputeMetrics(mutedState, []Nudge{nudge(&ack)}, now).Verdict; !strings.Contains(v, "falsification") {
		t.Fatalf("muted at day 14 must read as falsified, got %q", v)
	}

	engaged := ComputeMetrics(day15, []Nudge{nudge(&ack), nudge(&ack), nudge(nil)}, now).Verdict
	if !strings.Contains(engaged, "wedge holds") {
		t.Fatalf("want wedge holds, got %q", engaged)
	}

	tolerated := ComputeMetrics(day15, []Nudge{nudge(nil), nudge(nil), nudge(nil), nudge(&ack)}, now).Verdict
	if !strings.Contains(tolerated, "tolerated, not valued") {
		t.Fatalf("want tolerated, got %q", tolerated)
	}
}

// LoadStateEmpty is a fresh in-memory state, so tests never touch ~/.pulse.
func LoadStateEmpty() State {
	return State{InstalledAt: time.Now().UnixMilli(), LastSeenKeys: map[string]int64{}}
}

// --- config loading (viper) ---

// withConfigHome points Home() at a temp dir so config tests never touch the
// real ~/.pulse and cannot disturb an experiment in flight.
func withConfigHome(t *testing.T, filename, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".pulse"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pulse", filename), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const minimalConfig = `
user:
  githubLogin: me
quietHours:
  start: "22:30"
  end: "08:00"
phrasing:
  useLLM: true
  provider: api
  apiUrl: https://gateway.example/v1
  modelName: gpt-luna
`

func TestLoadConfigReadsYml(t *testing.T) {
	// GitHub's own tooling writes .yml as often as .yaml; both must work.
	withConfigHome(t, "config.yml", minimalConfig)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("config.yml should load: %v", err)
	}
	if cfg.Phrasing.ResolvedModel() != "gpt-luna" {
		t.Fatalf("got model %q", cfg.Phrasing.ResolvedModel())
	}
}

func TestLoadConfigReadsYaml(t *testing.T) {
	withConfigHome(t, "config.yaml", minimalConfig)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Phrasing.Provider != "api" || cfg.Phrasing.APIURL != "https://gateway.example/v1" {
		t.Fatalf("api fields not loaded: %+v", cfg.Phrasing)
	}
}

func TestEnvOverridesConfigFile(t *testing.T) {
	withConfigHome(t, "config.yaml", minimalConfig)
	t.Setenv("PULSE_PHRASING_MODELNAME", "from-env")
	t.Setenv("PULSE_PHRASING_APIKEY", "secret-from-env")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Phrasing.ResolvedModel() != "from-env" {
		t.Fatalf("env must win, got %q", cfg.Phrasing.ResolvedModel())
	}
	if cfg.Phrasing.APIKey != "secret-from-env" {
		t.Fatalf("env key must load so no secret need sit on disk, got %q", cfg.Phrasing.APIKey)
	}
}

func TestLegacyModelKeyStillWorks(t *testing.T) {
	// An existing install has `model:`, not `modelName:`. Upgrading Pulse must
	// not silently change which model is in use.
	withConfigHome(t, "config.yaml", "phrasing:\n  useLLM: true\n  model: claude-opus-5\n")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Phrasing.ResolvedModel(); got != "claude-opus-5" {
		t.Fatalf("legacy model key ignored, got %q", got)
	}
}

func TestModelNameBeatsLegacyModel(t *testing.T) {
	withConfigHome(t, "config.yaml", "phrasing:\n  model: old\n  modelName: new\n")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Phrasing.ResolvedModel(); got != "new" {
		t.Fatalf("modelName should win, got %q", got)
	}
}

func TestMissingConfigNamesTheFix(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	_, err := LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "pulse init") {
		t.Fatalf("a missing config should point at `pulse init`, got %v", err)
	}
}

func TestDefaultsFillGapsInAPartialConfig(t *testing.T) {
	withConfigHome(t, "config.yaml", "user:\n  githubLogin: me\n")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	d := DefaultConfig()
	if cfg.MaxNudgesPerDay != d.MaxNudgesPerDay || cfg.Thresholds.MaxPerKind != d.Thresholds.MaxPerKind {
		t.Fatalf("defaults did not fill gaps: %+v", cfg)
	}
}

// --- phrasing guards ---

func TestCleanPhrasingRejectsWorseThanTemplate(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
		ok             bool
	}{
		{"plain", "CI is red on app#12.", "CI is red on app#12.", true},
		{"strips quotes", `"CI is red on app#12."`, "CI is red on app#12.", true},
		{"empty", "   ", "", false},
		{"multiline", "one\ntwo", "", false},
		{"too long", strings.Repeat("x", 201), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := cleanPhrasing(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("got (%q,%v) want (%q,%v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestPhraseFallsBackToTemplateWhenMisconfigured(t *testing.T) {
	cfg := testCfg()
	cfg.Phrasing = PhrasingConfig{UseLLM: true, Provider: "api"} // no url, no model
	c := Candidate{Kind: KindStalePR, Text: "template wording"}

	got := Phrase(cfg, c, nil)
	if got.By != "template" || got.Text != "template wording" {
		t.Fatalf("a broken provider must never block a nudge, got %+v", got)
	}
}

package onboard

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"pulse/internal/pulse"
	"pulse/internal/pulse/ui"
)

func init() { ui.SetEnabled(false) }

// answers drives the questionnaire; "" means "accept the default".
func run(t *testing.T, answers ...string) (pulse.Config, string) {
	t.Helper()
	base := pulse.DefaultConfig()
	base.User.GithubLogin = "detected-login"
	base.RepoRoots = []string{"/detected"}

	var out bytes.Buffer
	// interactive=false: pickers fall back to numbered/text prompts, which is
	// what these answer strings drive.
	cfg := RunOpts(strings.NewReader(strings.Join(answers, "\n")+"\n"), &out, base, false)
	return cfg, out.String()
}

func TestEnterThroughEverythingGivesWorkingDefaults(t *testing.T) {
	// The fastest correct path through setup should be holding Enter.
	cfg, _ := run(t, strings.Split(strings.Repeat(" \n", 20), "\n")...)

	if cfg.User.GithubLogin != "detected-login" {
		t.Errorf("detection should survive an empty answer, got %q", cfg.User.GithubLogin)
	}
	if cfg.Quiet.Start != "22:30" || cfg.Quiet.End != "09:00" {
		t.Errorf("quiet hours should come from the day defaults, got %+v", cfg.Quiet)
	}
	if cfg.MaxNudgesPerDay != 6 {
		t.Errorf("normal should mean 6 a day, got %d", cfg.MaxNudgesPerDay)
	}
	if len(cfg.Routines) != 3 {
		t.Fatalf("stand-up, gym and posture default to yes: got %d", len(cfg.Routines))
	}
}

func TestQuietHoursAreTheComplementOfTheWorkingDay(t *testing.T) {
	// Answer: login, roots, day start, day end, work days, then decline the rest.
	cfg, _ := run(t, "me", "/code", "08:30", "21:00", "weekdays", "n", "n", "n", "n", "1", "60", "n")
	if cfg.Quiet.Start != "21:00" || cfg.Quiet.End != "08:30" {
		t.Fatalf("quiet window should run from the evening cutoff to the morning start, got %+v", cfg.Quiet)
	}
}

func TestChattinessSetsBothCeilingAndGap(t *testing.T) {
	for _, tc := range []struct {
		answer  string
		perDay  int
		gapMins int
	}{
		{"1", 3, 90},
		{"2", 6, 45},
		{"3", 10, 20},
	} {
		cfg, _ := run(t, "me", "/c", "09:00", "22:00", "weekdays",
			"n", "n", "n", "n", tc.answer, "90", "n")
		if cfg.MaxNudgesPerDay != tc.perDay || cfg.MinMinutesBetweenNudge != tc.gapMins {
			t.Errorf("%q: got %d/day %dm apart, want %d/%d",
				tc.answer, cfg.MaxNudgesPerDay, cfg.MinMinutesBetweenNudge, tc.perDay, tc.gapMins)
		}
	}
}

func TestRoutinesAreBuiltFromAnswers(t *testing.T) {
	cfg, _ := run(t, "me", "/c", "09:00", "22:00", "weekdays",
		"y", "09:45", "mon,tue,wed,thu,fri", // stand-up
		"y", "06:30", "tue,thu,sat", // gym
		"n", // no posture
		"n", // no extra routine
		"normal", "90", "n")

	if len(cfg.Routines) != 2 {
		t.Fatalf("want 2 routines, got %d: %+v", len(cfg.Routines), cfg.Routines)
	}
	gym := cfg.Routines[1]
	if gym.Name != "Gym" || gym.At != "06:30" {
		t.Errorf("gym not built from answers: %+v", gym)
	}
	if len(gym.Days) != 3 || gym.Days[0] != 2 || gym.Days[2] != 6 {
		t.Errorf("gym days should be tue,thu,sat: %v", gym.Days)
	}
}

func TestPostureRoutineOnlyFiresWhenActive(t *testing.T) {
	// A posture nudge to an empty chair is pure noise.
	// posture now asks: from, until, how often (choice), days
	cfg, _ := run(t, "me", "/c", "09:00", "22:00", "weekdays",
		"n", "n",
		"y", "10:00", "18:00", "3", "weekdays",
		"n", "2", "90", "n")
	var posture *pulse.Routine
	for i := range cfg.Routines {
		if strings.HasPrefix(cfg.Routines[i].Name, "Posture") {
			posture = &cfg.Routines[i]
		}
	}
	if posture == nil {
		t.Fatal("posture routine missing")
	}
	if !posture.RequireActive {
		t.Error("posture must require an active keyboard")
	}
}

func TestCustomRoutineCanBeAdded(t *testing.T) {
	cfg, _ := run(t, "me", "/c", "09:00", "22:00", "weekdays",
		"n", "n", "n",
		"y", "Walk the dog", "18:15", "n", "daily", // name, time, no repeat, days
		"n", "2", "90", "n")
	if len(cfg.Routines) != 1 || cfg.Routines[0].Name != "Walk the dog" {
		t.Fatalf("custom routine not captured: %+v", cfg.Routines)
	}
	if cfg.Routines[0].Days != nil {
		t.Errorf("daily should mean no day restriction, got %v", cfg.Routines[0].Days)
	}
}

func TestTheQuestionnaireNeverAsksForTheKey(t *testing.T) {
	// A key typed at a prompt lands in a file on disk. The environment is the
	// safer home, and the flow says so instead of collecting it.
	cfg, out := run(t, "me", "/c", "09:00", "22:00", "weekdays",
		"n", "n", "n", "n", "2", "90",
		"y", "n", "https://gw.example/v1", "some-model")

	if cfg.Phrasing.APIKey != "" {
		t.Fatal("the questionnaire must not collect a credential")
	}
	if !strings.Contains(out, "PULSE_API_KEY") {
		t.Error("it should tell the user where the key belongs")
	}
	if !strings.Contains(out, "launchctl setenv") {
		t.Error("launchd does not inherit the shell; that has to be said here")
	}
	if cfg.Phrasing.Provider != "api" || cfg.Phrasing.APIURL != "https://gw.example/v1" {
		t.Errorf("provider answers not captured: %+v", cfg.Phrasing)
	}
}

func TestEndOfInputFallsBackToDefaultsInsteadOfHanging(t *testing.T) {
	// Piped or truncated input must not block forever on a prompt.
	cfg, _ := run(t, "me")
	if cfg.MaxNudgesPerDay == 0 || cfg.Quiet.Start == "" {
		t.Fatalf("truncated input should still yield a usable config: %+v", cfg)
	}
}

func TestMalformedTimeIsRejectedThenAccepted(t *testing.T) {
	cfg, out := run(t, "me", "/c", "not a time", "25:99", "08:00", "22:00",
		"weekdays", "n", "n", "n", "n", "2", "90", "n")
	if cfg.Quiet.End != "08:00" {
		t.Fatalf("should have recovered to the valid answer, got %q", cfg.Quiet.End)
	}
	if !strings.Contains(out, "24-hour") {
		t.Error("a rejected time should explain the format")
	}
}

func TestTimeFormatsAreForgiving(t *testing.T) {
	for in, want := range map[string]string{
		"9": "09:00", "9:00": "09:00", "09:00": "09:00",
		"0900": "09:00", "19.30": "19:30", "7:05": "07:05",
		"": "", "24:00": "", "abc": "", "12:60": "",
	} {
		if got := normaliseTime(in); got != want {
			t.Errorf("normaliseTime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDayFormatsAreForgiving(t *testing.T) {
	cases := []struct {
		in   string
		want []int
		ok   bool
	}{
		{"weekdays", []int{1, 2, 3, 4, 5}, true},
		{"weekends", []int{0, 6}, true},
		{"daily", nil, true},
		{"mon,wed,fri", []int{1, 3, 5}, true},
		{"Mon Wed", []int{1, 3}, true},
		{"tue/thu", []int{2, 4}, true},
		{"mon,mon", []int{1}, true},
		{"funday", nil, false},
	}
	for _, tc := range cases {
		got, ok := parseDays(tc.in)
		if ok != tc.ok {
			t.Errorf("parseDays(%q) ok=%v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("parseDays(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSummaryShowsWhatWasConfigured(t *testing.T) {
	cfg, _ := run(t, strings.Split(strings.Repeat(" \n", 20), "\n")...)
	joined := strings.Join(Summary(cfg), "\n")
	for _, want := range []string{"quiet hours", "ceiling", "Stand-up", "Gym", "only when active"} {
		if !strings.Contains(joined, want) {
			t.Errorf("summary missing %q\n%s", want, joined)
		}
	}
}

// --- day picker and repeat intervals ---

func TestDayPickerRoundTripsThroughIndices(t *testing.T) {
	// The bug this guards: the text path yields weekday numbers and the picker
	// yields row indices. Converting twice turns Tuesday into Wednesday.
	for _, tc := range []struct {
		text string
		want []int
	}{
		{"weekdays", []int{1, 2, 3, 4, 5}},
		{"tue,thu,sat", []int{2, 4, 6}},
		{"sun", []int{0}},
		{"daily", nil}, // every row selected is stored as no restriction
	} {
		a := newAsker(strings.NewReader(tc.text+"\n"), io.Discard, false)
		idx, _ := a.pickMany("days", weekdayItems, nil, "weekdays")
		got := indicesToDays(idx)
		if len(got) != len(tc.want) {
			t.Errorf("%q → %v, want %v", tc.text, got, tc.want)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("%q → %v, want %v", tc.text, got, tc.want)
				break
			}
		}
	}
}

func TestWeekdayItemsAreMondayFirst(t *testing.T) {
	// People describe a working week starting Monday; time.Weekday starts Sunday.
	if weekdayItems[0].Label != "Monday" || weekdayItems[0].Value.(int) != 1 {
		t.Fatalf("first row should be Monday=1, got %+v", weekdayItems[0])
	}
	if weekdayItems[6].Label != "Sunday" || weekdayItems[6].Value.(int) != 0 {
		t.Fatalf("last row should be Sunday=0, got %+v", weekdayItems[6])
	}
}

func TestIntervalOffersCommonChoices(t *testing.T) {
	for i, want := range intervalChoices {
		a := newAsker(strings.NewReader(fmt.Sprintf("%d\n", i+1)), io.Discard, false)
		got, _ := a.interval("how often", 30)
		if got != want {
			t.Errorf("choice %d → %d, want %d", i+1, got, want)
		}
	}
}

func TestIntervalCustomAsksForAValue(t *testing.T) {
	// The last row is "custom…", which then prompts for minutes.
	custom := strconv.Itoa(len(intervalChoices) + 1)
	a := newAsker(strings.NewReader(custom+"\n7\n"), io.Discard, false)
	got, _ := a.interval("how often", 30)
	if got != 7 {
		t.Fatalf("custom should take the typed value, got %d", got)
	}
}

func TestIntervalRejectsNonsenseThenAccepts(t *testing.T) {
	custom := strconv.Itoa(len(intervalChoices) + 1)
	var out bytes.Buffer
	a := newAsker(strings.NewReader(custom+"\nabc\n0\n99999\n25\n"), &out, false)
	got, _ := a.interval("how often", 30)
	if got != 25 {
		t.Fatalf("should recover to the valid answer, got %d", got)
	}
	if !strings.Contains(out.String(), "1 to 1440") {
		t.Error("a rejected interval should state the accepted range")
	}
}

func TestIntervalAcceptsMinutesSuffix(t *testing.T) {
	custom := strconv.Itoa(len(intervalChoices) + 1)
	a := newAsker(strings.NewReader(custom+"\n20m\n"), io.Discard, false)
	got, _ := a.interval("how often", 30)
	if got != 20 {
		t.Fatalf("\"20m\" should parse as 20, got %d", got)
	}
}

func TestPostureBecomesARepeatingWindow(t *testing.T) {
	cfg, _ := run(t, "me", "/c", "09:00", "22:00", "weekdays",
		"n", "n",
		"y", "10:00", "17:00", "3", "weekdays", // from, until, every 30m, days
		"n", "2", "90", "n")

	var posture *pulse.Routine
	for i := range cfg.Routines {
		if strings.HasPrefix(cfg.Routines[i].Name, "Posture") {
			posture = &cfg.Routines[i]
		}
	}
	if posture == nil {
		t.Fatal("posture routine missing")
	}
	if posture.At != "10:00" || posture.Until != "17:00" {
		t.Errorf("window not captured: %+v", posture)
	}
	if posture.Every != 30 {
		t.Errorf("interval not captured: %d", posture.Every)
	}
	if got := len(posture.Slots()); got != 15 { // 10:00–17:00 every 30m
		t.Errorf("want 15 reminders across the window, got %d", got)
	}
}

func TestSummaryShowsARepeatingWindow(t *testing.T) {
	cfg := pulse.DefaultConfig()
	cfg.Routines = []pulse.Routine{
		{Name: "Posture check", At: "10:00", Until: "18:00", Every: 30, RequireActive: true},
	}
	joined := strings.Join(Summary(cfg), "\n")
	if !strings.Contains(joined, "10:00–18:00 every 30m") {
		t.Fatalf("summary should show the window and cadence:\n%s", joined)
	}
}

// --- picker behaviour ---

func pkey(p picker, k string) picker {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, _ := p.Update(msg)
	return next.(picker)
}

func TestSpaceTogglesAndEnterConfirms(t *testing.T) {
	p := newPicker("days", weekdayItems, true, weekdayIdx)
	p = pkey(pkey(p, "down"), " ") // uncheck Tuesday
	if got := indicesToDays(p.chosen()); len(got) != 4 {
		t.Fatalf("want 4 days after unchecking one, got %v", got)
	}
	if !pkey(p, "enter").done {
		t.Error("enter should confirm")
	}
}

func TestToggleAllIsOneKeystroke(t *testing.T) {
	p := newPicker("days", weekdayItems, true, weekdayIdx)
	p = pkey(p, "a")
	if len(p.chosen()) != 7 {
		t.Fatalf("`a` should select every day, got %d", len(p.chosen()))
	}
	if len(pkey(p, "a").chosen()) != 0 {
		t.Error("`a` again should clear the selection")
	}
}

func TestSingleSelectTakesTheCursorRow(t *testing.T) {
	p := newPicker("how often", []Item{{Label: "a"}, {Label: "b"}, {Label: "c"}}, false, nil)
	p = pkey(pkey(pkey(p, "down"), "down"), "enter")
	got := p.chosen()
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("want row 2, got %v", got)
	}
}

func TestCursorStopsAtPickerEdges(t *testing.T) {
	p := newPicker("days", weekdayItems, true, nil)
	for range 20 {
		p = pkey(p, "up")
	}
	if p.cursor != 0 {
		t.Errorf("cursor should stop at the top, got %d", p.cursor)
	}
	for range 40 {
		p = pkey(p, "down")
	}
	if p.cursor != len(weekdayItems)-1 {
		t.Errorf("cursor should stop at the bottom, got %d", p.cursor)
	}
}

func TestEscapeGoesBackRatherThanCancelling(t *testing.T) {
	// Escape must never abandon setup: a stray keypress should cost one
	// question, not the whole questionnaire.
	p := pkey(newPicker("days", weekdayItems, true, weekdayIdx), "esc")
	if !p.back {
		t.Fatal("esc should request going back")
	}
	if p.quit {
		t.Fatal("esc must not quit setup")
	}
}

func TestSpaceDoesNothingInSingleSelect(t *testing.T) {
	p := newPicker("one", []Item{{Label: "a"}, {Label: "b"}}, false, nil)
	if len(pkey(p, " ").chosen()) != 0 {
		t.Fatal("space should not toggle in a single-select list")
	}
}

// --- going back ---

func TestBackReturnsToThePreviousQuestion(t *testing.T) {
	// "b" is the text-path spelling of Escape, since a buffered read cannot
	// see the key itself.
	var out bytes.Buffer
	base := pulse.DefaultConfig()
	base.User.GithubLogin = "first-answer"

	// login, roots, then back, then correct the roots, then decline everything.
	answers := []string{
		"me", "/wrong", "b", "/right",
		"09:00", "22:00", "weekdays",
		"n", "n", "n", "n", "2", "90", "n",
	}
	cfg := RunOpts(strings.NewReader(strings.Join(answers, "\n")+"\n"), &out, base, false)

	if len(cfg.RepoRoots) != 1 || cfg.RepoRoots[0] != "/right" {
		t.Fatalf("going back should let the answer be corrected, got %v", cfg.RepoRoots)
	}
}

func TestBackSkipsQuestionsNoLongerBeingAsked(t *testing.T) {
	// Declining stand-up removes its follow-ups. Stepping back from the next
	// question must land on "Daily stand-up?", not on its orphaned time prompt.
	steps := plan()
	ans := &answers{standup: false, gym: true}

	gymIdx := -1
	for i, s := range steps {
		if s.id == "gym" {
			gymIdx = i
		}
	}
	if gymIdx < 0 {
		t.Fatal("gym step missing from the plan")
	}

	prev := prevAsked(steps, ans, gymIdx)
	if steps[prev].id != "standup" {
		t.Fatalf("back from gym should reach standup, reached %q", steps[prev].id)
	}
}

func TestBackFromAnsweredBranchReachesItsOwnQuestions(t *testing.T) {
	steps := plan()
	ans := &answers{standup: true}

	gymIdx := -1
	for i, s := range steps {
		if s.id == "gym" {
			gymIdx = i
		}
	}
	prev := prevAsked(steps, ans, gymIdx)
	if steps[prev].id != "standup-days" {
		t.Fatalf("with stand-up enabled, back from gym should reach its days question, reached %q", steps[prev].id)
	}
}

func TestBackAtTheFirstQuestionStaysOnIt(t *testing.T) {
	// There is nowhere earlier to go, and quitting setup over a stray keypress
	// would be hostile — so it re-asks the first real question.
	steps := plan()
	first := 0
	for i, s := range steps {
		if !s.header {
			first = i
			break
		}
	}
	if got := prevAsked(steps, &answers{}, first); got != first {
		t.Fatalf("want to stay on %q, went to %q", steps[first].id, steps[got].id)
	}
}

func TestBackNeverLandsOnASectionHeader(t *testing.T) {
	// A header asks nothing, so landing on one bounces straight forward and
	// makes Escape look broken.
	steps := plan()
	ans := &answers{standup: true, gym: true, posture: true, useAI: true}
	for i := range steps {
		if steps[i].header {
			continue
		}
		if got := prevAsked(steps, ans, i); steps[got].header {
			t.Errorf("back from %q landed on header %q", steps[i].id, steps[got].id)
		}
	}
}

func TestRevisitedQuestionOffersThePreviousAnswer(t *testing.T) {
	// Answers live separately from the config precisely so a revisit can show
	// what was said last time rather than the original default.
	ans := &answers{gymAt: "06:30", gym: true}
	var out bytes.Buffer
	a := newAsker(strings.NewReader("\n"), &out, false)

	for _, s := range plan() {
		if s.id == "gym-at" {
			if err := s.ask(a, ans); err != nil {
				t.Fatal(err)
			}
		}
	}
	if ans.gymAt != "06:30" {
		t.Fatalf("an empty answer should keep the previous one, got %q", ans.gymAt)
	}
	if !strings.Contains(out.String(), "06:30") {
		t.Errorf("the previous answer should be offered as the default:\n%s", out.String())
	}
}

func TestAnswersFoldIntoConfigOnlyAtTheEnd(t *testing.T) {
	// Building the config incrementally would leave stale values behind when a
	// question is revisited and answered differently.
	ans := &answers{
		login: "me", roots: "/a, /b",
		dayStart: "08:00", dayEnd: "21:00",
		standup: false, gym: true, gymAt: "07:00", gymDays: []int{2, 4},
		posture: false, chatty: 0, focusMin: "45",
	}
	cfg := ans.toConfig(pulse.DefaultConfig())

	if len(cfg.Routines) != 1 || cfg.Routines[0].Name != "Gym" {
		t.Fatalf("declined routines must not appear: %+v", cfg.Routines)
	}
	if cfg.Quiet.Start != "21:00" || cfg.Quiet.End != "08:00" {
		t.Errorf("quiet hours wrong: %+v", cfg.Quiet)
	}
	if cfg.MaxNudgesPerDay != 3 {
		t.Errorf("quiet should mean 3 a day, got %d", cfg.MaxNudgesPerDay)
	}
	if len(cfg.RepoRoots) != 2 {
		t.Errorf("roots not split: %v", cfg.RepoRoots)
	}
}

func TestTextInputEscapeReportsBack(t *testing.T) {
	ti := newTextInput("name", "def", "")
	next, _ := ti.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !next.(textInput).back {
		t.Fatal("escape in a text prompt should request going back")
	}
}

func TestTextInputKeepsDefaultWhenNothingTyped(t *testing.T) {
	ti := newTextInput("name", "default-value", "")
	next, _ := ti.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := next.(textInput).answer(); got != "default-value" {
		t.Fatalf("want the default, got %q", got)
	}
}

func TestTextInputEditing(t *testing.T) {
	ti := newTextInput("name", "", "")
	for _, r := range "gymm" {
		next, _ := ti.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		ti = next.(textInput)
	}
	next, _ := ti.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := next.(textInput).answer(); got != "gym" {
		t.Fatalf("backspace should delete one rune, got %q", got)
	}
}

// --- section recaps ---

func TestEachSectionEchoesWhatWasCaptured(t *testing.T) {
	var out bytes.Buffer
	base := pulse.DefaultConfig()
	base.User.GithubLogin = "me"
	base.RepoRoots = []string{"/code"}

	RunOpts(strings.NewReader(strings.Join([]string{
		"me", "/code", "08:30", "21:00", "weekdays",
		"n", "n",
		"y", "10:00", "17:00", "1", "weekdays", // posture, every 10m
		"n", "1", "60", "n",
	}, "\n")+"\n"), &out, base, false)

	v := out.String()
	for _, want := range []string{
		"github", "code in",
		"working day", "08:30 – 21:00",
		"silent", "21:00 – 08:30", "(derived)",
		"Posture check", "10:00–17:00 every 10m",
		"ceiling", "at most 3 a day, 90m apart",
		"focus break", "after 60m",
		"fixed wording",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("recap missing %q\n%s", want, v)
		}
	}
}

func TestRecapStatesTheDerivedQuietWindow(t *testing.T) {
	// Nobody was asked for quiet hours, so showing them is the only way the
	// user learns what was inferred from the working day.
	var out bytes.Buffer
	RunOpts(strings.NewReader(strings.Join([]string{
		"me", "/c", "07:00", "20:00", "weekdays",
		"n", "n", "n", "n", "2", "90", "n",
	}, "\n")+"\n"), &out, pulse.DefaultConfig(), false)

	if !strings.Contains(out.String(), "20:00 – 07:00") {
		t.Fatalf("the derived silent window should be shown:\n%s", out.String())
	}
}

func TestRecapAgreesWithTheSavedConfig(t *testing.T) {
	// The recap and the final config both render from answers.routines(), so
	// what you are shown cannot drift from what is written.
	ans := &answers{
		workDays: []int{1, 2, 3, 4, 5},
		standup:  true, standupAt: "09:45",
		posture: true, postFrom: "10:00", postUntil: "16:00", postEvery: 15,
		chatty: 1, focusMin: "90",
	}
	fromAnswers := ans.routines()
	saved := ans.toConfig(pulse.DefaultConfig()).Routines

	if len(fromAnswers) != len(saved) {
		t.Fatalf("recap shows %d routines, config saves %d", len(fromAnswers), len(saved))
	}
	for i := range saved {
		if describeRoutine(saved[i]) != describeRoutine(fromAnswers[i]) {
			t.Errorf("row %d differs:\n  recap  %s\n  saved  %s",
				i, describeRoutine(fromAnswers[i]), describeRoutine(saved[i]))
		}
	}
}

func TestRoutineInheritsWorkingDaysWhenNotAsked(t *testing.T) {
	// Skipping the days question must not produce a routine that never fires.
	ans := &answers{workDays: []int{1, 3, 5}, standup: true, standupAt: "10:00"}
	rs := ans.routines()
	if len(rs) != 1 || len(rs[0].Days) != 3 {
		t.Fatalf("stand-up should inherit the working days, got %+v", rs)
	}
}

func TestRecapShowsTheCorrectedAnswerAfterGoingBack(t *testing.T) {
	// A recap re-renders each time the driver passes it, so correcting an
	// answer and moving on must show the new value, not the first one.
	var out bytes.Buffer
	RunOpts(strings.NewReader(strings.Join([]string{
		"me", "/wrong", "b", "/right",
		"09:00", "22:00", "weekdays",
		"n", "n", "n", "n", "2", "90", "n",
	}, "\n")+"\n"), &out, pulse.DefaultConfig(), false)

	v := out.String()
	last := strings.LastIndex(v, "code in")
	if last < 0 {
		t.Fatal("no recap rendered")
	}
	if !strings.Contains(v[last:], "/right") {
		t.Fatalf("the final recap should show the corrected value:\n%s", v[last:])
	}
}

func TestNoRoutinesSaysSoRatherThanShowingNothing(t *testing.T) {
	var out bytes.Buffer
	RunOpts(strings.NewReader(strings.Join([]string{
		"me", "/c", "09:00", "22:00", "weekdays",
		"n", "n", "n", "n", "2", "90", "n",
	}, "\n")+"\n"), &out, pulse.DefaultConfig(), false)

	if !strings.Contains(out.String(), "no routines") {
		t.Fatal("an empty section should say so rather than render blank")
	}
}

func TestRecapsAreSteppedOverByBackNavigation(t *testing.T) {
	steps := plan()
	ans := &answers{standup: true, gym: true, posture: true, useAI: true}
	for i, s := range steps {
		if s.header {
			continue
		}
		got := prevAsked(steps, ans, i)
		if strings.HasPrefix(steps[got].id, "recap-") {
			t.Errorf("back from %q landed on recap %q", s.id, steps[got].id)
		}
	}
}

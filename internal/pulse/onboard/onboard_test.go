package onboard

import (
	"bytes"
	"strings"
	"testing"

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
	cfg := Run(strings.NewReader(strings.Join(answers, "\n")+"\n"), &out, base)
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
	cfg, _ := run(t, "me", "/code", "08:30", "21:00", "weekdays", "n", "n", "n", "n", "quiet", "60", "n")
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
		{"quiet", 3, 90},
		{"normal", 6, 45},
		{"chatty", 10, 20},
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
	cfg, _ := run(t, "me", "/c", "09:00", "22:00", "weekdays",
		"n", "n", "y", "15:30", "n", "normal", "90", "n")
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
		"y", "Walk the dog", "18:15", "daily",
		"n", "normal", "90", "n")
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
		"n", "n", "n", "n", "normal", "90",
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
		"weekdays", "n", "n", "n", "n", "normal", "90", "n")
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

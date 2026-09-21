package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pulse/internal/pulse"
	"pulse/internal/pulse/ui"
)

func init() { ui.SetEnabled(false) }

func TestReportCountsSeveritySeparately(t *testing.T) {
	// A degraded install still runs; a failed one does not. Conflating them
	// would either block on a warning or wave through a real breakage.
	rep := Report{Results: []Result{
		ok("a", ""), warn("b", "", ""), fail("c", "", ""), fail("d", "", ""),
	}}
	if rep.Failed() != 2 {
		t.Errorf("Failed() = %d, want 2", rep.Failed())
	}
	if rep.Warned() != 1 {
		t.Errorf("Warned() = %d, want 1", rep.Warned())
	}
}

func TestEveryProblemNamesItsFix(t *testing.T) {
	// A check that reports a problem without the remedy just moves the
	// confusion somewhere else.
	t.Setenv("HOME", t.TempDir())
	cfg, cfgErr := pulse.LoadConfig()

	for _, r := range Run(cfg, cfgErr).Results {
		if r.Status != statusFail && r.Status != statusWarn {
			continue
		}
		if strings.TrimSpace(r.Fix) == "" {
			t.Errorf("%q reports %q with no fix", r.Name, r.Detail)
		}
	}
}

func TestMissingConfigIsAFailureNotAWarning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, cfgErr := pulse.LoadConfig()
	if cfgErr == nil {
		t.Skip("config unexpectedly present")
	}
	got := checkConfig(cfgErr)
	if got.Status != statusFail {
		t.Fatalf("no config should be a failure, got status %d", got.Status)
	}
	if !strings.Contains(got.Fix, "pulse init") {
		t.Errorf("the fix should be `pulse init`, got %q", got.Fix)
	}
}

func TestSilentlyIgnoredConfigKeysAreReported(t *testing.T) {
	// The failure that motivated this check: an env-var name written where a
	// config field belongs looks applied and does nothing.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".pulse"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "user:\n  githubLogin: me\nphrasing:\n  PULSE_API_KEY: \"x\"\n"
	if err := os.WriteFile(filepath.Join(home, ".pulse", "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got := checkUnknownKeys()
	if got.Status != statusWarn {
		t.Fatalf("an ignored key should warn, got status %d", got.Status)
	}
	if !strings.Contains(got.Detail, "PULSE_API_KEY") {
		t.Errorf("the offending key should be named, got %q", got.Detail)
	}
}

func TestWritableStateDirectoryPasses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := checkStateDir(); got.Status != statusOK {
		t.Fatalf("a writable home should pass, got %q", got.Detail)
	}
}

func TestMissingRepoRootIsAFailure(t *testing.T) {
	cfg := pulse.DefaultConfig()
	cfg.RepoRoots = []string{filepath.Join(t.TempDir(), "does-not-exist")}
	got := checkRepos(cfg)
	if got.Status != statusFail {
		t.Fatalf("a missing root should fail, got status %d", got.Status)
	}
	if !strings.Contains(got.Fix, "repoRoots") {
		t.Errorf("the fix should name the field, got %q", got.Fix)
	}
}

func TestPhrasingOffIsNotAProblem(t *testing.T) {
	// Running without a model is a supported configuration, not a fault.
	cfg := pulse.DefaultConfig()
	cfg.Phrasing.UseLLM = false
	if got := checkPhrasing(cfg); got.Status != statusOK {
		t.Fatalf("phrasing off should pass, got %q", got.Detail)
	}
}

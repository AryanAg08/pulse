package pulse

import "testing"

func TestUnknownKeysCatchesTypos(t *testing.T) {
	withConfigHome(t, "config.yaml", `
user:
  githubLogin: me
phrasing:
  useLLM: true
  PULSE_API_KEY: "sk-wrong-field"
  modelname: lower
maxNudgesPerDay: 6
`)
	got := UnknownKeys()
	if len(got) == 0 {
		t.Fatal("an env-var name used as a config field must be reported")
	}
	t.Logf("unknown: %v", got)
}

func TestUnknownKeysQuietOnValidConfig(t *testing.T) {
	withConfigHome(t, "config.yaml", minimalConfig)
	if got := UnknownKeys(); len(got) != 0 {
		t.Fatalf("valid config should report nothing, got %v", got)
	}
}

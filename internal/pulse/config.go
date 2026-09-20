package pulse

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Home is the state directory, shared with any other Pulse implementation on
// this machine so an in-flight experiment survives a rewrite.
func Home() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".pulse")
}

func ConfigPath() string { return filepath.Join(Home(), "config.yaml") }

func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		User:                   UserConfig{Timezone: time.Local.String()},
		RepoRoots:              []string{filepath.Join(home, "code"), filepath.Join(home, "src")},
		Quiet:                  QuietHours{Start: "22:30", End: "08:00"},
		MaxNudgesPerDay:        6,
		MinMinutesBetweenNudge: 45,
		Focus:                  FocusConfig{BreakAfterMinutes: 90},
		Thresholds: Thresholds{
			StalePRHours:       24,
			ReviewDebtHours:    12,
			UncommittedLines:   400,
			AbandonedAfterDays: 14,
			MaxPerKind:         2,
		},
		Routines:   []Routine{},
		MutedKinds: []NudgeKind{},
		Phrasing:   PhrasingConfig{UseLLM: true, Model: "claude-opus-5"},
	}
}

func ConfigExists() bool {
	_, err := os.Stat(ConfigPath())
	return err == nil
}

// LoadConfig reads the config, filling any absent field from the defaults so an
// older config file keeps working after new options are added.
func LoadConfig() (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		return cfg, fmt.Errorf("no config at %s: run `pulse init`", ConfigPath())
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config at %s is not valid YAML: %w", ConfigPath(), err)
	}
	// yaml leaves zero values where keys are absent; restore the meaningful ones.
	d := DefaultConfig()
	if cfg.MaxNudgesPerDay == 0 {
		cfg.MaxNudgesPerDay = d.MaxNudgesPerDay
	}
	if cfg.MinMinutesBetweenNudge == 0 {
		cfg.MinMinutesBetweenNudge = d.MinMinutesBetweenNudge
	}
	if cfg.Focus.BreakAfterMinutes == 0 {
		cfg.Focus.BreakAfterMinutes = d.Focus.BreakAfterMinutes
	}
	if cfg.Thresholds.AbandonedAfterDays == 0 {
		cfg.Thresholds.AbandonedAfterDays = d.Thresholds.AbandonedAfterDays
	}
	if cfg.Thresholds.MaxPerKind == 0 {
		cfg.Thresholds.MaxPerKind = d.Thresholds.MaxPerKind
	}
	if cfg.Thresholds.StalePRHours == 0 {
		cfg.Thresholds.StalePRHours = d.Thresholds.StalePRHours
	}
	if cfg.Thresholds.ReviewDebtHours == 0 {
		cfg.Thresholds.ReviewDebtHours = d.Thresholds.ReviewDebtHours
	}
	if cfg.Thresholds.UncommittedLines == 0 {
		cfg.Thresholds.UncommittedLines = d.Thresholds.UncommittedLines
	}
	if cfg.Quiet.Start == "" {
		cfg.Quiet = d.Quiet
	}
	if cfg.Phrasing.Model == "" {
		cfg.Phrasing.Model = d.Phrasing.Model
	}
	return cfg, nil
}

func SaveConfig(cfg Config) error {
	if err := os.MkdirAll(Home(), 0o755); err != nil {
		return err
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(), out, 0o644)
}

// parseHHMM converts "HH:MM" to minutes past midnight, -1 if malformed.
func parseHHMM(s string) int {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return -1
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return -1
	}
	return h*60 + m
}

// InQuietHours reports whether now falls inside quiet hours, handling the
// overnight wrap where the window crosses midnight.
func InQuietHours(cfg Config, now time.Time) bool {
	start, end := parseHHMM(cfg.Quiet.Start), parseHHMM(cfg.Quiet.End)
	if start < 0 || end < 0 {
		return false
	}
	mins := now.Hour()*60 + now.Minute()
	if start <= end {
		return mins >= start && mins < end
	}
	return mins >= start || mins < end
}

package pulse

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func nudgesPath() string { return filepath.Join(Home(), "nudges.jsonl") }
func statePath() string  { return filepath.Join(Home(), "state.json") }

// State is persisted between cycles. JSON tags match the TypeScript version so
// both implementations read the same file.
type State struct {
	// InstalledAt is day zero for the retention metric.
	InstalledAt int64 `json:"installedAt"`
	// LastSeenKeys maps dedupeKey to the last time we nudged about it.
	LastSeenKeys map[string]int64 `json:"lastSeenKeys"`
	// MutedUntil is the honest kill switch.
	MutedUntil *int64 `json:"mutedUntil"`
	// FocusStartedAt is the start of the current uninterrupted work streak.
	FocusStartedAt *int64 `json:"focusStartedAt"`
	// LastActivityAt is the last cycle at which any repo looked actively edited.
	LastActivityAt *int64 `json:"lastActivityAt"`
}

func LoadState() State {
	s := State{InstalledAt: time.Now().UnixMilli(), LastSeenKeys: map[string]int64{}}
	data, err := os.ReadFile(statePath())
	if err != nil {
		return s
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return State{InstalledAt: time.Now().UnixMilli(), LastSeenKeys: map[string]int64{}}
	}
	if s.LastSeenKeys == nil {
		s.LastSeenKeys = map[string]int64{}
	}
	return s
}

func SaveState(s State) error {
	if err := os.MkdirAll(Home(), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), out, 0o644)
}

func AppendNudge(n Nudge) error {
	if err := os.MkdirAll(Home(), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(nudgesPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

func ReadNudges() []Nudge {
	f, err := os.Open(nudgesPath())
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []Nudge
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var n Nudge
		if err := json.Unmarshal([]byte(line), &n); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// UpdateNudge rewrites the log with one entry patched. The file stays small by
// design — a handful of lines a day — so a full rewrite beats maintaining an index.
func UpdateNudge(idPrefix string, response string, at int64) (*Nudge, error) {
	all := ReadNudges()
	idx := -1
	for i, n := range all {
		if n.ID == idPrefix || strings.HasPrefix(n.ID, idPrefix) {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, nil
	}
	all[idx].Response = &response
	all[idx].RespondedAt = &at

	var b strings.Builder
	for _, n := range all {
		line, err := json.Marshal(n)
		if err != nil {
			return nil, err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(nudgesPath(), []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	return &all[idx], nil
}

func StartOfToday(now time.Time) int64 {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location()).UnixMilli()
}

func NudgesSince(ms int64) []Nudge {
	var out []Nudge
	for _, n := range ReadNudges() {
		if n.SentAt >= ms {
			out = append(out, n)
		}
	}
	return out
}

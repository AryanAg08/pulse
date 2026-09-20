package pulse

// NudgeKind identifies a category of nudge. Users mute per-kind, so these are
// the unit of consent and must stay stable across versions.
type NudgeKind string

const (
	KindStalePR         NudgeKind = "stale_pr"         // your PR has been waiting on a human
	KindReviewDebt      NudgeKind = "review_debt"      // someone is waiting on YOUR review
	KindCIFailed        NudgeKind = "ci_failed"        // your PR's checks are red
	KindLongFocus       NudgeKind = "long_focus"       // you've been heads-down a long time
	KindUncommittedWork NudgeKind = "uncommitted_work" // large diff sitting uncommitted
	KindRoutine         NudgeKind = "routine"          // a routine the user declared
)

// Signals is everything Pulse knows about the world at one instant.
type Signals struct {
	At             int64
	Repos          []RepoSignal
	PRs            []PRSignal
	ReviewRequests []PRSignal
	// FocusMinutes is continuous activity in the most-active repo, 0 if idle.
	FocusMinutes int
	FocusRepo    string
	GithubOK     bool
	Errors       []string
}

type RepoSignal struct {
	Name   string
	Path   string
	Branch string
	// MsSinceLastCommit is -1 when the user has never committed here.
	MsSinceLastCommit int64
	// MsSinceLastEdit is -1 when nothing is modified.
	MsSinceLastEdit int64
	DirtyFiles      int
	DirtyLines      int
	AheadOfRemote   int
}

type PRSignal struct {
	Repo           string
	Number         int
	Title          string
	URL            string
	IsDraft        bool
	CreatedAt      string
	UpdatedAt      string
	StaleHours     float64
	ReviewDecision string
	Checks         string // passing | failing | pending | none
	Author         string
}

// Candidate is a nudge the rules think is worth sending, before arbitration.
type Candidate struct {
	Kind NudgeKind
	// DedupeKey is a stable identity, so the same fact isn't re-nudged.
	DedupeKey string
	// Priority is 0-100. The arbiter sends at most one per cycle: the highest.
	Priority int
	// Text is the deterministic fallback, used when LLM phrasing is unavailable.
	Text string
	// Facts is the structured context handed to the phrasing model.
	Facts map[string]any
	// Action is a single suggested action, typically a URL.
	Action string
}

// Nudge is one that was actually delivered. Field tags match the TypeScript
// implementation exactly so an in-flight experiment's log stays readable.
type Nudge struct {
	ID          string    `json:"id"`
	Kind        NudgeKind `json:"kind"`
	DedupeKey   string    `json:"dedupeKey"`
	Text        string    `json:"text"`
	Action      string    `json:"action,omitempty"`
	SentAt      int64     `json:"sentAt"`
	PhrasedBy   string    `json:"phrasedBy"` // llm | template
	Response    *string   `json:"response"`  // ack | dismiss | snooze, nil = ignored
	RespondedAt *int64    `json:"respondedAt"`
}

type Routine struct {
	Name string `yaml:"name"`
	// At is "HH:MM" local time.
	At string `yaml:"at"`
	// Days is 0=Sun..6=Sat. Empty means every day.
	Days []int `yaml:"days,omitempty"`
	// RequireActive skips the nudge when the user is away from the keyboard.
	RequireActive bool   `yaml:"requireActive,omitempty"`
	Note          string `yaml:"note,omitempty"`
}

type UserConfig struct {
	GithubLogin string `yaml:"githubLogin"`
	Timezone    string `yaml:"timezone"`
}

type QuietHours struct {
	Start string `yaml:"start"`
	End   string `yaml:"end"`
}

type FocusConfig struct {
	BreakAfterMinutes int `yaml:"breakAfterMinutes"`
}

type Thresholds struct {
	StalePRHours     int `yaml:"stalePrHours"`
	ReviewDebtHours  int `yaml:"reviewDebtHours"`
	UncommittedLines int `yaml:"uncommittedLines"`
	// AbandonedAfterDays: past this age a PR is dead, not stale. Nudging is noise.
	AbandonedAfterDays int `yaml:"abandonedAfterDays"`
	// MaxPerKind caps candidates of one kind, so a backlog can't become a firehose.
	MaxPerKind int `yaml:"maxPerKind"`
}

type PhrasingConfig struct {
	UseLLM bool   `yaml:"useLLM"`
	Model  string `yaml:"model"`
}

type Config struct {
	User                   UserConfig     `yaml:"user"`
	RepoRoots              []string       `yaml:"repoRoots"`
	Quiet                  QuietHours     `yaml:"quietHours"`
	MaxNudgesPerDay        int            `yaml:"maxNudgesPerDay"`
	MinMinutesBetweenNudge int            `yaml:"minMinutesBetweenNudges"`
	Focus                  FocusConfig    `yaml:"focus"`
	Thresholds             Thresholds     `yaml:"thresholds"`
	Routines               []Routine      `yaml:"routines"`
	MutedKinds             []NudgeKind    `yaml:"mutedKinds"`
	Phrasing               PhrasingConfig `yaml:"phrasing"`
}

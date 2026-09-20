// Command pulse is a context-aware assistant for developers. It reads your real
// work state — git, GitHub, edit activity — and speaks only when the evidence
// justifies an interruption.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pulse/internal/pulse"
)

// The launchd agent writes to a file, not a terminal; colour codes there are noise.
var isTTY = func() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}()

func dim(s string) string {
	if !isTTY {
		return s
	}
	return "\x1b[2m" + s + "\x1b[0m"
}

func bold(s string) string {
	if !isTTY {
		return s
	}
	return "\x1b[1m" + s + "\x1b[0m"
}

var args []string

// flag returns the value of --name=value, or "true" for a bare --name.
func flag(name string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "--"+name+"=") {
			return strings.SplitN(a, "=", 2)[1]
		}
		if a == "--"+name {
			return "true"
		}
	}
	return ""
}

func flagInt(name string, def int) int {
	if v := flag(name); v != "" && v != "true" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	cmd := os.Args[1]
	args = os.Args[2:]

	switch cmd {
	case "init":
		cmdInit()
	case "run":
		cmdRun()
	case "start":
		cmdStart()
	case "daemon":
		cmdDaemon()
	case "status":
		cmdStatus()
	case "ack", "dismiss", "snooze":
		cmdRespond(cmd)
	case "mute":
		cmdMute()
	case "unmute":
		cmdUnmute()
	case "metrics":
		cmdMetrics()
	case "log":
		cmdLog()
	case "config":
		cmdConfig()
	default:
		usage()
	}
}

func detectGithubLogin() string {
	out, err := exec.Command("gh", "api", "user").Output()
	if err != nil {
		return ""
	}
	var u struct {
		Login string `json:"login"`
	}
	if json.Unmarshal(out, &u) != nil {
		return ""
	}
	return u.Login
}

// detectRepoRoots looks in the handful of places developers actually keep repos.
func detectRepoRoots() []string {
	home, _ := os.UserHomeDir()
	var roots []string
	for _, g := range []string{"code", "src", "dev", "projects", "work", "repos", "Developer", "github"} {
		p := filepath.Join(home, g)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			roots = append(roots, p)
		}
	}
	if len(roots) == 0 {
		return []string{home}
	}
	return roots
}

func cmdInit() {
	if pulse.ConfigExists() && flag("force") == "" {
		fmt.Printf("Config already exists at %s. Use --force to overwrite.\n", pulse.ConfigPath())
		return
	}
	cfg := pulse.DefaultConfig()
	cfg.User.GithubLogin = detectGithubLogin()
	cfg.RepoRoots = detectRepoRoots()
	cfg.Routines = []pulse.Routine{
		{Name: "Stand-up", At: "10:00", Days: []int{1, 2, 3, 4, 5}, Note: "what you shipped yesterday"},
		{Name: "Gym", At: "19:00", Days: []int{1, 3, 5}},
	}
	// Written out so the AI keys are discoverable in the file rather than
	// only in the docs. The key itself is deliberately left empty: the
	// environment is the recommended place for it.
	if flag("provider") != "" && flag("provider") != "true" {
		cfg.Phrasing.Provider = flag("provider")
	}
	if v := flag("api-url"); v != "" && v != "true" {
		cfg.Phrasing.APIURL = v
	}
	if v := flag("model"); v != "" && v != "true" {
		cfg.Phrasing.ModelName = v
	}
	if err := pulse.SaveConfig(cfg); err != nil {
		fail("could not write config: %v", err)
	}

	// Preserve installedAt if a run is already in flight, so porting the
	// implementation does not silently reset the 14-day clock.
	state := pulse.LoadState()
	if len(pulse.ReadNudges()) == 0 {
		state.InstalledAt = time.Now().UnixMilli()
	}
	if err := pulse.SaveState(state); err != nil {
		fail("could not write state: %v", err)
	}

	repos := pulse.DiscoverRepos(cfg.RepoRoots, 3)
	fmt.Println(bold("Pulse initialised."))
	fmt.Printf("  config      %s\n", pulse.ConfigPath())
	login := cfg.User.GithubLogin
	if login == "" {
		login = dim("not detected — run `gh auth login`, then edit config")
	}
	fmt.Printf("  github      %s\n", login)
	fmt.Printf("  repo roots  %s\n", strings.Join(cfg.RepoRoots, ", "))
	fmt.Printf("  repos found %d\n", len(repos))
	if name, err := pulse.PhraseProvider(cfg); err != nil {
		fmt.Printf("  phrasing    %s\n", dim("templates — "+err.Error()))
	} else if name != "" {
		fmt.Printf("  phrasing    %s\n", name)
	}
	fmt.Printf("\nNext: %s to see what it would say right now.\n", bold("pulse run --dry --now"))
}

func mustConfig() pulse.Config {
	cfg, err := pulse.LoadConfig()
	if err != nil {
		fail("%v", err)
	}
	return cfg
}

func printReasoning(r pulse.CycleResult, preview bool) {
	s := r.Signals
	dirty := 0
	for _, repo := range s.Repos {
		if repo.DirtyFiles > 0 {
			dirty++
		}
	}
	fmt.Println(bold("signals"))
	fmt.Printf("  repos         %d (%d dirty)\n", len(s.Repos), dirty)
	fmt.Printf("  my open PRs   %d\n", len(s.PRs))
	fmt.Printf("  review queue  %d\n", len(s.ReviewRequests))
	focus := fmt.Sprintf("%dm", s.FocusMinutes)
	if s.FocusRepo != "" {
		focus += " in " + s.FocusRepo
	}
	fmt.Printf("  focus         %s\n", focus)
	if !s.GithubOK {
		fmt.Println(dim("  github unavailable — local signals only"))
	}
	for _, e := range s.Errors {
		fmt.Println(dim("  ! " + e))
	}

	sorted := append([]pulse.Candidate(nil), r.Candidates...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority > sorted[j].Priority })
	fmt.Printf("\n%s (%d)\n", bold("candidates"), len(sorted))
	for _, c := range sorted {
		fmt.Printf("  %3d %-17s %s\n", c.Priority, c.Kind, c.Text)
	}

	suffix := ""
	if preview {
		suffix = dim("  (gates bypassed)")
	}
	fmt.Printf("\n%s  %s%s\n", bold("decision"), r.Decision.Reason, suffix)
	if preview && r.Decision.Winner != nil {
		fmt.Printf("  %s  %s\n", bold("would send"), r.Decision.Winner.Text)
	}
	for _, sup := range r.Decision.Suppressed {
		fmt.Println(dim(fmt.Sprintf("  suppressed %s: %s", sup.Candidate.Kind, sup.Reason)))
	}
}

func cmdRun() {
	cfg := mustConfig()
	dry := flag("dry") != ""
	preview := flag("now") != ""

	r, err := pulse.RunCycle(cfg, dry, preview)
	if err != nil {
		fail("cycle failed: %v", err)
	}

	verbose := flag("verbose") != ""
	if verbose || dry {
		printReasoning(r, preview && dry)
	}

	switch {
	case r.Sent != nil:
		fmt.Printf("\n%s [%s] %s\n", bold("sent"), r.Sent.ID[:6], r.Sent.Text)
		if r.Sent.Action != "" {
			fmt.Println(dim("  " + r.Sent.Action))
		}
		fmt.Println(dim("  phrased by " + r.Sent.PhrasedBy))
	case dry:
		fmt.Println(dim("\ndry run — nothing delivered"))
	case !verbose:
		fmt.Println(dim("silent: " + r.Decision.Reason))
	}
}

func cmdStart() {
	cfg := mustConfig()
	interval := flagInt("interval", 10)

	// Focus streaks are inferred from consecutive samples. Sampling slower than
	// the break gap means every sample looks like a break and focus never builds.
	maxInterval := int(pulse.BreakGap.Minutes()) / 2
	if interval > maxInterval {
		fmt.Println(dim(fmt.Sprintf(
			"note: --interval=%d exceeds %dm, so focus tracking will stay at 0. PR and routine nudges still work.",
			interval, maxInterval)))
	}
	fmt.Printf("Pulse running. Cycle every %dm. Ctrl-C to stop.\n", interval)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	tick := func() {
		r, err := pulse.RunCycle(cfg, false, false)
		stamp := time.Now().Format("15:04:05")
		if err != nil {
			fmt.Println(dim(fmt.Sprintf("%s cycle failed: %v", stamp, err)))
			return
		}
		if r.Sent != nil {
			fmt.Printf("%s sent [%s] %s\n", dim(stamp), r.Sent.ID[:6], r.Sent.Text)
		} else {
			fmt.Println(dim(stamp + " silent: " + r.Decision.Reason))
		}
	}

	tick()
	ticker := time.NewTicker(time.Duration(interval) * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			tick()
		case <-stop:
			fmt.Println("\nstopped")
			return
		}
	}
}

func cmdDaemon() {
	if flag("uninstall") != "" {
		if pulse.UninstallAgent() {
			fmt.Println("Pulse background agent removed.")
		} else {
			fmt.Println("No agent was installed.")
		}
		return
	}
	interval := flagInt("interval", 10)
	p, err := pulse.InstallAgent(interval)
	if err != nil {
		fail("%v", err)
	}
	fmt.Println(bold("Pulse installed as a background agent."))
	fmt.Printf("  plist     %s\n", p)
	fmt.Printf("  interval  every %d minutes, starting at login\n", interval)
	fmt.Printf("  logs      %s\n\n", filepath.Join(pulse.Home(), "agent.log"))
	fmt.Println(dim("Remove with: pulse daemon --uninstall"))
}

func cmdStatus() {
	cfg := mustConfig()
	now := time.Now()
	state := pulse.LoadState()
	s := pulse.CollectSignals(cfg, &state, now)
	_ = pulse.SaveState(state)
	candidates := pulse.GenerateCandidates(cfg, s, now)
	today := pulse.NudgesSince(pulse.StartOfToday(now))

	fmt.Println(bold("\nnow"))
	if s.FocusMinutes > 0 {
		fmt.Printf("  focus         %dm in %s\n", s.FocusMinutes, s.FocusRepo)
	} else {
		fmt.Printf("  focus         %s\n", dim("idle"))
	}
	red := 0
	for _, p := range s.PRs {
		if p.Checks == "failing" {
			red++
		}
	}
	redNote := ""
	if red > 0 {
		redNote = fmt.Sprintf(" (%d red)", red)
	}
	fmt.Printf("  open PRs      %d%s\n", len(s.PRs), redNote)
	fmt.Printf("  reviews owed  %d\n", len(s.ReviewRequests))

	var dirty []string
	for _, r := range s.Repos {
		if r.DirtyLines > 0 {
			dirty = append(dirty, fmt.Sprintf("%s:%d", r.Name, r.DirtyLines))
		}
	}
	if len(dirty) == 0 {
		fmt.Printf("  dirty repos   %s\n", dim("none"))
	} else {
		fmt.Printf("  dirty repos   %s\n", strings.Join(dirty, ", "))
	}
	quiet := "no"
	if pulse.InQuietHours(cfg, now) {
		quiet = "yes"
	}
	fmt.Printf("  quiet hours   %s  %s\n", quiet, dim(fmt.Sprintf("(%s–%s)", cfg.Quiet.Start, cfg.Quiet.End)))
	bg := dim("not installed — run `pulse daemon`")
	if pulse.AgentInstalled() {
		bg = "installed"
	}
	fmt.Printf("  background    %s\n", bg)

	// Surface phrasing state here: a bad key or URL otherwise degrades to
	// templates silently, and you would never learn the AI half was dead.
	switch name, err := pulse.PhraseProvider(cfg); {
	case err != nil:
		fmt.Printf("  phrasing      %s\n", dim("templates — "+err.Error()))
	case name == "":
		fmt.Printf("  phrasing      %s\n", dim("templates (useLLM is off)"))
	default:
		fmt.Printf("  phrasing      %s\n", name)
	}

	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Priority > candidates[j].Priority })
	plural := "s"
	if len(candidates) == 1 {
		plural = ""
	}
	fmt.Printf("\n%s (%d candidate%s)\n", bold("queued"), len(candidates), plural)
	for _, c := range candidates {
		fmt.Printf("  %s %s\n", dim(fmt.Sprintf("%3d", c.Priority)), c.Text)
	}

	fmt.Printf("\n%s %d/%d nudges\n", bold("today"), len(today), cfg.MaxNudgesPerDay)
	for _, n := range today {
		mark := dim("no response")
		if n.Response != nil {
			mark = *n.Response
		}
		fmt.Printf("  %s [%s] %s %s\n",
			dim(time.UnixMilli(n.SentAt).Format("15:04:05")), n.ID[:6], n.Text, dim("— "+mark))
	}
	fmt.Println()
}

func cmdRespond(cmd string) {
	if len(args) == 0 {
		fail("Usage: pulse %s <nudge-id>", cmd)
	}
	updated, err := pulse.UpdateNudge(args[0], cmd, time.Now().UnixMilli())
	if err != nil {
		fail("could not update log: %v", err)
	}
	if updated == nil {
		fail("No nudge matching %q.", args[0])
	}
	fmt.Printf("%s: %s\n", cmd, updated.Text)
	if cmd == "ack" && updated.Action != "" {
		pulse.OpenAction(updated.Action)
	}
}

func cmdMute() {
	cfg := mustConfig()
	hours := flagInt("hours", 0)

	if hours > 0 || flag("all") != "" {
		if hours == 0 {
			hours = 24
		}
		state := pulse.LoadState()
		until := time.Now().Add(time.Duration(hours) * time.Hour).UnixMilli()
		state.MutedUntil = &until
		if err := pulse.SaveState(state); err != nil {
			fail("could not save state: %v", err)
		}
		fmt.Printf("Muted until %s.\n", time.UnixMilli(until).Format("2 Jan 15:04"))
		return
	}

	if len(args) == 0 {
		fail("Usage: pulse mute <kind> | pulse mute --hours=4 | pulse mute --all")
	}
	kind := pulse.NudgeKind(args[0])
	for _, k := range cfg.MutedKinds {
		if k == kind {
			fmt.Printf("%q is already muted.\n", kind)
			return
		}
	}
	cfg.MutedKinds = append(cfg.MutedKinds, kind)
	if err := pulse.SaveConfig(cfg); err != nil {
		fail("could not save config: %v", err)
	}
	var names []string
	for _, k := range cfg.MutedKinds {
		names = append(names, string(k))
	}
	fmt.Printf("Muted %q. Now muted: %s\n", kind, strings.Join(names, ", "))
}

func cmdUnmute() {
	cfg := mustConfig()
	state := pulse.LoadState()
	state.MutedUntil = nil
	if err := pulse.SaveState(state); err != nil {
		fail("could not save state: %v", err)
	}

	if len(args) > 0 {
		kind := pulse.NudgeKind(args[0])
		var kept []pulse.NudgeKind
		for _, k := range cfg.MutedKinds {
			if k != kind {
				kept = append(kept, k)
			}
		}
		cfg.MutedKinds = kept
		if err := pulse.SaveConfig(cfg); err != nil {
			fail("could not save config: %v", err)
		}
		fmt.Printf("Unmuted %q.\n", kind)
		return
	}
	cfg.MutedKinds = nil
	if err := pulse.SaveConfig(cfg); err != nil {
		fail("could not save config: %v", err)
	}
	fmt.Println("Unmuted everything.")
}

func cmdMetrics() {
	m := pulse.ComputeMetrics(pulse.LoadState(), pulse.ReadNudges(), time.Now())
	fmt.Printf("\n%s\n", bold("Pulse — the only metric that matters"))
	fmt.Printf("  day             %d of 14\n", m.DaysInstalled)
	fmt.Printf("  nudges sent     %d (%.1f/active day)\n", m.TotalNudges, m.NudgesPerActiveDay)
	fmt.Printf("  engaged         %.0f%%  %s\n", m.EngagedRate*100, dim("(ack or dismiss)"))
	fmt.Printf("  acked           %.0f%%\n", m.AckRate*100)
	fmt.Printf("  ignored         %.0f%%\n", m.IgnoredRate*100)
	if len(m.ByKind) > 0 {
		fmt.Printf("\n%s\n", bold("by kind"))
		fmt.Println(dim("  kind              sent  ack  dismiss  ignored"))
		for _, k := range m.ByKind {
			fmt.Printf("  %-17s %4d %4d %8d %8d\n", k.Kind, k.Sent, k.Ack, k.Dismiss, k.Ignored)
		}
	}
	fmt.Printf("\n  %s  %s\n\n", bold("verdict"), m.Verdict)
}

func cmdLog() {
	for _, n := range pulse.ReadNudges() {
		resp := "-"
		if n.Response != nil {
			resp = *n.Response
		}
		fmt.Printf("%s  %-17s %-8s %s\n",
			time.UnixMilli(n.SentAt).Format("2006-01-02 15:04:05"), n.Kind, resp, n.Text)
	}
}

func cmdConfig() {
	fmt.Println(pulse.ConfigPath())
	if data, err := os.ReadFile(pulse.ConfigPath()); err == nil {
		fmt.Print(string(data))
	}
}

func usage() {
	fmt.Print(bold("pulse") + ` — a context-aware assistant for developers

  pulse init                   detect your repos and GitHub identity, write config
        [--provider=api] [--api-url=URL] [--model=NAME]
  pulse run [--dry] [--now]    run one cycle  (--dry shows reasoning, sends nothing)
                               --now also ignores quiet hours, for previewing
  pulse start [--interval=10]  run continuously in this terminal
  pulse daemon [--interval=10] install as a background agent (survives reboots)
  pulse daemon --uninstall

  pulse status                 what Pulse sees right now, and what it's holding back

  pulse ack <id>               you acted on it (opens the PR if there is one)
  pulse dismiss <id>           you read it, it wasn't useful
  pulse snooze <id>            later
  pulse mute <kind>            stop a whole category
  pulse mute --hours=4         silence everything for a while
  pulse unmute [kind]

  pulse metrics                day-14 survival: the number this bet rests on
  pulse log                    every nudge ever sent
  pulse config                 print config path and contents
`)
}

// Command pulse is a context-aware assistant for developers. It reads your real
// work state — git, GitHub, edit activity — and speaks only when the evidence
// justifies an interruption.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pulse/internal/pulse"
	"pulse/internal/pulse/doctor"
	"pulse/internal/pulse/llm"
	"pulse/internal/pulse/onboard"
	"pulse/internal/pulse/tui"
	"pulse/internal/pulse/ui"
)

// Thin aliases so call sites stay readable; all styling lives in the ui package.
var (
	dim  = ui.Dim
	bold = ui.Bold
	grey = ui.Grey
)

// version is set at build time: -ldflags "-X main.version=v0.1.0". It stays
// "dev" for a plain `go build`, so an unreleased binary never claims a tag.
var version = "dev"

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
	case "browse", "repos", "tui":
		cmdBrowse()
	case "doctor":
		cmdDoctor()
	case "version", "--version", "-v":
		fmt.Printf("pulse %s\n", version)
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

// applyInitFlags lets a scripted install override anything the questionnaire
// would have asked about the provider.
func applyInitFlags(cfg *pulse.Config) {
	if v := flag("provider"); v != "" && v != "true" {
		cfg.Phrasing.Provider, cfg.Phrasing.UseLLM = v, true
	}
	if v := flag("api-url"); v != "" && v != "true" {
		cfg.Phrasing.APIURL = v
	}
	if v := flag("model"); v != "" && v != "true" {
		cfg.Phrasing.ModelName = v
	}
}

func orString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
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

	// Re-running setup is editing, not starting over. Seed from the existing
	// config so answers default to what is already configured — and carry the
	// credential across untouched, since the questionnaire deliberately never
	// asks for one and would otherwise silently delete it.
	existing, existingErr := pulse.LoadConfig()
	if existingErr == nil {
		cfg.User.GithubLogin = orString(existing.User.GithubLogin, cfg.User.GithubLogin)
		if len(existing.RepoRoots) > 0 {
			cfg.RepoRoots = existing.RepoRoots
		}
		cfg.Phrasing = existing.Phrasing
		cfg.Quiet = existing.Quiet
		cfg.Thresholds = existing.Thresholds
		cfg.RepoScanDepth = existing.RepoScanDepth
	}

	// Flags win over both the questionnaire and the defaults, so a scripted
	// install can set the AI up without answering anything.
	applyInitFlags(&cfg)

	// Ask, unless there is nobody to ask: --yes, or stdin is not a terminal.
	interactive := flag("yes") == "" && isTTY(os.Stdin)
	if interactive {
		keepKey := cfg.Phrasing.APIKey
		cfg = onboard.RunOpts(os.Stdin, os.Stdout, cfg, true)
		if cfg.Phrasing.APIKey == "" {
			cfg.Phrasing.APIKey = keepKey
		}
		applyInitFlags(&cfg) // flags still win after the questionnaire
	} else {
		cfg.Routines = []pulse.Routine{
			{Name: "Stand-up", At: "10:00", Days: []int{1, 2, 3, 4, 5}, Note: "what you shipped yesterday"},
			{Name: "Gym", At: "19:00", Days: []int{1, 3, 5}},
		}
	}

	if err := pulse.SaveConfig(cfg); err != nil {
		fail("could not write config: %v", err)
	}

	// Preserve installedAt if a run is already in flight, so reconfiguring
	// does not silently reset the 14-day clock.
	state := pulse.LoadState()
	if len(pulse.ReadNudges()) == 0 {
		state.InstalledAt = time.Now().UnixMilli()
	}
	if err := pulse.SaveState(state); err != nil {
		fail("could not write state: %v", err)
	}

	repos := pulse.DiscoverRepos(cfg.RepoRoots, cfg.RepoScanDepth)
	fmt.Println()
	fmt.Println(bold("Pulse is set up."))
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

	fmt.Println()
	fmt.Println(ui.Header("what you told it"))
	for _, row := range onboard.Summary(cfg) {
		fmt.Println("  " + row)
	}

	fmt.Printf("\n%s\n", bold("Next:"))
	fmt.Printf("  %s   %s\n", ui.Pad("pulse run --dry --now", 24), dim("see what it would say right now"))
	fmt.Printf("  %s   %s\n", ui.Pad("pulse daemon", 24), dim("run it in the background, surviving reboots"))
	fmt.Printf("  %s   %s\n", ui.Pad("pulse browse", 24), dim("the dashboard"))
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
	fmt.Println(ui.Header("signals"))
	fmt.Println(ui.KV("repos", fmt.Sprintf("%d %s", len(s.Repos), grey(fmt.Sprintf("(%d dirty)", dirty)))))
	fmt.Println(ui.KV("my open PRs", fmt.Sprintf("%d", len(s.PRs))))
	fmt.Println(ui.KV("review queue", fmt.Sprintf("%d", len(s.ReviewRequests))))
	focus := fmt.Sprintf("%dm", s.FocusMinutes)
	if s.FocusRepo != "" {
		focus += " in " + s.FocusRepo
	}
	fmt.Println(ui.KV("focus", focus))
	if !s.GithubOK {
		fmt.Println("  " + ui.Symbol("warn") + " " + dim("github unavailable — local signals only"))
	}
	for _, e := range s.Errors {
		fmt.Println("  " + ui.Symbol("warn") + " " + dim(e))
	}

	sorted := append([]pulse.Candidate(nil), r.Candidates...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Priority > sorted[j].Priority })
	fmt.Printf("\n%s %s\n", ui.Header("candidates"), grey(fmt.Sprintf("(%d)", len(sorted))))
	for _, c := range sorted {
		sev := ui.Severity(c.Priority)
		fmt.Printf("  %s %s %s %s\n",
			ui.SeveritySymbol(c.Priority),
			sev(fmt.Sprintf("%3d", c.Priority)),
			ui.Pad(grey(string(c.Kind)), 17),
			ui.Truncate(c.Text, 72))
	}

	suffix := ""
	if preview {
		suffix = "  " + ui.Amber("(gates bypassed)")
	}
	fmt.Printf("\n%s %s%s\n", ui.Header("decision"), r.Decision.Reason, suffix)
	if preview && r.Decision.Winner != nil {
		fmt.Printf("  %s %s\n", ui.Symbol("arrow"), ui.Bold(r.Decision.Winner.Text))
	}
	if len(r.Decision.Suppressed) > 0 {
		fmt.Printf("\n%s %s\n", ui.Header("suppressed"), grey(fmt.Sprintf("(%d)", len(r.Decision.Suppressed))))
		for _, sup := range r.Decision.Suppressed {
			fmt.Printf("  %s %s %s\n",
				ui.Symbol("quiet"),
				ui.Pad(grey(string(sup.Candidate.Kind)), 17),
				dim(sup.Reason))
		}
	}
}

func cmdRun() {
	cfg := mustConfig()
	dry := flag("dry") != ""
	preview := flag("now") != ""

	phraseDry := flag("phrase") != ""
	if phraseDry && !dry {
		fail("--phrase only applies to a dry run; use: pulse run --dry --now --phrase")
	}
	r, err := pulse.RunCycleOpts(cfg, dry, preview, phraseDry)
	if err != nil {
		fail("cycle failed: %v", err)
	}

	verbose := flag("verbose") != ""
	if verbose || dry {
		printReasoning(r, preview && dry)
	}

	switch {
	case r.Sent != nil:
		fmt.Printf("\n%s %s %s\n", ui.Green("▸ sent"), grey(r.Sent.ID[:6]), ui.Bold(r.Sent.Text))
		if r.Sent.Action != "" {
			fmt.Println("  " + grey(r.Sent.Action))
		}
		fmt.Println("  " + dim("phrased by "+r.Sent.PhrasedBy))
	case r.Phrased != "":
		fmt.Printf("\n%s %s\n", ui.Cyan("▸ would say"), ui.Bold(r.Phrased))
		fmt.Println("  " + dim("worded by "+phrasedLabel(r.PhrasedBy)+" · not delivered, not logged"))
	case dry:
		fmt.Println(dim("\ndry run — nothing delivered"))
	case !verbose:
		fmt.Println(dim("silent: " + r.Decision.Reason))
	}
}

// phrasedLabel keeps attribution in product terms, matching every other surface.
func phrasedLabel(by string) string {
	if by == "llm" {
		return llm.DisplayName
	}
	return "template"
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

func cmdDoctor() {
	cfg, cfgErr := pulse.LoadConfig()
	rep := doctor.Run(cfg, cfgErr)

	// The notification check needs a human, so it is interactive and skipped
	// when there is nobody to ask.
	if runtime.GOOS == "darwin" && flag("no-notify") == "" && isTTY(os.Stdin) {
		rep.Results = append(rep.Results, notificationCheck())
	}

	doctor.Render(rep, os.Stdout)

	fmt.Println()
	switch {
	case rep.Failed() > 0:
		fmt.Printf("  %s %s\n\n", ui.Red(ui.Symbol("crit")),
			bold(fmt.Sprintf("%d problem(s) will stop Pulse working. Fix those first.", rep.Failed())))
		os.Exit(1)
	case rep.Warned() > 0:
		fmt.Printf("  %s %s\n\n", ui.Amber(ui.Symbol("warn")),
			fmt.Sprintf("%d thing(s) degraded, but Pulse will run.", rep.Warned()))
	default:
		fmt.Printf("  %s %s\n\n", ui.Green(ui.Symbol("ok")), bold("Everything checks out."))
	}
}

// notificationCheck posts a banner and asks whether it appeared. This is the
// only check that needs a person: macOS attributes the banner to Script
// Editor, and if that app lacks permission every nudge is logged and never
// shown — an install that looks dead while working perfectly.
func notificationCheck() doctor.Result {
	fmt.Printf("\n  %s\n", bold("Sending a test notification…"))
	if err := doctor.NotificationCheck(); err != nil {
		return doctor.Result{Name: "notifications", Status: 2, Detail: err.Error()}
	}

	fmt.Printf("  did a banner appear? %s ", dim("[y/N]"))
	sc := bufio.NewScanner(os.Stdin)
	answered := false
	if sc.Scan() {
		v := strings.ToLower(strings.TrimSpace(sc.Text()))
		answered = v == "y" || v == "yes"
	}

	// Remember it: an unconfirmed install may be logging nudges nobody sees,
	// which would make the whole 14-day measurement meaningless.
	state := pulse.LoadState()
	state.NotificationsOK = &answered
	_ = pulse.SaveState(state)

	if answered {
		return doctor.Result{Name: "notifications", Status: 0, Detail: "banner confirmed"}
	}
	return doctor.Result{
		Name:   "notifications",
		Status: 2,
		Detail: "no banner — nudges are being logged but never shown",
		Fix:    "System Settings → Notifications → Script Editor → Allow, style: Alerts",
	}
}

func cmdBrowse() {
	cfg := mustConfig()
	if err := tui.Run(cfg); err != nil {
		fail("browser failed: %v", err)
	}
}

func cmdStatus() {
	cfg := mustConfig()
	now := time.Now()
	state := pulse.LoadState()
	s := pulse.CollectSignals(cfg, &state, now)
	_ = pulse.SaveState(state)
	candidates := pulse.GenerateCandidates(cfg, s, now)
	today := pulse.NudgesSince(pulse.StartOfToday(now))

	fmt.Println()
	fmt.Println(ui.Header("now"))
	switch {
	case s.FocusMinutes > 0 && s.FocusRepo != "":
		fmt.Println(ui.KV("focus", ui.Cyan(fmt.Sprintf("%dm", s.FocusMinutes))+" in "+s.FocusRepo))
	case s.FocusMinutes > 0:
		// Streak held inside the grace gap: no repo is active this instant.
		fmt.Println(ui.KV("focus", fmt.Sprintf("%dm ", s.FocusMinutes)+dim("(paused)")))
	default:
		fmt.Println(ui.KV("focus", dim("idle")))
	}
	red := 0
	for _, p := range s.PRs {
		if p.Checks == "failing" {
			red++
		}
	}
	prs := fmt.Sprintf("%d", len(s.PRs))
	if red > 0 {
		prs += " " + ui.Red(fmt.Sprintf("(%d red)", red))
	}
	fmt.Println(ui.KV("open PRs", prs))
	owed := fmt.Sprintf("%d", len(s.ReviewRequests))
	if len(s.ReviewRequests) > 0 {
		owed = ui.Amber(owed)
	}
	fmt.Println(ui.KV("reviews owed", owed))

	var dirty []string
	for _, r := range s.Repos {
		if r.DirtyLines > 0 {
			dirty = append(dirty, fmt.Sprintf("%s:%d", r.Name, r.DirtyLines))
		}
	}
	if len(dirty) == 0 {
		fmt.Println(ui.KV("dirty repos", dim("none")))
	} else {
		fmt.Println(ui.KV("dirty repos", strings.Join(dirty, ", ")))
	}

	quiet := ui.Green("no")
	if pulse.InQuietHours(cfg, now) {
		quiet = ui.Amber("yes")
	}
	fmt.Println(ui.KV("quiet hours", quiet+"  "+dim(fmt.Sprintf("(%s–%s)", cfg.Quiet.Start, cfg.Quiet.End))))

	bg := ui.Symbol("quiet") + " " + dim("not installed — run `pulse daemon`")
	if pulse.AgentInstalled() {
		bg = ui.Symbol("ok") + " running"
	}
	fmt.Println(ui.KV("background", bg))

	// Surface phrasing state here: a bad key or URL otherwise degrades to
	// templates silently, and you would never learn the AI half was dead.
	// An unrecognised field is silently ignored by the config loader, so a typo
	// or an env-var name written as a field looks applied but does nothing.
	if unknown := pulse.UnknownKeys(); len(unknown) > 0 {
		fmt.Println(ui.KV("config", ui.Amber(ui.Symbol("warn")+" unknown keys ignored: "+strings.Join(unknown, ", "))))
	}

	if flag("check") != "" {
		name, err := pulse.VerifyPhrasing(cfg)
		if err != nil {
			fmt.Println(ui.KV("phrasing", ui.Red(ui.Symbol("crit")+" "+name+" — "+err.Error())))
		} else {
			fmt.Println(ui.KV("phrasing", ui.Green(ui.Symbol("ok")+" "+name+" — live round-trip ok")))
		}
		fmt.Println()
		return
	}

	switch name, err := pulse.PhraseProvider(cfg); {
	case err != nil:
		fmt.Println(ui.KV("phrasing", ui.Symbol("warn")+" "+dim("templates — "+err.Error())))
	case name == "":
		fmt.Println(ui.KV("phrasing", dim("templates (useLLM is off)")))
	default:
		fmt.Println(ui.KV("phrasing", ui.Symbol("ok")+" "+ui.Cyan(name)))
	}

	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Priority > candidates[j].Priority })
	plural := "s"
	if len(candidates) == 1 {
		plural = ""
	}
	fmt.Printf("\n%s %s\n", ui.Header("queued"), grey(fmt.Sprintf("(%d candidate%s held back)", len(candidates), plural)))
	if len(candidates) == 0 {
		fmt.Println("  " + dim("nothing worth saying"))
	}
	for _, c := range candidates {
		fmt.Printf("  %s %s %s\n",
			ui.SeveritySymbol(c.Priority),
			ui.Severity(c.Priority)(ui.Pad(string(c.Kind), 17)),
			ui.Truncate(c.Text, 78))
	}

	fmt.Printf("\n%s %s %s\n", ui.Header("today"),
		ui.Bar(float64(len(today))/float64(cfg.MaxNudgesPerDay), 12, ui.Cyan),
		grey(fmt.Sprintf("%d/%d sent", len(today), cfg.MaxNudgesPerDay)))
	if len(today) == 0 {
		fmt.Println("  " + dim("silent so far"))
	}
	for _, n := range today {
		mark := ui.Symbol("quiet") + " " + dim("no response")
		if n.Response != nil {
			switch *n.Response {
			case "ack":
				mark = ui.Symbol("ok") + " " + ui.Green("acked")
			case "dismiss":
				mark = ui.Symbol("warn") + " " + ui.Amber("dismissed")
			default:
				mark = ui.Symbol("quiet") + " " + dim(*n.Response)
			}
		}
		fmt.Printf("  %s %s %s %s\n",
			grey(time.UnixMilli(n.SentAt).Format("15:04")),
			grey(n.ID[:6]),
			ui.Pad(ui.Truncate(n.Text, 56), 56),
			mark)
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
	state := pulse.LoadState()
	m := pulse.ComputeMetrics(state, pulse.ReadNudges(), time.Now())
	fmt.Printf("\n%s\n", ui.Header("the only metric that matters"))

	dayFrac := float64(m.DaysInstalled) / 14
	fmt.Println(ui.KV("day", fmt.Sprintf("%s  %s",
		ui.Bar(dayFrac, 14, ui.Cyan), grey(fmt.Sprintf("%d of 14", m.DaysInstalled)))))
	fmt.Println(ui.KV("nudges sent", fmt.Sprintf("%d %s",
		m.TotalNudges, grey(fmt.Sprintf("(%.1f per active day)", m.NudgesPerActiveDay)))))

	// Engagement is the headline: colour it by whether it clears the bar.
	engagedColour := ui.Red
	if m.EngagedRate >= 0.4 {
		engagedColour = ui.Green
	} else if m.EngagedRate >= 0.25 {
		engagedColour = ui.Amber
	}
	fmt.Println(ui.KV("engaged", fmt.Sprintf("%s  %s  %s",
		ui.Bar(m.EngagedRate, 14, engagedColour),
		engagedColour(fmt.Sprintf("%3.0f%%", m.EngagedRate*100)),
		grey("(ack or dismiss · 40% is the bar)"))))
	fmt.Println(ui.KV("acked", fmt.Sprintf("%s  %s",
		ui.Bar(m.AckRate, 14, ui.Green), grey(fmt.Sprintf("%3.0f%%", m.AckRate*100)))))
	fmt.Println(ui.KV("ignored", fmt.Sprintf("%s  %s",
		ui.Bar(m.IgnoredRate, 14, ui.Grey), grey(fmt.Sprintf("%3.0f%%", m.IgnoredRate*100)))))

	if len(m.ByKind) > 0 {
		fmt.Printf("\n%s\n", ui.Header("by kind"))
		fmt.Println("  " + grey(ui.Pad("kind", 18)+ui.Pad("sent", 6)+ui.Pad("ack", 6)+ui.Pad("dismiss", 9)+"ignored"))
		for _, k := range m.ByKind {
			fmt.Printf("  %s%s%s%s%s\n",
				ui.Pad(string(k.Kind), 18),
				ui.Pad(fmt.Sprintf("%d", k.Sent), 6),
				ui.Pad(ui.Green(fmt.Sprintf("%d", k.Ack)), 6),
				ui.Pad(ui.Amber(fmt.Sprintf("%d", k.Dismiss)), 9),
				grey(fmt.Sprintf("%d", k.Ignored)))
		}
	}

	// The repository breakdown needs live signals, so it is opt-out: `--no-repos`
	// keeps `pulse metrics` instant and offline when only the verdict matters.
	if flag("no-repos") == "" {
		if cfg, err := pulse.LoadConfig(); err == nil {
			printRepoBreakdown(cfg, &state)
		}
	}

	fmt.Println()
	fmt.Println(ui.Box("verdict", []string{m.Verdict}))
	fmt.Println()
}

// printRepoBreakdown lists every discovered repo joined to its GitHub state.
// It takes a state copy and never saves it: reporting must not advance the
// focus streak that the daemon is tracking.
func printRepoBreakdown(cfg pulse.Config, state *pulse.State) {
	local := *state
	s := pulse.CollectSignals(cfg, &local, time.Now())

	// Follow renames and transfers before matching: a clone whose origin still
	// points at an old org would otherwise look like it was never cloned.
	moved := pulse.CanonicalizeRepos(s.Repos)
	b := pulse.BuildRepoBreakdown(s, cfg.Thresholds.StalePRHours)

	fmt.Printf("\n%s %s\n", ui.Header("repositories"),
		grey(fmt.Sprintf("(%d local · %s · %s owed)",
			len(b.Repos),
			plural(b.TotalPRs, "open PR", "open PRs"),
			plural(b.TotalReviews, "review", "reviews"))))

	if !s.GithubOK {
		fmt.Println("  " + ui.Symbol("warn") + " " +
			dim("github unavailable — PR columns are blank, local state is accurate"))
	}
	if len(b.Repos) == 0 {
		fmt.Println("  " + dim("no repos found — check repoRoots in your config"))
		return
	}

	fmt.Println("  " + grey(
		ui.Pad("repo", 26)+ui.Pad("branch", 20)+ui.Pad("dirty", 8)+
			ui.Pad("PRs", 6)+ui.Pad("red", 5)+ui.Pad("stale", 7)+"review"))

	shown := 0
	for _, r := range b.Repos {
		// Quiet repos are counted in the header but not printed; a 15-row table
		// of zeros buries the three rows that matter.
		if r.OpenPRs == 0 && r.ReviewsOwed == 0 && r.DirtyLines == 0 {
			continue
		}
		shown++

		// Build the label as plain text and truncate it as a unit. Styling any
		// part of it first risks slicing an escape sequence, which corrupts the
		// colour and every column after it on the line.
		label := r.Name
		if r.Remote != "" {
			repoName := r.Remote[strings.LastIndex(r.Remote, "/")+1:]
			if !strings.EqualFold(repoName, r.Name) {
				// Surface the real repository when the directory differs.
				label += " →" + repoName
			}
		}
		if r.Clones > 1 {
			// Warn that this row's PR counts are repeated on a sibling row.
			label += fmt.Sprintf(" ×%d", r.Clones)
		}
		name := ui.Truncate(label, 25)

		dirty := grey("·")
		if r.DirtyLines > 0 {
			dirty = ui.Amber(fmt.Sprintf("%d", r.DirtyLines))
		}
		prs, red, stale, rev := grey("·"), grey("·"), grey("·"), grey("·")
		if !r.Tracked {
			prs, red, stale, rev = grey("?"), grey("?"), grey("?"), grey("?")
		} else {
			if r.OpenPRs > 0 {
				prs = fmt.Sprintf("%d", r.OpenPRs)
			}
			if r.RedPRs > 0 {
				red = ui.Red(fmt.Sprintf("%d", r.RedPRs))
			}
			if r.StalePRs > 0 {
				stale = ui.Amber(fmt.Sprintf("%d", r.StalePRs))
			}
			if r.ReviewsOwed > 0 {
				rev = ui.Amber(fmt.Sprintf("%d", r.ReviewsOwed))
			}
		}

		fmt.Printf("  %s%s%s%s%s%s%s\n",
			ui.Pad(name, 26),
			ui.Pad(grey(ui.Truncate(r.Branch, 19)), 20),
			ui.Pad(dirty, 8), ui.Pad(prs, 6), ui.Pad(red, 5), ui.Pad(stale, 7), rev)
	}

	if quiet := len(b.Repos) - shown; quiet > 0 {
		fmt.Println("  " + grey(fmt.Sprintf("+ %d clean repos with nothing open", quiet)))
	}
	if untracked := countUntracked(b.Repos); untracked > 0 {
		fmt.Println("  " + grey(fmt.Sprintf("%s %d repos have no usable origin, shown as ?", ui.Symbol("quiet"), untracked)))
	}

	// Stale origins are worth naming: the local git remote is now wrong, and
	// fetch/push still work only because GitHub redirects.
	if len(moved) > 0 {
		keys := make([]string, 0, len(moved))
		for was := range moved {
			keys = append(keys, was)
		}
		sort.Strings(keys) // map order is random; the list must not reshuffle

		fmt.Printf("\n  %s %s\n", ui.Symbol("warn"),
			ui.Amber(plural(len(moved), "repo has", "repos have")+" moved; the local remote is stale"))
		const showMoved = 3
		for i, was := range keys {
			if i == showMoved {
				fmt.Println("    " + grey(fmt.Sprintf("+ %d more", len(keys)-showMoved)))
				break
			}
			fmt.Printf("    %s %s %s\n", grey(was), grey("→"), moved[was])
		}
		// Fetch and push still work because GitHub redirects, which is exactly
		// why this goes unnoticed. Name the fix rather than just the problem.
		fmt.Println("    " + grey("fix: git -C <repo> remote set-url origin <new-url>"))
	}

	// PRs whose repo is not cloned here, so the totals visibly add up.
	if len(b.Orphans) > 0 {
		names := make([]string, 0, len(b.Orphans))
		for repo, n := range b.Orphans {
			short := repo[strings.LastIndex(repo, "/")+1:]
			names = append(names, fmt.Sprintf("%s(%d)", short, n))
		}
		sort.Strings(names)
		fmt.Printf("\n  %s %s\n", ui.Symbol("quiet"),
			grey("open PRs in repos not cloned here: "+strings.Join(names, ", ")))
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func countUntracked(rs []pulse.RepoStat) int {
	n := 0
	for _, r := range rs {
		if !r.Tracked {
			n++
		}
	}
	return n
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

  pulse init [--yes]           set up, asking about your day and routines
                               --yes skips the questions and takes defaults
        [--provider=api] [--api-url=URL] [--model=NAME]
  pulse run [--dry] [--now]    run one cycle  (--dry shows reasoning, sends nothing)
        [--phrase]             on a dry run, also show the pulse-ai wording
                               --now also ignores quiet hours, for previewing
  pulse start [--interval=10]  run continuously in this terminal
  pulse daemon [--interval=10] install as a background agent (survives reboots)
  pulse daemon --uninstall

  pulse status [--check]       what Pulse sees right now (--check tests the AI live)
  pulse doctor                 diagnose the install and name the fix for anything broken, and what it's holding back
  pulse browse                 dashboard: repositories · metrics · config

  pulse ack <id>               you acted on it (opens the PR if there is one)
  pulse dismiss <id>           you read it, it wasn't useful
  pulse snooze <id>            later
  pulse mute <kind>            stop a whole category
  pulse mute --hours=4         silence everything for a while
  pulse unmute [kind]

  pulse metrics [--no-repos]   day-14 survival, plus every repo and its open PRs
                               --no-repos keeps it instant and offline
  pulse log                    every nudge ever sent
  pulse config                 print config path and contents
  pulse version
`)
}

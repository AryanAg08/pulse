// Package doctor diagnoses a Pulse install.
//
// Every check here exists because the failure it catches is silent: Pulse
// keeps running and simply never says anything, which is indistinguishable
// from a quiet day. That ambiguity is the single biggest risk to the 14-day
// experiment, and to anyone else being handed this.
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"pulse/internal/pulse"
	"pulse/internal/pulse/ui"
)

type status int

const (
	statusOK status = iota
	statusWarn
	statusFail
	statusUnknown
)

// Result is one diagnosis. Fix is the exact command or setting that resolves
// it — a check that reports a problem without naming the remedy just moves the
// confusion somewhere else.
type Result struct {
	Name   string
	Status status
	Detail string
	Fix    string
}

func (r Result) symbol() string {
	switch r.Status {
	case statusOK:
		return ui.Symbol("ok")
	case statusWarn:
		return ui.Symbol("warn")
	case statusFail:
		return ui.Symbol("crit")
	default:
		return ui.Symbol("quiet")
	}
}

// Report is the full diagnosis plus whether anything is actually broken.
type Report struct {
	Results []Result
}

func (rep Report) Failed() int {
	n := 0
	for _, r := range rep.Results {
		if r.Status == statusFail {
			n++
		}
	}
	return n
}

func (rep Report) Warned() int {
	n := 0
	for _, r := range rep.Results {
		if r.Status == statusWarn {
			n++
		}
	}
	return n
}

func ok(name, detail string) Result   { return Result{name, statusOK, detail, ""} }
func warn(name, d, fix string) Result { return Result{name, statusWarn, d, fix} }
func fail(name, d, fix string) Result { return Result{name, statusFail, d, fix} }

// Run performs every check. It never mutates anything except, optionally, the
// notification answer the caller records.
func Run(cfg pulse.Config, cfgErr error) Report {
	var rs []Result

	rs = append(rs, checkBinary())
	rs = append(rs, checkConfig(cfgErr))
	if cfgErr == nil {
		rs = append(rs, checkUnknownKeys())
		rs = append(rs, checkRepos(cfg))
	}
	rs = append(rs, checkGH()...)
	if cfgErr == nil {
		rs = append(rs, checkPhrasing(cfg))
	}
	rs = append(rs, checkAgent())
	rs = append(rs, checkStateDir())

	return Report{Results: rs}
}

// checkBinary catches the first cliff: installed somewhere not on PATH, so
// every documented command fails with "command not found".
func checkBinary() Result {
	self, err := os.Executable()
	if err != nil {
		return warn("binary", "cannot determine own path", "")
	}
	self, _ = filepath.EvalSymlinks(self)

	found, err := exec.LookPath("pulse")
	if err != nil {
		return fail("binary", "`pulse` is not on your PATH",
			"make install   # or: ln -sf "+self+" /opt/homebrew/bin/pulse")
	}
	resolved, _ := filepath.EvalSymlinks(found)
	if resolved != self {
		return warn("binary",
			fmt.Sprintf("`pulse` on PATH is %s, but this is %s", found, self),
			"an older copy is shadowing this one; remove it or re-run `make install`")
	}
	return ok("binary", found)
}

func checkConfig(cfgErr error) Result {
	if cfgErr != nil {
		return fail("config", cfgErr.Error(), "pulse init")
	}
	return ok("config", pulse.ConfigPath())
}

// checkUnknownKeys catches a setting that looks applied but is silently
// ignored — an env-var name written as a config field, or a typo.
func checkUnknownKeys() Result {
	unknown := pulse.UnknownKeys()
	if len(unknown) == 0 {
		return ok("config keys", "all recognised")
	}
	return warn("config keys",
		"ignored: "+strings.Join(unknown, ", "),
		"these do nothing; check the spelling against example.yml")
}

func checkRepos(cfg pulse.Config) Result {
	var missing []string
	for _, root := range cfg.RepoRoots {
		if _, err := os.Stat(root); err != nil {
			missing = append(missing, root)
		}
	}
	if len(missing) > 0 {
		return fail("repo roots", "do not exist: "+strings.Join(missing, ", "),
			"fix repoRoots in "+pulse.ConfigPath())
	}
	n := len(pulse.DiscoverRepos(cfg.RepoRoots, cfg.RepoScanDepth))
	if n == 0 {
		return fail("repos", "no git repositories found under "+strings.Join(cfg.RepoRoots, ", "),
			"point repoRoots at your code, or raise repoScanDepth")
	}
	return ok("repos", fmt.Sprintf("%d found", n))
}

// checkGH covers the GitHub path in three steps, because "it doesn't work"
// means something different at each one.
func checkGH() []Result {
	if _, err := exec.LookPath("gh"); err != nil {
		return []Result{fail("gh cli", "not installed",
			"brew install gh   # Pulse reads GitHub through it and stores no token")}
	}

	out, err := exec.Command("gh", "auth", "status").CombinedOutput()
	if err != nil {
		return []Result{
			ok("gh cli", "installed"),
			fail("github auth", "not logged in", "gh auth login"),
		}
	}
	login := ""
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, "account "); i >= 0 {
			login = strings.Fields(line[i+len("account "):])[0]
			break
		}
	}

	probe := exec.Command("gh", "api", "graphql", "-f",
		"query={search(query:\"is:pr is:open author:@me\", type:ISSUE, first:1){issueCount}}")
	if err := probe.Run(); err != nil {
		return []Result{
			ok("gh cli", "installed"),
			ok("github auth", login),
			fail("github api", "query failed", "gh auth refresh -s read:org,repo"),
		}
	}
	return []Result{
		ok("gh cli", "installed"),
		ok("github auth", login),
		ok("github api", "queries working"),
	}
}

func checkPhrasing(cfg pulse.Config) Result {
	if !cfg.Phrasing.UseLLM {
		return ok("pulse-ai", "off — fixed wording, fully offline")
	}
	name, err := pulse.VerifyPhrasing(cfg)
	if err != nil {
		return warn("pulse-ai", err.Error(),
			"nudges still work with fixed wording; check phrasing.* and PULSE_API_KEY")
	}
	return ok("pulse-ai", name+" — live round-trip ok")
}

// checkAgent verifies the thing that actually sends nudges. A plist on disk
// that launchd has not loaded is the quiet failure here.
func checkAgent() Result {
	if !pulse.AgentInstalled() {
		return warn("background agent", "not installed",
			"pulse daemon   # without it, nudges only happen when you run pulse yourself")
	}
	out, err := exec.Command("launchctl", "list").Output()
	if err != nil || !strings.Contains(string(out), "dev.pulse.agent") {
		return fail("background agent", "plist exists but launchd has not loaded it",
			"pulse daemon   # reinstall to reload it")
	}
	return ok("background agent", "installed and loaded")
}

func checkStateDir() Result {
	probe := filepath.Join(pulse.Home(), ".doctor-write-test")
	if err := os.MkdirAll(pulse.Home(), 0o755); err != nil {
		return fail("state", "cannot create "+pulse.Home(), "check permissions on your home directory")
	}
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		return fail("state", "cannot write to "+pulse.Home(), "check permissions")
	}
	_ = os.Remove(probe)
	return ok("state", pulse.Home())
}

// NotificationCheck posts a banner and returns the script it used. Whether it
// rendered cannot be determined in code — osascript exits 0 either way — so
// the caller has to ask a human.
func NotificationCheck() error {
	if _, err := exec.LookPath("osascript"); err != nil {
		return fmt.Errorf("osascript not available; notifications are macOS-only")
	}
	cmd := exec.Command("osascript", "-e",
		`display notification "If you can read this, delivery works." with title "Pulse" subtitle "doctor"`)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	// Give the banner a moment to appear before the prompt covers the screen.
	time.Sleep(600 * time.Millisecond)
	return nil
}

// Render prints the report.
func Render(rep Report, w *os.File) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, ui.Header("diagnosis"))
	for _, r := range rep.Results {
		fmt.Fprintf(w, "  %s %s %s\n", r.symbol(), ui.Pad(r.Name, 18), r.Detail)
		if r.Fix != "" {
			fmt.Fprintf(w, "    %s\n", ui.Grey(r.Fix))
		}
	}
}

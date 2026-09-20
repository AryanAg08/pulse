package pulse

import (
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

// NotifyMacOS posts a banner. `display notification` is fire-and-forget: unlike
// `display dialog` it never blocks, which matters because a blocked osascript
// would wedge the daemon.
func NotifyMacOS(n Nudge) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	short := n.ID
	if len(short) > 6 {
		short = short[:6]
	}
	script := `display notification "` + escapeAppleScript(n.Text) + `" ` +
		`with title "Pulse" subtitle "` + escapeAppleScript(string(n.Kind)) + ` · pulse ack ` + short + `"`

	cmd := exec.Command("osascript", "-e", script)
	if err := cmd.Start(); err != nil {
		return false
	}
	// Reap asynchronously so a hung osascript can't block the cycle.
	go func() {
		timer := time.AfterFunc(5*time.Second, func() { _ = cmd.Process.Kill() })
		defer timer.Stop()
		_ = cmd.Wait()
	}()
	return true
}

func OpenAction(url string) {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	_ = exec.Command(opener, url).Start()
}

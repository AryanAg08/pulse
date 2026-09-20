package pulse

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const agentLabel = "dev.pulse.agent"

func PlistPath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "LaunchAgents", agentLabel+".plist")
}

// The 14-day test only produces a number if Pulse actually runs for 14 days.
// launchd survives reboots and logouts; a terminal tab does not.
//
// A Go binary is its own entry point, so unlike the Node version there is no
// interpreter path to pin and no runtime upgrade that can silently break this.
func plistBody(binPath string, intervalMin int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>run</string>
  </array>
  <key>StartInterval</key><integer>%d</integer>
  <key>RunAtLoad</key><true/>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>%s</string>
  </dict>
</dict>
</plist>
`, agentLabel, binPath, intervalMin*60,
		filepath.Join(Home(), "agent.log"),
		filepath.Join(Home(), "agent.err"),
		os.Getenv("PATH"))
}

func InstallAgent(intervalMin int) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("background install is macOS-only; use `pulse start` instead")
	}
	binPath, err := os.Executable()
	if err != nil {
		return "", err
	}
	binPath, err = filepath.EvalSymlinks(binPath)
	if err != nil {
		return "", err
	}

	p := PlistPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(Home(), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(plistBody(binPath, intervalMin)), 0o644); err != nil {
		return "", err
	}
	// Unload first so a reinstall picks up the new plist; failure is expected
	// on a first install and is not an error.
	_ = exec.Command("launchctl", "unload", p).Run()
	if err := exec.Command("launchctl", "load", p).Run(); err != nil {
		return "", fmt.Errorf("launchctl load failed: %w", err)
	}
	return p, nil
}

func UninstallAgent() bool {
	p := PlistPath()
	if _, err := os.Stat(p); err != nil {
		return false
	}
	_ = exec.Command("launchctl", "unload", p).Run()
	return os.Remove(p) == nil
}

func AgentInstalled() bool {
	_, err := os.Stat(PlistPath())
	return err == nil
}

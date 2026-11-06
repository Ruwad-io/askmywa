// Package service installs the daemon as a macOS launchd user agent.
package service

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"whatsapp-mcp/internal/config"
)

// Install writes ~/Library/LaunchAgents/<label>.plist running
// `binary daemon --log-file <logDir>/daemon.log` with KeepAlive and
// RunAtLoad, then bootstraps it. extraArgs are appended to the command.
func Install(binary string, paths config.Paths, extraArgs []string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(binary)
	if err != nil {
		return "", err
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return "", fmt.Errorf("resolve binary path: %w", err)
	}
	if err := paths.EnsureDirs(); err != nil {
		return "", err
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", config.ServiceLabel+".plist")
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return "", err
	}
	args := []string{abs, "daemon", "--data-dir", paths.DataDir, "--send-root", paths.SendRoot, "--log-file", filepath.Join(paths.LogDir, "daemon.log")}
	args = append(args, extraArgs...)
	if err := os.WriteFile(plistPath, []byte(Plist(args, paths.LogDir, home)), 0o644); err != nil {
		return "", err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	target := domain + "/" + config.ServiceLabel
	_ = run("launchctl", "bootout", target)
	// bootout returns before the job is gone; bootstrapping too early fails
	// with "Input/output error", so wait for the job to disappear.
	waitUntilGone(target, 10*time.Second)
	for attempt := 0; attempt < 5; attempt++ {
		if err = run("launchctl", "bootstrap", domain, plistPath); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return plistPath, fmt.Errorf("launchctl bootstrap: %w", err)
	}
	if err := run("launchctl", "kickstart", "-k", target); err != nil {
		return plistPath, fmt.Errorf("launchctl kickstart: %w", err)
	}
	return plistPath, nil
}

func waitUntilGone(target string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if exec.Command("launchctl", "print", target).Run() != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Uninstall stops the agent and removes the plist.
func Uninstall() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = run("launchctl", "bootout", domain+"/"+config.ServiceLabel)
	plistPath := filepath.Join(home, "Library", "LaunchAgents", config.ServiceLabel+".plist")
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Status reports whether the agent is loaded and its pid.
func Status() (loaded bool, pid string) {
	domain := "gui/" + strconv.Itoa(os.Getuid())
	out, err := exec.Command("launchctl", "print", domain+"/"+config.ServiceLabel).CombinedOutput()
	if err != nil {
		return false, ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "pid = ") {
			return true, strings.TrimPrefix(line, "pid = ")
		}
	}
	return true, ""
}

// Plist renders the launchd property list. Log rotation is done by the
// daemon itself (lumberjack); launchd only captures what escapes before
// logging is set up.
func Plist(args []string, logDir, home string) string {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>` + config.ServiceLabel + `</string>
    <key>ProgramArguments</key>
    <array>
`)
	for _, a := range args {
		b.WriteString("        <string>" + html.EscapeString(a) + "</string>\n")
	}
	b.WriteString(`    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
        <key>HOME</key>
        <string>` + html.EscapeString(home) + `</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>10</integer>
    <key>ProcessType</key>
    <string>Background</string>
    <key>StandardOutPath</key>
    <string>` + html.EscapeString(filepath.Join(logDir, "launchd.log")) + `</string>
    <key>StandardErrorPath</key>
    <string>` + html.EscapeString(filepath.Join(logDir, "launchd.log")) + `</string>
</dict>
</plist>
`)
	return b.String()
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

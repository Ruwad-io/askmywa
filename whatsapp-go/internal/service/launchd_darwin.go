package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"askmywa/internal/config"
)

const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

func platformName() string    { return "launchd agent " + config.ServiceLabel }
func platformSupported() bool { return true }
func launchdDomain() string   { return "gui/" + strconv.Itoa(os.Getuid()) }
func launchdTarget() string   { return launchdDomain() + "/" + config.ServiceLabel }

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", config.ServiceLabel+".plist"), nil
}

func platformDefinition(args []string, paths config.Paths) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	path, err := plistPath()
	if err != nil {
		return "", "", err
	}
	return path, Plist(args, paths.LogDir, home), nil
}

func platformInstall(args []string, paths config.Paths) (Installed, error) {
	path, content, err := platformDefinition(args, paths)
	if err != nil {
		return Installed{}, err
	}
	if err := writeFile(path, []byte(content)); err != nil {
		return Installed{}, err
	}
	// System Settings finds the app named by AssociatedBundleIdentifiers
	// through Launch Services; an app installed by copying (the extension,
	// an archive) may not be registered yet.
	if app := AppBundle(args[0]); app != "" {
		_ = run(lsregister, "-f", app)
	}
	_ = run("launchctl", "bootout", launchdTarget())
	// bootout returns before the job is gone; bootstrapping too early fails
	// with "Input/output error", so wait for the job to disappear.
	waitUntilGone(launchdTarget(), 10*time.Second)
	for attempt := 0; attempt < 5; attempt++ {
		if err = run("launchctl", "bootstrap", launchdDomain(), path); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return Installed{Path: path}, fmt.Errorf("launchctl bootstrap: %w", err)
	}
	if err := run("launchctl", "kickstart", "-k", launchdTarget()); err != nil {
		return Installed{Path: path}, fmt.Errorf("launchctl kickstart: %w", err)
	}
	return Installed{Path: path}, nil
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

// platformRemoveLegacy stops and removes the agent installed as
// com.whatsapp-mcp.daemon, waiting for its daemon to exit.
func platformRemoveLegacy() error {
	target := launchdDomain() + "/" + config.LegacyServiceLabel
	if exec.Command("launchctl", "print", target).Run() == nil {
		_ = run("launchctl", "bootout", target)
		waitUntilGone(target, 10*time.Second)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, "Library", "LaunchAgents", config.LegacyServiceLabel+".plist")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func platformUninstall() error {
	_ = run("launchctl", "bootout", launchdTarget())
	path, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func platformStatus() State {
	out, err := output("launchctl", "print", launchdTarget())
	if err != nil {
		return State{}
	}
	st := State{Loaded: true}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "pid = ") {
			st.PID = strings.TrimPrefix(line, "pid = ")
			st.Running = true
		}
	}
	return st
}

func platformRestart() error {
	return run("launchctl", "kickstart", "-k", launchdTarget())
}

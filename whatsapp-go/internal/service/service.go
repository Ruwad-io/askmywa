// Package service registers the daemon with the operating system so that
// it starts at login and restarts after a crash:
//
//	macOS          a launchd user agent    ~/Library/LaunchAgents/com.askmywa.daemon.plist
//	Linux          a systemd user service  ~/.config/systemd/user/askmywa.service
//	Windows        a scheduled task        "askmywa daemon", at logon
//
// The definitions are rendered by pure functions (Plist, SystemdUnit,
// TaskXML) so they can be tested on any OS; the files named *_darwin.go,
// *_linux.go and *_windows.go talk to the OS.
package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"askmywa/internal/config"
)

// ErrUnsupported is returned on systems without a service implementation.
var ErrUnsupported = errors.New("installing the background service is not supported on this system: run `askmywa daemon` yourself")

// Installed describes a successful Install.
type Installed struct {
	// Path is the service definition that was written.
	Path string
	// Warning is something the user should know (for example that the
	// service stops at logout on Linux).
	Warning string
}

// State describes the registered service.
type State struct {
	Loaded  bool
	Running bool
	PID     string
}

// String renders the state for the status command.
func (s State) String() string {
	switch {
	case !s.Loaded:
		return "not installed"
	case s.PID != "":
		return "running (pid " + s.PID + ")"
	case s.Running:
		return "running"
	}
	return "installed, not running"
}

// Name is the human name of the service on this system.
func Name() string { return platformName() }

// Supported reports whether this system can install the service.
func Supported() bool { return platformSupported() }

// DaemonArgs is the command line the service runs.
func DaemonArgs(binary string, paths config.Paths, extra []string) []string {
	args := []string{binary, "daemon", "--background",
		"--data-dir", paths.DataDir, "--send-root", paths.SendRoot,
		"--log-file", filepath.Join(paths.LogDir, "daemon.log")}
	return append(args, extra...)
}

// Install registers the service for binary with these paths and starts it,
// replacing a previous registration.
func Install(binary string, paths config.Paths, extra []string) (Installed, error) {
	abs, err := resolveBinary(binary)
	if err != nil {
		return Installed{}, err
	}
	if err := paths.EnsureDirs(); err != nil {
		return Installed{}, err
	}
	// A service installed as whatsapp-mcp would run a second daemon on the
	// same session; it is stopped and removed first.
	if err := platformRemoveLegacy(); err != nil {
		return Installed{}, fmt.Errorf("remove the service installed as whatsapp-mcp: %w", err)
	}
	return platformInstall(DaemonArgs(abs, paths, extra), paths)
}

// Definition returns the path and content Install would write, without
// writing anything (install-service --dry-run).
func Definition(binary string, paths config.Paths, extra []string) (path, content string, err error) {
	abs, err := resolveBinary(binary)
	if err != nil {
		return "", "", err
	}
	return platformDefinition(DaemonArgs(abs, paths, extra), paths)
}

// Uninstall stops the service and removes its definition, including one
// installed under the pre-rename name.
func Uninstall() error {
	if err := platformRemoveLegacy(); err != nil {
		return err
	}
	return platformUninstall()
}

// Status reports the service state.
func Status() State { return platformStatus() }

// Restart restarts the service.
func Restart() error { return platformRestart() }

func resolveBinary(binary string) (string, error) {
	abs, err := filepath.Abs(binary)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	} else {
		return "", fmt.Errorf("resolve binary path: %w", err)
	}
	// A Homebrew install runs from the formula's stable opt link, so the
	// service survives `brew upgrade` removing the old version's folder.
	if opt := homebrewOpt(abs); opt != "" {
		if _, err := os.Stat(opt); err == nil {
			return opt, nil
		}
	}
	return abs, nil
}

// homebrewOpt maps <prefix>/Cellar/<formula>/<version>/bin/<name> to
// <prefix>/opt/<formula>/bin/<name>, or returns "" for other paths.
func homebrewOpt(exe string) string {
	sep := string(filepath.Separator)
	marker := sep + "Cellar" + sep
	i := strings.LastIndex(exe, marker)
	if i < 0 {
		return ""
	}
	parts := strings.Split(exe[i+len(marker):], sep)
	if len(parts) != 4 || parts[2] != "bin" {
		return ""
	}
	return filepath.Join(exe[:i], "opt", parts[0], "bin", parts[3])
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Manager adapts this package to the MCP server's ServiceManager.
type Manager struct {
	// Binary is the absolute path of the askmywa executable to run.
	Binary string
}

// Loaded reports whether the service is registered.
func (m Manager) Loaded() bool { return Status().Loaded }

// Install registers the service with these paths and starts it.
func (m Manager) Install(p config.Paths) error {
	_, err := Install(m.Binary, p, nil)
	return err
}

// Restart restarts the service.
func (m Manager) Restart() error { return Restart() }

// Uninstall stops and removes the service.
func (m Manager) Uninstall() error { return Uninstall() }

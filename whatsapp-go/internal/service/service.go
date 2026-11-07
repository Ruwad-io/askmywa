// Package service registers the daemon with the operating system so that
// it starts at login and restarts after a crash:
//
//	macOS          a launchd user agent    ~/Library/LaunchAgents/com.whatsapp-mcp.daemon.plist
//	Linux          a systemd user service  ~/.config/systemd/user/whatsapp-mcp.service
//	Windows        a scheduled task        "whatsapp-mcp daemon", at logon
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

	"whatsapp-mcp/internal/config"
)

// ErrUnsupported is returned on systems without a service implementation.
var ErrUnsupported = errors.New("installing the background service is not supported on this system: run `whatsapp-mcp daemon` yourself")

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

// Uninstall stops the service and removes its definition.
func Uninstall() error { return platformUninstall() }

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
	return abs, nil
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
	// Binary is the absolute path of the whatsapp-mcp executable to run.
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

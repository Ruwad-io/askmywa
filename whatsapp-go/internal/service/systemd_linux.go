package service

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"askmywa/internal/config"
)

func platformName() string    { return "systemd user service " + SystemdUnitName }
func platformSupported() bool { return true }

func unitPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user", SystemdUnitName), nil
}

func systemctl(args ...string) error { return run("systemctl", append([]string{"--user"}, args...)...) }

func platformDefinition(args []string, _ config.Paths) (string, string, error) {
	path, err := unitPath()
	if err != nil {
		return "", "", err
	}
	return path, SystemdUnit(args), nil
}

func platformInstall(args []string, paths config.Paths) (Installed, error) {
	path, content, err := platformDefinition(args, paths)
	if err != nil {
		return Installed{}, err
	}
	if err := writeFile(path, []byte(content)); err != nil {
		return Installed{}, err
	}
	res := Installed{Path: path}
	if err := systemctl("daemon-reload"); err != nil {
		return res, fmt.Errorf("%w (is a systemd user session running? on a server, log in once over SSH or run `loginctl enable-linger`)", err)
	}
	if err := systemctl("enable", SystemdUnitName); err != nil {
		return res, err
	}
	if err := systemctl("restart", SystemdUnitName); err != nil {
		return res, err
	}
	// Without lingering, user services stop at logout and do not start at
	// boot, which defeats the point on a Raspberry Pi left on a shelf.
	if !lingering() {
		u, _ := user.Current()
		name := ""
		if u != nil {
			name = u.Username
		}
		if err := run("loginctl", "enable-linger", name); err != nil || !lingering() {
			res.Warning = "the service stops when you log out and does not start at boot until lingering is enabled: run `sudo loginctl enable-linger " + name + "`"
		}
	}
	return res, nil
}

func lingering() bool {
	u, err := user.Current()
	if err != nil {
		return false
	}
	out, err := output("loginctl", "show-user", u.Username, "--property=Linger", "--value")
	return err == nil && strings.TrimSpace(out) == "yes"
}

// platformRemoveLegacy stops and removes the unit installed as
// whatsapp-mcp.service.
func platformRemoveLegacy() error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	legacy := filepath.Join(filepath.Dir(path), LegacySystemdUnitName)
	if _, err := os.Stat(legacy); err != nil {
		return nil
	}
	_ = systemctl("disable", "--now", LegacySystemdUnitName)
	if err := os.Remove(legacy); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = systemctl("daemon-reload")
	return nil
}

func platformUninstall() error {
	_ = systemctl("disable", "--now", SystemdUnitName)
	path, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = systemctl("daemon-reload")
	return nil
}

func platformStatus() State {
	out, err := output("systemctl", "--user", "show", SystemdUnitName, "--property=LoadState,ActiveState,MainPID")
	if err != nil {
		return State{}
	}
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[k] = v
		}
	}
	st := State{Loaded: props["LoadState"] == "loaded", Running: props["ActiveState"] == "active"}
	if pid := props["MainPID"]; pid != "" && pid != "0" {
		st.PID = pid
	}
	return st
}

func platformRestart() error { return systemctl("restart", SystemdUnitName) }

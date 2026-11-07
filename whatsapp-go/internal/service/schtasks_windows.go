package service

import (
	"encoding/csv"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"whatsapp-mcp/internal/config"
)

func platformName() string    { return "scheduled task \"" + TaskName + "\"" }
func platformSupported() bool { return true }

func taskFile(paths config.Paths) string { return filepath.Join(paths.DataDir, "task.xml") }

func platformDefinition(args []string, paths config.Paths) (string, string, error) {
	u, err := user.Current()
	if err != nil {
		return "", "", err
	}
	return taskFile(paths), TaskXML(args, u.Username), nil
}

func platformInstall(args []string, paths config.Paths) (Installed, error) {
	path, content, err := platformDefinition(args, paths)
	if err != nil {
		return Installed{}, err
	}
	if err := os.WriteFile(path, utf16File(content), 0o600); err != nil {
		return Installed{}, err
	}
	res := Installed{Path: path}
	_ = run("schtasks", "/End", "/TN", TaskName)
	if err := run("schtasks", "/Create", "/TN", TaskName, "/XML", path, "/F"); err != nil {
		return res, fmt.Errorf("register the scheduled task: %w", err)
	}
	if err := run("schtasks", "/Run", "/TN", TaskName); err != nil {
		return res, fmt.Errorf("start the scheduled task: %w", err)
	}
	return res, nil
}

func platformUninstall() error {
	_ = run("schtasks", "/End", "/TN", TaskName)
	if !platformStatus().Loaded {
		return nil
	}
	return run("schtasks", "/Delete", "/TN", TaskName, "/F")
}

func platformStatus() State {
	out, err := output("schtasks", "/Query", "/TN", TaskName, "/FO", "CSV", "/NH")
	if err != nil {
		return State{}
	}
	st := State{Loaded: true}
	// "TaskName","Next Run Time","Status"
	if rows, err := csv.NewReader(strings.NewReader(out)).ReadAll(); err == nil && len(rows) > 0 && len(rows[0]) >= 3 {
		st.Running = strings.EqualFold(strings.TrimSpace(rows[0][2]), "Running")
	}
	return st
}

func platformRestart() error {
	_ = run("schtasks", "/End", "/TN", TaskName)
	return run("schtasks", "/Run", "/TN", TaskName)
}

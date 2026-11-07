package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultDirs(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	home := filepath.Join("/", "home", "u")
	cases := []struct {
		goos       string
		env        map[string]string
		data, logs string
	}{
		{"darwin", nil, filepath.Join(home, "Library", "Application Support", "askmywa"), filepath.Join(home, "Library", "Logs", "askmywa")},
		{"linux", nil, filepath.Join(home, ".local", "share", "askmywa"), filepath.Join(home, ".local", "state", "askmywa")},
		{"linux", map[string]string{"XDG_DATA_HOME": "/d", "XDG_STATE_HOME": "/s"}, filepath.Join("/d", "askmywa"), filepath.Join("/s", "askmywa")},
		{"windows", map[string]string{"LOCALAPPDATA": "/L"}, filepath.Join("/L", "askmywa"), filepath.Join("/L", "askmywa", "logs")},
		{"windows", nil, filepath.Join(home, "AppData", "Local", "askmywa"), filepath.Join(home, "AppData", "Local", "askmywa", "logs")},
	}
	for _, c := range cases {
		data, logs := defaultDirs(c.goos, home, env(c.env), AppName)
		if data != c.data || logs != c.logs {
			t.Errorf("%s %v: got %s, %s", c.goos, c.env, data, logs)
		}
	}
}

func TestPickDirsKeepsPreRenameInstall(t *testing.T) {
	home := filepath.Join("/", "Users", "u")
	none := func(string) string { return "" }
	newData := filepath.Join(home, "Library", "Application Support", "askmywa")
	oldData := filepath.Join(home, "Library", "Application Support", "whatsapp-mcp")
	oldLogs := filepath.Join(home, "Library", "Logs", "whatsapp-mcp")
	only := func(paths ...string) func(string) bool {
		return func(p string) bool {
			for _, x := range paths {
				if p == x {
					return true
				}
			}
			return false
		}
	}
	if d, _ := pickDirs("darwin", home, none, only()); d != newData {
		t.Errorf("fresh install: %s", d)
	}
	if d, l := pickDirs("darwin", home, none, only(oldData)); d != oldData || l != oldLogs {
		t.Errorf("pre-rename install: %s, %s", d, l)
	}
	if d, _ := pickDirs("darwin", home, none, only(oldData, newData)); d != newData {
		t.Errorf("both exist: %s", d)
	}
}

func TestGetenvFallsBackToOldName(t *testing.T) {
	t.Setenv("ASKMYWA_SEND_ROOT", "")
	t.Setenv("WHATSAPP_MCP_SEND_ROOT", "/old")
	if got := Getenv("SEND_ROOT"); got != "/old" {
		t.Errorf("fallback: %q", got)
	}
	t.Setenv("ASKMYWA_SEND_ROOT", "/new")
	if got := Getenv("SEND_ROOT"); got != "/new" {
		t.Errorf("new name: %q", got)
	}
}

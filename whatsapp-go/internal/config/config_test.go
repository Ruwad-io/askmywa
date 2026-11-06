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
		{"darwin", nil, filepath.Join(home, "Library", "Application Support", "whatsapp-mcp"), filepath.Join(home, "Library", "Logs", "whatsapp-mcp")},
		{"linux", nil, filepath.Join(home, ".local", "share", "whatsapp-mcp"), filepath.Join(home, ".local", "state", "whatsapp-mcp")},
		{"linux", map[string]string{"XDG_DATA_HOME": "/d", "XDG_STATE_HOME": "/s"}, filepath.Join("/d", "whatsapp-mcp"), filepath.Join("/s", "whatsapp-mcp")},
		{"windows", map[string]string{"LOCALAPPDATA": "/L"}, filepath.Join("/L", "whatsapp-mcp"), filepath.Join("/L", "whatsapp-mcp", "logs")},
		{"windows", nil, filepath.Join(home, "AppData", "Local", "whatsapp-mcp"), filepath.Join(home, "AppData", "Local", "whatsapp-mcp", "logs")},
	}
	for _, c := range cases {
		data, logs := defaultDirs(c.goos, home, env(c.env))
		if data != c.data || logs != c.logs {
			t.Errorf("%s %v: got %s, %s", c.goos, c.env, data, logs)
		}
	}
}

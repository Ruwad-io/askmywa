package service

import (
	"path/filepath"
	"testing"
)

func TestHomebrewOpt(t *testing.T) {
	cases := map[string]string{
		"/opt/homebrew/Cellar/whatsapp-mcp/0.1.0/bin/whatsapp-mcp":                "/opt/homebrew/opt/whatsapp-mcp/bin/whatsapp-mcp",
		"/home/linuxbrew/.linuxbrew/Cellar/whatsapp-mcp/0.2.0_1/bin/whatsapp-mcp": "/home/linuxbrew/.linuxbrew/opt/whatsapp-mcp/bin/whatsapp-mcp",
		"/Users/me/.local/bin/whatsapp-mcp":                                       "",
		"/opt/homebrew/Cellar/whatsapp-mcp/0.1.0/libexec/whatsapp-mcp":            "",
	}
	for in, want := range cases {
		if got := homebrewOpt(filepath.FromSlash(in)); got != filepath.FromSlash(want) {
			t.Errorf("homebrewOpt(%q) = %q, want %q", in, got, want)
		}
	}
}

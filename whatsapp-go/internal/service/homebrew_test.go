package service

import (
	"path/filepath"
	"testing"
)

func TestHomebrewOpt(t *testing.T) {
	cases := map[string]string{
		"/opt/homebrew/Cellar/askmywa/0.1.0/bin/askmywa":                "/opt/homebrew/opt/askmywa/bin/askmywa",
		"/home/linuxbrew/.linuxbrew/Cellar/askmywa/0.2.0_1/bin/askmywa": "/home/linuxbrew/.linuxbrew/opt/askmywa/bin/askmywa",
		"/Users/me/.local/bin/askmywa":                                  "",
		"/opt/homebrew/Cellar/askmywa/0.1.0/libexec/askmywa":            "",
	}
	for in, want := range cases {
		if got := homebrewOpt(filepath.FromSlash(in)); got != filepath.FromSlash(want) {
			t.Errorf("homebrewOpt(%q) = %q, want %q", in, got, want)
		}
	}
}

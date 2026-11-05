package service

import (
	"strings"
	"testing"
)

func TestPlist(t *testing.T) {
	p := Plist([]string{"/usr/local/bin/whatsapp-mcp", "daemon", "--log-file", "/Users/x/Library/Logs/whatsapp-mcp/daemon.log"}, "/Users/x/Library/Logs/whatsapp-mcp", "/Users/x")
	for _, want := range []string{"<string>com.whatsapp-mcp.daemon</string>", "<key>KeepAlive</key>\n    <true/>", "<key>RunAtLoad</key>\n    <true/>", "<string>daemon</string>", "launchd.log"} {
		if !strings.Contains(p, want) {
			t.Errorf("plist lacks %q", want)
		}
	}
}

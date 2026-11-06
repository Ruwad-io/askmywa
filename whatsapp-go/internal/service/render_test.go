package service

import (
	"encoding/xml"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"whatsapp-mcp/internal/config"
)

func testArgs() []string {
	return DaemonArgs("/opt/whatsapp mcp/whatsapp-mcp", config.FromDataDir("/data dir/wm"), nil)
}

func TestDaemonArgs(t *testing.T) {
	args := testArgs()
	joined := strings.Join(args, "|")
	for _, want := range []string{"daemon", "--background", "--data-dir|/data dir/wm", "--send-root|" + filepath.Join("/data dir/wm", "files"), "--log-file"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args lack %q: %v", want, args)
		}
	}
}

func TestPlist(t *testing.T) {
	p := Plist(testArgs(), "/Users/x/Library/Logs/whatsapp-mcp", "/Users/x")
	for _, want := range []string{"<string>com.whatsapp-mcp.daemon</string>", "<key>KeepAlive</key>\n    <true/>", "<key>RunAtLoad</key>\n    <true/>", "<string>daemon</string>", "<string>/opt/whatsapp mcp/whatsapp-mcp</string>", "launchd.log"} {
		if !strings.Contains(p, want) {
			t.Errorf("plist lacks %q", want)
		}
	}
	if err := xml.Unmarshal([]byte(strings.Replace(p, `<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`, "", 1)), new(struct{})); err != nil {
		t.Errorf("plist is not well-formed XML: %v", err)
	}
}

func TestSystemdUnit(t *testing.T) {
	u := SystemdUnit(append(testArgs(), "--send-root", `/a "b" 100%$x`))
	for _, want := range []string{`ExecStart="/opt/whatsapp mcp/whatsapp-mcp" daemon --background --data-dir "/data dir/wm"`, "Restart=always", "WantedBy=default.target", `"/a \"b\" 100%%$$x"`} {
		if !strings.Contains(u, want) {
			t.Errorf("unit lacks %q:\n%s", want, u)
		}
	}
}

func TestTaskXML(t *testing.T) {
	x := TaskXML([]string{`C:\Program Files\wm\whatsapp-mcp.exe`, "daemon", "--data-dir", `C:\Users\A B\AppData\Local\whatsapp-mcp`, "--x", `a"b`, "--y", `C:\dir\`}, `PC\A & B`)
	for _, want := range []string{
		`<Command>C:\Program Files\wm\whatsapp-mcp.exe</Command>`,
		`<Arguments>daemon --data-dir &#34;C:\Users\A B\AppData\Local\whatsapp-mcp&#34; --x &#34;a\&#34;b&#34; --y C:\dir\</Arguments>`,
		`<UserId>PC\A &amp; B</UserId>`,
		"<LogonTrigger>", "<RestartOnFailure>", "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
	} {
		if !strings.Contains(x, want) {
			t.Errorf("task XML lacks %q:\n%s", want, x)
		}
	}
	var v struct {
		XMLName xml.Name `xml:"Task"`
	}
	if err := xml.Unmarshal([]byte(strings.Replace(x, `encoding="UTF-16"`, `encoding="UTF-8"`, 1)), &v); err != nil {
		t.Errorf("task XML is not well-formed: %v", err)
	}
}

func TestWindowsQuote(t *testing.T) {
	for in, want := range map[string]string{
		"plain":        "plain",
		"two words":    `"two words"`,
		`say "hi"`:     `"say \"hi\""`,
		`C:\dir with\`: `"C:\dir with\\"`,
		"":             `""`,
	} {
		if got := windowsQuote(in); got != want {
			t.Errorf("windowsQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUTF16File(t *testing.T) {
	b := utf16File("aé")
	if b[0] != 0xFF || b[1] != 0xFE {
		t.Fatal("missing BOM")
	}
	u := make([]uint16, (len(b)-2)/2)
	for i := range u {
		u[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
	}
	if string(utf16.Decode(u)) != "aé" {
		t.Error("round trip failed")
	}
}

func TestStateString(t *testing.T) {
	if (State{}).String() != "not installed" || (State{Loaded: true, Running: true, PID: "4"}).String() != "running (pid 4)" {
		t.Error("State.String")
	}
}

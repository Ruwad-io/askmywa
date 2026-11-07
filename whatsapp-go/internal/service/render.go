package service

import (
	"bytes"
	"html"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"askmywa/internal/config"
)

// Plist renders the launchd property list. Log rotation is done by the
// daemon itself (lumberjack); launchd only captures what escapes before
// logging is set up.
func Plist(args []string, logDir, home string) string {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>` + config.ServiceLabel + `</string>
    <key>ProgramArguments</key>
    <array>
`)
	for _, a := range args {
		b.WriteString("        <string>" + html.EscapeString(a) + "</string>\n")
	}
	b.WriteString(`    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
        <key>HOME</key>
        <string>` + html.EscapeString(home) + `</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>10</integer>
    <key>ProcessType</key>
    <string>Background</string>
    <key>StandardOutPath</key>
    <string>` + html.EscapeString(filepath.Join(logDir, "launchd.log")) + `</string>
    <key>StandardErrorPath</key>
    <string>` + html.EscapeString(filepath.Join(logDir, "launchd.log")) + `</string>
</dict>
</plist>
`)
	return b.String()
}

// SystemdUnitName is the name of the Linux user service.
const SystemdUnitName = "askmywa.service"

// LegacySystemdUnitName is the unit installed before the rename to askmywa.
const LegacySystemdUnitName = "whatsapp-mcp.service"

// SystemdUnit renders the systemd user service.
func SystemdUnit(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = systemdQuote(a)
	}
	return `[Unit]
Description=WhatsApp MCP daemon (archives WhatsApp messages for MCP clients)
Documentation=https://github.com/abonur/askmywa
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=` + strings.Join(quoted, " ") + `
Restart=always
RestartSec=10
Environment=PATH=/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=default.target
`
}

// systemdQuote quotes one ExecStart argument: systemd splits on spaces,
// expands $ and %, and understands C-style escapes inside double quotes.
func systemdQuote(a string) string {
	a = strings.ReplaceAll(a, "%", "%%")
	a = strings.ReplaceAll(a, "$", "$$")
	if a != "" && !strings.ContainsAny(a, " \t\"'\\;") {
		return a
	}
	a = strings.ReplaceAll(a, `\`, `\\`)
	a = strings.ReplaceAll(a, `"`, `\"`)
	return `"` + a + `"`
}

// TaskName is the name of the Windows scheduled task.
const TaskName = "askmywa daemon"

// LegacyTaskName is the task registered before the rename to askmywa.
const LegacyTaskName = "whatsapp-mcp daemon"

// TaskXML renders the Windows scheduled task: start at logon of user,
// restart every minute after a failure, no time limit, run on battery.
func TaskXML(args []string, user string) string {
	var cmdline []string
	for _, a := range args[1:] {
		cmdline = append(cmdline, windowsQuote(a))
	}
	esc := html.EscapeString
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Archives WhatsApp messages for MCP clients such as Claude Desktop. https://github.com/abonur/askmywa</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>` + esc(user) + `</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + esc(user) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + esc(args[0]) + `</Command>
      <Arguments>` + esc(strings.Join(cmdline, " ")) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

// windowsQuote quotes one argument for the Windows command-line parser
// (CommandLineToArgvW rules).
func windowsQuote(a string) string {
	if a != "" && !strings.ContainsAny(a, " \t\"") {
		return a
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range a {
		switch r {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, 2*slashes+1))
			b.WriteRune('"')
			slashes = 0
			continue
		default:
			b.WriteString(strings.Repeat(`\`, slashes))
			slashes = 0
			b.WriteRune(r)
			continue
		}
	}
	b.WriteString(strings.Repeat(`\`, 2*slashes))
	b.WriteByte('"')
	return b.String()
}

// utf16File encodes s as UTF-16LE with a byte-order mark, the encoding
// schtasks expects for /XML.
func utf16File(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 2, 2+2*len(units))
	out[0], out[1] = 0xFF, 0xFE
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

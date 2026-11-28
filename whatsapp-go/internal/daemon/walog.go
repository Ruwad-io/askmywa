package daemon

import (
	"fmt"
	"log/slog"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// slogAdapter routes whatsmeow's logger to log/slog.
type slogAdapter struct {
	log *slog.Logger
}

func newWALogger(log *slog.Logger, module string) waLog.Logger {
	return &slogAdapter{log: log.With("module", module)}
}

func (l *slogAdapter) Warnf(msg string, args ...any)  { l.log.Warn(fmt.Sprintf(msg, args...)) }
func (l *slogAdapter) Errorf(msg string, args ...any) { l.log.Error(fmt.Sprintf(msg, args...)) }
func (l *slogAdapter) Infof(msg string, args ...any)  { l.log.Info(fmt.Sprintf(msg, args...)) }
func (l *slogAdapter) Debugf(msg string, args ...any) { l.log.Debug(fmt.Sprintf(msg, args...)) }
func (l *slogAdapter) Sub(module string) waLog.Logger {
	return &slogAdapter{log: l.log.With("sub", module)}
}

// Some whatsmeow trace format strings include raw QR credentials and frames.
// In managed mode only report severity; the daemon emits its own status events.
type accountLogger struct{ log *slog.Logger }

func (l *accountLogger) Debugf(string, ...any)   {}
func (l *accountLogger) Infof(string, ...any)    {}
func (l *accountLogger) Warnf(string, ...any)    { l.log.Warn("WhatsApp protocol warning") }
func (l *accountLogger) Errorf(string, ...any)   { l.log.Error("WhatsApp protocol error") }
func (l *accountLogger) Sub(string) waLog.Logger { return l }

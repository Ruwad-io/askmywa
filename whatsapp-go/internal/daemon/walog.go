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

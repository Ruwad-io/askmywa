// Package config resolves the on-disk locations used by every subcommand.
//
// Everything lives under one data directory, by default
// ~/Library/Application Support/askmywa on macOS, %LOCALAPPDATA%\askmywa
// on Windows and ~/.local/share/askmywa on Linux:
//
//	whatsapp.db   whatsmeow session store (a copy of the legacy one, never the original)
//	messages.db   message history, contacts and chats written by the daemon
//	daemon.sock   Unix socket the MCP server uses to send and download
//	files/        the only directory files may be sent from; downloads land in files/media
//
// The directory can be moved with ASKMYWA_DATA_DIR, and the send root
// with ASKMYWA_SEND_ROOT.
//
// The project was called whatsapp-mcp before. Installs made under that name
// keep working: their folders are used while the new ones do not exist, and
// the old WHATSAPP_MCP_* variables are still read.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	// ServiceLabel is the launchd label of the daemon agent.
	ServiceLabel = "com.askmywa.daemon"
	// AppName is the folder name under Application Support and Logs.
	AppName = "askmywa"

	// LegacyServiceLabel and LegacyAppName are the names used before the
	// rename to askmywa.
	LegacyServiceLabel = "com.whatsapp-mcp.daemon"
	LegacyAppName      = "whatsapp-mcp"
)

// Paths holds every path the binary uses.
type Paths struct {
	DataDir    string
	SessionDB  string
	MessagesDB string
	Socket     string
	// SendRoot is the only directory (recursively) from which files may be sent.
	SendRoot string
	// MediaDir is where downloaded media is written; it lives under SendRoot so
	// downloaded files can be forwarded.
	MediaDir string
	LogDir   string
}

// Default returns the paths for the current user, honouring the environment overrides.
func Default() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home directory: %w", err)
	}
	dataDir, logDir := pickDirs(runtime.GOOS, home, os.Getenv, exists)
	if d := Getenv("DATA_DIR"); d != "" {
		dataDir = d
	}
	p := FromDataDir(dataDir)
	p.LogDir = logDir
	if root := Getenv("SEND_ROOT"); root != "" {
		p.SendRoot = root
	}
	return p, nil
}

// Getenv reads ASKMYWA_<name>, falling back to the pre-rename WHATSAPP_MCP_<name>.
func Getenv(name string) string {
	if v := os.Getenv("ASKMYWA_" + name); v != "" {
		return v
	}
	return os.Getenv("WHATSAPP_MCP_" + name)
}

// pickDirs returns the default directories, or the pre-rename ones when
// only those exist, so an install made as whatsapp-mcp keeps its session
// and archive without any migration.
func pickDirs(goos, home string, getenv func(string) string, exists func(string) bool) (dataDir, logDir string) {
	dataDir, logDir = defaultDirs(goos, home, getenv, AppName)
	if exists(dataDir) {
		return dataDir, logDir
	}
	if oldData, oldLog := defaultDirs(goos, home, getenv, LegacyAppName); exists(oldData) {
		return oldData, oldLog
	}
	return dataDir, logDir
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// defaultDirs returns the data and log directories each OS expects:
//
//	macOS    ~/Library/Application Support/askmywa, ~/Library/Logs/askmywa
//	Windows  %LOCALAPPDATA%\askmywa, %LOCALAPPDATA%\askmywa\logs
//	Linux    $XDG_DATA_HOME/askmywa (~/.local/share), $XDG_STATE_HOME/askmywa (~/.local/state)
func defaultDirs(goos, home string, getenv func(string) string, name string) (dataDir, logDir string) {
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", name), filepath.Join(home, "Library", "Logs", name)
	case "windows":
		base := getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, name), filepath.Join(base, name, "logs")
	default:
		data := getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		state := getenv("XDG_STATE_HOME")
		if state == "" {
			state = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(data, name), filepath.Join(state, name)
	}
}

// FromDataDir derives every path from a data directory (used by tests and flags).
func FromDataDir(dataDir string) Paths {
	return Paths{
		DataDir:    dataDir,
		SessionDB:  filepath.Join(dataDir, "whatsapp.db"),
		MessagesDB: filepath.Join(dataDir, "messages.db"),
		Socket:     filepath.Join(dataDir, "daemon.sock"),
		SendRoot:   filepath.Join(dataDir, "files"),
		MediaDir:   filepath.Join(dataDir, "files", "media"),
		LogDir:     filepath.Join(dataDir, "logs"),
	}
}

// EnsureDirs creates the data, send-root, media and log directories with
// private permissions (the session store holds the account keys).
func (p Paths) EnsureDirs() error {
	for _, dir := range []string{p.DataDir, p.SendRoot, p.MediaDir, p.LogDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

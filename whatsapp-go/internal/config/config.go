// Package config resolves the on-disk locations used by every subcommand.
//
// Everything lives under one data directory, by default
// ~/Library/Application Support/whatsapp-mcp:
//
//	whatsapp.db   whatsmeow session store (a copy of the legacy one, never the original)
//	messages.db   message history, contacts and chats written by the daemon
//	daemon.sock   Unix socket the MCP server uses to send and download
//	files/        the only directory files may be sent from; downloads land in files/media
//
// The directory can be moved with WHATSAPP_MCP_DATA_DIR, and the send root
// with WHATSAPP_MCP_SEND_ROOT.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// ServiceLabel is the launchd label of the daemon agent.
	ServiceLabel = "com.whatsapp-mcp.daemon"
	// AppName is the folder name under Application Support and Logs.
	AppName = "whatsapp-mcp"
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
	dataDir := os.Getenv("WHATSAPP_MCP_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(home, "Library", "Application Support", AppName)
	}
	p := FromDataDir(dataDir)
	p.LogDir = filepath.Join(home, "Library", "Logs", AppName)
	if root := os.Getenv("WHATSAPP_MCP_SEND_ROOT"); root != "" {
		p.SendRoot = root
	}
	return p, nil
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

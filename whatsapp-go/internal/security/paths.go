// Package security confines file access: a file may only be sent from the
// configured send root, and downloaded media may only be written under the
// media directory. Symlinks are resolved before the check, so a link that
// points outside the root is rejected (same model as sealjay/mcp-whatsapp).
package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ValidateSendPath resolves userPath and returns it when it is a regular
// file inside root. The error text tells the caller where files must live.
func ValidateSendPath(userPath, root string) (string, error) {
	if strings.TrimSpace(userPath) == "" {
		return "", fmt.Errorf("media_path is empty: pass the absolute path of a file under %s", root)
	}
	abs, err := filepath.Abs(userPath)
	if err != nil {
		return "", fmt.Errorf("invalid media_path %q: %w", userPath, err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(abs))
	if err != nil {
		return "", fmt.Errorf("media_path %q is not accessible: %w; files must be placed under %s", abs, err, root)
	}
	rootResolved := resolveRoot(root)
	if err := confine(resolved, rootResolved); err != nil {
		return "", err
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("media_path %q: %w", resolved, err)
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("media_path %q is not a regular file", resolved)
	}
	return resolved, nil
}

func resolveRoot(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	abs = filepath.Clean(abs)
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	return abs
}

func confine(resolved, root string) error {
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return fmt.Errorf("%q is outside the send root %q: copy the file under that directory (or start the daemon with --send-root) and retry", resolved, root)
	}
	return nil
}

// SafeFilename keeps only the base name and rejects degenerate names that
// could escape a directory join.
func SafeFilename(raw string) string {
	if strings.ContainsRune(raw, '\x00') {
		return fallback()
	}
	base := filepath.Base(strings.ReplaceAll(raw, `\`, `/`))
	switch base {
	case "", ".", "..", "/":
		return fallback()
	}
	return base
}

func fallback() string { return "document_" + time.Now().UTC().Format("20060102_150405") }

// SafeDirName turns a chat JID into a directory name.
func SafeDirName(jid string) string {
	r := strings.NewReplacer("/", "_", `\`, "_", ":", "_", "..", "_")
	name := r.Replace(jid)
	if name == "" || name == "." {
		return "_"
	}
	return name
}

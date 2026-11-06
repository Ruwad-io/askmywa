package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DisconnectInput is the input of disconnect_whatsapp.
type DisconnectInput struct {
	DeleteArchive bool `json:"delete_archive,omitempty" jsonschema:"also delete the message archive, downloaded attachments and logs from this computer"`
	Confirm       bool `json:"confirm,omitempty" jsonschema:"false (default) only describes what would happen; true does it, after the user approved"`
}

// DisconnectOutput is the output of disconnect_whatsapp.
type DisconnectOutput struct {
	Done              bool     `json:"done" jsonschema:"true once everything listed in steps was carried out"`
	NeedsConfirmation bool     `json:"needs_confirmation"`
	Account           string   `json:"account,omitempty"`
	Steps             []string `json:"steps" jsonschema:"what is (or would be) done, in plain words"`
	Message           string   `json:"message"`
}

func (s *Server) registerDisconnect(srv *mcp.Server) {
	t := true
	mcp.AddTool(srv, &mcp.Tool{Name: "disconnect_whatsapp", Title: "Disconnect WhatsApp",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &t, OpenWorldHint: &t, IdempotentHint: true},
		Description: "Unlink this computer from WhatsApp (it disappears from Linked devices on the phone) and stop the background service; with delete_archive, also delete the stored messages, downloaded attachments and logs. Use it when the user wants to stop using WhatsApp here or before uninstalling the extension. Files in the send folder are never deleted.\n\nSafety: with confirm=false (the default) NOTHING is changed; the tool returns the list of steps. Show it to the user and call again with confirm=true only after they approved. Never set confirm=true on the first call."},
		s.disconnect)
}

func (s *Server) disconnect(ctx context.Context, _ *mcp.CallToolRequest, in DisconnectInput) (*mcp.CallToolResult, DisconnectOutput, error) {
	out := DisconnectOutput{}
	st, statusErr := s.daemonStatus(ctx)
	linked := statusErr == nil && st.LoggedIn
	if linked {
		out.Account = strings.TrimSuffix(st.JID, "@s.whatsapp.net")
		out.Steps = append(out.Steps, "Unlink this computer from the WhatsApp account "+out.Account+" (it disappears from Linked devices on the phone)")
	}
	if s.svc != nil && s.svc.Loaded() {
		out.Steps = append(out.Steps, "Stop and remove the background service")
	}
	if in.DeleteArchive {
		out.Steps = append(out.Steps, "Delete the message archive, downloaded attachments and logs ("+s.paths.DataDir+", "+s.paths.LogDir+")")
	}
	if len(out.Steps) == 0 {
		out.Done = true
		out.Message = "WhatsApp is not set up on this computer: there is nothing to disconnect."
		return nil, out, nil
	}
	if !in.Confirm {
		out.NeedsConfirmation = true
		out.Message = "Nothing was changed. Show these steps to the user and, only once they approve, call again with confirm=true."
		if !in.DeleteArchive {
			out.Message += " The message archive stays on this computer unless delete_archive is true."
		}
		return nil, out, nil
	}

	if linked {
		if _, err := s.sender.Logout(ctx); err != nil {
			return nil, DisconnectOutput{}, err
		}
	}
	if s.svc != nil && s.svc.Loaded() {
		if err := s.svc.Uninstall(); err != nil {
			return nil, DisconnectOutput{}, fmt.Errorf("remove the background service: %w", err)
		}
	}
	if in.DeleteArchive {
		if err := s.deleteArchive(); err != nil {
			return nil, DisconnectOutput{}, err
		}
	}
	out.Done = true
	out.Message = "Done. WhatsApp is disconnected from this computer. The extension can now be uninstalled from Claude's settings."
	return nil, out, nil
}

// deleteArchive removes what the daemon wrote: session, archive, socket,
// downloaded media and logs. The send folder, which holds the user's own
// files, is left alone even when it lives inside the data directory.
func (s *Server) deleteArchive() error {
	s.storeMu.Lock()
	if s.store != nil {
		s.store.Close()
		s.store = nil
	}
	s.storeMu.Unlock()
	var errs []error
	remove := func(path string) {
		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, err)
		}
	}
	for _, name := range []string{"whatsapp.db", "messages.db", "daemon.sock", "tmp"} {
		matches, _ := filepath.Glob(filepath.Join(s.paths.DataDir, name+"*"))
		for _, m := range matches {
			remove(m)
		}
	}
	remove(s.paths.MediaDir)
	remove(s.paths.LogDir)
	// Remove the data directory itself when nothing of the user's is left in it.
	_ = os.Remove(filepath.Dir(s.paths.MediaDir))
	_ = os.Remove(s.paths.DataDir)
	return errors.Join(errs...)
}

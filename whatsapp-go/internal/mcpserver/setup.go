package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"whatsapp-mcp/internal/config"
	"whatsapp-mcp/internal/ipc"
	"whatsapp-mcp/internal/store"
)

// ServiceManager installs and restarts the background daemon. The OS
// implementation lives in internal/service; tests use a fake.
type ServiceManager interface {
	// Loaded reports whether the service is registered with the OS.
	Loaded() bool
	// Install registers (or re-registers) the service with these paths and starts it.
	Install(paths config.Paths) error
	// Restart restarts the running service.
	Restart() error
	// Uninstall stops the service and unregisters it.
	Uninstall() error
}

// daemonWait bounds how long setup waits for the daemon to answer after
// starting it (launchd may hold a restarted job for 10 s).
var daemonWait = 45 * time.Second

// ensureStore opens the archive the first time it exists.
func (s *Server) ensureStore(ctx context.Context) error {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	if s.store != nil {
		return nil
	}
	if _, err := os.Stat(s.paths.MessagesDB); err != nil {
		return errors.New("WhatsApp is not set up on this computer yet: call whatsapp_status, then connect_whatsapp with the user's phone number")
	}
	st, err := store.Open(ctx, s.paths.MessagesDB, true)
	if err != nil {
		return err
	}
	s.store = st
	return nil
}

// withStore makes a tool fail with setup guidance until the archive exists.
func withStore[In, Out any](s *Server, h mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		if err := s.ensureStore(ctx); err != nil {
			var zero Out
			return nil, zero, err
		}
		return h(ctx, req, in)
	}
}

// sendRoot is the folder the daemon accepts files from. The daemon's own
// setting wins, since it is the one that enforces it.
func (s *Server) sendRoot(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if st, err := s.sender.Status(ctx); err == nil && st.SendRoot != "" {
		return st.SendRoot
	}
	return s.paths.SendRoot
}

func (s *Server) daemonStatus(ctx context.Context) (ipc.StatusResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.sender.Status(ctx)
}

// waitForDaemon polls the socket until the daemon answers.
func (s *Server) waitForDaemon(ctx context.Context) (ipc.StatusResult, error) {
	deadline := time.Now().Add(daemonWait)
	for {
		st, err := s.daemonStatus(ctx)
		if err == nil {
			return st, nil
		}
		if time.Now().After(deadline) {
			return st, fmt.Errorf("the WhatsApp background service did not start within %s; its log is %s/daemon.log", daemonWait, s.paths.LogDir)
		}
		select {
		case <-ctx.Done():
			return st, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (s *Server) registerSetup(srv *mcp.Server) {
	f, t := false, true
	mcp.AddTool(srv, &mcp.Tool{Name: "whatsapp_status", Title: "WhatsApp status",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &f, OpenWorldHint: &f, IdempotentHint: true},
		Description: "Report whether WhatsApp is set up on this computer: background service, link with the phone, connection, archive size, and the folder files can be sent from. next_step says in plain words what to do when something is missing. Call it first when another WhatsApp tool fails."},
		s.status)
	mcp.AddTool(srv, &mcp.Tool{Name: "connect_whatsapp", Title: "Connect WhatsApp",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &f, OpenWorldHint: &t, IdempotentHint: true},
		Description: "Set up WhatsApp on this computer: start the background service if needed and, when the computer is not linked yet, get an 8-character pairing code for the user's phone number. Only call this when the user asked to connect or set up WhatsApp, and ask them for the phone number of their own WhatsApp account first (international format, for example 33612345678). Show the code and the instructions to the user exactly as returned: they type the code on their phone, which is what links the account. WhatsApp also shows a notification on the phone. Calling it again while a code is valid returns the same code; when the computer is already linked it only reports the account."},
		s.connect)
}

// StatusOutput is the output of whatsapp_status.
type StatusOutput struct {
	ServiceInstalled  bool   `json:"service_installed" jsonschema:"the background service is registered to start at login"`
	ServiceRunning    bool   `json:"service_running" jsonschema:"the background service answers"`
	Linked            bool   `json:"linked" jsonschema:"this computer is linked to a WhatsApp account"`
	Connected         bool   `json:"connected" jsonschema:"currently connected to WhatsApp"`
	Account           string `json:"account,omitempty" jsonschema:"the linked account"`
	PairingInProgress bool   `json:"pairing_in_progress,omitempty" jsonschema:"a pairing code is waiting to be typed on the phone"`
	SendFolder        string `json:"send_folder,omitempty" jsonschema:"files must be in this folder to be sent"`
	Messages          int    `json:"messages"`
	Chats             int    `json:"chats"`
	NextStep          string `json:"next_step" jsonschema:"what to do next, in plain words"`
}

func (s *Server) status(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, StatusOutput, error) {
	out := StatusOutput{SendFolder: s.paths.SendRoot}
	if s.svc != nil {
		out.ServiceInstalled = s.svc.Loaded()
	}
	st, err := s.daemonStatus(ctx)
	if err != nil {
		if out.ServiceInstalled {
			out.NextStep = "The background service is installed but not answering. Call connect_whatsapp to restart it."
		} else {
			out.NextStep = "WhatsApp is not set up on this computer. Ask the user for the phone number of their WhatsApp account (with country code) and call connect_whatsapp."
		}
		return nil, out, nil
	}
	out.ServiceRunning = true
	out.Linked, out.Connected = st.LoggedIn, st.Connected
	out.Account = strings.TrimSuffix(st.JID, "@s.whatsapp.net")
	out.PairingInProgress = st.Pairing
	out.SendFolder = st.SendRoot
	out.Messages, out.Chats = st.Counts.Messages, st.Counts.Chats
	switch {
	case st.LoggedOut:
		out.NextStep = "WhatsApp unlinked this computer (removed from Linked devices on the phone). Ask the user for their phone number and call connect_whatsapp to link it again."
	case !st.LoggedIn && st.Pairing:
		out.NextStep = "A pairing code is waiting to be typed on the phone. Once the user has entered it, call whatsapp_status again."
	case !st.LoggedIn && st.PairError != "":
		out.NextStep = "The last pairing attempt failed (" + st.PairError + "). Call connect_whatsapp again for a new code."
	case !st.LoggedIn:
		out.NextStep = "The service runs but is not linked to WhatsApp yet. Ask the user for their phone number and call connect_whatsapp."
	case !st.Connected:
		out.NextStep = "Linked, but offline right now. It reconnects by itself; check the internet connection if this lasts."
	case st.Counts.Messages == 0:
		out.NextStep = "Ready. WhatsApp is still sending the message history; older messages appear over the next minutes."
	default:
		out.NextStep = "Ready."
	}
	return nil, out, nil
}

// ConnectInput is the input of connect_whatsapp.
type ConnectInput struct {
	PhoneNumber string `json:"phone_number,omitempty" jsonschema:"the user's own WhatsApp number in international format, for example 33612345678; needed only when the computer is not linked yet"`
}

// ConnectOutput is the output of connect_whatsapp.
type ConnectOutput struct {
	State        string     `json:"state" jsonschema:"connected, waiting_for_code or needs_phone_number"`
	Account      string     `json:"account,omitempty"`
	Code         string     `json:"code,omitempty" jsonschema:"pairing code to type on the phone"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Instructions string     `json:"instructions" jsonschema:"what to tell the user, in plain words"`
}

const pairInstructions = "On the phone, open WhatsApp, then Settings (on Android, the ⋮ menu) > Linked devices > Link a device. Choose \"Link with phone number instead\" and type the code %s. The code is valid for about two minutes. Messages start appearing here a few minutes after linking."

func (s *Server) connect(ctx context.Context, _ *mcp.CallToolRequest, in ConnectInput) (*mcp.CallToolResult, ConnectOutput, error) {
	st, err := s.daemonStatus(ctx)
	switch {
	case err != nil:
		if s.svc == nil {
			return nil, ConnectOutput{}, errors.New("the WhatsApp background service is not running and cannot be started from here: run `whatsapp-mcp install-service` in a terminal")
		}
		if s.svc.Loaded() {
			err = s.svc.Restart()
		} else {
			err = s.svc.Install(s.paths)
		}
		if err != nil {
			return nil, ConnectOutput{}, fmt.Errorf("start the WhatsApp background service: %w", err)
		}
		if st, err = s.waitForDaemon(ctx); err != nil {
			return nil, ConnectOutput{}, err
		}
	case s.svc != nil && st.SendRoot != "" && st.SendRoot != s.paths.SendRoot:
		// The send folder changed in the settings: apply it.
		if err := s.svc.Install(s.paths); err != nil {
			return nil, ConnectOutput{}, fmt.Errorf("apply the new send folder: %w", err)
		}
		if st, err = s.waitForDaemon(ctx); err != nil {
			return nil, ConnectOutput{}, err
		}
	}
	if st.LoggedOut && s.svc != nil {
		if err := s.svc.Restart(); err != nil {
			return nil, ConnectOutput{}, fmt.Errorf("restart the WhatsApp background service: %w", err)
		}
		// Give the OS time to stop the old process before polling.
		time.Sleep(2 * time.Second)
		if st, err = s.waitForDaemon(ctx); err != nil {
			return nil, ConnectOutput{}, err
		}
	}
	if st.LoggedIn {
		account := strings.TrimSuffix(st.JID, "@s.whatsapp.net")
		return nil, ConnectOutput{State: "connected", Account: account,
			Instructions: "WhatsApp is linked to " + account + " and ready."}, nil
	}
	if strings.TrimSpace(in.PhoneNumber) == "" {
		return nil, ConnectOutput{State: "needs_phone_number",
			Instructions: "The service is running but not linked yet. Ask the user for the phone number of their WhatsApp account, with country code (for example 33612345678), and call connect_whatsapp again with phone_number."}, nil
	}
	res, err := s.sender.Pair(ctx, ipc.PairParams{Phone: in.PhoneNumber})
	if err != nil {
		return nil, ConnectOutput{}, err
	}
	if res.AlreadyPaired {
		account := strings.TrimSuffix(res.JID, "@s.whatsapp.net")
		return nil, ConnectOutput{State: "connected", Account: account, Instructions: "WhatsApp is linked to " + account + " and ready."}, nil
	}
	exp := res.ExpiresAt
	return nil, ConnectOutput{State: "waiting_for_code", Code: res.Code, ExpiresAt: &exp,
		Instructions: fmt.Sprintf(pairInstructions, res.Code)}, nil
}

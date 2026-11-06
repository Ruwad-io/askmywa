package mcpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"whatsapp-mcp/internal/config"
	"whatsapp-mcp/internal/ipc"
	"whatsapp-mcp/internal/store"
)

type fakeService struct {
	loaded  bool
	actions []string
	onStart func() // called after Install or Restart, simulates the daemon coming up
}

func (f *fakeService) Loaded() bool { return f.loaded }
func (f *fakeService) Install(p config.Paths) error {
	f.actions = append(f.actions, "install:"+p.SendRoot)
	f.loaded = true
	if f.onStart != nil {
		f.onStart()
	}
	return nil
}
func (f *fakeService) Uninstall() error {
	f.actions = append(f.actions, "uninstall")
	f.loaded = false
	return nil
}
func (f *fakeService) Restart() error {
	f.actions = append(f.actions, "restart")
	if f.onStart != nil {
		f.onStart()
	}
	return nil
}

// firstRun is a server on a machine where nothing is set up: no archive,
// no service, daemon not answering until the fake service starts it.
func firstRun(t *testing.T) (*harness, *fakeService) {
	t.Helper()
	daemonWait = 2 * time.Second
	paths := config.FromDataDir(t.TempDir())
	var daemon *ipc.StatusResult // nil = not answering
	sender := &fakeSender{status: func() (ipc.StatusResult, error) {
		if daemon == nil {
			return ipc.StatusResult{}, errors.New("not reachable")
		}
		return *daemon, nil
	}}
	svc := &fakeService{}
	svc.onStart = func() { daemon = &ipc.StatusResult{SendRoot: paths.SendRoot} }
	srv := New(nil, sender, svc, paths, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ct, stt := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := srv.Connect(ctx, stt, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return &harness{session: session, sender: sender, paths: paths}, svc
}

func TestFirstRunGuidesTheUser(t *testing.T) {
	h, svc := firstRun(t)
	_, text, isErr := h.call(t, "list_chats", nil)
	if !isErr || !strings.Contains(text, "connect_whatsapp") {
		t.Errorf("read tool before setup should point to setup: %q", text)
	}
	out, _, isErr := h.call(t, "whatsapp_status", nil)
	if isErr || out["service_running"] != false || !strings.Contains(out["next_step"].(string), "phone number") {
		t.Errorf("status: %+v", out)
	}
	// Without a number: the service is started, then the tool asks for the number.
	out, _, isErr = h.call(t, "connect_whatsapp", nil)
	if isErr || out["state"] != "needs_phone_number" {
		t.Errorf("connect without number: %+v", out)
	}
	if len(svc.actions) != 1 || !strings.HasPrefix(svc.actions[0], "install:") {
		t.Errorf("service not installed: %v", svc.actions)
	}
	out, _, isErr = h.call(t, "connect_whatsapp", map[string]any{"phone_number": "+33 6 12 34 56 78"})
	if isErr || out["state"] != "waiting_for_code" || out["code"] != "ABCD-EFGH" || !strings.Contains(out["instructions"].(string), "ABCD-EFGH") {
		t.Errorf("connect with number: %+v", out)
	}
	if h.sender.calls[len(h.sender.calls)-1] != "pair:+33 6 12 34 56 78" {
		t.Errorf("pair not requested: %v", h.sender.calls)
	}
	if len(svc.actions) != 1 {
		t.Errorf("service reinstalled needlessly: %v", svc.actions)
	}
}

func TestConnectWhenAlreadyLinked(t *testing.T) {
	h := newHarness(t)
	h.sender.status = func() (ipc.StatusResult, error) {
		return ipc.StatusResult{Connected: true, LoggedIn: true, JID: "33600000000@s.whatsapp.net", SendRoot: h.paths.SendRoot, Counts: store.Counts{Messages: 3}}, nil
	}
	out, _, isErr := h.call(t, "connect_whatsapp", map[string]any{"phone_number": "33600000000"})
	if isErr || out["state"] != "connected" || out["account"] != "33600000000" {
		t.Errorf("already linked: %+v", out)
	}
	for _, c := range h.sender.calls {
		if strings.HasPrefix(c, "pair:") {
			t.Error("pairing requested on a linked device")
		}
	}
	out, _, _ = h.call(t, "whatsapp_status", nil)
	if out["linked"] != true || out["next_step"] != "Ready." {
		t.Errorf("status: %+v", out)
	}
}

func TestConnectAfterLogout(t *testing.T) {
	h, svc := firstRun(t)
	logged := true
	h.sender.status = func() (ipc.StatusResult, error) {
		return ipc.StatusResult{LoggedOut: logged, SendRoot: h.paths.SendRoot}, nil
	}
	svc.loaded = true
	svc.onStart = func() { logged = false }
	out, _, _ := h.call(t, "whatsapp_status", nil)
	if !strings.Contains(out["next_step"].(string), "unlinked") {
		t.Errorf("status after logout: %+v", out)
	}
	out, _, isErr := h.call(t, "connect_whatsapp", map[string]any{"phone_number": "33612345678"})
	if isErr || out["state"] != "waiting_for_code" {
		t.Errorf("connect after logout: %+v", out)
	}
	if len(svc.actions) != 1 || svc.actions[0] != "restart" {
		t.Errorf("service not restarted: %v", svc.actions)
	}
}

func TestPairErrorsAreReported(t *testing.T) {
	h := newHarness(t)
	h.sender.status = func() (ipc.StatusResult, error) { return ipc.StatusResult{SendRoot: h.paths.SendRoot}, nil }
	h.sender.pair = func(ipc.PairParams) (ipc.PairResult, error) {
		return ipc.PairResult{}, errors.New(`phone number "0612" is too short`)
	}
	_, text, isErr := h.call(t, "connect_whatsapp", map[string]any{"phone_number": "0612"})
	if !isErr || !strings.Contains(text, "too short") {
		t.Errorf("pair error: %q", text)
	}
}

func TestDisconnectPreviewsThenUnlinks(t *testing.T) {
	h, svc := firstRun(t)
	svc.loaded = true
	h.sender.status = func() (ipc.StatusResult, error) {
		return ipc.StatusResult{Connected: true, LoggedIn: true, JID: "33600000000@s.whatsapp.net", SendRoot: h.paths.SendRoot}, nil
	}
	// Archive files, a downloaded photo, and a user file in the send folder.
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(h.paths.MediaDir+"/chat", 0o700))
	must(os.MkdirAll(h.paths.SendRoot, 0o700))
	for _, f := range []string{h.paths.SessionDB, h.paths.MessagesDB, h.paths.MessagesDB + "-wal", h.paths.MediaDir + "/chat/p.jpg", h.paths.SendRoot + "/mine.pdf"} {
		must(os.WriteFile(f, []byte("x"), 0o600))
	}

	out, _, isErr := h.call(t, "disconnect_whatsapp", map[string]any{"delete_archive": true})
	if isErr || out["needs_confirmation"] != true || out["done"] != false || len(out["steps"].([]any)) != 3 {
		t.Fatalf("preview: %+v", out)
	}
	for _, c := range h.sender.calls {
		if c == "logout" {
			t.Fatal("preview logged out")
		}
	}
	if len(svc.actions) != 0 {
		t.Fatalf("preview touched the service: %v", svc.actions)
	}
	if _, err := os.Stat(h.paths.MessagesDB); err != nil {
		t.Fatal("preview deleted the archive")
	}

	out, _, isErr = h.call(t, "disconnect_whatsapp", map[string]any{"delete_archive": true, "confirm": true})
	if isErr || out["done"] != true {
		t.Fatalf("confirm: %+v", out)
	}
	if h.sender.calls[len(h.sender.calls)-1] != "logout" || len(svc.actions) != 1 || svc.actions[0] != "uninstall" {
		t.Errorf("calls %v actions %v", h.sender.calls, svc.actions)
	}
	for _, gone := range []string{h.paths.SessionDB, h.paths.MessagesDB, h.paths.MessagesDB + "-wal", h.paths.MediaDir} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s still exists", gone)
		}
	}
	if _, err := os.Stat(h.paths.SendRoot + "/mine.pdf"); err != nil {
		t.Error("the user's file in the send folder was deleted")
	}
}

func TestDisconnectWhenNothingIsSetUp(t *testing.T) {
	h, _ := firstRun(t)
	out, _, isErr := h.call(t, "disconnect_whatsapp", map[string]any{"confirm": true})
	if isErr || out["done"] != true || !strings.Contains(out["message"].(string), "nothing to disconnect") {
		t.Errorf("nothing set up: %+v", out)
	}
}

package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"whatsapp-mcp/internal/config"
	"whatsapp-mcp/internal/ipc"
	"whatsapp-mcp/internal/store"
)

// fakeSender records calls; the preview tests assert it is never called.
type fakeSender struct {
	calls  []string
	status func() (ipc.StatusResult, error)
	pair   func(ipc.PairParams) (ipc.PairResult, error)
}

func (f *fakeSender) SendMessage(_ context.Context, p ipc.SendMessageParams) (ipc.SendResult, error) {
	f.calls = append(f.calls, "send_message:"+p.To+":"+p.Text)
	return ipc.SendResult{MessageID: "SENT1", To: p.To, Timestamp: time.Now()}, nil
}
func (f *fakeSender) SendFile(_ context.Context, p ipc.SendFileParams) (ipc.SendResult, error) {
	f.calls = append(f.calls, "send_file:"+p.To+":"+filepath.Base(p.Path))
	return ipc.SendResult{MessageID: "SENT2", To: p.To}, nil
}
func (f *fakeSender) SendAudio(_ context.Context, p ipc.SendAudioParams) (ipc.SendResult, error) {
	f.calls = append(f.calls, "send_audio:"+p.To)
	return ipc.SendResult{MessageID: "SENT3", To: p.To}, nil
}
func (f *fakeSender) DownloadMedia(_ context.Context, p ipc.DownloadParams) (ipc.DownloadResult, error) {
	f.calls = append(f.calls, "download:"+p.MessageID)
	return ipc.DownloadResult{Path: "/tmp/x", MediaType: "image", Filename: "x.jpg", Size: 1}, nil
}
func (f *fakeSender) Status(context.Context) (ipc.StatusResult, error) {
	if f.status != nil {
		return f.status()
	}
	return ipc.StatusResult{}, nil
}
func (f *fakeSender) Logout(context.Context) (ipc.LogoutResult, error) {
	f.calls = append(f.calls, "logout")
	return ipc.LogoutResult{Unlinked: true}, nil
}
func (f *fakeSender) Pair(_ context.Context, p ipc.PairParams) (ipc.PairResult, error) {
	f.calls = append(f.calls, "pair:"+p.Phone)
	if f.pair != nil {
		return f.pair(p)
	}
	return ipc.PairResult{Code: "ABCD-EFGH", ExpiresAt: time.Now().Add(2 * time.Minute)}, nil
}

func seed(t *testing.T) (*store.Store, config.Paths) {
	t.Helper()
	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	paths := config.FromDataDir(t.TempDir())
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	_, _ = st.PutContact(ctx, "33600000001", "100000000000001", "Hani Comptable", store.SourceFullName)
	_, _ = st.PutContact(ctx, "", "100000000000002", "Élodie", store.SourcePushName)
	_, _ = st.PutContact(ctx, "33600000003", "", "Marc Dupont", store.SourceFullName)
	_, _ = st.PutContact(ctx, "33600000004", "", "Marc Durand", store.SourceFullName)
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	_ = st.PutChat(ctx, "120363@g.us", "Copropriété", true, base.Add(2*time.Hour))
	_ = st.PutChat(ctx, "33600000001@s.whatsapp.net", "Hani Comptable", false, base.Add(time.Hour))
	_, _ = st.PutMessage(ctx, store.NewMessage{ID: "M1", ChatJID: "120363@g.us", SenderLID: "100000000000001", SenderName: "Hani Comptable", Content: "Bonjour, l'école est fermée", Timestamp: base})
	_, _ = st.PutMessage(ctx, store.NewMessage{ID: "M2", ChatJID: "33600000001@s.whatsapp.net", SenderPhone: "33600000001", SenderName: "Hani Comptable", Content: "Le bilan", Timestamp: base.Add(time.Hour)})
	_, _ = st.PutMessage(ctx, store.NewMessage{ID: "M3", ChatJID: "120363@g.us", SenderLID: "100000000000002", SenderName: "Élodie", Content: "Photo", Timestamp: base.Add(2 * time.Hour), Media: &store.MediaInfo{MediaType: "image", Filename: "p.jpg", DirectPath: "/v", MediaKey: []byte{1}, FileEncSHA256: []byte{2}}})
	return st, paths
}

type harness struct {
	session *mcp.ClientSession
	sender  *fakeSender
	paths   config.Paths
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, paths := seed(t)
	sender := &fakeSender{}
	srv := New(st, sender, nil, paths, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ct, stt := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := srv.Connect(ctx, stt, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return &harness{session: session, sender: sender, paths: paths}
}

func (h *harness) call(t *testing.T, name string, args map[string]any) (map[string]any, string, bool) {
	t.Helper()
	res, err := h.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	var out map[string]any
	if res.StructuredContent != nil {
		raw, _ := json.Marshal(res.StructuredContent)
		_ = json.Unmarshal(raw, &out)
	}
	return out, text, res.IsError
}

func TestToolsAreAnnotated(t *testing.T) {
	h := newHarness(t)
	tools, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"search_contacts": true, "list_messages": true, "list_chats": true, "get_chat": true, "get_direct_chat_by_contact": true,
		"get_contact_chats": true, "get_last_interaction": true, "get_message_context": true, "send_message": false, "send_file": false, "send_audio_message": false, "download_media": false,
		"whatsapp_status": true, "connect_whatsapp": false, "disconnect_whatsapp": false}
	if len(tools.Tools) != len(want) {
		t.Errorf("got %d tools", len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		ro, ok := want[tool.Name]
		if !ok {
			t.Errorf("unexpected tool %s", tool.Name)
			continue
		}
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != ro {
			t.Errorf("%s: readOnlyHint = %v, want %v", tool.Name, tool.Annotations, ro)
		}
		if strings.HasPrefix(tool.Name, "send_") {
			if tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint || tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
				t.Errorf("%s: missing destructive/open-world hints", tool.Name)
			}
			if !strings.Contains(tool.Description, "confirm=true") {
				t.Errorf("%s: description does not document confirm", tool.Name)
			}
		}
		if tool.OutputSchema == nil {
			t.Errorf("%s: no output schema", tool.Name)
		}
	}
}

func TestReads(t *testing.T) {
	h := newHarness(t)
	out, _, isErr := h.call(t, "list_chats", map[string]any{"limit": 1})
	if isErr || out["has_more"] != true || len(out["chats"].([]any)) != 1 {
		t.Errorf("list_chats: %+v", out)
	}
	next := out["next_cursor"].(string)
	out, _, _ = h.call(t, "list_chats", map[string]any{"limit": 1, "cursor": next})
	if out["has_more"] != false || out["chats"].([]any)[0].(map[string]any)["jid"] != "33600000001@s.whatsapp.net" {
		t.Errorf("second page: %+v", out)
	}
	out, _, isErr = h.call(t, "search_contacts", map[string]any{"query": "hani"})
	if isErr || len(out["contacts"].([]any)) != 1 {
		t.Errorf("search_contacts: %+v", out)
	}
	out, _, isErr = h.call(t, "list_messages", map[string]any{"sender": "Hani", "query": "ecole"})
	if isErr || len(out["messages"].([]any)) != 1 || out["messages"].([]any)[0].(map[string]any)["sender_name"] != "Hani Comptable" {
		t.Errorf("list_messages: %+v", out)
	}
	_, text, isErr := h.call(t, "list_messages", map[string]any{"sender": "Personne"})
	if !isErr || !strings.Contains(text, "search_contacts") {
		t.Errorf("unknown sender should be a tool error with guidance: %q", text)
	}
	_, text, isErr = h.call(t, "list_messages", map[string]any{"after": "hier"})
	if !isErr || !strings.Contains(text, "ISO-8601") {
		t.Errorf("bad date: %q", text)
	}
	out, _, isErr = h.call(t, "get_direct_chat_by_contact", map[string]any{"contact": "33600000001"})
	if isErr || out["jid"] != "33600000001@s.whatsapp.net" {
		t.Errorf("get_direct_chat_by_contact: %+v", out)
	}
	out, _, isErr = h.call(t, "get_last_interaction", map[string]any{"contact": "Élodie"})
	if isErr || out["found"] != true || out["message"].(map[string]any)["id"] != "M3" {
		t.Errorf("get_last_interaction: %+v", out)
	}
	out, _, isErr = h.call(t, "get_message_context", map[string]any{"message_id": "M3", "chat_jid": "120363@g.us"})
	if isErr || len(out["before"].([]any)) != 1 {
		t.Errorf("get_message_context: %+v", out)
	}
	out, _, isErr = h.call(t, "get_contact_chats", map[string]any{"contact": "Hani"})
	if isErr || len(out["chats"].([]any)) != 2 {
		t.Errorf("get_contact_chats: %+v", out)
	}
}

func TestSendPreviewThenConfirm(t *testing.T) {
	h := newHarness(t)
	out, _, isErr := h.call(t, "send_message", map[string]any{"recipient": "Hani", "message": "Bonjour"})
	if isErr {
		t.Fatalf("preview errored: %+v", out)
	}
	if out["sent"] != false || out["needs_confirmation"] != true {
		t.Errorf("preview flags: %+v", out)
	}
	p := out["preview"].(map[string]any)
	if p["recipient_jid"] != "33600000001@s.whatsapp.net" || p["recipient_name"] != "Hani Comptable" || p["is_group"] != false || p["message"] != "Bonjour" {
		t.Errorf("preview: %+v", p)
	}
	if len(h.sender.calls) != 0 {
		t.Fatalf("preview must not send: %v", h.sender.calls)
	}
	// Group by JID.
	out, _, _ = h.call(t, "send_message", map[string]any{"recipient": "120363@g.us", "message": "Salut"})
	p = out["preview"].(map[string]any)
	if p["is_group"] != true || p["recipient_name"] != "Copropriété" {
		t.Errorf("group preview: %+v", p)
	}
	// Ambiguous name.
	_, text, isErr := h.call(t, "send_message", map[string]any{"recipient": "Marc", "message": "x"})
	if !isErr || !strings.Contains(text, "several contacts") {
		t.Errorf("ambiguous: %q", text)
	}
	// Confirm sends to the resolved JID.
	out, _, isErr = h.call(t, "send_message", map[string]any{"recipient": "Hani", "message": "Bonjour", "confirm": true})
	if isErr || out["sent"] != true || out["message_id"] != "SENT1" {
		t.Errorf("confirm: %+v", out)
	}
	if len(h.sender.calls) != 1 || h.sender.calls[0] != "send_message:33600000001@s.whatsapp.net:Bonjour" {
		t.Errorf("calls: %v", h.sender.calls)
	}
}

func TestSendFileChecksRoot(t *testing.T) {
	h := newHarness(t)
	inside := filepath.Join(h.paths.SendRoot, "doc.pdf")
	_ = os.WriteFile(inside, []byte("pdf"), 0o600)
	outside := filepath.Join(t.TempDir(), "doc.pdf")
	_ = os.WriteFile(outside, []byte("pdf"), 0o600)

	_, text, isErr := h.call(t, "send_file", map[string]any{"recipient": "Hani", "media_path": outside})
	if !isErr || !strings.Contains(text, "send root") {
		t.Errorf("outside file: %q", text)
	}
	out, _, isErr := h.call(t, "send_file", map[string]any{"recipient": "Hani", "media_path": inside, "caption": "le doc"})
	if isErr || out["needs_confirmation"] != true || out["preview"].(map[string]any)["file_size"] != float64(3) {
		t.Errorf("file preview: %+v", out)
	}
	if len(h.sender.calls) != 0 {
		t.Fatal("preview sent something")
	}
	out, _, isErr = h.call(t, "send_file", map[string]any{"recipient": "Hani", "media_path": inside, "confirm": true})
	if isErr || out["sent"] != true || h.sender.calls[0] != "send_file:33600000001@s.whatsapp.net:doc.pdf" {
		t.Errorf("confirm file: %+v %v", out, h.sender.calls)
	}
	out, _, isErr = h.call(t, "send_audio_message", map[string]any{"recipient": "Hani", "media_path": inside})
	if isErr || out["needs_confirmation"] != true {
		t.Errorf("audio preview: %+v", out)
	}
}

func TestDownloadMedia(t *testing.T) {
	h := newHarness(t)
	_, text, isErr := h.call(t, "download_media", map[string]any{"message_id": "M1", "chat_jid": "120363@g.us"})
	if !isErr || !strings.Contains(text, "no media") {
		t.Errorf("text message: %q", text)
	}
	out, _, isErr := h.call(t, "download_media", map[string]any{"message_id": "M3", "chat_jid": "120363@g.us"})
	if isErr || out["path"] != "/tmp/x" {
		t.Errorf("download: %+v", out)
	}
}

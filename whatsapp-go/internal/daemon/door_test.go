package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"askmywa/internal/store"
	"askmywa/internal/webhook"
)

// A name the other person chose (push or business name) never vouches for a number.
func TestDoorContactNameTrustsOnlyTheAddressBook(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "messages.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d := &Daemon{store: st}
	if _, err := st.PutContact(ctx, "33600000001", "", "Maman", store.SourcePushName); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutContact(ctx, "33600000002", "", "Banque", store.SourceBusinessName); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutContact(ctx, "33600000003", "", "Karim Tech", store.SourceFullName); err != nil {
		t.Fatal(err)
	}
	for digits, want := range map[string]string{"33600000001": "", "33600000002": "", "33600000003": "Karim Tech", "33600000004": ""} {
		got, err := d.DoorContactName(ctx, digits)
		if err != nil || got != want {
			t.Fatalf("%s: %q, %v (want %q)", digits, got, err, want)
		}
	}
}

// Send-only keeps no message, yet a live one reaches the webhook, text included.
func TestSendOnlyPassesLiveMessagesToTheWebhookAndStoresNone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "messages.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got := make(chan webhook.Event, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e webhook.Event
		_ = json.NewDecoder(r.Body).Decode(&e)
		got <- e
	}))
	defer srv.Close()
	hook := webhook.New(webhook.Config{URL: srv.URL, Secret: strings.Repeat("s", 40), Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	go hook.Run(ctx)
	d := &Daemon{store: st, log: slog.New(slog.NewTextHandler(io.Discard, nil)), opts: Options{SendOnly: true, AccountID: "23456789-1234-4abc-8abc-123456789012"}, hook: hook,
		linked: map[string]bool{}, pushNames: map[string]string{}, groupNames: map[string]string{}}
	sender := types.NewJID("33600000001", types.DefaultUserServer)
	d.handleMessage(ctx, &events.Message{
		Info:    types.MessageInfo{MessageSource: types.MessageSource{Chat: sender, Sender: sender}, ID: "3EB0IN0001", PushName: "Karim", Timestamp: time.Unix(1764000000, 0)},
		Message: &waE2E.Message{Conversation: proto.String("Le HAR est prêt")},
	})
	select {
	case e := <-got:
		if e.Type != "account.message" || e.AccountID != d.opts.AccountID || e.ID != "3EB0IN0001" || e.Text != "Le HAR est prêt" || e.Sender.Phone != "+33600000001" || e.Sender.Name != "Karim" || e.Sender.NameSource != store.SourcePushName || e.Chat.IsGroup {
			t.Fatalf("event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no webhook event")
	}
	counts, err := st.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Messages != 0 || counts.Chats != 0 {
		t.Fatalf("send-only stored %+v", counts)
	}
}

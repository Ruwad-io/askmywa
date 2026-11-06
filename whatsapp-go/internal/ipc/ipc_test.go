package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

type echo struct{}

func (echo) Handle(_ context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case MethodSendMessage:
		var p SendMessageParams
		_ = json.Unmarshal(params, &p)
		return SendResult{MessageID: "ID1", To: p.To, Timestamp: time.Unix(0, 0).UTC()}, nil
	case MethodPing:
		return "pong", nil
	}
	return nil, errors.New("unknown method " + method)
}

func TestRoundTrip(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Serve(ctx, sock, echo{}, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	c := NewClient(sock)
	var r SendResult
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := c.Call(ctx, MethodSendMessage, SendMessageParams{To: "1@s.whatsapp.net", Text: "hi"}, &r)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if r.MessageID != "ID1" || r.To != "1@s.whatsapp.net" {
		t.Errorf("result: %+v", r)
	}
	if err := c.Call(ctx, "nope", nil, nil); err == nil || err.Error() != "unknown method nope" {
		t.Errorf("error not propagated: %v", err)
	}
	if err := NewClient(filepath.Join(t.TempDir(), "missing.sock")).Call(ctx, MethodPing, nil, nil); err == nil {
		t.Error("expected a connection error")
	}
}

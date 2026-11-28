package webhook

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const secret = "0123456789abcdef0123456789abcdef-test"

type receiver struct {
	mu     sync.Mutex
	codes  []int
	got    []Event
	bodies int
}

func (r *receiver) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		if Sign(secret, req.Header.Get("X-Askmywa-Timestamp"), body) != req.Header.Get("X-Askmywa-Signature") {
			t.Errorf("bad signature")
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.bodies++
		code := http.StatusOK
		if len(r.codes) > 0 {
			code, r.codes = r.codes[0], r.codes[1:]
		}
		if code == http.StatusOK {
			var e Event
			_ = json.Unmarshal(body, &e)
			r.got = append(r.got, e)
		}
		w.WriteHeader(code)
	}
}

func run(t *testing.T, r *receiver, events ...Event) *Notifier {
	t.Helper()
	srv := httptest.NewServer(r.handler(t))
	t.Cleanup(srv.Close)
	cfg := Config{URL: srv.URL, Secret: secret, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Backoff: func(int) time.Duration { return time.Millisecond }}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	n := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go n.Run(ctx)
	for _, e := range events {
		n.Enqueue(e)
	}
	deadline := time.Now().Add(3 * time.Second)
	for n.Pending() > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return n
}

func TestValidate(t *testing.T) {
	for _, c := range []Config{
		{URL: "ftp://x.test/h", Secret: secret},
		{URL: "https://user:pw@x.test/h", Secret: secret},
		{URL: "https://x.test/h", Secret: "short"},
	} {
		if c.Validate() == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
}

func TestSignedInOrderWithText(t *testing.T) {
	r := &receiver{}
	run(t, r, Event{Type: "message", ID: "A1", Text: "Bonjour", Sender: Person{Phone: "+33600000001", Name: "Karim"}},
		Event{Type: "message", ID: "A2", Text: "Suite"})
	if len(r.got) != 2 || r.got[0].ID != "A1" || r.got[0].Text != "Bonjour" || r.got[1].ID != "A2" {
		t.Fatalf("got %+v", r.got)
	}
}

func TestRetriesUntilAccepted(t *testing.T) {
	r := &receiver{codes: []int{http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusOK}}
	n := run(t, r, Event{Type: "message", ID: "B1", Text: "x"})
	if n.Pending() != 0 || len(r.got) != 1 || r.bodies != 3 {
		t.Fatalf("pending %d, got %d, tries %d", n.Pending(), len(r.got), r.bodies)
	}
}

func TestARefusedEventIsDroppedNotRetriedForever(t *testing.T) {
	r := &receiver{codes: []int{http.StatusUnauthorized}}
	n := run(t, r, Event{Type: "message", ID: "C1"}, Event{Type: "message", ID: "C2"})
	if n.Pending() != 0 || len(r.got) != 1 || r.got[0].ID != "C2" {
		t.Fatalf("pending %d, got %+v", n.Pending(), r.got)
	}
}

func TestEnqueueDropsTheOldestPastTheBound(t *testing.T) {
	n := New(Config{URL: "https://x.test/h", Secret: secret, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	for i := 0; i < QueueSize+3; i++ {
		n.Enqueue(Event{ID: "E"})
	}
	if n.Pending() != QueueSize {
		t.Fatalf("pending %d", n.Pending())
	}
}

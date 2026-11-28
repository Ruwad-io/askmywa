package httpdoor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const token = "0123456789abcdef0123456789abcdef-test"

type fake struct {
	down  bool
	mu    sync.Mutex
	sends []string
	fail  error
	names map[string]string
}

func (f *fake) DoorStatus(context.Context) (Status, error) {
	return Status{Connected: !f.down, LoggedIn: true, SendOnly: true}, nil
}

func (f *fake) DoorContactName(_ context.Context, digits string) (string, error) {
	return f.names[digits], nil
}

func (f *fake) DoorSend(_ context.Context, digits, text string) (Sent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return Sent{}, f.fail
	}
	f.sends = append(f.sends, digits+":"+text)
	return Sent{MessageID: "3EB0TEST", Timestamp: time.Unix(0, 0).UTC()}, nil
}

func newDoor(t *testing.T, f *fake, limits Limits, now func() time.Time) *Door {
	t.Helper()
	cfg := Config{Addr: "127.0.0.1:0", Token: token, Limits: limits, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: now}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return New(cfg, f)
}

func call(d *Door, method, path, auth, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	d.ServeHTTP(rec, req)
	return rec
}

func TestValidateRefusesAWeakOrOpenDoor(t *testing.T) {
	ok := Limits{PerMinute: 1, PerDay: 1}
	for _, cfg := range []Config{
		{Addr: "", Token: token, Limits: ok},
		{Addr: ":8080", Token: "short", Limits: ok},
		{Addr: ":8080", Token: token, Limits: Limits{}},
	} {
		if cfg.Validate() == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
}

func TestEveryRouteNeedsTheSecret(t *testing.T) {
	f := &fake{}
	d := newDoor(t, f, Limits{PerMinute: 5, PerDay: 5}, nil)
	for _, auth := range []string{"", "Bearer wrong", "Basic " + token, "Bearer " + token + "x"} {
		for _, r := range [][2]string{{http.MethodGet, "/v1/status"}, {http.MethodGet, "/v1/contact?phone=%2B33600000001"}, {http.MethodPost, "/v1/send"}} {
			if rec := call(d, r[0], r[1], auth, `{}`); rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s with %q: %d", r[0], r[1], auth, rec.Code)
			}
		}
	}
	if len(f.sends) != 0 {
		t.Fatal("sent without the secret")
	}
}

func TestOnlyThreeRoutes(t *testing.T) {
	d := newDoor(t, &fake{}, Limits{PerMinute: 5, PerDay: 5}, nil)
	for _, r := range [][2]string{{http.MethodGet, "/v1/chats"}, {http.MethodGet, "/v1/messages"}, {http.MethodPost, "/v1/status"}, {http.MethodGet, "/v1/send"}} {
		if rec := call(d, r[0], r[1], "Bearer "+token, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s: %d", r[0], r[1], rec.Code)
		}
	}
}

func TestContactName(t *testing.T) {
	d := newDoor(t, &fake{names: map[string]string{"33600000001": "Karim"}}, Limits{PerMinute: 5, PerDay: 5}, nil)
	if rec := call(d, http.MethodGet, "/v1/contact?phone=%2B33600000001", "Bearer "+token, ""); !strings.Contains(rec.Body.String(), `"name":"Karim"`) {
		t.Fatalf("known: %s", rec.Body)
	}
	if rec := call(d, http.MethodGet, "/v1/contact?phone=%2B33600000002", "Bearer "+token, ""); !strings.Contains(rec.Body.String(), `"name":null`) {
		t.Fatalf("unknown: %s", rec.Body)
	}
	if rec := call(d, http.MethodGet, "/v1/contact?phone=0600000001", "Bearer "+token, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("national number: %d", rec.Code)
	}
}

func TestSendRefusesGroupsBadNumbersAndBadBodies(t *testing.T) {
	f := &fake{}
	d := newDoor(t, f, Limits{PerMinute: 5, PerDay: 5}, nil)
	for _, body := range []string{
		`{"to":"120363000000000000@g.us","text":"x","idempotency_key":"key-00001"}`,
		`{"to":"33600000001","text":"x","idempotency_key":"key-00001"}`,
		`{"to":"+33600000001","text":"   ","idempotency_key":"key-00001"}`,
		`{"to":"+33600000001","text":"x","idempotency_key":"short"}`,
		`{"to":"+33600000001","text":"x","idempotency_key":"key-00001","path":"/etc/passwd"}`,
		`{"to":"+33600000001","text":"` + strings.Repeat("a", MaxTextRunes+1) + `","idempotency_key":"key-00001"}`,
	} {
		if rec := call(d, http.MethodPost, "/v1/send", "Bearer "+token, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%.60s: %d %s", body, rec.Code, rec.Body)
		}
	}
	if len(f.sends) != 0 {
		t.Fatal("a refused body was sent")
	}
}

func TestSameKeySendsOnce(t *testing.T) {
	f := &fake{}
	d := newDoor(t, f, Limits{PerMinute: 5, PerDay: 5}, nil)
	body := `{"to":"+33 6 00 00 00 01","text":"Bonjour","idempotency_key":"draft-1:v1"}`
	for i := 0; i < 3; i++ {
		if rec := call(d, http.MethodPost, "/v1/send", "Bearer "+token, body); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "3EB0TEST") {
			t.Fatalf("try %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	if len(f.sends) != 1 || f.sends[0] != "33600000001:Bonjour" {
		t.Fatalf("sends: %v", f.sends)
	}
}

func TestLimitsPerMinuteAndPerDay(t *testing.T) {
	f := &fake{}
	now := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
	d := newDoor(t, f, Limits{PerMinute: 2, PerDay: 3}, func() time.Time { return now })
	send := func(key string) int {
		return call(d, http.MethodPost, "/v1/send", "Bearer "+token, `{"to":"+33600000001","text":"x","idempotency_key":"`+key+`"}`).Code
	}
	if send("key-00001") != 200 || send("key-00002") != 200 {
		t.Fatal("first two refused")
	}
	if code := send("key-00003"); code != http.StatusTooManyRequests {
		t.Fatalf("third in the minute: %d", code)
	}
	now = now.Add(time.Minute)
	if send("key-00004") != 200 {
		t.Fatal("next minute refused")
	}
	if code := send("key-00005"); code != http.StatusTooManyRequests {
		t.Fatalf("fourth in the day: %d", code)
	}
	if len(f.sends) != 3 {
		t.Fatalf("sends: %d", len(f.sends))
	}
}

func TestAFailedSendIsNotRemembered(t *testing.T) {
	f := &fake{fail: errors.New("boom")}
	d := newDoor(t, f, Limits{PerMinute: 5, PerDay: 5}, nil)
	body := `{"to":"+33600000001","text":"x","idempotency_key":"key-00001"}`
	if rec := call(d, http.MethodPost, "/v1/send", "Bearer "+token, body); rec.Code != http.StatusBadGateway {
		t.Fatalf("failure: %d", rec.Code)
	}
	if strings.Contains(call(d, http.MethodGet, "/v1/status", "Bearer "+token, "").Body.String(), "boom") {
		t.Fatal("error leaked")
	}
}

func TestNotConnectedIsACleanRefusal(t *testing.T) {
	f := &fake{down: true}
	d := newDoor(t, f, Limits{PerMinute: 1, PerDay: 1}, nil)
	rec := call(d, http.MethodPost, "/v1/send", "Bearer "+token, `{"to":"+33600000001","text":"x","idempotency_key":"key-00001"}`)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "nothing was sent") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	f.down = false
	if rec := call(d, http.MethodPost, "/v1/send", "Bearer "+token, `{"to":"+33600000001","text":"x","idempotency_key":"key-00002"}`); rec.Code != http.StatusOK {
		t.Fatalf("a refusal must not use the limit: %d", rec.Code)
	}
}

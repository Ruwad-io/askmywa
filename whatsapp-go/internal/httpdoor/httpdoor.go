// Package httpdoor is askmywa's optional HTTP door for a trusted server
// (for example Mīzān on a private network). It is off unless an address and
// a secret are given, and it does three things only: report the connection,
// give the name WhatsApp knows for one phone number, and send one text to one
// person. It never lists chats, reads messages, sends files or writes to a
// group.
package httpdoor

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MinTokenLen is the shortest secret the door accepts.
const MinTokenLen = 32

// MaxTextRunes caps one message.
const MaxTextRunes = 4000

// Handler is what the daemon gives the door.
type Handler interface {
	DoorStatus(ctx context.Context) (Status, error)
	// DoorContactName is the name WhatsApp knows for these digits ("" if none).
	DoorContactName(ctx context.Context, digits string) (string, error)
	// DoorSend sends text to the person with these digits.
	DoorSend(ctx context.Context, digits, text string) (Sent, error)
}

// Status is the door's view of the connection.
type Status struct {
	Connected bool `json:"connected"`
	LoggedIn  bool `json:"logged_in"`
	SendOnly  bool `json:"send_only"`
}

// Sent is a delivered message.
type Sent struct {
	MessageID string    `json:"message_id"`
	Timestamp time.Time `json:"timestamp"`
}

// Limits bound how much the door sends.
type Limits struct {
	PerMinute int
	PerDay    int
}

// Config configures Serve.
type Config struct {
	Addr   string
	Token  string
	Limits Limits
	Log    *slog.Logger
	// Now is the clock (tests).
	Now func() time.Time
}

// Validate refuses a door that would be open or weak.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Addr) == "" {
		return errors.New("http door: no address")
	}
	if len(c.Token) < MinTokenLen {
		return fmt.Errorf("http door: ASKMYWA_HTTP_TOKEN must hold at least %d characters", MinTokenLen)
	}
	if c.Limits.PerMinute < 1 || c.Limits.PerDay < 1 {
		return errors.New("http door: send limits must be positive")
	}
	return nil
}

// Door is the HTTP handler; Serve wraps it in a server.
type Door struct {
	cfg Config
	h   Handler

	mu      sync.Mutex
	minute  []time.Time
	day     []time.Time
	done    map[string]doneSend // idempotency key -> result
	pending map[string]bool
}

type doneSend struct {
	sent Sent
	at   time.Time
}

// New builds the door. Call Validate first.
func New(cfg Config, h Handler) *Door {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Door{cfg: cfg, h: h, done: map[string]doneSend{}, pending: map[string]bool{}}
}

// Serve listens until ctx ends.
func Serve(ctx context.Context, cfg Config, h Handler) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	door := New(cfg, h)
	srv := &http.Server{Addr: cfg.Addr, Handler: door, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 3 * time.Minute, IdleTimeout: time.Minute, MaxHeaderBytes: 8 << 10}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("http door: %w", err)
	}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	door.cfg.Log.Info("http door listening", "addr", ln.Addr().String(), "per_minute", cfg.Limits.PerMinute, "per_day", cfg.Limits.PerDay)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ServeHTTP routes the three operations; everything else is 404.
func (d *Door) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !d.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/status":
		st, err := d.h.DoorStatus(r.Context())
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "status unavailable")
			return
		}
		writeJSON(w, http.StatusOK, st)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/contact":
		digits, ok := phoneDigits(r.URL.Query().Get("phone"))
		if !ok {
			writeError(w, http.StatusBadRequest, "phone: international number, + then 8 to 15 digits")
			return
		}
		name, err := d.h.DoorContactName(r.Context(), digits)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "contact lookup failed")
			return
		}
		var out struct {
			Name *string `json:"name"`
		}
		if name != "" {
			out.Name = &name
		}
		writeJSON(w, http.StatusOK, out)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/send":
		d.send(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (d *Door) authorized(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := []byte(strings.TrimSpace(header[len(prefix):]))
	return subtle.ConstantTimeCompare(got, []byte(d.cfg.Token)) == 1
}

type sendBody struct {
	To   string `json:"to"`
	Text string `json:"text"`
	Key  string `json:"idempotency_key"`
}

func (d *Door) send(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var body sendBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "body: {to, text, idempotency_key}")
		return
	}
	digits, ok := phoneDigits(body.To)
	if !ok {
		writeError(w, http.StatusBadRequest, "to: international number, + then 8 to 15 digits (no group)")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > MaxTextRunes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("text: 1 to %d characters", MaxTextRunes))
		return
	}
	key := strings.TrimSpace(body.Key)
	if len(key) < 8 || len(key) > 128 {
		writeError(w, http.StatusBadRequest, "idempotency_key: 8 to 128 characters")
		return
	}

	// Not paired or not connected: a clean refusal, nothing left.
	if st, err := d.h.DoorStatus(r.Context()); err != nil || !st.LoggedIn || !st.Connected {
		writeError(w, http.StatusServiceUnavailable, "WhatsApp is not connected: nothing was sent")
		return
	}

	d.mu.Lock()
	now := d.cfg.Now()
	d.prune(now)
	if prev, ok := d.done[key]; ok {
		d.mu.Unlock()
		writeJSON(w, http.StatusOK, prev.sent)
		return
	}
	if d.pending[key] {
		d.mu.Unlock()
		writeError(w, http.StatusConflict, "this send is already in progress")
		return
	}
	if len(d.minute) >= d.cfg.Limits.PerMinute || len(d.day) >= d.cfg.Limits.PerDay {
		d.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, "send limit reached: nothing was sent")
		return
	}
	d.pending[key] = true
	d.minute = append(d.minute, now)
	d.day = append(d.day, now)
	d.mu.Unlock()

	sent, err := d.h.DoorSend(r.Context(), digits, text)

	d.mu.Lock()
	delete(d.pending, key)
	if err == nil {
		d.done[key] = doneSend{sent: sent, at: d.cfg.Now()}
	}
	d.mu.Unlock()
	if err != nil {
		// Never the text or the number in the log.
		d.cfg.Log.Warn("http door send failed", "error", err.Error())
		writeError(w, http.StatusBadGateway, "WhatsApp did not confirm the send")
		return
	}
	d.cfg.Log.Info("http door message sent", "id", sent.MessageID)
	writeJSON(w, http.StatusOK, sent)
}

// prune drops counters and idempotency results older than their window.
func (d *Door) prune(now time.Time) {
	keep := func(ts []time.Time, window time.Duration) []time.Time {
		i := 0
		for i < len(ts) && now.Sub(ts[i]) >= window {
			i++
		}
		return ts[i:]
	}
	d.minute = keep(d.minute, time.Minute)
	d.day = keep(d.day, 24*time.Hour)
	for k, v := range d.done {
		if now.Sub(v.at) >= 24*time.Hour {
			delete(d.done, k)
		}
	}
}

// phoneDigits accepts « +33612345678 » (spaces, dots, dashes and brackets
// ignored) and returns the digits; a group or any JID is refused.
func phoneDigits(raw string) (string, bool) {
	s := strings.NewReplacer(" ", "", ".", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(raw))
	if !strings.HasPrefix(s, "+") {
		return "", false
	}
	digits := s[1:]
	if len(digits) < 8 || len(digits) > 15 || digits[0] == '0' {
		return "", false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	return digits, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

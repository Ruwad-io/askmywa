// Package webhook tells another app, as each WhatsApp message arrives, what
// was said, by whom and where: one signed HTTP POST per live message. It is
// off unless a URL and a secret are given. It is generic: any app can verify
// the signature and use the event (Mīzān files them into its streams).
//
// Signature: X-Askmywa-Signature = "sha256=" + hex(HMAC-SHA256(secret,
// X-Askmywa-Timestamp + "." + body)). The receiver should refuse a timestamp
// older than five minutes and keep the event id to ignore a repeat.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// MinSecretLen is the shortest signing secret accepted.
const MinSecretLen = 32

// QueueSize bounds the events waiting for delivery; past it the oldest is dropped.
const QueueSize = 2000

// Event is one WhatsApp message, as sent to the receiver.
type Event struct {
	AccountID string    `json:"account_id,omitempty"`
	Type      string    `json:"type"` // "message"
	ID        string    `json:"id"`   // WhatsApp message id; the receiver's dedupe key
	Timestamp time.Time `json:"timestamp"`
	FromMe    bool      `json:"from_me"`
	Chat      Chat      `json:"chat"`
	Sender    Person    `json:"sender"`
	Text      string    `json:"text,omitempty"`
	Media     *Media    `json:"media,omitempty"`
}

// Chat is the conversation: a person or a group.
type Chat struct {
	JID     string `json:"jid"`
	Name    string `json:"name,omitempty"`
	IsGroup bool   `json:"is_group"`
}

// Person is who wrote. Phone is « +33… » when known; LID otherwise.
type Person struct {
	Phone string `json:"phone,omitempty"`
	LID   string `json:"lid,omitempty"`
	// Name as the owner saved it, else as the person chose it (NameSource says which).
	Name       string `json:"name,omitempty"`
	NameSource string `json:"name_source,omitempty"`
}

// Media describes an attachment; its content is never sent.
type Media struct {
	Type     string `json:"type"`
	Filename string `json:"filename,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

// Config configures a Notifier.
type Config struct {
	URL    string
	Secret string
	Log    *slog.Logger
	Client *http.Client
	Now    func() time.Time
	// Backoff gives the wait before retry n (tests).
	Backoff func(n int) time.Duration
}

// Validate refuses a webhook that could leak or be forged.
func (c Config) Validate() error {
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return errors.New("webhook: ASKMYWA_WEBHOOK_URL must be an http(s) URL without credentials")
	}
	if len(c.Secret) < MinSecretLen {
		return fmt.Errorf("webhook: ASKMYWA_WEBHOOK_SECRET must hold at least %d characters", MinSecretLen)
	}
	return nil
}

// Notifier delivers events in order, retrying until the receiver answers 2xx.
type Notifier struct {
	cfg   Config
	mu    sync.Mutex
	queue []Event
	wake  chan struct{}
}

// New builds a notifier; call Validate first, then Run.
func New(cfg Config) *Notifier {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Backoff == nil {
		cfg.Backoff = func(n int) time.Duration {
			d := time.Second << min(n, 8) // 1 s … ~4 min
			return min(d, 5*time.Minute)
		}
	}
	return &Notifier{cfg: cfg, wake: make(chan struct{}, 1)}
}

// Enqueue never blocks the WhatsApp event loop.
func (n *Notifier) Enqueue(e Event) {
	n.mu.Lock()
	if len(n.queue) >= QueueSize {
		n.queue = n.queue[1:]
		n.cfg.Log.Warn("webhook queue full: oldest event dropped")
	}
	n.queue = append(n.queue, e)
	n.mu.Unlock()
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

// Pending is the number of events not yet delivered.
func (n *Notifier) Pending() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.queue)
}

// Run delivers until ctx ends.
func (n *Notifier) Run(ctx context.Context) {
	failures := 0
	for {
		n.mu.Lock()
		var next *Event
		if len(n.queue) > 0 {
			e := n.queue[0]
			next = &e
		}
		n.mu.Unlock()
		if next == nil {
			select {
			case <-ctx.Done():
				return
			case <-n.wake:
				continue
			}
		}
		status, err := n.deliver(ctx, *next)
		switch {
		case err == nil && status >= 200 && status < 300:
			n.pop(next.ID)
			failures = 0
			continue
		case err == nil && status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests:
			// The receiver refuses this event for good: never its content in the log.
			n.cfg.Log.Warn("webhook event refused by the receiver", "status", status)
			n.pop(next.ID)
			failures = 0
			continue
		}
		failures++
		if err != nil {
			n.cfg.Log.Warn("webhook delivery failed, will retry", "attempt", failures)
		} else {
			n.cfg.Log.Warn("webhook delivery failed, will retry", "attempt", failures, "status", status)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(n.cfg.Backoff(failures - 1)):
		}
	}
}

func (n *Notifier) pop(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.queue) > 0 && n.queue[0].ID == id {
		n.queue = n.queue[1:]
	}
}

func (n *Notifier) deliver(ctx context.Context, e Event) (int, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return 0, err
	}
	ts := strconv.FormatInt(n.cfg.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "askmywa-webhook")
	req.Header.Set("X-Askmywa-Event-Id", e.ID)
	req.Header.Set("X-Askmywa-Timestamp", ts)
	req.Header.Set("X-Askmywa-Signature", Sign(n.cfg.Secret, ts, body))
	res, err := n.cfg.Client.Do(req)
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.StatusCode, nil
}

// Sign is the signature header value for a body sent at ts (Unix seconds).
func Sign(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

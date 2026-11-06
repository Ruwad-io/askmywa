package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"

	"whatsapp-mcp/internal/ipc"
	"whatsapp-mcp/internal/store"
)

// Pairing modes for a daemon that has no session yet.
const (
	// PairQR prints a QR code on stdout (interactive terminal).
	PairQR = "qr"
	// PairWait opens the socket and waits for a pair_phone request
	// (launchd service, Claude Desktop extension).
	PairWait = "wait"
)

// ErrLoggedOut tells the caller the session was revoked and the daemon must
// be restarted before pairing again (whatsmeow deletes the device keys).
var ErrLoggedOut = errors.New("this device was logged out by WhatsApp; restart the daemon to pair again")

// pairCodeLifetime is how long WhatsApp keeps a phone-pairing code valid:
// the login websocket closes once its QR codes run out, after about 160 s.
const pairCodeLifetime = 150 * time.Second

// pairState tracks a pairing in progress. Guarded by Daemon.mu.
type pairState struct {
	active  bool
	phone   string
	code    string
	expires time.Time
	lastErr string
}

// pairPhone starts phone-number pairing and returns the 8-character code the
// user types in WhatsApp (Linked devices > Link with phone number). It
// returns immediately; a goroutine waits for the user to enter the code.
// Calling it again for the same number while the code is valid returns the
// same code.
func (d *Daemon) pairPhone(p ipc.PairParams) (ipc.PairResult, error) {
	phone := store.DigitsOf(p.Phone)
	if len(phone) < 7 {
		return ipc.PairResult{}, fmt.Errorf("phone number %q is too short: pass the full international number, with country code and without the leading 0 (for example 33612345678)", p.Phone)
	}
	if strings.HasPrefix(strings.TrimSpace(p.Phone), "0") && !strings.HasPrefix(strings.TrimSpace(p.Phone), "00") {
		return ipc.PairResult{}, fmt.Errorf("phone number %q looks national: pass it with the country code, for example 33612345678 for a French number", p.Phone)
	}
	phone = strings.TrimPrefix(phone, "00")

	d.mu.Lock()
	switch {
	case d.loggedOut:
		d.mu.Unlock()
		return ipc.PairResult{}, ErrLoggedOut
	case d.cli.Store.ID != nil:
		jid := d.cli.Store.ID.ToNonAD().String()
		d.mu.Unlock()
		return ipc.PairResult{AlreadyPaired: true, JID: jid}, nil
	case d.opts.PairMode != PairWait:
		d.mu.Unlock()
		return ipc.PairResult{}, errors.New("this daemon was started in a terminal and is pairing by QR code: scan the code shown there, or stop it and use the service")
	case d.pair.active && d.pair.phone == phone && time.Now().Before(d.pair.expires):
		res := ipc.PairResult{Code: formatCode(d.pair.code), ExpiresAt: d.pair.expires}
		d.mu.Unlock()
		return res, nil
	case d.pair.active:
		d.mu.Unlock()
		return ipc.PairResult{}, errors.New("a pairing for another number is in progress: wait about two minutes for it to expire, then retry")
	}
	d.pair = pairState{active: true, phone: phone}
	d.mu.Unlock()

	code, expires, err := d.startPhonePairing(phone)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		d.pair = pairState{lastErr: err.Error()}
		return ipc.PairResult{}, err
	}
	d.pair.code, d.pair.expires = code, expires
	return ipc.PairResult{Code: formatCode(code), ExpiresAt: expires}, nil
}

func (d *Daemon) startPhonePairing(phone string) (string, time.Time, error) {
	// The pairing outlives the request that started it.
	pctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	if d.cli.IsConnected() {
		d.cli.Disconnect()
	}
	qrChan, err := d.cli.GetQRChannel(pctx)
	if err != nil {
		cancel()
		return "", time.Time{}, fmt.Errorf("prepare pairing: %w", err)
	}
	if err := d.cli.Connect(); err != nil {
		cancel()
		return "", time.Time{}, fmt.Errorf("connect to WhatsApp for pairing: %w", err)
	}
	// whatsmeow needs the first QR event before PairPhone.
	select {
	case item, ok := <-qrChan:
		if !ok || item.Event != "code" {
			cancel()
			d.cli.Disconnect()
			return "", time.Time{}, fmt.Errorf("WhatsApp did not start pairing (event %q); retry in a minute", item.Event)
		}
	case <-time.After(30 * time.Second):
		cancel()
		d.cli.Disconnect()
		return "", time.Time{}, errors.New("WhatsApp did not answer within 30 s; check the internet connection and retry")
	}
	code, err := d.cli.PairPhone(pctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Mac OS)")
	if err != nil {
		cancel()
		d.cli.Disconnect()
		return "", time.Time{}, fmt.Errorf("request a pairing code for %s: %w", phone, err)
	}
	expires := time.Now().Add(pairCodeLifetime)
	d.log.Info("pairing code issued, waiting for it to be entered on the phone")
	go func() {
		defer cancel()
		for item := range qrChan {
			switch item.Event {
			case "code":
				// New QR codes keep coming; they are not shown in phone pairing.
			case "success":
				d.log.Info("paired with WhatsApp")
				d.mu.Lock()
				d.pair = pairState{}
				d.mu.Unlock()
				return
			default:
				msg := item.Event
				if item.Error != nil {
					msg += ": " + item.Error.Error()
				}
				d.log.Warn("pairing ended without success", "event", msg)
				d.cli.Disconnect()
				d.mu.Lock()
				d.pair = pairState{lastErr: "pairing ended without success (" + msg + "); request a new code"}
				d.mu.Unlock()
				return
			}
		}
	}()
	return code, expires, nil
}

// logout unlinks this device from the WhatsApp account (it disappears from
// Linked devices on the phone) and deletes its keys from the session store.
func (d *Daemon) logout(ctx context.Context) (ipc.LogoutResult, error) {
	if !d.isLoggedIn() {
		return ipc.LogoutResult{}, nil
	}
	jid := d.cli.Store.ID.ToNonAD().String()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := d.cli.Logout(ctx); err != nil {
		return ipc.LogoutResult{}, fmt.Errorf("unlink from WhatsApp: %w; you can also remove this computer from Linked devices on the phone", err)
	}
	d.mu.Lock()
	d.loggedOut = true
	d.mu.Unlock()
	d.log.Info("unlinked from WhatsApp at the user's request")
	return ipc.LogoutResult{Unlinked: true, JID: jid}, nil
}

// formatCode renders "ABCD1234" as "ABCD-1234", the way WhatsApp displays it.
func formatCode(code string) string {
	if len(code) == 8 && !strings.Contains(code, "-") {
		return code[:4] + "-" + code[4:]
	}
	return code
}

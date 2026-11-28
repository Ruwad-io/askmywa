package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"askmywa/internal/config"
	"askmywa/internal/httpdoor"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"rsc.io/qr"
)

type account struct {
	daemon   *Daemon
	identity string
	pairing  httpdoor.PairState
	running  bool
	paired   bool
	revoked  bool
}

// The original account keeps the exact data directory, device keys and socket
// used before the HTTP API supported multiple numbers.
type accounts struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	workers sync.WaitGroup
	root    *Daemon
	byID    map[string]*account
}

func openAccounts(ctx context.Context, root *Daemon) (*accounts, error) {
	ctx, cancel := context.WithCancel(ctx)
	a := &accounts{ctx: ctx, cancel: cancel, root: root, byID: make(map[string]*account)}
	if err := a.register("primary", root); err != nil {
		cancel()
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root.opts.Paths.DataDir, "accounts"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		cancel()
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "primary" || !httpdoor.ValidAccountID(entry.Name()) {
			continue
		}
		if _, err := a.load(entry.Name()); err != nil {
			a.close()
			return nil, err
		}
	}
	// Restore clients only after the complete registry exists.
	for id, account := range a.byID {
		if id != "primary" && account.daemon.cli.Store.ID != nil {
			a.workers.Go(func() {
				for a.ctx.Err() == nil {
					if err := account.daemon.cli.ConnectContext(a.ctx); err == nil {
						return
					}
					a.root.log.Warn("account reconnect failed; retrying")
					select {
					case <-a.ctx.Done():
						return
					case <-time.After(15 * time.Second):
					}
				}
			})
		}
	}
	return a, nil
}

func (a *accounts) register(id string, d *Daemon) error {
	path := filepath.Join(d.opts.Paths.DataDir, "account-identity")
	saved, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	identity := string(saved)
	if user := d.cli.Store.GetJID().User; user != "" {
		if identity != "" && identity != user {
			return errors.New("stored account identity mismatch")
		}
		identity = user
		if len(saved) == 0 {
			if err := os.WriteFile(path, []byte(user), 0600); err != nil {
				return err
			}
		}
	}
	current := &account{daemon: d, identity: identity, paired: d.cli.Store.ID != nil}
	d.cli.AddEventHandler(func(event any) {
		a.mu.Lock()
		defer a.mu.Unlock()
		switch event.(type) {
		case *events.PairSuccess, *events.Connected:
			current.paired, current.revoked = true, false
		case *events.LoggedOut:
			current.paired, current.revoked = false, true
		}
	})
	a.byID[id] = current
	d.cli.PrePairCallback = func(jid types.JID, _, _ string) bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		if jid.User == "" || (current.identity != "" && current.identity != jid.User) {
			return false
		}
		for key, other := range a.byID {
			if key != id && other.identity == jid.User {
				return false
			}
		}
		if err := os.WriteFile(path, []byte(jid.User), 0600); err != nil {
			d.log.Error("account identity could not be saved")
			return false
		}
		current.identity = jid.User
		return true
	}
	return nil
}

func (a *accounts) load(id string) (*account, error) {
	opts := a.root.opts
	opts.AccountID, opts.LegacyStore, opts.HTTP = id, "", nil
	opts.Paths = config.FromDataDir(filepath.Join(a.root.opts.Paths.DataDir, "accounts", id))
	d, err := Prepare(a.ctx, opts)
	if err != nil {
		return nil, err
	}
	if err := a.register(id, d); err != nil {
		d.Close()
		return nil, err
	}
	d.startEvents(a.ctx)
	return a.byID[id], nil
}

func (a *accounts) close() {
	// Synchronize cancellation with Pair registering its worker.
	a.mu.Lock()
	a.cancel()
	a.mu.Unlock()
	a.workers.Wait()
	for id, account := range a.byID {
		if id != "primary" {
			account.daemon.cli.Disconnect()
			account.daemon.Close()
		}
	}
}

func (a *accounts) Lookup(id string) (httpdoor.Handler, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	item, ok := a.byID[id]
	if !ok {
		return nil, false
	}
	return item.daemon, true
}

func (a *accounts) Pair(id string, start bool) (httpdoor.PairState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !httpdoor.ValidAccountID(id) || a.ctx.Err() != nil {
		return httpdoor.PairState{}, errors.New("account unavailable")
	}
	entry := a.byID[id]
	if entry == nil {
		if !start {
			return httpdoor.PairState{State: "idle"}, nil
		}
		var err error
		entry, err = a.load(id)
		if err != nil {
			return httpdoor.PairState{}, err
		}
	}
	d := entry.daemon
	if entry.identity != "" && d.cli.IsLoggedIn() {
		return httpdoor.PairState{State: "connected", Phone: "+" + entry.identity}, nil
	}
	if entry.paired {
		return httpdoor.PairState{State: "offline"}, nil
	}
	if !entry.running && start {
		if entry.revoked {
			return httpdoor.PairState{State: "revoked"}, nil
		}
		d.mu.Lock()
		if d.pair.active {
			d.mu.Unlock()
			return httpdoor.PairState{}, errors.New("phone pairing already in progress")
		}
		d.qrPairActive = true
		d.mu.Unlock()
		entry.running = true
		entry.pairing = httpdoor.PairState{State: "starting"}
		a.workers.Go(func() { a.runQR(entry) })
	}
	state := entry.pairing
	if state.State == "" {
		state.State = "idle"
	}
	if state.ExpiresAt != nil && !time.Now().Before(*state.ExpiresAt) {
		return httpdoor.PairState{State: "starting"}, nil
	}
	return state, nil
}

func (a *accounts) runQR(entry *account) {
	final := "failed"
	defer func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		entry.daemon.mu.Lock()
		entry.daemon.qrPairActive = false
		entry.daemon.mu.Unlock()
		entry.running = false
		entry.pairing = httpdoor.PairState{State: final}
	}()
	ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
	defer cancel()
	cli := entry.daemon.cli
	channel, err := cli.GetQRChannel(ctx)
	if err != nil {
		return
	}
	if err := cli.ConnectContext(a.ctx); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			cli.Disconnect()
			final = "expired"
			return
		case item, ok := <-channel:
			if !ok {
				return
			}
			switch item.Event {
			case "code":
				code, err := qr.Encode(item.Code, qr.M)
				if err != nil {
					return
				}
				expires := time.Now().Add(item.Timeout)
				a.mu.Lock()
				entry.pairing = httpdoor.PairState{State: "qr", QR: code.PNG(), ExpiresAt: &expires}
				a.mu.Unlock()
			case "success":
				a.root.log.Info("WhatsApp QR pairing accepted")
				final = "offline"
				return
			case "timeout":
				final = "expired"
				return
			default:
				return
			}
		}
	}
}

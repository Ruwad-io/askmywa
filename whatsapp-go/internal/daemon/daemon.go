// Package daemon keeps the WhatsApp connection: it stores every message in
// messages.db, resolves names, and serves send and download requests over
// the Unix socket.
package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mdp/qrterminal"
	"go.mau.fi/whatsmeow"
	wastore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-mcp/internal/config"
	"whatsapp-mcp/internal/ipc"
	"whatsapp-mcp/internal/store"
)

// Options configures Run.
type Options struct {
	Paths config.Paths
	// LegacyStore is the old whatsapp-bridge/store directory. When set, its
	// whatsapp.db is copied to the data directory if no session exists yet,
	// and its messages.db is imported if the message table is empty.
	LegacyStore string
	Log         *slog.Logger
	// QROut receives the pairing QR code (stdout by default).
	QROut *os.File
	// PairMode is PairQR or PairWait; it only matters when there is no session.
	PairMode string
}

// Daemon is the running service.
type Daemon struct {
	opts    Options
	log     *slog.Logger
	store   *store.Store
	session *sql.DB
	cli     *whatsmeow.Client
	started time.Time

	mu         sync.Mutex
	linked     map[string]bool   // "phone|lid" pairs already recorded
	pushNames  map[string]string // identity key -> last push name recorded
	groupNames map[string]string // group JID -> name
	loggedOut  bool
	pair       pairState
}

// Prepare opens the stores, copies the legacy session if needed, syncs the
// contact names and imports the legacy history, without connecting to
// WhatsApp. Run calls it; the import subcommand uses it alone.
func Prepare(ctx context.Context, opts Options) (*Daemon, error) {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.QROut == nil {
		opts.QROut = os.Stdout
	}
	p := opts.Paths
	if err := p.EnsureDirs(); err != nil {
		return nil, err
	}
	if err := ensureSession(ctx, p, opts.LegacyStore, opts.Log); err != nil {
		return nil, err
	}
	d := &Daemon{opts: opts, log: opts.Log, started: time.Now(),
		linked: map[string]bool{}, pushNames: map[string]string{}, groupNames: map[string]string{}}

	st, err := store.Open(ctx, p.MessagesDB, false)
	if err != nil {
		return nil, err
	}
	d.store = st

	session, err := sql.Open("sqlite", sessionDSN(p.SessionDB))
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("open session store: %w", err)
	}
	session.SetMaxOpenConns(1)
	d.session = session
	container := sqlstore.NewWithDB(session, "sqlite3", newWALogger(d.log, "sqlstore"))
	if err := container.Upgrade(ctx); err != nil {
		d.Close()
		return nil, fmt.Errorf("upgrade session store: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		device = container.NewDevice()
		d.log.Info("no paired device in the session store, a QR code will be shown")
	} else if err != nil {
		d.Close()
		return nil, fmt.Errorf("read device: %w", err)
	}
	// Name shown in WhatsApp > Linked devices for new pairings.
	wastore.SetOSInfo("WhatsApp MCP", [3]uint32{1, 0, 0})
	d.cli = whatsmeow.NewClient(device, newWALogger(d.log, "whatsmeow"))

	if d.cli.Store.ID == nil {
		d.log.Info("no session yet, skipping the contact sync")
	} else if n, err := d.syncNames(ctx); err != nil {
		d.log.Warn("initial contact sync failed", "error", err)
	} else {
		d.log.Info("contacts synced from session store", "changed", n)
	}
	if err := d.importLegacy(ctx); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// Close releases the databases (the connection, if any, is closed by Run).
func (d *Daemon) Close() {
	if d.session != nil {
		d.session.Close()
	}
	if d.store != nil {
		d.store.Close()
	}
}

// Store exposes the message store (status output of the import command).
func (d *Daemon) Store() *store.Store { return d.store }

// Run starts the daemon and blocks until ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	d, err := Prepare(ctx, opts)
	if err != nil {
		return err
	}
	defer d.Close()
	p := opts.Paths

	d.cli.AddEventHandler(d.handleEvent)
	errCh := make(chan error, 1)
	go func() { errCh <- ipc.Serve(ctx, p.Socket, d, d.log) }()
	defer d.cli.Disconnect()
	if d.cli.Store.ID == nil && opts.PairMode == PairWait {
		d.log.Info("not paired yet: waiting for a pairing request (connect_whatsapp tool)")
	} else if err := d.connect(ctx); err != nil {
		return err
	}
	d.log.Info("daemon ready", "data_dir", p.DataDir, "send_root", p.SendRoot)
	select {
	case <-ctx.Done():
		d.log.Info("shutting down")
		return nil
	case err := <-errCh:
		return err
	}
}

func sessionDSN(path string) string {
	return store.FileURI(path) + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)"
}

// ensureSession copies the legacy whatsapp.db into the data directory when
// no session exists there yet. The original is opened read-only and never
// modified; VACUUM INTO produces a consistent copy.
func ensureSession(ctx context.Context, p config.Paths, legacyDir string, log *slog.Logger) error {
	if _, err := os.Stat(p.SessionDB); err == nil {
		return nil
	}
	if legacyDir == "" {
		return nil
	}
	src := filepath.Join(legacyDir, "whatsapp.db")
	if _, err := os.Stat(src); err != nil {
		// Pairing a new device here would silently consume a linked-device
		// slot, so a wrong --legacy-store is an error, not a fallback.
		return fmt.Errorf("--legacy-store %s has no whatsapp.db (%w): pass the absolute path of the old whatsapp-bridge/store directory, or omit the flag to pair a new device", legacyDir, err)
	}
	if err := copySQLite(ctx, src, p.SessionDB); err != nil {
		return fmt.Errorf("copy legacy session %s: %w", src, err)
	}
	log.Info("legacy session copied", "from", src, "to", p.SessionDB)
	return nil
}

func copySQLite(ctx context.Context, src, dst string) error {
	db, err := sql.Open("sqlite", store.FileURI(src)+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	tmp := dst + ".tmp"
	os.Remove(tmp)
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func (d *Daemon) importLegacy(ctx context.Context) error {
	if d.opts.LegacyStore == "" {
		return nil
	}
	src := filepath.Join(d.opts.LegacyStore, "messages.db")
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	counts, err := d.store.Count(ctx)
	if err != nil {
		return err
	}
	if counts.Messages > 0 {
		d.log.Info("legacy import skipped: messages already present", "messages", counts.Messages)
		return nil
	}
	d.log.Info("importing legacy history", "from", src)
	stats, err := d.store.ImportLegacy(ctx, src, d.log)
	if err != nil {
		return fmt.Errorf("import legacy history: %w", err)
	}
	d.log.Info("legacy history imported", "chats", stats.Chats, "messages", stats.Messages, "skipped", stats.Skipped)
	return nil
}

// connect pairs (QR) when needed and connects.
func (d *Daemon) connect(ctx context.Context) error {
	if d.cli.Store.ID != nil {
		if err := d.cli.Connect(); err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		d.log.Info("connecting with the stored session", "jid", d.cli.Store.ID.String())
		return nil
	}
	qrChan, err := d.cli.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("prepare pairing: %w", err)
	}
	if err := d.cli.Connect(); err != nil {
		return fmt.Errorf("connect for pairing: %w", err)
	}
	fmt.Fprintln(d.opts.QROut, "Scan this QR code with WhatsApp (Linked devices > Link a device):")
	for item := range qrChan {
		switch item.Event {
		case "code":
			qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, d.opts.QROut)
			fmt.Fprintf(d.opts.QROut, "(a new code appears every %s)\n", item.Timeout.Round(time.Second))
		case "success":
			fmt.Fprintln(d.opts.QROut, "Paired.")
			return nil
		case "timeout":
			return errors.New("pairing timed out: run `whatsapp-mcp daemon` again and scan the code")
		case "error":
			return fmt.Errorf("pairing failed: %w", item.Error)
		default:
			d.log.Warn("pairing event", "event", item.Event)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("pairing ended without success")
}

// handleEvent dispatches whatsmeow events.
func (d *Daemon) handleEvent(evt any) {
	ctx := context.Background()
	switch v := evt.(type) {
	case *events.Message:
		d.handleMessage(ctx, v)
	case *events.HistorySync:
		d.handleHistorySync(ctx, v)
	case *events.Contact:
		d.handleContact(ctx, v)
	case *events.PushName:
		d.handlePushName(ctx, v)
	case *events.BusinessName:
		phone, lid := identityOf(v.JID, types.EmptyJID)
		d.putName(ctx, phone, lid, v.NewBusinessName, store.SourceBusinessName)
	case *events.GroupInfo:
		if v.Name != nil && v.Name.Name != "" {
			d.setGroupName(ctx, v.JID, v.Name.Name)
		}
	case *events.JoinedGroup:
		if v.Name != "" {
			d.setGroupName(ctx, v.JID, v.Name)
		}
	case *events.Connected:
		d.mu.Lock()
		d.loggedOut = false
		d.mu.Unlock()
		d.log.Info("connected to WhatsApp")
		go func() {
			if n, err := d.syncNames(ctx); err != nil {
				d.log.Warn("contact sync failed", "error", err)
			} else if n > 0 {
				d.log.Info("contacts refreshed", "changed", n)
			}
		}()
	case *events.OfflineSyncCompleted:
		d.log.Info("offline sync completed", "count", v.Count)
	case *events.Disconnected:
		d.log.Warn("disconnected, whatsmeow will reconnect")
	case *events.StreamReplaced:
		d.log.Error("stream replaced: another process is using this session; stop the old bridge or any other whatsapp-mcp daemon using the same session")
	case *events.LoggedOut:
		d.mu.Lock()
		d.loggedOut = true
		d.mu.Unlock()
		d.log.Error("logged out by WhatsApp: pair again with the connect_whatsapp tool or `whatsapp-mcp daemon`", "reason", v.Reason.String())
	case *events.TemporaryBan:
		d.log.Error("temporary ban", "code", v.Code, "expire", v.Expire)
	case *events.KeepAliveTimeout:
		d.log.Warn("keepalive timeout", "error_count", v.ErrorCount)
	}
}

// identityOf extracts the phone number and LID from a JID and its alternative.
func identityOf(j, alt types.JID) (phone, lid string) {
	for _, x := range []types.JID{j, alt} {
		switch x.Server {
		case types.DefaultUserServer:
			if phone == "" {
				phone = x.User
			}
		case types.HiddenUserServer:
			if lid == "" {
				lid = x.User
			}
		}
	}
	return phone, lid
}

func (d *Daemon) ownIdentity() (phone, lid string) {
	if d.cli.Store.ID != nil {
		phone = d.cli.Store.ID.User
	}
	lid = d.cli.Store.LID.User
	return phone, lid
}

func (d *Daemon) isLoggedIn() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cli.Store.ID != nil && !d.loggedOut
}

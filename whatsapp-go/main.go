// Command whatsapp-mcp is a single binary that archives a WhatsApp account
// (daemon), exposes it to MCP clients over stdio (serve), and installs
// itself as a launchd agent (install-service / uninstall-service).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/natefinch/lumberjack.v2"

	"whatsapp-mcp/internal/config"
	"whatsapp-mcp/internal/daemon"
	"whatsapp-mcp/internal/ipc"
	"whatsapp-mcp/internal/mcpserver"
	"whatsapp-mcp/internal/service"
	"whatsapp-mcp/internal/store"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `whatsapp-mcp — WhatsApp archive and MCP server in one binary

Usage:
  whatsapp-mcp daemon [--data-dir DIR] [--send-root DIR] [--legacy-store DIR] [--log-file FILE]
      Connect to WhatsApp (QR code on first run), archive messages, serve the socket.
  whatsapp-mcp serve [--data-dir DIR]
      MCP server over stdio (for claude_desktop_config.json).
  whatsapp-mcp import --legacy-store DIR [--data-dir DIR]
      Copy the old bridge's session, sync contact names and import its history, without connecting.
  whatsapp-mcp install-service [--data-dir DIR] [--send-root DIR] [--legacy-store DIR]
      Install and start the launchd agent ` + config.ServiceLabel + `.
  whatsapp-mcp uninstall-service
      Stop and remove the launchd agent.
  whatsapp-mcp status [--data-dir DIR]
      Show the daemon's connection state and database counts.
  whatsapp-mcp version

Environment: WHATSAPP_MCP_DATA_DIR, WHATSAPP_MCP_SEND_ROOT.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "daemon":
		err = runDaemon(os.Args[2:])
	case "serve":
		err = runServe(os.Args[2:])
	case "import":
		err = runImport(os.Args[2:])
	case "install-service":
		err = runInstall(os.Args[2:])
	case "uninstall-service":
		err = runUninstall()
	case "status":
		err = runStatus(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("whatsapp-mcp", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func pathFlags(fs *flag.FlagSet) (dataDir, sendRoot *string) {
	dataDir = fs.String("data-dir", "", "data directory (default ~/Library/Application Support/whatsapp-mcp or $WHATSAPP_MCP_DATA_DIR)")
	sendRoot = fs.String("send-root", "", "directory files may be sent from (default <data-dir>/files or $WHATSAPP_MCP_SEND_ROOT)")
	return
}

func resolvePaths(dataDir, sendRoot string) (config.Paths, error) {
	p, err := config.Default()
	if err != nil {
		return p, err
	}
	if dataDir != "" {
		abs, err := filepath.Abs(dataDir)
		if err != nil {
			return p, err
		}
		logDir := p.LogDir
		p = config.FromDataDir(abs)
		p.LogDir = logDir
		if root := os.Getenv("WHATSAPP_MCP_SEND_ROOT"); root != "" {
			p.SendRoot = root
		}
	}
	if sendRoot != "" {
		abs, err := filepath.Abs(sendRoot)
		if err != nil {
			return p, err
		}
		p.SendRoot = abs
	}
	return p, nil
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func runDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	dataDir, sendRoot := pathFlags(fs)
	legacy := fs.String("legacy-store", "", "old whatsapp-bridge/store directory: its session is copied and its history imported on first run")
	logFile := fs.String("log-file", "", "write logs to this file with rotation instead of stderr")
	pairMode := fs.String("pair", "auto", "without a session: qr (show a QR code), wait (wait for connect_whatsapp), or auto (qr in a terminal, wait otherwise)")
	debug := fs.Bool("debug", false, "verbose logging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths, err := resolvePaths(*dataDir, *sendRoot)
	if err != nil {
		return err
	}
	var out io.Writer = os.Stderr
	if *logFile != "" {
		if err := os.MkdirAll(filepath.Dir(*logFile), 0o700); err != nil {
			return err
		}
		out = &lumberjack.Logger{Filename: *logFile, MaxSize: 10, MaxBackups: 5, MaxAge: 30, Compress: true}
	}
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: level}))
	log.Info("whatsapp-mcp daemon starting", "version", version)
	ctx, cancel := signalContext()
	defer cancel()
	legacyDir := *legacy
	if legacyDir != "" {
		if legacyDir, err = filepath.Abs(legacyDir); err != nil {
			return err
		}
	}
	mode := *pairMode
	switch mode {
	case "auto":
		mode = daemon.PairWait
		if st, err := os.Stdout.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
			mode = daemon.PairQR
		}
	case daemon.PairQR, daemon.PairWait:
	default:
		return fmt.Errorf("invalid --pair %q: use qr, wait or auto", mode)
	}
	return daemon.Run(ctx, daemon.Options{Paths: paths, LegacyStore: legacyDir, Log: log, QROut: os.Stdout, PairMode: mode})
}

func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	dataDir, sendRoot := pathFlags(fs)
	legacy := fs.String("legacy-store", "", "old whatsapp-bridge/store directory (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *legacy == "" {
		return errors.New("--legacy-store is required")
	}
	legacyDir, err := filepath.Abs(*legacy)
	if err != nil {
		return err
	}
	paths, err := resolvePaths(*dataDir, *sendRoot)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, cancel := signalContext()
	defer cancel()
	d, err := daemon.Prepare(ctx, daemon.Options{Paths: paths, LegacyStore: legacyDir, Log: log})
	if err != nil {
		return err
	}
	defer d.Close()
	counts, err := d.Store().Count(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("session: %s\nmessages.db: %d contacts, %d chats, %d messages\n", paths.SessionDB, counts.Contacts, counts.Chats, counts.Messages)
	return nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dataDir, sendRoot := pathFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths, err := resolvePaths(*dataDir, *sendRoot)
	if err != nil {
		return err
	}
	// stdout is the MCP transport: everything else goes to stderr.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx, cancel := signalContext()
	defer cancel()
	// The send folder is where the user drops files to send; make sure it exists.
	_ = os.MkdirAll(paths.SendRoot, 0o700)
	// The archive may not exist yet (first run of the Claude Desktop
	// extension): the server then starts anyway and opens it on first use.
	var st *store.Store
	if _, err := os.Stat(paths.MessagesDB); err == nil {
		if st, err = store.Open(ctx, paths.MessagesDB, true); err != nil {
			return err
		}
		defer st.Close()
	}
	var svc mcpserver.ServiceManager
	if runtime.GOOS == "darwin" {
		if exe, err := os.Executable(); err == nil {
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				exe = resolved
			}
			svc = service.Launchd{Binary: exe}
		}
	}
	mcpserver.Version = version
	srv := mcpserver.New(st, ipc.NewClient(paths.Socket), svc, paths, log)
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func runInstall(args []string) error {
	fs := flag.NewFlagSet("install-service", flag.ExitOnError)
	dataDir, sendRoot := pathFlags(fs)
	legacy := fs.String("legacy-store", "", "old whatsapp-bridge/store directory to migrate from on first start")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths, err := resolvePaths(*dataDir, *sendRoot)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var extra []string
	if *legacy != "" {
		abs, err := filepath.Abs(*legacy)
		if err != nil {
			return err
		}
		extra = append(extra, "--legacy-store", abs)
	}
	plist, err := service.Install(exe, paths, extra)
	if err != nil {
		return err
	}
	logFile := filepath.Join(paths.LogDir, "daemon.log")
	fmt.Printf("Installed %s\nLogs: %s\n", plist, logFile)
	// launchd may hold a restarted job for its 10 s throttle interval, so
	// wait for the daemon to answer before reporting.
	fmt.Println("Waiting for the daemon to connect...")
	client := ipc.NewClient(paths.Socket)
	deadline := time.Now().Add(45 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		st, err := client.Status(ctx)
		cancel()
		if err == nil && st.Connected {
			_, pid := service.Status()
			fmt.Printf("%s is running (pid %s) and connected as %s\n", config.ServiceLabel, pid, st.JID)
			return nil
		}
		if err == nil && !st.LoggedIn {
			_, pid := service.Status()
			fmt.Printf("%s is running (pid %s) but not linked to WhatsApp yet.\n", config.ServiceLabel, pid)
			fmt.Println("Ask Claude to connect WhatsApp (it gives you a code to type on your phone), or")
			fmt.Printf("uninstall the service and run `%s daemon` in a terminal to scan a QR code.\n", exe)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the daemon is not connected after 45 s; see %s", logFile)
		}
		time.Sleep(time.Second)
	}
}

func runUninstall() error {
	if err := service.Uninstall(); err != nil {
		return err
	}
	fmt.Println("Removed", config.ServiceLabel)
	return nil
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	dataDir, sendRoot := pathFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths, err := resolvePaths(*dataDir, *sendRoot)
	if err != nil {
		return err
	}
	if loaded, pid := service.Status(); loaded {
		fmt.Printf("launchd agent %s: loaded (pid %s)\n", config.ServiceLabel, pid)
	} else {
		fmt.Printf("launchd agent %s: not loaded\n", config.ServiceLabel)
	}
	ctx, cancel := signalContext()
	defer cancel()
	st, err := ipc.NewClient(paths.Socket).Status(ctx)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(st)
}

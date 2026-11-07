package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"askmywa/internal/ipc"
	"askmywa/internal/service"
)

// linker is the part of the daemon client that link needs.
type linker interface {
	Status(ctx context.Context) (ipc.StatusResult, error)
	Pair(ctx context.Context, p ipc.PairParams) (ipc.PairResult, error)
}

// runLink links WhatsApp from a terminal with a phone-pairing code, through
// the running daemon: the same flow as the connect_whatsapp tool, for
// machines without Claude Desktop such as a Raspberry Pi reached over SSH.
func runLink(args []string) error {
	fs := flag.NewFlagSet("link", flag.ExitOnError)
	dataDir, sendRoot := pathFlags(fs)
	phone := fs.String("phone", "", "phone number of the WhatsApp account, with country code (asked if omitted)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	paths, err := resolvePaths(*dataDir, *sendRoot)
	if err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	client := ipc.NewClient(paths.Socket)

	st, err := client.Status(ctx)
	if err != nil {
		return err // already says to run install-service
	}
	if st.LoggedOut {
		// WhatsApp revoked this device: the daemon pairs again after a restart.
		if !service.Supported() {
			return errors.New("WhatsApp unlinked this device: restart the daemon, then run link again")
		}
		fmt.Println("WhatsApp unlinked this device; restarting the service…")
		if err := service.Restart(); err != nil {
			return fmt.Errorf("restart the service: %w", err)
		}
		if err := waitReady(ctx, client, 60*time.Second); err != nil {
			return err
		}
	}
	number := *phone
	if number == "" && !st.LoggedIn {
		if number, err = askPhone(os.Stdin, os.Stdout); err != nil {
			return err
		}
	}
	return link(ctx, client, number, os.Stdout, 2*time.Second)
}

// link asks the daemon for a pairing code, prints it and waits until the
// phone has entered it or the code expires.
func link(ctx context.Context, c linker, phone string, out io.Writer, poll time.Duration) error {
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	if st.LoggedIn {
		fmt.Fprintf(out, "WhatsApp is already linked to %s.\n", account(st.JID))
		return nil
	}
	res, err := c.Pair(ctx, ipc.PairParams{Phone: phone})
	if err != nil {
		return err
	}
	if res.AlreadyPaired {
		fmt.Fprintf(out, "WhatsApp is already linked to %s.\n", account(res.JID))
		return nil
	}
	fmt.Fprintf(out, `
Your code: %s

On your phone, open WhatsApp:
  iPhone:  Settings > Linked devices > Link a device
  Android: ⋮ menu > Linked devices > Link a device
Choose "Link with phone number instead" and type the code.
Waiting for the phone (the code is valid until %s)…
`, res.Code, res.ExpiresAt.Local().Format("15:04:05"))

	deadline := res.ExpiresAt.Add(30 * time.Second)
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		st, err := c.Status(ctx)
		if err != nil {
			continue // the daemon reconnects right after pairing
		}
		if st.LoggedIn {
			fmt.Fprintf(out, "\nLinked to %s. Messages start arriving in the next few minutes.\n", account(st.JID))
			return nil
		}
		if st.PairError != "" {
			return fmt.Errorf("pairing failed: %s; run `askmywa link` again for a new code", st.PairError)
		}
		if time.Now().After(deadline) {
			return errors.New("the code expired before it was entered; run `askmywa link` again for a new one")
		}
	}
}

// waitReady waits for the daemon socket to answer after a restart.
func waitReady(ctx context.Context, c linker, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := c.Status(ctx); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("the service did not come back after the restart; see `askmywa status`")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func askPhone(in io.Reader, out io.Writer) (string, error) {
	fmt.Fprint(out, "Phone number of your WhatsApp account, with country code (for example 33612345678): ")
	line, err := bufio.NewReader(in).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		if err == nil || errors.Is(err, io.EOF) {
			return "", errors.New("no phone number given; pass it with --phone")
		}
		return "", err
	}
	return line, nil
}

func account(jid string) string {
	return strings.TrimSuffix(jid, "@s.whatsapp.net")
}

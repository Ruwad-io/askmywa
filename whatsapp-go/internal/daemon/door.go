package daemon

import (
	"context"

	"askmywa/internal/httpdoor"
	"askmywa/internal/ipc"
)

// DoorStatus implements httpdoor.Handler.
func (d *Daemon) DoorStatus(_ context.Context) (httpdoor.Status, error) {
	return httpdoor.Status{Connected: d.cli.IsConnected(), LoggedIn: d.isLoggedIn(), SendOnly: d.opts.SendOnly}, nil
}

// DoorContactName implements httpdoor.Handler: the name stored for these
// digits (address book, push name or business name), "" when none.
func (d *Daemon) DoorContactName(ctx context.Context, digits string) (string, error) {
	return d.store.ContactName(ctx, digits, "")
}

// DoorSend implements httpdoor.Handler: one text to one person.
func (d *Daemon) DoorSend(ctx context.Context, digits, text string) (httpdoor.Sent, error) {
	res, err := d.sendText(ctx, ipc.SendMessageParams{To: digits, Text: text})
	if err != nil {
		return httpdoor.Sent{}, err
	}
	return httpdoor.Sent{MessageID: res.MessageID, Timestamp: res.Timestamp}, nil
}

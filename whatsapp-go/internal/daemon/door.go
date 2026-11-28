package daemon

import (
	"context"

	"askmywa/internal/httpdoor"
	"askmywa/internal/ipc"
	"askmywa/internal/store"
)

// DoorStatus implements httpdoor.Handler.
func (d *Daemon) DoorStatus(_ context.Context) (httpdoor.Status, error) {
	return httpdoor.Status{Connected: d.cli.IsConnected(), LoggedIn: d.isLoggedIn(), SendOnly: d.opts.SendOnly}, nil
}

// DoorContactName implements httpdoor.Handler: the name the owner gave this
// number in his address book, "" when none. A push name or a business name
// is chosen by the other person (anyone can call themselves « Maman »), so it
// never vouches for a number.
func (d *Daemon) DoorContactName(ctx context.Context, digits string) (string, error) {
	c, ok, err := d.store.LookupContact(ctx, digits, "")
	if err != nil || !ok {
		return "", err
	}
	if c.NameSource != store.SourceFullName && c.NameSource != store.SourceFirstName {
		return "", nil
	}
	return c.Name, nil
}

// DoorSend implements httpdoor.Handler: one text to one person.
func (d *Daemon) DoorSend(ctx context.Context, digits, text string) (httpdoor.Sent, error) {
	// A caller that gives up must not cut a send WhatsApp may already have
	// taken: the send runs to its end (send() bounds it to two minutes).
	res, err := d.sendText(context.WithoutCancel(ctx), ipc.SendMessageParams{To: digits, Text: text})
	if err != nil {
		return httpdoor.Sent{}, err
	}
	return httpdoor.Sent{MessageID: res.MessageID, Timestamp: res.Timestamp}, nil
}

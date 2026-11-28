package daemon

import (
	"context"
	"testing"

	"askmywa/internal/config"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/types"
)

func TestAccountRegistryRestoresOriginalAndIsolatedIdentities(t *testing.T) {
	const id = "22222222-1111-4111-8111-111111111111"
	opts := Options{Paths: config.FromDataDir(t.TempDir()), SendOnly: true}
	ctx := context.Background()
	root, err := Prepare(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an installation already paired before the registry existed.
	jid := types.NewJID("33600000001", types.DefaultUserServer)
	device := root.cli.Store
	device.ID = &jid
	device.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte{1}, AccountSignature: make([]byte, 64), AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64)}
	if err := device.Save(ctx); err != nil {
		t.Fatal(err)
	}
	originalPublicKey := *device.IdentityKey.Pub
	root.Close()
	root, err = Prepare(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if root.cli.Store.GetJID() != jid || *root.cli.Store.IdentityKey.Pub != originalPublicKey {
		t.Fatal("original linked device was replaced")
	}
	registry, err := openAccounts(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	extra, err := registry.load(id)
	if err != nil {
		t.Fatal(err)
	}
	if extra.daemon.opts.Paths.SessionDB == root.opts.Paths.SessionDB {
		t.Fatal("shared session database")
	}
	primaryPhone := types.NewJID("33600000001", types.DefaultUserServer)
	otherPhone := types.NewJID("33600000002", types.DefaultUserServer)
	if !root.cli.PrePairCallback(primaryPhone, "", "") || extra.daemon.cli.PrePairCallback(primaryPhone, "", "") || !extra.daemon.cli.PrePairCallback(otherPhone, "", "") {
		t.Fatal("account identity collision")
	}
	registry.close()
	root.Close()
	root, err = Prepare(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	registry, err = openAccounts(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.close()
	if registry.byID["primary"].daemon != root || registry.byID[id].identity != otherPhone.User || registry.byID["primary"].identity != primaryPhone.User {
		t.Fatal("lost identity after restart")
	}
	if registry.byID[id].daemon.cli.PrePairCallback(primaryPhone, "", "") {
		t.Fatal("a bound account changed numbers")
	}
}

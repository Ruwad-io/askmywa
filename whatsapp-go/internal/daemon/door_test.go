package daemon

import (
	"context"
	"path/filepath"
	"testing"

	"askmywa/internal/store"
)

// A name the other person chose (push or business name) never vouches for a number.
func TestDoorContactNameTrustsOnlyTheAddressBook(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "messages.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d := &Daemon{store: st}
	if _, err := st.PutContact(ctx, "33600000001", "", "Maman", store.SourcePushName); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutContact(ctx, "33600000002", "", "Banque", store.SourceBusinessName); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutContact(ctx, "33600000003", "", "Karim Tech", store.SourceFullName); err != nil {
		t.Fatal(err)
	}
	for digits, want := range map[string]string{"33600000001": "", "33600000002": "", "33600000003": "Karim Tech", "33600000004": ""} {
		got, err := d.DoorContactName(ctx, digits)
		if err != nil || got != want {
			t.Fatalf("%s: %q, %v (want %q)", digits, got, err, want)
		}
	}
}

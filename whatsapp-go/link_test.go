package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"whatsapp-mcp/internal/ipc"
)

// fakeLinker returns the given statuses in order, repeating the last one.
type fakeLinker struct {
	statuses []ipc.StatusResult
	pair     ipc.PairResult
	phone    string
	calls    int
}

func (f *fakeLinker) Status(context.Context) (ipc.StatusResult, error) {
	i := min(f.calls, len(f.statuses)-1)
	f.calls++
	return f.statuses[i], nil
}

func (f *fakeLinker) Pair(_ context.Context, p ipc.PairParams) (ipc.PairResult, error) {
	f.phone = p.Phone
	return f.pair, nil
}

const testJID = "15550000001@s.whatsapp.net"

func TestLinkPrintsCodeAndWaits(t *testing.T) {
	f := &fakeLinker{
		statuses: []ipc.StatusResult{{}, {Pairing: true}, {LoggedIn: true, JID: testJID}},
		pair:     ipc.PairResult{Code: "ABCD-EFGH", ExpiresAt: time.Now().Add(2 * time.Minute)},
	}
	var out bytes.Buffer
	if err := link(context.Background(), f, "15550000001", &out, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if f.phone != "15550000001" {
		t.Errorf("phone sent = %q", f.phone)
	}
	for _, want := range []string{"ABCD-EFGH", "Link with phone number instead", "Linked to 15550000001"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestLinkAlreadyLinked(t *testing.T) {
	f := &fakeLinker{statuses: []ipc.StatusResult{{LoggedIn: true, JID: testJID}}}
	var out bytes.Buffer
	if err := link(context.Background(), f, "", &out, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if f.phone != "" || !strings.Contains(out.String(), "already linked to 15550000001") {
		t.Errorf("unexpected: phone=%q output=%q", f.phone, out.String())
	}
}

func TestLinkReportsPairError(t *testing.T) {
	f := &fakeLinker{
		statuses: []ipc.StatusResult{{}, {PairError: "code rejected"}},
		pair:     ipc.PairResult{Code: "ABCD-EFGH", ExpiresAt: time.Now().Add(2 * time.Minute)},
	}
	err := link(context.Background(), f, "15550000001", &bytes.Buffer{}, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "code rejected") {
		t.Fatalf("err = %v", err)
	}
}

func TestLinkExpires(t *testing.T) {
	f := &fakeLinker{
		statuses: []ipc.StatusResult{{}},
		pair:     ipc.PairResult{Code: "ABCD-EFGH", ExpiresAt: time.Now().Add(-time.Minute)},
	}
	err := link(context.Background(), f, "15550000001", &bytes.Buffer{}, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v", err)
	}
}

func TestAskPhone(t *testing.T) {
	got, err := askPhone(strings.NewReader(" 15550000001 \n"), &bytes.Buffer{})
	if err != nil || got != "15550000001" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := askPhone(strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Fatal("empty input accepted")
	}
}

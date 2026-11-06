package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// fixture seeds a store with three people and two chats:
//
//   - Hani: address-book full name under the phone JID, push name under a
//     separate LID row, linked by the LID map (like 802 of the user's contacts).
//   - Élodie: only a push name, only a LID (no phone known).
//   - Marc: phone only, first name only.
//
// Messages come in the three identity shapes the old bridge mixed up.
func fixture(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	s, err := OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	put := func(phone, lid, name, source string) {
		t.Helper()
		_, err := s.PutContact(ctx, phone, lid, name, source)
		must(err)
	}
	put("33600000001", "", "Hany", SourcePushName)
	put("", "100000000000001", "Hani.M", SourcePushName)
	put("33600000001", "", "Hani Comptable", SourceFullName)
	put("33600000001", "100000000000001", "", "") // LID map link
	put("", "100000000000002", "Élodie", SourcePushName)
	put("33612345678", "", "Marc", SourceFirstName)

	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	must(s.PutChat(ctx, "120363@g.us", "Copropriété", true, base.Add(5*time.Hour)))
	must(s.PutChat(ctx, "33600000001@s.whatsapp.net", "Hani Comptable", false, base.Add(3*time.Hour)))
	must(s.PutChat(ctx, "100000000000002@lid", "", false, base.Add(1*time.Hour)))
	must(s.PutChat(ctx, "33612345678@s.whatsapp.net", "Marc", false, base.Add(2*time.Hour)))

	msgs := []NewMessage{
		{ID: "M1", ChatJID: "120363@g.us", SenderLID: "100000000000001", SenderName: "Hani Comptable", Content: "Bonjour à tous, l'école est fermée", Timestamp: base},
		{ID: "M2", ChatJID: "120363@g.us", SenderPhone: "33612345678", SenderName: "Marc", Content: "Merci Hani", Timestamp: base.Add(1 * time.Hour)},
		{ID: "M3", ChatJID: "120363@g.us", SenderLID: "100000000000002", SenderName: "Élodie", Content: "Réunion demain à 18h", Timestamp: base.Add(2 * time.Hour)},
		{ID: "M4", ChatJID: "33600000001@s.whatsapp.net", SenderPhone: "33600000001", SenderLID: "100000000000001", SenderName: "Hani Comptable", Content: "Le bilan est prêt", Timestamp: base.Add(3 * time.Hour)},
		{ID: "M5", ChatJID: "33600000001@s.whatsapp.net", SenderPhone: "33699999999", IsFromMe: true, Content: "Parfait, merci", Timestamp: base.Add(4 * time.Hour)},
		{ID: "M6", ChatJID: "120363@g.us", SenderPhone: "33600000001", SenderName: "Hani Comptable", Content: "Photo du compteur", Timestamp: base.Add(5 * time.Hour), Media: &MediaInfo{MediaType: "image", Filename: "photo.jpg", DirectPath: "/v/t62.jpg", MediaKey: []byte{1}, FileSHA256: []byte{2}, FileEncSHA256: []byte{3}, FileLength: 10}},
		{ID: "M7", ChatJID: "100000000000002@lid", SenderLID: "100000000000002", SenderName: "Élodie", Content: "Coucou", Timestamp: base.Add(1 * time.Hour)},
		{ID: "M8", ChatJID: "33612345678@s.whatsapp.net", SenderPhone: "33612345678", SenderName: "Marc", Content: "ecole ou école ?", Timestamp: base.Add(2 * time.Hour)},
	}
	for _, m := range msgs {
		ok, err := s.PutMessage(ctx, m)
		must(err)
		if !ok {
			t.Fatalf("message %s skipped", m.ID)
		}
	}
	return s, ctx
}

func ids[M interface{ Message | Hit }](msgs []M) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		switch v := any(m).(type) {
		case Message:
			out[i] = v.ID
		case Hit:
			out[i] = v.ID
		}
	}
	return out
}

func TestNameResolutionPriorityAndLinking(t *testing.T) {
	s, ctx := fixture(t)
	c, ok, err := s.LookupContact(ctx, "", "100000000000001")
	if err != nil || !ok {
		t.Fatalf("lookup by lid: ok=%v err=%v", ok, err)
	}
	if c.Phone != "33600000001" || c.LID != "100000000000001" {
		t.Errorf("phone and lid not merged: %+v", c)
	}
	if c.Name != "Hani Comptable" || c.NameSource != SourceFullName {
		t.Errorf("full name should beat push names: %+v", c)
	}
	// A later push name must not replace the address-book name.
	if _, err := s.PutContact(ctx, "", "100000000000001", "Hani.M", SourcePushName); err != nil {
		t.Fatal(err)
	}
	c, _, _ = s.LookupContact(ctx, "33600000001", "")
	if c.Name != "Hani Comptable" {
		t.Errorf("push name replaced full name: %+v", c)
	}
	// An equal-ranked name replaces (renamed in the address book).
	res, err := s.PutContact(ctx, "33600000001", "", "Dr Hani", SourceFullName)
	if err != nil || !res.NameChanged || res.Name != "Dr Hani" {
		t.Errorf("rename not applied: %+v err=%v", res, err)
	}
	// A name equal to the identifier is not a name.
	if _, err := s.PutContact(ctx, "33700000000", "", "33700000000", SourcePushName); err != nil {
		t.Fatal(err)
	}
	c, _, _ = s.LookupContact(ctx, "33700000000", "")
	if c.Name != "" {
		t.Errorf("identifier stored as name: %+v", c)
	}
	all, _ := s.AllContacts(ctx)
	if len(all) != 4 {
		t.Errorf("expected 4 contacts after merges, got %d: %+v", len(all), all)
	}
}

func TestSearchContacts(t *testing.T) {
	s, ctx := fixture(t)
	got, page, err := s.SearchContacts(ctx, "hani", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Hani Comptable" || got[0].JID != "33600000001@s.whatsapp.net" || !got[0].HasChat {
		t.Errorf("search hani: %+v", got)
	}
	if page.HasMore {
		t.Error("unexpected has_more")
	}
	got, _, _ = s.SearchContacts(ctx, "elodie", 10, "")
	if len(got) != 1 || got[0].JID != "100000000000002@lid" || got[0].Phone != "" {
		t.Errorf("accent-insensitive search failed: %+v", got)
	}
	if _, err := s.PutContact(ctx, "33600000099", "", "Stéphanie", SourceFullName); err != nil { // contains "hani" mid-word
		t.Fatal(err)
	}
	got, _, _ = s.SearchContacts(ctx, "hani", 10, "")
	if len(got) != 2 || got[0].Name != "Hani Comptable" || got[1].Name != "Stéphanie" {
		t.Errorf("word-prefix matches should rank first: %+v", got)
	}
	got, _, _ = s.SearchContacts(ctx, "+33 6 12", 10, "")
	if len(got) != 1 || got[0].Name != "Marc" {
		t.Errorf("number search failed: %+v", got)
	}
	// Pagination by cursor.
	first, page, _ := s.SearchContacts(ctx, "a", 1, "")
	if !page.HasMore || len(first) != 1 {
		t.Fatalf("expected more pages: %+v %+v", first, page)
	}
	second, _, err := s.SearchContacts(ctx, "a", 1, page.NextCursor)
	if err != nil || len(second) != 1 || second[0].JID == first[0].JID {
		t.Errorf("cursor page wrong: %+v err=%v", second, err)
	}
}

func TestFTSIgnoresAccentsAndCase(t *testing.T) {
	s, ctx := fixture(t)
	for query, want := range map[string][]string{
		"ecole":   {"M8", "M1"},
		"ÉCOLE":   {"M8", "M1"},
		"reunion": {"M3"},
		"bilan":   {"M4"},
		"merci":   {"M5", "M2"},
		"bil":     {"M4"}, // prefix
		`"quote"`: nil,
	} {
		got, _, err := s.ListMessages(ctx, MessageQuery{Query: query})
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		if fmt.Sprint(ids(got)) != fmt.Sprint(want) {
			t.Errorf("query %q: got %v want %v", query, ids(got), want)
		}
	}
}

func TestSenderFilterAcceptsEveryIdentityForm(t *testing.T) {
	s, ctx := fixture(t)
	for _, ident := range []string{"33600000001", "+33 6 00 00 00 01", "33600000001@s.whatsapp.net", "100000000000001@lid", "100000000000001", "Hani", "comptable"} {
		people, err := s.ResolveIdentities(ctx, ident)
		if err != nil {
			t.Fatalf("%q: %v", ident, err)
		}
		got, _, err := s.ListMessages(ctx, MessageQuery{Senders: people})
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(ids(got)) != "[M6 M4 M1]" {
			t.Errorf("filter %q: got %v", ident, ids(got))
		}
	}
	if _, err := s.ResolveIdentities(ctx, "Nobody"); err == nil {
		t.Error("expected an error for an unknown name")
	}
	if people, _ := s.ResolveIdentities(ctx, "120363@g.us"); people != nil {
		t.Errorf("group JID should resolve to no identity: %+v", people)
	}
}

func TestSenderNamesAreStoredAndRefreshed(t *testing.T) {
	s, ctx := fixture(t)
	got, _, _ := s.ListMessages(ctx, MessageQuery{ChatJID: "120363@g.us"})
	names := map[string]string{}
	for _, m := range got {
		names[m.ID] = m.SenderName
	}
	if names["M1"] != "Hani Comptable" || names["M3"] != "Élodie" || names["M2"] != "Marc" {
		t.Errorf("sender names: %v", names)
	}
	if err := s.RefreshSenderName(ctx, "", "100000000000002", "Élodie Martin"); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.ListMessages(ctx, MessageQuery{Senders: []Identity{{LID: "100000000000002"}}})
	for _, m := range got {
		if m.SenderName != "Élodie Martin" {
			t.Errorf("name not refreshed on %s: %q", m.ID, m.SenderName)
		}
	}
	mine, _, _ := s.ListMessages(ctx, MessageQuery{ChatJID: "33600000001@s.whatsapp.net"})
	if mine[0].ID != "M5" || mine[0].SenderName != "Me" {
		t.Errorf("own message should be named Me: %+v", mine[0])
	}
}

func TestMessagePaginationByCursor(t *testing.T) {
	s, ctx := fixture(t)
	var all []string
	cursor := ""
	for pages := 0; ; pages++ {
		got, page, err := s.ListMessages(ctx, MessageQuery{Limit: 3, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, ids(got)...)
		if !page.HasMore {
			if page.NextCursor != "" {
				t.Error("next_cursor set on last page")
			}
			break
		}
		if len(got) != 3 {
			t.Errorf("page %d has %d rows", pages, len(got))
		}
		cursor = page.NextCursor
	}
	if fmt.Sprint(all) != "[M6 M5 M4 M8 M3 M7 M2 M1]" {
		t.Errorf("pages do not cover the set exactly once: %v", all)
	}
	// Page size 4 ends on M8, which shares its timestamp with M3: the cursor must split the tie.
	first, page, _ := s.ListMessages(ctx, MessageQuery{Limit: 4})
	second, _, _ := s.ListMessages(ctx, MessageQuery{Limit: 4, Cursor: page.NextCursor})
	if fmt.Sprint(ids(first)) != "[M6 M5 M4 M8]" || fmt.Sprint(ids(second)) != "[M3 M7 M2 M1]" {
		t.Errorf("tie split: %v then %v", ids(first), ids(second))
	}
	if _, _, err := s.ListMessages(ctx, MessageQuery{Cursor: "garbage"}); err == nil {
		t.Error("expected an error for a bad cursor")
	}
}

func TestDateFiltersAndContext(t *testing.T) {
	s, ctx := fixture(t)
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	got, _, _ := s.ListMessages(ctx, MessageQuery{After: base.Add(90 * time.Minute), Before: base.Add(4 * time.Hour)})
	if fmt.Sprint(ids(got)) != "[M4 M8 M3]" {
		t.Errorf("date filter: %v", ids(got))
	}
	got, _, _ = s.ListMessages(ctx, MessageQuery{Query: "reunion", ContextBefore: 1, ContextAfter: 1})
	if len(got) != 1 || len(got[0].Before) != 1 || got[0].Before[0].ID != "M2" || len(got[0].After) != 1 || got[0].After[0].ID != "M6" {
		t.Errorf("context: %+v", got)
	}
	mc, err := s.GetMessageContext(ctx, "M2", "", 5, 5)
	if err != nil || mc.Message.ID != "M2" || fmt.Sprint(ids(mc.Before)) != "[M1]" || fmt.Sprint(ids(mc.After)) != "[M3 M6]" {
		t.Errorf("message context: %+v err=%v", mc, err)
	}
	if _, err := s.GetMessageContext(ctx, "nope", "", 1, 1); err == nil {
		t.Error("expected an error for an unknown message")
	}
}

func TestChats(t *testing.T) {
	s, ctx := fixture(t)
	chats, page, err := s.ListChats(ctx, ChatQuery{Limit: 2, IncludeLastMessage: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 2 || chats[0].JID != "120363@g.us" || !chats[0].IsGroup || chats[0].LastMessage == nil || chats[0].LastMessage.ID != "M6" {
		t.Errorf("first page: %+v", chats)
	}
	rest, page2, _ := s.ListChats(ctx, ChatQuery{Limit: 2, Cursor: page.NextCursor})
	if !page.HasMore || page2.HasMore || len(rest) != 2 || rest[0].JID != "33612345678@s.whatsapp.net" || rest[1].JID != "100000000000002@lid" {
		t.Errorf("second page: %+v more=%v", rest, page2.HasMore)
	}
	byName, _, _ := s.ListChats(ctx, ChatQuery{SortBy: "name"})
	if byName[0].Name != "Copropriété" || byName[1].Name != "Hani Comptable" || byName[len(byName)-1].JID != "100000000000002@lid" {
		t.Errorf("sort by name: %+v", byName)
	}
	found, _, _ := s.ListChats(ctx, ChatQuery{Query: "hani"})
	if len(found) != 1 || found[0].JID != "33600000001@s.whatsapp.net" {
		t.Errorf("query by name: %+v", found)
	}
	found, _, _ = s.ListChats(ctx, ChatQuery{Query: "elodie"})
	if len(found) != 1 || found[0].JID != "100000000000002@lid" {
		t.Errorf("query by contact name on a LID chat: %+v", found)
	}
	people, _ := s.ResolveIdentities(ctx, "Hani")
	dc, ok, _ := s.DirectChat(ctx, people, true)
	if !ok || dc.JID != "33600000001@s.whatsapp.net" {
		t.Errorf("direct chat: %+v", dc)
	}
	cc, _, _ := s.ContactChats(ctx, people, 10, "")
	if len(cc) != 2 || cc[0].JID != "120363@g.us" || cc[1].JID != "33600000001@s.whatsapp.net" {
		t.Errorf("contact chats: %+v", cc)
	}
	last, ok, _ := s.LastInteraction(ctx, people)
	if !ok || last.ID != "M6" {
		t.Errorf("last interaction: %+v", last)
	}
	if _, _, err := s.ListChats(ctx, ChatQuery{SortBy: "size"}); err == nil {
		t.Error("expected an error for a bad sort_by")
	}
}

func TestMergeLIDChat(t *testing.T) {
	s, ctx := fixture(t)
	if _, err := s.PutContact(ctx, "33777777777", "100000000000002", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.MergeLIDChat(ctx, "100000000000002", "33777777777"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetChat(ctx, "100000000000002@lid", false); ok {
		t.Error("lid chat still present")
	}
	ch, ok, _ := s.GetChat(ctx, "33777777777@s.whatsapp.net", true)
	if !ok || ch.LastMessage == nil || ch.LastMessage.ID != "M7" {
		t.Errorf("merged chat: %+v", ch)
	}
	got, _, _ := s.ListMessages(ctx, MessageQuery{Query: "coucou"})
	if len(got) != 1 || got[0].ChatJID != "33777777777@s.whatsapp.net" {
		t.Errorf("fts after merge: %+v", got)
	}
}

func TestImportLegacy(t *testing.T) {
	s, ctx := fixture(t)
	legacy := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+legacy)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP);
		CREATE TABLE messages (id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP, is_from_me BOOLEAN,
			media_type TEXT, filename TEXT, url TEXT, media_key BLOB, file_sha256 BLOB, file_enc_sha256 BLOB, file_length INTEGER, PRIMARY KEY (id, chat_jid));
		INSERT INTO chats VALUES ('100000000000001@lid', '100000000000001', '2026-10-01 10:00:00+02:00');
		INSERT INTO chats VALUES ('999@g.us', 'Old group', '2026-10-02 10:00:00+02:00');
		INSERT INTO chats VALUES ('status@broadcast', 'status', '2026-10-02 10:00:00+02:00');
		INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me) VALUES ('S1', 'status@broadcast', '33612345678', 'a status', '2026-10-02 10:00:00+02:00', 0);
		INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me) VALUES
			('L1', '100000000000001@lid', '100000000000001', 'ancien message', '2026-10-01 10:00:00+02:00', 0),
			('L2', '999@g.us', '100000000000001@lid', 'lid suffixed', '2026-10-02 09:00:00+02:00', 0),
			('L3', '999@g.us', '33612345678@s.whatsapp.net', 'pn suffixed', '2026-10-02 09:30:00+02:00', 0),
			('L4', '999@g.us', '100000000000009', 'unknown bare lid', '2026-10-02 10:00:00+02:00', 0),
			('L5', '999@g.us', '33612345678', 'bare phone', '2026-10-02 10:00:00.5+02:00', 0),
			('L6', '999@g.us', '', '', '2026-10-02 10:00:00+02:00', 0);
		INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename, url, media_key, file_sha256, file_enc_sha256, file_length) VALUES
			('L7', '999@g.us', '33612345678', '', '2026-10-02 11:00:00+02:00', 0, 'image', 'photo.jpg', 'https://mmg.whatsapp.net/v/t62.7118-24/abc.enc?ccb=11', x'01', x'02', x'03', 42);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	stats, err := s.ImportLegacy(ctx, legacy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Chats != 2 || stats.Messages != 6 || stats.Skipped != 2 {
		t.Errorf("stats: %+v", stats)
	}
	// The LID chat moved to the phone JID because the contact is known.
	if _, ok, _ := s.GetChat(ctx, "100000000000001@lid", false); ok {
		t.Error("lid chat should have been canonicalised")
	}
	got, _, _ := s.ListMessages(ctx, MessageQuery{Query: "ancien"})
	if len(got) != 1 || got[0].ChatJID != "33600000001@s.whatsapp.net" || got[0].SenderName != "Hani Comptable" || got[0].SenderPhone != "33600000001" {
		t.Errorf("L1: %+v", got)
	}
	byID := map[string]Message{}
	all, _, _ := s.ListMessages(ctx, MessageQuery{ChatJID: "999@g.us", Limit: 50})
	for _, m := range all {
		byID[m.ID] = m.Message
	}
	if m := byID["L2"]; m.SenderLID != "100000000000001" || m.SenderPhone != "33600000001" || m.SenderName != "Hani Comptable" {
		t.Errorf("L2: %+v", m)
	}
	if m := byID["L3"]; m.SenderPhone != "33612345678" || m.SenderName != "Marc" {
		t.Errorf("L3: %+v", m)
	}
	if m := byID["L4"]; m.SenderLID != "100000000000009" || m.SenderPhone != "" || m.SenderName != "100000000000009" {
		t.Errorf("L4: %+v", m)
	}
	if m := byID["L5"]; m.SenderPhone != "33612345678" || m.SenderName != "Marc" || m.Timestamp.UTC().Hour() != 8 {
		t.Errorf("L5: %+v", m)
	}
	if m := byID["L7"]; m.MediaType != "image" || m.Filename != "photo.jpg" {
		t.Errorf("L7: %+v", m)
	}
	mi, err := s.GetMediaInfo(ctx, "L7", "999@g.us")
	if err != nil || mi.DirectPath != "/v/t62.7118-24/abc.enc?ccb=11" || mi.FileLength != 42 {
		t.Errorf("media info: %+v err=%v", mi, err)
	}
	// Re-importing is idempotent.
	stats, _ = s.ImportLegacy(ctx, legacy, nil)
	if stats.Messages != 0 {
		t.Errorf("second import wrote %d messages", stats.Messages)
	}
}

func TestParseUserTime(t *testing.T) {
	for _, v := range []string{"2026-11-05", "2026-11-05T14:30:00+01:00", "2026-11-05 14:30", "2026-11-05T14:30:00Z"} {
		if _, err := ParseUserTime(v, "after"); err != nil {
			t.Errorf("%q: %v", v, err)
		}
	}
	if _, err := ParseUserTime("yesterday", "after"); err == nil {
		t.Error("expected an error")
	}
}

func TestFTSQuery(t *testing.T) {
	if got := FTSQuery(`école "bonjour" OR x`); got != `"école"* "bonjour"* "OR"* "x"*` {
		t.Errorf("FTSQuery = %q", got)
	}
}

func TestRepairsOnOpen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m.db")
	s, err := Open(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB().ExecContext(ctx, `
		INSERT INTO chats (jid, is_group) VALUES ('status@broadcast', 0), ('1@s.whatsapp.net', 0);
		INSERT INTO messages (id, chat_jid, content, timestamp) VALUES ('S', 'status@broadcast', 'x', '2026-01-01T00:00:00Z');
		INSERT INTO messages (id, chat_jid, content, timestamp, media_type, url, direct_path) VALUES
			('M', '1@s.whatsapp.net', '', '2026-01-01T00:00:00Z', 'image', 'https://mmg.whatsapp.net/v/a.enc?ccb=1&oh=2', '/v/a.enc');`)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok, _ := s.GetChat(ctx, "status@broadcast", false); ok {
		t.Error("broadcast chat not removed")
	}
	mi, err := s.GetMediaInfo(ctx, "M", "1@s.whatsapp.net")
	if err != nil || mi.DirectPath != "/v/a.enc?ccb=1&oh=2" {
		t.Errorf("direct path not repaired: %+v err=%v", mi, err)
	}
}

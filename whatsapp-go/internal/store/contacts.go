package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"askmywa/internal/textfold"
)

// Name sources, best first. They mirror the priority of the Python
// _load_name_index: the address-book name beats the first name, which beats
// the business name, which beats the push name the person chose themselves.
const (
	SourceFullName     = "full_name"
	SourceFirstName    = "first_name"
	SourceBusinessName = "business_name"
	SourcePushName     = "push_name"
	SourceChat         = "chat"
)

// SourceRank orders name sources; lower is better. Unknown sources rank last.
func SourceRank(source string) int {
	switch source {
	case SourceFullName:
		return 0
	case SourceFirstName:
		return 1
	case SourceBusinessName:
		return 2
	case SourcePushName:
		return 3
	case SourceChat:
		return 4
	}
	return 99
}

// Contact is one person: a phone number, a LID (WhatsApp's hidden user id),
// or both, with the best name known for them.
type Contact struct {
	Phone      string `json:"phone,omitempty" jsonschema:"phone number with country code and no plus sign, empty when WhatsApp only exposed the LID"`
	LID        string `json:"lid,omitempty" jsonschema:"WhatsApp hidden user id (the user part of an @lid JID), empty when unknown"`
	Name       string `json:"name,omitempty" jsonschema:"best known display name, empty when the person has no name in the address book and no push name"`
	NameSource string `json:"name_source,omitempty" jsonschema:"where the name comes from: full_name, first_name, business_name, push_name or chat"`
	// JID is the JID of the direct chat with this person when one exists in
	// the database, otherwise the JID WhatsApp would use (phone first).
	JID string `json:"jid" jsonschema:"JID to pass as chat_jid or recipient; the direct chat with this contact when one exists"`
	// HasChat reports whether a direct chat with this contact is stored.
	HasChat bool `json:"has_chat"`
}

// PutResult describes what PutContact changed.
type PutResult struct {
	Phone, LID, Name, Source string
	NameChanged              bool
	// Linked is true when this call joined a phone number and a LID that were
	// previously unknown to belong to the same person.
	Linked bool
}

// PutContact records that the person identified by phone and/or lid (bare
// digits, either may be empty) is known under name from source. Rows for the
// phone and for the LID are merged into one; a worse-ranked name never
// replaces a better one, an equal or better one does (push names change).
// Passing an empty name just links the two identifiers.
func (s *Store) PutContact(ctx context.Context, phone, lid, name, source string) (PutResult, error) {
	phone = textfold.Digits(phone)
	lid = textfold.Digits(lid)
	name = strings.TrimSpace(name)
	if name == phone || name == lid {
		name = ""
	}
	if phone == "" && lid == "" {
		return PutResult{}, errors.New("PutContact: phone and lid are both empty")
	}
	if name == "" {
		source = ""
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PutResult{}, err
	}
	defer tx.Rollback()

	type row struct {
		id                       int64
		phone, lid, name, source string
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, phone, lid, name, name_source FROM contacts
		WHERE (?1 <> '' AND phone = ?1) OR (?2 <> '' AND lid = ?2) ORDER BY id`, phone, lid)
	if err != nil {
		return PutResult{}, err
	}
	var existing []row
	for rows.Next() {
		var r row
		var p, l, n, src sql.NullString
		if err := rows.Scan(&r.id, &p, &l, &n, &src); err != nil {
			rows.Close()
			return PutResult{}, err
		}
		r.phone, r.lid, r.name, r.source = p.String, l.String, n.String, src.String
		existing = append(existing, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PutResult{}, err
	}

	merged := row{id: -1}
	for _, r := range existing {
		if merged.id < 0 {
			merged = r
			continue
		}
		if merged.phone == "" {
			merged.phone = r.phone
		}
		if merged.lid == "" {
			merged.lid = r.lid
		}
		if r.name != "" && (merged.name == "" || SourceRank(r.source) < SourceRank(merged.source)) {
			merged.name, merged.source = r.name, r.source
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM contacts WHERE id = ?`, r.id); err != nil {
			return PutResult{}, err
		}
	}
	res := PutResult{}
	if merged.phone == "" && phone != "" {
		res.Linked = merged.id >= 0 && merged.lid != ""
		merged.phone = phone
	}
	if merged.lid == "" && lid != "" {
		res.Linked = res.Linked || (merged.id >= 0 && merged.phone != "")
		merged.lid = lid
	}
	if name != "" && (merged.name == "" || SourceRank(source) <= SourceRank(merged.source)) && (merged.name != name || merged.source != source) {
		merged.name, merged.source = name, source
		res.NameChanged = true
	}
	now := FormatTime(time.Now())
	nameFold := nullable(textfold.Fold(merged.name))
	if merged.id < 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO contacts (phone, lid, name, name_fold, name_source, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			nullable(merged.phone), nullable(merged.lid), nullable(merged.name), nameFold, nullable(merged.source), now)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE contacts SET phone = ?, lid = ?, name = ?, name_fold = ?, name_source = ?, updated_at = ? WHERE id = ?`,
			nullable(merged.phone), nullable(merged.lid), nullable(merged.name), nameFold, nullable(merged.source), now, merged.id)
	}
	if err != nil {
		return PutResult{}, fmt.Errorf("write contact %s/%s: %w", merged.phone, merged.lid, err)
	}
	if err := tx.Commit(); err != nil {
		return PutResult{}, err
	}
	res.Phone, res.LID, res.Name, res.Source = merged.phone, merged.lid, merged.name, merged.source
	return res, nil
}

// LookupContact returns the contact row holding phone or lid (either may be empty).
func (s *Store) LookupContact(ctx context.Context, phone, lid string) (Contact, bool, error) {
	if phone == "" && lid == "" {
		return Contact{}, false, nil
	}
	var c Contact
	var p, l, n, src sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT phone, lid, name, name_source FROM contacts
		WHERE (?1 <> '' AND phone = ?1) OR (?2 <> '' AND lid = ?2) LIMIT 1`, phone, lid).Scan(&p, &l, &n, &src)
	if errors.Is(err, sql.ErrNoRows) {
		return Contact{}, false, nil
	}
	if err != nil {
		return Contact{}, false, err
	}
	c.Phone, c.LID, c.Name, c.NameSource = p.String, l.String, n.String, src.String
	return c, true, nil
}

// Identity is a person as they can appear in a message: by phone, by LID, or both.
type Identity struct {
	Phone string
	LID   string
}

// JIDs returns the chat JIDs under which a direct chat with this person may be stored.
func (id Identity) JIDs() []string {
	var out []string
	if id.Phone != "" {
		out = append(out, id.Phone+"@s.whatsapp.net")
	}
	if id.LID != "" {
		out = append(out, id.LID+"@lid")
	}
	return out
}

// ResolveIdentities turns a user-supplied identifier into the people it
// designates. The identifier may be a phone number (with or without "+",
// spaces, dashes), a JID (x@s.whatsapp.net, x@lid), or a contact name
// (accent- and case-insensitive substring). A group JID yields no identity
// and no error. A name that matches nobody is an error that tells the
// caller to use search_contacts.
func (s *Store) ResolveIdentities(ctx context.Context, identifier string) ([]Identity, error) {
	ident := strings.TrimSpace(identifier)
	if ident == "" {
		return nil, errors.New("identifier is empty: pass a phone number, a JID or a contact name")
	}
	if at := strings.IndexByte(ident, '@'); at >= 0 {
		user, server := ident[:at], ident[at+1:]
		if i := strings.IndexByte(user, ':'); i >= 0 { // strip device suffix
			user = user[:i]
		}
		switch server {
		case "s.whatsapp.net", "c.us":
			return s.identityFor(ctx, user, "")
		case "lid":
			return s.identityFor(ctx, "", user)
		case "g.us", "broadcast", "newsletter":
			return nil, nil
		}
		return nil, fmt.Errorf("unknown JID server in %q: use x@s.whatsapp.net, x@lid or x@g.us", ident)
	}
	digits := textfold.Digits(ident)
	if digits != "" && textfold.IsDigits(strings.NewReplacer("+", "", " ", "", "-", "", ".", "", "(", "", ")", "").Replace(ident)) {
		// A bare number: phone first, but accept a LID typed from a previous result.
		ids, err := s.identityFor(ctx, digits, "")
		if err != nil || len(ids) == 0 || ids[0].LID != "" || ids[0].Phone != digits {
			return ids, err
		}
		if c, ok, err := s.LookupContact(ctx, "", digits); err != nil {
			return nil, err
		} else if ok {
			return []Identity{{Phone: c.Phone, LID: c.LID}}, nil
		}
		return ids, nil
	}
	needle := "%" + escapeLike(textfold.Fold(ident)) + "%"
	rows, err := s.db.QueryContext(ctx, `SELECT phone, lid FROM contacts WHERE name_fold LIKE ? ESCAPE '\' ORDER BY name_fold, id`, needle)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Identity
	for rows.Next() {
		var p, l sql.NullString
		if err := rows.Scan(&p, &l); err != nil {
			return nil, err
		}
		out = append(out, Identity{Phone: p.String, LID: l.String})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no contact matches %q: call search_contacts to find the person, then pass their phone number or JID", identifier)
	}
	return out, nil
}

func (s *Store) identityFor(ctx context.Context, phone, lid string) ([]Identity, error) {
	c, ok, err := s.LookupContact(ctx, phone, lid)
	if err != nil {
		return nil, err
	}
	if ok {
		return []Identity{{Phone: c.Phone, LID: c.LID}}, nil
	}
	return []Identity{{Phone: phone, LID: lid}}, nil
}

// senderFilter builds "(sender_phone IN (...) OR sender_lid IN (...))".
func senderFilter(ids []Identity) (clause string, args []any) {
	var phones, lids []any
	for _, id := range ids {
		if id.Phone != "" {
			phones = append(phones, id.Phone)
		}
		if id.LID != "" {
			lids = append(lids, id.LID)
		}
	}
	var parts []string
	if len(phones) > 0 {
		parts = append(parts, "m.sender_phone IN ("+placeholders(len(phones))+")")
		args = append(args, phones...)
	}
	if len(lids) > 0 {
		parts = append(parts, "m.sender_lid IN ("+placeholders(len(lids))+")")
		args = append(args, lids...)
	}
	if len(parts) == 0 {
		return "0", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

func chatJIDFilter(column string, ids []Identity) (clause string, args []any) {
	var jids []any
	for _, id := range ids {
		for _, j := range id.JIDs() {
			jids = append(jids, j)
		}
	}
	if len(jids) == 0 {
		return "0", nil
	}
	return column + " IN (" + placeholders(len(jids)) + ")", jids
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ContactName returns the stored name for a sender, or "" when unknown.
func (s *Store) ContactName(ctx context.Context, phone, lid string) (string, error) {
	c, ok, err := s.LookupContact(ctx, phone, lid)
	if err != nil || !ok {
		return "", err
	}
	return c.Name, nil
}

// SearchContacts finds people by name (accent- and case-insensitive
// substring) or by number (substring of the phone number or LID). Names
// where the query starts a word rank first.
func (s *Store) SearchContacts(ctx context.Context, query string, limit int, cursor string) ([]Contact, Page, error) {
	limit = clampLimit(limit)
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, Page{}, errors.New("query is empty: pass part of a name or of a phone number")
	}
	parts, err := decodeCursor(cursor, 2)
	if err != nil {
		return nil, Page{}, err
	}
	folded := escapeLike(textfold.Fold(query))
	where := []string{`c.name_fold LIKE ? ESCAPE '\'`}
	args := []any{"%" + folded + "%"}
	if digits := textfold.Digits(query); digits != "" && len(digits) >= 3 {
		where = append(where, `c.phone LIKE ?`, `c.lid LIKE ?`)
		args = append(args, "%"+digits+"%", "%"+digits+"%")
	}
	rank := `CASE WHEN c.name_fold LIKE ? ESCAPE '\' OR c.name_fold LIKE ? ESCAPE '\' THEN 0 ELSE 1 END`
	rankArgs := []any{folded + "%", "% " + folded + "%"}
	sqlText := `SELECT ` + rank + ` AS rank, c.id, c.phone, c.lid, c.name, c.name_source,
			(SELECT jid FROM chats WHERE jid = c.phone || '@s.whatsapp.net'),
			(SELECT jid FROM chats WHERE jid = c.lid || '@lid')
		FROM contacts c WHERE (` + strings.Join(where, " OR ") + `)`
	args = append(rankArgs, args...)
	if parts != nil {
		lastRank, err := cursorInt(parts[0], cursor)
		if err != nil {
			return nil, Page{}, err
		}
		lastID, err := cursorInt(parts[1], cursor)
		if err != nil {
			return nil, Page{}, err
		}
		sqlText += ` AND (` + rank + ` > ? OR (` + rank + ` = ? AND c.id > ?))`
		args = append(args, rankArgs...)
		args = append(args, lastRank)
		args = append(args, rankArgs...)
		args = append(args, lastRank, lastID)
	}
	sqlText += ` ORDER BY rank, c.id LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, Page{}, fmt.Errorf("search contacts: %w", err)
	}
	defer rows.Close()
	var out []Contact
	var keys [][2]string
	for rows.Next() {
		var rank, id int64
		var p, l, n, src, pnChat, lidChat sql.NullString
		if err := rows.Scan(&rank, &id, &p, &l, &n, &src, &pnChat, &lidChat); err != nil {
			return nil, Page{}, err
		}
		keys = append(keys, [2]string{fmt.Sprint(rank), fmt.Sprint(id)})
		c := Contact{Phone: p.String, LID: l.String, Name: n.String, NameSource: src.String}
		switch {
		case pnChat.Valid:
			c.JID, c.HasChat = pnChat.String, true
		case lidChat.Valid:
			c.JID, c.HasChat = lidChat.String, true
		case c.Phone != "":
			c.JID = c.Phone + "@s.whatsapp.net"
		default:
			c.JID = c.LID + "@lid"
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, Page{}, err
	}
	page := Page{}
	if len(out) > limit {
		out = out[:limit]
		page.HasMore = true
		page.NextCursor = encodeCursor(keys[limit-1][0], keys[limit-1][1])
	}
	return out, page, nil
}

// AllContacts streams every contact (used by the daemon to seed caches and by tests).
func (s *Store) AllContacts(ctx context.Context) ([]Contact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT phone, lid, name, name_source FROM contacts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Contact
	for rows.Next() {
		var p, l, n, src sql.NullString
		if err := rows.Scan(&p, &l, &n, &src); err != nil {
			return nil, err
		}
		out = append(out, Contact{Phone: p.String, LID: l.String, Name: n.String, NameSource: src.String})
	}
	return out, rows.Err()
}

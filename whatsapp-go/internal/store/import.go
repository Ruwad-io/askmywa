package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ImportStats summarises ImportLegacy.
type ImportStats struct {
	Chats    int
	Messages int
	Skipped  int
}

// ImportLegacy copies the history of a messages.db written by the previous
// Go bridge (tables chats(jid, name, last_message_time) and messages(id,
// chat_jid, sender, content, timestamp, is_from_me, media_type, filename,
// url, media_key, file_sha256, file_enc_sha256, file_length)).
//
// The old bridge stored senders in three shapes: bare digits (a phone number
// or a LID), "x@s.whatsapp.net" and "x@lid". Each is mapped to the
// phone/LID pair with the help of the contacts table, which must therefore
// be populated first. Direct chats stored under a LID whose phone number is
// known are moved to the phone JID. Existing rows are not overwritten.
func (s *Store) ImportLegacy(ctx context.Context, legacyPath string, log *slog.Logger) (ImportStats, error) {
	var stats ImportStats
	src, err := sql.Open("sqlite", "file:"+legacyPath+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return stats, fmt.Errorf("open legacy database %s: %w", legacyPath, err)
	}
	defer src.Close()
	if err := src.PingContext(ctx); err != nil {
		return stats, fmt.Errorf("open legacy database %s: %w", legacyPath, err)
	}

	chatRows, err := src.QueryContext(ctx, `SELECT jid, name, last_message_time FROM chats`)
	if err != nil {
		return stats, fmt.Errorf("read legacy chats: %w", err)
	}
	type legacyChat struct {
		jid, name string
		last      time.Time
	}
	var chats []legacyChat
	for chatRows.Next() {
		var jid string
		var name, last sql.NullString
		if err := chatRows.Scan(&jid, &name, &last); err != nil {
			chatRows.Close()
			return stats, err
		}
		t, _ := parseLegacyTime(last.String)
		chats = append(chats, legacyChat{jid: jid, name: name.String, last: t})
	}
	chatRows.Close()

	// Snapshot the contacts: the import runs in one transaction on the single
	// connection, so no other query may run while it is open.
	known, err := s.AllContacts(ctx)
	if err != nil {
		return stats, err
	}
	byPhone, byLID := map[string]Contact{}, map[string]Contact{}
	for _, c := range known {
		if c.Phone != "" {
			byPhone[c.Phone] = c
		}
		if c.LID != "" {
			byLID[c.LID] = c
		}
	}

	// Canonical JID for every legacy chat (LID chats move to the phone JID
	// when known); direct chats take the contact's name.
	canonical := map[string]string{}
	for _, ch := range chats {
		target := ch.jid
		name := ch.name
		user, server := SplitJID(ch.jid)
		if name == user {
			name = ""
		}
		if server == "lid" || server == "s.whatsapp.net" {
			c, ok := byLID[user]
			if server != "lid" {
				c, ok = byPhone[user]
			}
			if ok {
				if c.Phone != "" {
					target = c.Phone + "@s.whatsapp.net"
				}
				if c.Name != "" {
					name = c.Name
				}
			}
		}
		canonical[ch.jid] = target
		if err := s.PutChat(ctx, target, name, IsGroupJID(target), ch.last); err != nil {
			return stats, err
		}
		stats.Chats++
	}

	msgRows, err := src.QueryContext(ctx, `SELECT id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename, url, media_key, file_sha256, file_enc_sha256, file_length FROM messages`)
	if err != nil {
		return stats, fmt.Errorf("read legacy messages: %w", err)
	}
	defer msgRows.Close()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return stats, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO messages (id, chat_jid, sender_phone, sender_lid, sender_name, content, timestamp, is_from_me,
		media_type, filename, url, direct_path, media_key, file_sha256, file_enc_sha256, file_length) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return stats, err
	}
	defer stmt.Close()
	for msgRows.Next() {
		var id, chatJID string
		var sender, content, ts, mediaType, filename, u sql.NullString
		var fromMe sql.NullBool
		var mediaKey, sha, encSha []byte
		var fileLength sql.NullInt64
		if err := msgRows.Scan(&id, &chatJID, &sender, &content, &ts, &fromMe, &mediaType, &filename, &u, &mediaKey, &sha, &encSha, &fileLength); err != nil {
			return stats, err
		}
		t, err := parseLegacyTime(ts.String)
		if err != nil || id == "" || (content.String == "" && mediaType.String == "") {
			stats.Skipped++
			continue
		}
		target := canonical[chatJID]
		if target == "" {
			target = chatJID
			canonical[chatJID] = target
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO chats (jid, is_group, last_message_time) VALUES (?, ?, ?)`, target, boolInt(IsGroupJID(target)), FormatTime(t)); err != nil {
				return stats, err
			}
		}
		phone, lid, name := classifyLegacySender(sender.String, chatJID, byPhone, byLID)
		var fl any
		if fileLength.Valid && fileLength.Int64 > 0 {
			fl = fileLength.Int64
		}
		res, err := stmt.ExecContext(ctx, id, target, nullable(phone), nullable(lid), nullable(name), content.String, FormatTime(t), boolInt(fromMe.Bool),
			nullable(mediaType.String), nullable(filename.String), nullable(u.String), nullable(extractDirectPath(u.String)), mediaKey, sha, encSha, fl)
		if err != nil {
			return stats, fmt.Errorf("import message %s: %w", id, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			stats.Messages++
		} else {
			stats.Skipped++
		}
	}
	if err := msgRows.Err(); err != nil {
		return stats, err
	}
	if err := tx.Commit(); err != nil {
		return stats, err
	}
	if log != nil {
		log.Info("legacy import done", "chats", stats.Chats, "messages", stats.Messages, "skipped", stats.Skipped)
	}
	return stats, nil
}

// classifyLegacySender maps an old sender value to (phone, lid, name).
func classifyLegacySender(sender, chatJID string, byPhone, byLID map[string]Contact) (phone, lid, name string) {
	user, server := SplitJID(sender)
	switch server {
	case "s.whatsapp.net", "c.us":
		phone = user
	case "lid":
		lid = user
	case "":
		// Bare digits: ask the contacts table, then the chat it was stored in,
		// then the length (LIDs have 14-15 digits, phone numbers usually 10-13).
		if c, ok := byPhone[user]; ok {
			return c.Phone, c.LID, c.Name
		}
		if c, ok := byLID[user]; ok {
			return c.Phone, c.LID, c.Name
		}
		chatUser, chatServer := SplitJID(chatJID)
		switch {
		case chatUser == user && chatServer == "lid":
			lid = user
		case chatUser == user:
			phone = user
		case len(user) >= 14:
			lid = user
		default:
			phone = user
		}
	default:
		phone = user
	}
	var c Contact
	var ok bool
	if phone != "" {
		c, ok = byPhone[phone]
	}
	if !ok && lid != "" {
		c, ok = byLID[lid]
	}
	if ok {
		if phone == "" {
			phone = c.Phone
		}
		if lid == "" {
			lid = c.LID
		}
		name = c.Name
	}
	return phone, lid, name
}

func parseLegacyTime(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	for _, l := range []string{"2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05-07:00", "2006-01-02T15:04:05.999999999-07:00",
		time.RFC3339Nano, time.RFC3339, TimeLayout, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(l, v); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised timestamp %q", v)
}

// extractDirectPath recovers the media direct path from a full mmg.whatsapp.net URL.
func extractDirectPath(u string) string {
	parts := strings.SplitN(u, ".net/", 2)
	if len(parts) < 2 {
		return ""
	}
	return "/" + strings.SplitN(parts[1], "?", 2)[0]
}

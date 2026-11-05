package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"whatsapp-mcp/internal/textfold"
)

// Message is one stored message, as returned to the MCP client.
type Message struct {
	ID          string    `json:"id" jsonschema:"WhatsApp message id, unique within the chat"`
	ChatJID     string    `json:"chat_jid" jsonschema:"JID of the chat the message belongs to"`
	ChatName    string    `json:"chat_name,omitempty" jsonschema:"name of the chat (group name or contact name)"`
	SenderPhone string    `json:"sender_phone,omitempty" jsonschema:"phone number of the sender when known"`
	SenderLID   string    `json:"sender_lid,omitempty" jsonschema:"hidden user id of the sender when known"`
	SenderName  string    `json:"sender_name" jsonschema:"display name of the sender; 'Me' for your own messages; falls back to the phone number or LID when no name is known"`
	Content     string    `json:"content" jsonschema:"text of the message, or the caption of a media message"`
	Timestamp   time.Time `json:"timestamp" jsonschema:"when the message was sent, in UTC"`
	IsFromMe    bool      `json:"is_from_me"`
	MediaType   string    `json:"media_type,omitempty" jsonschema:"image, video, audio, document or sticker when the message carries media; pass id and chat_jid to download_media"`
	Filename    string    `json:"filename,omitempty" jsonschema:"file name the media is saved under by download_media"`
}

// Hit is a message returned by ListMessages, with its neighbours when the
// caller asked for context. (A separate type because output schemas cannot
// be recursive.)
type Hit struct {
	Message
	Before []Message `json:"context_before,omitempty" jsonschema:"messages that precede this one in the same chat, oldest first"`
	After  []Message `json:"context_after,omitempty" jsonschema:"messages that follow this one in the same chat, oldest first"`
}

// MediaInfo is what the daemon needs to download a media message again.
type MediaInfo struct {
	MediaType     string
	Filename      string
	MimeType      string
	URL           string
	DirectPath    string
	MediaKey      []byte
	FileSHA256    []byte
	FileEncSHA256 []byte
	FileLength    uint64
}

// NewMessage is the input of PutMessage.
type NewMessage struct {
	ID          string
	ChatJID     string
	SenderPhone string
	SenderLID   string
	SenderName  string
	Content     string
	Timestamp   time.Time
	IsFromMe    bool
	Media       *MediaInfo
}

// PutMessage inserts or updates a message. A message with neither text nor
// media is skipped (returns false). An existing row keeps its sender_name
// unless the new one is non-empty.
func (s *Store) PutMessage(ctx context.Context, m NewMessage) (bool, error) {
	if m.Content == "" && (m.Media == nil || m.Media.MediaType == "") {
		return false, nil
	}
	if m.ID == "" || m.ChatJID == "" {
		return false, errors.New("PutMessage: id and chat_jid are required")
	}
	media := m.Media
	if media == nil {
		media = &MediaInfo{}
	}
	var fileLength any
	if media.FileLength > 0 {
		fileLength = int64(media.FileLength)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO messages (id, chat_jid, sender_phone, sender_lid, sender_name, content, timestamp, is_from_me,
			media_type, filename, mime_type, url, direct_path, media_key, file_sha256, file_enc_sha256, file_length)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id, chat_jid) DO UPDATE SET
			sender_phone = COALESCE(excluded.sender_phone, messages.sender_phone),
			sender_lid = COALESCE(excluded.sender_lid, messages.sender_lid),
			sender_name = COALESCE(excluded.sender_name, messages.sender_name),
			content = CASE WHEN excluded.content <> '' THEN excluded.content ELSE messages.content END,
			timestamp = excluded.timestamp,
			is_from_me = excluded.is_from_me,
			media_type = COALESCE(excluded.media_type, messages.media_type),
			filename = COALESCE(excluded.filename, messages.filename),
			mime_type = COALESCE(excluded.mime_type, messages.mime_type),
			url = COALESCE(excluded.url, messages.url),
			direct_path = COALESCE(excluded.direct_path, messages.direct_path),
			media_key = COALESCE(excluded.media_key, messages.media_key),
			file_sha256 = COALESCE(excluded.file_sha256, messages.file_sha256),
			file_enc_sha256 = COALESCE(excluded.file_enc_sha256, messages.file_enc_sha256),
			file_length = COALESCE(excluded.file_length, messages.file_length)`,
		m.ID, m.ChatJID, nullable(m.SenderPhone), nullable(m.SenderLID), nullable(m.SenderName), m.Content,
		FormatTime(m.Timestamp), boolInt(m.IsFromMe),
		nullable(media.MediaType), nullable(media.Filename), nullable(media.MimeType), nullable(media.URL), nullable(media.DirectPath),
		media.MediaKey, media.FileSHA256, media.FileEncSHA256, fileLength)
	if err != nil {
		return false, fmt.Errorf("write message %s in %s: %w", m.ID, m.ChatJID, err)
	}
	return true, nil
}

// RefreshSenderName rewrites sender_name for every message from this person
// (called when the daemon learns a better name).
func (s *Store) RefreshSenderName(ctx context.Context, phone, lid, name string) error {
	if name == "" || (phone == "" && lid == "") {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET sender_name = ?1
		WHERE is_from_me = 0 AND ((?2 <> '' AND sender_phone = ?2) OR (?3 <> '' AND sender_lid = ?3))
		AND (sender_name IS NULL OR sender_name <> ?1)`, name, phone, lid)
	if err != nil {
		return fmt.Errorf("refresh sender name for %s/%s: %w", phone, lid, err)
	}
	return nil
}

// GetMediaInfo returns the download parameters of a media message.
func (s *Store) GetMediaInfo(ctx context.Context, id, chatJID string) (MediaInfo, error) {
	var mi MediaInfo
	var mediaType, filename, mime, u, dp sql.NullString
	var fileLength sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT media_type, filename, mime_type, url, direct_path, media_key, file_sha256, file_enc_sha256, file_length
		FROM messages WHERE id = ? AND chat_jid = ?`, id, chatJID).
		Scan(&mediaType, &filename, &mime, &u, &dp, &mi.MediaKey, &mi.FileSHA256, &mi.FileEncSHA256, &fileLength)
	if errors.Is(err, sql.ErrNoRows) {
		return mi, fmt.Errorf("no message with id %q in chat %q: check both values against list_messages output", id, chatJID)
	}
	if err != nil {
		return mi, err
	}
	mi.MediaType, mi.Filename, mi.MimeType, mi.URL, mi.DirectPath = mediaType.String, filename.String, mime.String, u.String, dp.String
	mi.FileLength = uint64(fileLength.Int64)
	if mi.MediaType == "" {
		return mi, fmt.Errorf("message %q in chat %q carries no media: only messages with a media_type can be downloaded", id, chatJID)
	}
	return mi, nil
}

// MessageQuery is the filter of ListMessages.
type MessageQuery struct {
	ChatJID string
	After   time.Time
	Before  time.Time
	// Senders restricts to messages from these people (any form of identity).
	Senders []Identity
	// Query is a full-text search; words are matched by prefix, accent- and case-insensitively.
	Query  string
	Limit  int
	Cursor string
	// ContextBefore / ContextAfter add surrounding messages of the same chat to each result.
	ContextBefore int
	ContextAfter  int
}

const messageColumns = `m.seq, m.id, m.chat_jid, c.name, m.sender_phone, m.sender_lid, m.sender_name, m.content, m.timestamp, m.is_from_me, m.media_type, m.filename`

type scanner interface {
	Scan(dest ...any) error
}

func scanMessage(sc scanner) (Message, int64, error) {
	var msg Message
	var seq int64
	var chatName, phone, lid, name, mediaType, filename sql.NullString
	var ts string
	var fromMe int
	if err := sc.Scan(&seq, &msg.ID, &msg.ChatJID, &chatName, &phone, &lid, &name, &msg.Content, &ts, &fromMe, &mediaType, &filename); err != nil {
		return msg, 0, err
	}
	t, err := ParseTime(ts)
	if err != nil {
		return msg, 0, err
	}
	msg.Timestamp = t
	msg.ChatName = chatName.String
	msg.SenderPhone, msg.SenderLID = phone.String, lid.String
	msg.IsFromMe = fromMe != 0
	msg.MediaType, msg.Filename = mediaType.String, filename.String
	switch {
	case msg.IsFromMe:
		msg.SenderName = "Me"
	case name.String != "":
		msg.SenderName = name.String
	case msg.SenderPhone != "":
		msg.SenderName = msg.SenderPhone
	default:
		msg.SenderName = msg.SenderLID
	}
	return msg, seq, nil
}

// FTSQuery turns free text into a safe FTS5 expression: each word becomes a
// quoted prefix term, and words are ANDed. Quotes and operators in the input
// are never interpreted.
func FTSQuery(text string) string {
	var terms []string
	for _, w := range strings.Fields(text) {
		w = strings.Trim(w, `"'`)
		w = strings.ReplaceAll(w, `"`, `""`)
		if w == "" {
			continue
		}
		terms = append(terms, `"`+w+`"*`)
	}
	return strings.Join(terms, " ")
}

// ListMessages returns messages newest first, filtered by q, with cursor pagination.
func (s *Store) ListMessages(ctx context.Context, q MessageQuery) ([]Hit, Page, error) {
	limit := clampLimit(q.Limit)
	parts, err := decodeCursor(q.Cursor, 2)
	if err != nil {
		return nil, Page{}, err
	}
	where := []string{"1"}
	var args []any
	if q.ChatJID != "" {
		where = append(where, "m.chat_jid = ?")
		args = append(args, q.ChatJID)
	}
	if !q.After.IsZero() {
		where = append(where, "m.timestamp > ?")
		args = append(args, FormatTime(q.After))
	}
	if !q.Before.IsZero() {
		where = append(where, "m.timestamp < ?")
		args = append(args, FormatTime(q.Before))
	}
	if len(q.Senders) > 0 {
		clause, a := senderFilter(q.Senders)
		where = append(where, clause)
		args = append(args, a...)
	}
	if fts := FTSQuery(q.Query); fts != "" {
		where = append(where, "m.seq IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?)")
		args = append(args, fts)
	}
	if parts != nil {
		where = append(where, "(m.timestamp < ? OR (m.timestamp = ? AND m.seq < ?))")
		args = append(args, parts[0], parts[0], parts[1])
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `SELECT `+messageColumns+` FROM messages m LEFT JOIN chats c ON c.jid = m.chat_jid
		WHERE `+strings.Join(where, " AND ")+` ORDER BY m.timestamp DESC, m.seq DESC LIMIT ?`, args...)
	if err != nil {
		return nil, Page{}, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	var out []Hit
	var seqs []int64
	for rows.Next() {
		msg, seq, err := scanMessage(rows)
		if err != nil {
			return nil, Page{}, err
		}
		out = append(out, Hit{Message: msg})
		seqs = append(seqs, seq)
	}
	if err := rows.Err(); err != nil {
		return nil, Page{}, err
	}
	page := Page{}
	if len(out) > limit {
		out, seqs = out[:limit], seqs[:limit]
		page.HasMore = true
		page.NextCursor = encodeCursor(FormatTime(out[limit-1].Timestamp), strconv.FormatInt(seqs[limit-1], 10))
	}
	if q.ContextBefore > 0 || q.ContextAfter > 0 {
		for i := range out {
			before, after, err := s.neighbours(ctx, out[i].ChatJID, FormatTime(out[i].Timestamp), seqs[i], q.ContextBefore, q.ContextAfter)
			if err != nil {
				return nil, Page{}, err
			}
			out[i].Before, out[i].After = before, after
		}
	}
	return out, page, nil
}

func (s *Store) neighbours(ctx context.Context, chatJID, ts string, seq int64, nBefore, nAfter int) (before, after []Message, err error) {
	if nBefore > 0 {
		rows, err := s.db.QueryContext(ctx, `SELECT `+messageColumns+` FROM messages m LEFT JOIN chats c ON c.jid = m.chat_jid
			WHERE m.chat_jid = ? AND (m.timestamp < ? OR (m.timestamp = ? AND m.seq < ?))
			ORDER BY m.timestamp DESC, m.seq DESC LIMIT ?`, chatJID, ts, ts, seq, nBefore)
		if err != nil {
			return nil, nil, err
		}
		before, err = collect(rows)
		if err != nil {
			return nil, nil, err
		}
		for i, j := 0, len(before)-1; i < j; i, j = i+1, j-1 {
			before[i], before[j] = before[j], before[i]
		}
	}
	if nAfter > 0 {
		rows, err := s.db.QueryContext(ctx, `SELECT `+messageColumns+` FROM messages m LEFT JOIN chats c ON c.jid = m.chat_jid
			WHERE m.chat_jid = ? AND (m.timestamp > ? OR (m.timestamp = ? AND m.seq > ?))
			ORDER BY m.timestamp ASC, m.seq ASC LIMIT ?`, chatJID, ts, ts, seq, nAfter)
		if err != nil {
			return nil, nil, err
		}
		after, err = collect(rows)
		if err != nil {
			return nil, nil, err
		}
	}
	return before, after, nil
}

func collect(rows *sql.Rows) ([]Message, error) {
	defer rows.Close()
	var out []Message
	for rows.Next() {
		msg, _, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

// MessageContext is one message with its neighbours.
type MessageContext struct {
	Message Message   `json:"message"`
	Before  []Message `json:"before" jsonschema:"messages before the target, oldest first"`
	After   []Message `json:"after" jsonschema:"messages after the target, oldest first"`
}

// GetMessageContext finds a message by id (chatJID narrows the search when
// given) and returns the surrounding messages of its chat.
func (s *Store) GetMessageContext(ctx context.Context, id, chatJID string, nBefore, nAfter int) (MessageContext, error) {
	var row *sql.Row
	if chatJID != "" {
		row = s.db.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages m LEFT JOIN chats c ON c.jid = m.chat_jid WHERE m.id = ? AND m.chat_jid = ?`, id, chatJID)
	} else {
		row = s.db.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages m LEFT JOIN chats c ON c.jid = m.chat_jid WHERE m.id = ? ORDER BY m.timestamp DESC LIMIT 1`, id)
	}
	msg, seq, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageContext{}, fmt.Errorf("no message with id %q: use the id field of a list_messages result", id)
	}
	if err != nil {
		return MessageContext{}, err
	}
	before, after, err := s.neighbours(ctx, msg.ChatJID, FormatTime(msg.Timestamp), seq, nBefore, nAfter)
	if err != nil {
		return MessageContext{}, err
	}
	if before == nil {
		before = []Message{}
	}
	if after == nil {
		after = []Message{}
	}
	return MessageContext{Message: msg, Before: before, After: after}, nil
}

// LastInteraction returns the newest message sent by or exchanged with the
// given people (their direct chats count, their group messages count).
func (s *Store) LastInteraction(ctx context.Context, ids []Identity) (Message, bool, error) {
	senderClause, senderArgs := senderFilter(ids)
	chatClause, chatArgs := chatJIDFilter("m.chat_jid", ids)
	args := append(senderArgs, chatArgs...)
	row := s.db.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages m LEFT JOIN chats c ON c.jid = m.chat_jid
		WHERE `+senderClause+` OR `+chatClause+` ORDER BY m.timestamp DESC, m.seq DESC LIMIT 1`, args...)
	msg, _, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, false, nil
	}
	if err != nil {
		return Message{}, false, err
	}
	return msg, true, nil
}

// ParseUserTime accepts the ISO-8601 forms a caller is likely to type.
func ParseUserTime(value, field string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	layouts := []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, value, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid %s %q: use ISO-8601, for example 2026-11-05 or 2026-11-05T14:30:00+01:00", field, value)
}

// IsGroupJID reports whether jid designates a group.
func IsGroupJID(jid string) bool { return strings.HasSuffix(jid, "@g.us") }

// SplitJID returns the user and server parts of a JID (device suffix removed).
func SplitJID(jid string) (user, server string) {
	at := strings.IndexByte(jid, '@')
	if at < 0 {
		return jid, ""
	}
	user, server = jid[:at], jid[at+1:]
	if i := strings.IndexByte(user, ':'); i >= 0 {
		user = user[:i]
	}
	return user, server
}

// DigitsOf is re-exported for callers that only import store.
func DigitsOf(s string) string { return textfold.Digits(s) }

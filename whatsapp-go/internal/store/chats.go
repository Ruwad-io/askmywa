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

// Chat is a conversation (direct or group) with its latest message.
type Chat struct {
	JID             string     `json:"jid" jsonschema:"chat JID: x@s.whatsapp.net or x@lid for a person, x@g.us for a group"`
	Name            string     `json:"name,omitempty" jsonschema:"group name or contact name; empty when unknown"`
	IsGroup         bool       `json:"is_group"`
	LastMessageTime *time.Time `json:"last_message_time,omitempty" jsonschema:"timestamp of the newest stored message, UTC"`
	LastMessage     *Message   `json:"last_message,omitempty" jsonschema:"the newest stored message, when requested"`
}

// PutChat inserts or updates a chat. An empty name never erases a stored
// one; last_message_time only moves forward.
func (s *Store) PutChat(ctx context.Context, jid, name string, isGroup bool, lastMessage time.Time) error {
	if jid == "" {
		return errors.New("PutChat: jid is required")
	}
	name = strings.TrimSpace(name)
	user, _ := SplitJID(jid)
	if name == user || name == jid {
		name = ""
	}
	var ts any
	if !lastMessage.IsZero() {
		ts = FormatTime(lastMessage)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO chats (jid, name, name_fold, is_group, last_message_time) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (jid) DO UPDATE SET
			name = COALESCE(excluded.name, chats.name),
			name_fold = COALESCE(excluded.name_fold, chats.name_fold),
			is_group = excluded.is_group,
			last_message_time = MAX(COALESCE(excluded.last_message_time, ''), COALESCE(chats.last_message_time, ''))`,
		jid, nullable(name), nullable(textfold.Fold(name)), boolInt(isGroup), ts)
	if err != nil {
		return fmt.Errorf("write chat %s: %w", jid, err)
	}
	return nil
}

// RenameChat sets the display name of a chat (group rename, contact name learned).
func (s *Store) RenameChat(ctx context.Context, jid, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET name = ?, name_fold = ? WHERE jid = ?`, name, textfold.Fold(name), jid)
	return err
}

// ChatName returns the stored name of a chat ("" when unknown or absent).
func (s *Store) ChatName(ctx context.Context, jid string) (string, error) {
	var name sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT name FROM chats WHERE jid = ?`, jid).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name.String, err
}

// MergeLIDChat moves a direct chat stored under lid@lid to phone@s.whatsapp.net
// once the mapping is known, so one person has one chat.
func (s *Store) MergeLIDChat(ctx context.Context, lid, phone string) error {
	if lid == "" || phone == "" {
		return nil
	}
	lidJID, pnJID := lid+"@lid", phone+"@s.whatsapp.net"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var name, ts sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT name, last_message_time FROM chats WHERE jid = ?`, lidJID).Scan(&name, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO chats (jid, name, name_fold, is_group, last_message_time) VALUES (?, ?, ?, 0, ?)
		ON CONFLICT (jid) DO UPDATE SET
			name = COALESCE(chats.name, excluded.name),
			name_fold = COALESCE(chats.name_fold, excluded.name_fold),
			last_message_time = MAX(COALESCE(excluded.last_message_time, ''), COALESCE(chats.last_message_time, ''))`,
		pnJID, name, nullable(textfold.Fold(name.String)), ts); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE OR IGNORE messages SET chat_jid = ? WHERE chat_jid = ?`, pnJID, lidJID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE chat_jid = ?`, lidJID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chats WHERE jid = ?`, lidJID); err != nil {
		return err
	}
	return tx.Commit()
}

// ChatQuery filters ListChats.
type ChatQuery struct {
	// Query matches the chat name (accent-insensitive), the JID, or a contact name/number.
	Query              string
	Limit              int
	Cursor             string
	IncludeLastMessage bool
	// SortBy is "last_active" (default) or "name".
	SortBy string
}

const chatSelect = `SELECT c.jid, c.name, c.is_group, c.last_message_time, ` + messageColumns + `
	FROM chats c
	LEFT JOIN messages m ON m.seq = (SELECT seq FROM messages WHERE chat_jid = c.jid ORDER BY timestamp DESC, seq DESC LIMIT 1)`

func scanChat(sc scanner, withMessage bool) (Chat, error) {
	var ch Chat
	var name, ts sql.NullString
	var isGroup int
	var seq sql.NullInt64
	var mID, mChat, mChatName, mPhone, mLID, mName, mContent, mTS, mMedia, mFile sql.NullString
	var mFromMe sql.NullInt64
	if err := sc.Scan(&ch.JID, &name, &isGroup, &ts, &seq, &mID, &mChat, &mChatName, &mPhone, &mLID, &mName, &mContent, &mTS, &mFromMe, &mMedia, &mFile); err != nil {
		return ch, err
	}
	ch.Name = name.String
	ch.IsGroup = isGroup != 0
	if ts.Valid && ts.String != "" {
		t, err := ParseTime(ts.String)
		if err != nil {
			return ch, err
		}
		ch.LastMessageTime = &t
	}
	if withMessage && mID.Valid {
		t, err := ParseTime(mTS.String)
		if err != nil {
			return ch, err
		}
		msg := Message{ID: mID.String, ChatJID: mChat.String, ChatName: mChatName.String, SenderPhone: mPhone.String, SenderLID: mLID.String,
			Content: mContent.String, Timestamp: t, IsFromMe: mFromMe.Int64 != 0, MediaType: mMedia.String, Filename: mFile.String}
		switch {
		case msg.IsFromMe:
			msg.SenderName = "Me"
		case mName.String != "":
			msg.SenderName = mName.String
		case msg.SenderPhone != "":
			msg.SenderName = msg.SenderPhone
		default:
			msg.SenderName = msg.SenderLID
		}
		ch.LastMessage = &msg
	}
	return ch, nil
}

// ListChats lists chats, most recently active first (or by name).
func (s *Store) ListChats(ctx context.Context, q ChatQuery) ([]Chat, Page, error) {
	limit := clampLimit(q.Limit)
	byName := q.SortBy == "name"
	if q.SortBy != "" && q.SortBy != "name" && q.SortBy != "last_active" {
		return nil, Page{}, fmt.Errorf("invalid sort_by %q: use \"last_active\" or \"name\"", q.SortBy)
	}
	parts, err := decodeCursor(q.Cursor, 2)
	if err != nil {
		return nil, Page{}, err
	}
	where := []string{"1"}
	var args []any
	if query := strings.TrimSpace(q.Query); query != "" {
		clauses := []string{`c.name_fold LIKE ? ESCAPE '\'`, `c.jid LIKE ? ESCAPE '\'`}
		args = append(args, "%"+escapeLike(textfold.Fold(query))+"%", "%"+escapeLike(query)+"%")
		if ids, err := s.ResolveIdentities(ctx, query); err == nil && len(ids) > 0 {
			clause, a := chatJIDFilter("c.jid", ids)
			clauses = append(clauses, clause)
			args = append(args, a...)
		}
		where = append(where, "("+strings.Join(clauses, " OR ")+")")
	}
	order := "COALESCE(c.last_message_time, '') DESC, c.jid DESC"
	if byName {
		order = "COALESCE(c.name_fold, '~') ASC, c.jid ASC"
	}
	if parts != nil {
		if byName {
			where = append(where, "(COALESCE(c.name_fold, '~') > ? OR (COALESCE(c.name_fold, '~') = ? AND c.jid > ?))")
		} else {
			where = append(where, "(COALESCE(c.last_message_time, '') < ? OR (COALESCE(c.last_message_time, '') = ? AND c.jid < ?))")
		}
		args = append(args, parts[0], parts[0], parts[1])
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, chatSelect+` WHERE `+strings.Join(where, " AND ")+` ORDER BY `+order+` LIMIT ?`, args...)
	if err != nil {
		return nil, Page{}, fmt.Errorf("list chats: %w", err)
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		ch, err := scanChat(rows, q.IncludeLastMessage)
		if err != nil {
			return nil, Page{}, err
		}
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, Page{}, err
	}
	page := Page{}
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		page.HasMore = true
		if byName {
			key := "~"
			if last.Name != "" {
				key = textfold.Fold(last.Name)
			}
			page.NextCursor = encodeCursor(key, last.JID)
		} else {
			key := ""
			if last.LastMessageTime != nil {
				key = FormatTime(*last.LastMessageTime)
			}
			page.NextCursor = encodeCursor(key, last.JID)
		}
	}
	return out, page, nil
}

// GetChat returns one chat by JID.
func (s *Store) GetChat(ctx context.Context, jid string, includeLastMessage bool) (Chat, bool, error) {
	row := s.db.QueryRowContext(ctx, chatSelect+` WHERE c.jid = ?`, jid)
	ch, err := scanChat(row, includeLastMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return Chat{}, false, nil
	}
	if err != nil {
		return Chat{}, false, err
	}
	return ch, true, nil
}

// DirectChat returns the one-to-one chat with any of the given identities,
// preferring the most recently active one.
func (s *Store) DirectChat(ctx context.Context, ids []Identity, includeLastMessage bool) (Chat, bool, error) {
	clause, args := chatJIDFilter("c.jid", ids)
	row := s.db.QueryRowContext(ctx, chatSelect+` WHERE c.is_group = 0 AND `+clause+` ORDER BY COALESCE(c.last_message_time, '') DESC LIMIT 1`, args...)
	ch, err := scanChat(row, includeLastMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return Chat{}, false, nil
	}
	if err != nil {
		return Chat{}, false, err
	}
	return ch, true, nil
}

// ContactChats lists every chat involving the given people: their direct
// chat and the groups they wrote in, most recently active first.
func (s *Store) ContactChats(ctx context.Context, ids []Identity, limit int, cursor string) ([]Chat, Page, error) {
	limit = clampLimit(limit)
	parts, err := decodeCursor(cursor, 2)
	if err != nil {
		return nil, Page{}, err
	}
	jidClause, jidArgs := chatJIDFilter("c.jid", ids)
	senderClause, senderArgs := senderFilter(ids)
	where := `(` + jidClause + ` OR c.jid IN (SELECT DISTINCT m.chat_jid FROM messages m WHERE ` + senderClause + `))`
	args := append(jidArgs, senderArgs...)
	if parts != nil {
		where += ` AND (COALESCE(c.last_message_time, '') < ? OR (COALESCE(c.last_message_time, '') = ? AND c.jid < ?))`
		args = append(args, parts[0], parts[0], parts[1])
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, chatSelect+` WHERE `+where+` ORDER BY COALESCE(c.last_message_time, '') DESC, c.jid DESC LIMIT ?`, args...)
	if err != nil {
		return nil, Page{}, fmt.Errorf("contact chats: %w", err)
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		ch, err := scanChat(rows, true)
		if err != nil {
			return nil, Page{}, err
		}
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, Page{}, err
	}
	page := Page{}
	if len(out) > limit {
		out = out[:limit]
		last := out[limit-1]
		key := ""
		if last.LastMessageTime != nil {
			key = FormatTime(*last.LastMessageTime)
		}
		page.HasMore = true
		page.NextCursor = encodeCursor(key, last.JID)
	}
	return out, page, nil
}

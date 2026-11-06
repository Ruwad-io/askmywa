// Package store owns messages.db: the chats, messages and contacts written by
// the daemon and read by the MCP server.
//
// Design rules (they fix the defects of the previous bridge):
//
//   - A sender is stored as two bare identifiers, sender_phone and
//     sender_lid, never as a JID string, so a filter can match whichever
//     form WhatsApp used for a given message.
//   - sender_name is resolved when the row is written and refreshed when the
//     daemon learns a better name, so readers never join whatsmeow's tables.
//   - Timestamps are UTC text in RFC 3339 form ("2006-01-02T15:04:05Z"),
//     which sorts and compares lexicographically.
//   - Content is indexed by an FTS5 table with the unicode61 tokenizer and
//     remove_diacritics 2, so "ecole" finds "école".
//   - Every write is an upsert keyed by (id, chat_jid); seq is the stable
//     rowid used by the FTS index and by cursors.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"
)

// TimeLayout is the storage form of every timestamp.
const TimeLayout = "2006-01-02T15:04:05Z"

// Store is an open messages.db.
type Store struct {
	db *sql.DB
}

const schema = `
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS contacts (
	id          INTEGER PRIMARY KEY,
	phone       TEXT UNIQUE,
	lid         TEXT UNIQUE,
	name        TEXT,
	name_fold   TEXT,
	name_source TEXT,
	updated_at  TEXT NOT NULL,
	CHECK (phone IS NOT NULL OR lid IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS contacts_name_fold ON contacts(name_fold);

CREATE TABLE IF NOT EXISTS chats (
	jid               TEXT PRIMARY KEY,
	name              TEXT,
	name_fold         TEXT,
	is_group          INTEGER NOT NULL DEFAULT 0,
	last_message_time TEXT
);
CREATE INDEX IF NOT EXISTS chats_last_message_time ON chats(last_message_time);

CREATE TABLE IF NOT EXISTS messages (
	seq             INTEGER PRIMARY KEY,
	id              TEXT NOT NULL,
	chat_jid        TEXT NOT NULL,
	sender_phone    TEXT,
	sender_lid      TEXT,
	sender_name     TEXT,
	content         TEXT NOT NULL DEFAULT '',
	timestamp       TEXT NOT NULL,
	is_from_me      INTEGER NOT NULL DEFAULT 0,
	media_type      TEXT,
	filename        TEXT,
	mime_type       TEXT,
	url             TEXT,
	direct_path     TEXT,
	media_key       BLOB,
	file_sha256     BLOB,
	file_enc_sha256 BLOB,
	file_length     INTEGER,
	UNIQUE (id, chat_jid)
);
CREATE INDEX IF NOT EXISTS messages_chat_time ON messages(chat_jid, timestamp, seq);
CREATE INDEX IF NOT EXISTS messages_time ON messages(timestamp, seq);
CREATE INDEX IF NOT EXISTS messages_sender_phone ON messages(sender_phone);
CREATE INDEX IF NOT EXISTS messages_sender_lid ON messages(sender_lid);

CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
	content,
	content='messages',
	content_rowid='seq',
	tokenize='unicode61 remove_diacritics 2'
);
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
	INSERT INTO messages_fts(rowid, content) VALUES (new.seq, new.content);
END;
CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.seq, old.content);
END;
CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE OF content ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.seq, old.content);
	INSERT INTO messages_fts(rowid, content) VALUES (new.seq, new.content);
END;
`

// repairs fixes rows written by earlier versions of the import; every
// statement is idempotent.
const repairs = `
-- Status updates and channels are not archived.
DELETE FROM messages WHERE chat_jid LIKE '%@broadcast' OR chat_jid LIKE '%@newsletter';
DELETE FROM chats WHERE jid LIKE '%@broadcast' OR jid LIKE '%@newsletter';
-- The media direct path must keep the URL's query string.
UPDATE messages SET direct_path = '/' || substr(url, instr(url, '.net/') + 5)
	WHERE url LIKE '%.net/%?%' AND (direct_path IS NULL OR direct_path NOT LIKE '%?%');
`

// DSN builds the modernc.org/sqlite connection string for path.
func DSN(path string, readOnly bool) string {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	}
	return "file:" + path + "?" + q.Encode()
}

// Open creates or upgrades messages.db at path. With readOnly the connection
// refuses writes (the MCP server uses it); the file must then already exist.
func Open(ctx context.Context, path string, readOnly bool) (*Store, error) {
	if readOnly {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("message database %s not found: start `whatsapp-mcp daemon` first (%w)", path, err)
		}
	}
	db, err := sql.Open("sqlite", DSN(path, readOnly))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// SQLite serialises writers anyway; one connection avoids "database is locked".
	db.SetMaxOpenConns(1)
	if !readOnly {
		if _, err := db.ExecContext(ctx, schema); err != nil {
			db.Close()
			return nil, fmt.Errorf("create schema in %s: %w", path, err)
		}
		// The archive is private data.
		_ = os.Chmod(path, 0o600)
		if _, err := db.ExecContext(ctx, repairs); err != nil {
			db.Close()
			return nil, fmt.Errorf("repair %s: %w", path, err)
		}
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// OpenMemory opens a private in-memory database (tests).
func OpenMemory(ctx context.Context) (*Store, error) {
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the connection for callers that need raw access (import, tests).
func (s *Store) DB() *sql.DB { return s.db }

// FormatTime renders t for storage and comparisons.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// ParseTime reads a stored timestamp.
func ParseTime(s string) (time.Time, error) {
	t, err := time.Parse(TimeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored timestamp %q: %w", s, err)
	}
	return t, nil
}

func nullable(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Counts reports the number of rows per table (status output).
type Counts struct {
	Contacts int `json:"contacts"`
	Chats    int `json:"chats"`
	Messages int `json:"messages"`
}

// Count returns the row counts.
func (s *Store) Count(ctx context.Context) (Counts, error) {
	var c Counts
	row := s.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM contacts), (SELECT count(*) FROM chats), (SELECT count(*) FROM messages)`)
	if err := row.Scan(&c.Contacts, &c.Chats, &c.Messages); err != nil {
		return c, fmt.Errorf("count rows: %w", err)
	}
	return c, nil
}

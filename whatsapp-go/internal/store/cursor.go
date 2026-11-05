package store

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
)

// Page is the pagination part of every list result.
type Page struct {
	// NextCursor is passed back as `cursor` to fetch the following page. Empty when HasMore is false.
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// Cursors are opaque to callers: a base64url-encoded JSON array of the sort
// keys of the last row returned. They are stable across daemon restarts.
func encodeCursor(parts ...string) string {
	raw, _ := json.Marshal(parts)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(cursor string, n int) ([]string, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor %q: pass the next_cursor value of the previous page unchanged, or omit it to start over", cursor)
	}
	var parts []string
	if err := json.Unmarshal(raw, &parts); err != nil || len(parts) != n {
		return nil, fmt.Errorf("invalid cursor %q: pass the next_cursor value of the previous page unchanged, or omit it to start over", cursor)
	}
	return parts, nil
}

// cursorInt parses one numeric cursor part. Numbers must be bound as
// integers: SQLite orders every integer before every text value, so a
// string bound against an INTEGER column would never compare as intended.
func cursorInt(part, cursor string) (int64, error) {
	n, err := strconv.ParseInt(part, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor %q: pass the next_cursor value of the previous page unchanged, or omit it to start over", cursor)
	}
	return n, nil
}

// clampLimit applies the default and the hard maximum of list sizes.
func clampLimit(limit int) int {
	switch {
	case limit <= 0:
		return 20
	case limit > 200:
		return 200
	}
	return limit
}

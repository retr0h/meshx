// Copyright (c) 2026 John Dewey

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

package meshx

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	// SQLite driver registration — we talk to it via database/sql.
	_ "github.com/mattn/go-sqlite3"
)

// messageSchema is the CREATE statement for the sole table we persist —
// a flat mirror of messageItem's fields. Only live-mode radio traffic
// (incoming text + our outgoing commands/text) is written; demo mode
// stays in-memory so screenshot sessions never pollute the real log.
// System lines (/whois cards, flash messages) are skipped because
// their content is derived state that would be stale on replay.
const messageSchema = `
CREATE TABLE IF NOT EXISTS messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    channel    TEXT    NOT NULL,
    time       TEXT    NOT NULL,
    sender     TEXT    NOT NULL,
    text       TEXT    NOT NULL,
    mine       INTEGER NOT NULL,
    bang       TEXT    NOT NULL DEFAULT '',
    status     TEXT    NOT NULL DEFAULT '',
    hops       INTEGER NOT NULL DEFAULT 0,
    snr        TEXT    NOT NULL DEFAULT '',
    packet_id  INTEGER NOT NULL DEFAULT 0,
    reply_id   INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_messages_channel_id ON messages(channel, id);
`

// defaultStoragePath returns "$HOME/.meshx/meshx.db" with the parent
// directory created on demand. Used by live-radio mode (RunRadio) to
// persist chat history across restarts.
func defaultStoragePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	dir := filepath.Join(home, ".meshx")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return filepath.Join(dir, "meshx.db"), nil
}

// openStorage opens (creating if needed) the SQLite file at path and
// runs the schema DDL. Caller is responsible for closing the db.
func openStorage(path string) (*sql.DB, error) {
	// WAL journal mode survives power loss better than the default
	// rollback journal and also lets readers (us on startup replay)
	// not block writers. _busy_timeout=5000 means transient locks
	// during concurrent writes retry for up to 5s instead of failing.
	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	if _, err := db.Exec(messageSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return db, nil
}

// saveMessage persists one messageItem under a channel. System rows
// and entries without any wire origin (demo seeds, local-only) are
// skipped — they'd be stale on replay anyway. Failure is logged but
// non-fatal: losing history is preferable to crashing the UI.
func saveMessage(db *sql.DB, channel string, msg messageItem) error {
	if db == nil {
		return nil
	}
	if msg.status == "system" {
		return nil
	}
	mine := 0
	if msg.mine {
		mine = 1
	}
	_, err := db.Exec(`
        INSERT INTO messages
        (channel, time, sender, text, mine, bang, status, hops, snr, packet_id, reply_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		channel, msg.time, msg.from, msg.text, mine, msg.bang, msg.status,
		msg.hops, msg.snr, msg.packetID, msg.replyID,
	)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

// loadMessages reads the most recent `limit` rows, oldest-first (so
// callers can append directly to m.messages and selectedMsg = len-1
// lands on the newest). An empty channel string means "every
// channel" — used at boot before the handshake resolves which
// channel the user is on. A limit of 0 returns nothing; negative
// means "no cap".
func loadMessages(db *sql.DB, channel string, limit int) ([]messageItem, error) {
	if db == nil {
		return nil, nil
	}
	query := `
        SELECT time, sender, text, mine, bang, status, hops, snr, packet_id, reply_id
        FROM messages`
	var args []any
	if channel != "" {
		query += " WHERE channel = ?"
		args = append(args, channel)
	}
	query += " ORDER BY id DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []messageItem
	for rows.Next() {
		var (
			msg  messageItem
			mine int
		)
		if err := rows.Scan(
			&msg.time, &msg.from, &msg.text, &mine, &msg.bang, &msg.status,
			&msg.hops, &msg.snr, &msg.packetID, &msg.replyID,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msg.mine = mine != 0
		out = append(out, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iter: %w", err)
	}
	// Reverse to oldest-first so callers can append in natural order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

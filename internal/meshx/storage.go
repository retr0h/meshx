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
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"

	// SQLite driver registration — we talk to it via database/sql.
	_ "github.com/mattn/go-sqlite3"
)

// embedMigrations ships every SQL file under migrations/ into the
// binary so goose can apply them against a user's ~/.meshx/meshx.db
// without needing the source tree at runtime. Same pattern freebies
// uses — `001_schema.sql` is the initial create, every subsequent
// file is an incremental ALTER / CREATE / etc.
//
//go:embed migrations/*.sql
var embedMigrations embed.FS

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
// runs the goose migration chain. Caller closes the db.
func openStorage(path string) (*sql.DB, error) {
	// WAL journal mode survives power loss better than the default
	// rollback journal and also lets a startup reader not block
	// writers. _busy_timeout=5000 rides out transient lock
	// contention during concurrent writes.
	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// runMigrations applies every embedded migration via goose. Same
// dialect / base-FS / Up flow as freebies — no bespoke migration
// logic here, we let goose track applied versions in its
// goose_db_version table.
func runMigrations(db *sql.DB) error {
	goose.SetBaseFS(embedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
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
        (channel, time, sender, text, mine, bang, status, hops, snr, packet_id, reply_id, from_num)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		channel, msg.time, msg.from, msg.text, mine, msg.bang, msg.status,
		msg.hops, msg.snr, msg.packetID, msg.replyID, msg.fromNum,
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
        SELECT time, sender, text, mine, bang, status, hops, snr, packet_id, reply_id, from_num
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
			&msg.hops, &msg.snr, &msg.packetID, &msg.replyID, &msg.fromNum,
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

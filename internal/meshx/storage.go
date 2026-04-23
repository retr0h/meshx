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
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
//
// Wires goose's logger to a stderr prefix so every startup prints
// which versions applied (or "no pending migrations") — the tea
// UI renders over stderr can't swallow, so even in alt-screen mode
// these lines show up if the user exits or pipes stderr to a file
// (`meshx 2>meshx.log`).
func runMigrations(db *sql.DB) error {
	goose.SetBaseFS(embedMigrations)
	goose.SetLogger(log.New(os.Stderr, "meshx/storage: ", log.LstdFlags))
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	// One-off retroactive fix — rows written before saveMessage
	// learned about from_num still have it set to 0. Parse the
	// "node 0x<hex>" sender placeholder and back-fill the real
	// node num so ghost-peer replay resolves those senders to a
	// lookup-able nodeItem on next launch. Idempotent: only
	// touches rows where from_num == 0 AND sender matches the
	// placeholder shape.
	if err := backfillFromNum(db); err != nil {
		return fmt.Errorf("backfill from_num: %w", err)
	}
	return nil
}

// backfillFromNum parses the historical "node 0x<hex>" sender
// placeholder out of pre-from_num rows and writes the decoded
// number into from_num. Rows that already have a non-zero
// from_num are skipped. Anything that doesn't match the
// placeholder shape (real callsigns from peers we did resolve)
// is skipped too — we'd have no id to recover for them.
func backfillFromNum(db *sql.DB) error {
	rows, err := db.Query(
		`SELECT id, sender FROM messages WHERE from_num = 0 AND sender LIKE 'node 0x%'`,
	)
	if err != nil {
		return fmt.Errorf("query placeholder rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type update struct {
		id      int64
		fromNum uint64
	}
	var updates []update
	for rows.Next() {
		var (
			id     int64
			sender string
		)
		if err := rows.Scan(&id, &sender); err != nil {
			return fmt.Errorf("scan placeholder row: %w", err)
		}
		hex := strings.TrimPrefix(sender, "node 0x")
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			continue // sender looked close but didn't actually parse — skip
		}
		updates = append(updates, update{id: id, fromNum: n})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iter placeholder rows: %w", err)
	}
	if len(updates) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin backfill tx: %w", err)
	}
	stmt, err := tx.Prepare(`UPDATE messages SET from_num = ? WHERE id = ?`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare backfill update: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, u := range updates {
		if _, err := stmt.Exec(u.fromNum, u.id); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec backfill update id=%d: %w", u.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backfill tx: %w", err)
	}
	log.Printf("meshx/storage: back-filled from_num on %d historical rows", len(updates))
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

// saveNode persists a peer's current User info. Called on every
// upsertNode so once we learn a real longname / shortname for a
// node num, we remember it across sessions — mirrors what the
// official Meshtastic phone app does locally. Placeholder
// "node 0x…" callsigns are skipped; the point is to preserve
// the RESOLVED name, not the fallback. Failure is logged but
// non-fatal.
func saveNode(db *sql.DB, nodeNum uint32, longName, shortName, hwModel string) error {
	if db == nil {
		return nil
	}
	if longName == "" && shortName == "" {
		return nil
	}
	_, err := db.Exec(`
        INSERT INTO nodes (node_num, long_name, short_name, hw_model, last_seen)
        VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
        ON CONFLICT(node_num) DO UPDATE SET
            long_name  = excluded.long_name,
            short_name = excluded.short_name,
            hw_model   = excluded.hw_model,
            last_seen  = CURRENT_TIMESTAMP`,
		nodeNum, longName, shortName, hwModel,
	)
	if err != nil {
		return fmt.Errorf("insert node: %w", err)
	}
	return nil
}

// cachedNode is the slim shape of a persisted node row — identity
// fields + sticky UX preferences (favorite / muted). No telemetry
// (per-session by nature).
type cachedNode struct {
	nodeNum   uint32
	longName  string
	shortName string
	hwModel   string
	favorite  bool
	muted     bool
}

// loadNodes reads every persisted node. Used at startup to
// pre-populate m.nodes / m.nodesByNum with real callsigns AND the
// user's sticky favorite / muted preferences, so the star next to
// a node and the "⊘ muted" state survive restarts.
func loadNodes(db *sql.DB) ([]cachedNode, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.Query(
		`SELECT node_num, long_name, short_name, hw_model, favorite, muted FROM nodes`,
	)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []cachedNode
	for rows.Next() {
		var (
			n   cachedNode
			fav int
			mu  int
		)
		if err := rows.Scan(&n.nodeNum, &n.longName, &n.shortName, &n.hwModel, &fav, &mu); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		n.favorite = fav != 0
		n.muted = mu != 0
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iter: %w", err)
	}
	return out, nil
}

// saveNodePrefs writes just the sticky UX preferences (favorite /
// muted) for a single node num. INSERT-on-conflict so this works
// even if the NodeInfo identity row isn't saved yet (user stars a
// still-ghost peer). The identity fields stay empty until
// saveNode fills them in later; saveNode's ON CONFLICT UPDATE
// explicitly does NOT touch favorite / muted so this pref never
// gets clobbered.
func saveNodePrefs(db *sql.DB, nodeNum uint32, favorite, muted bool) error {
	if db == nil {
		return nil
	}
	fav, mu := 0, 0
	if favorite {
		fav = 1
	}
	if muted {
		mu = 1
	}
	_, err := db.Exec(`
        INSERT INTO nodes (node_num, favorite, muted)
        VALUES (?, ?, ?)
        ON CONFLICT(node_num) DO UPDATE SET
            favorite = excluded.favorite,
            muted    = excluded.muted`,
		nodeNum, fav, mu,
	)
	if err != nil {
		return fmt.Errorf("save node prefs: %w", err)
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

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
	"strconv"
	"strings"
	"time"

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
// runs the goose migration chain. Returns the db, any informational
// notes the caller should surface in the UI (migration versions
// applied, backfill counts, etc.), and an error. Caller closes the
// db.
func openStorage(path string) (*sql.DB, []string, error) {
	// WAL journal mode survives power loss better than the default
	// rollback journal and also lets a startup reader not block
	// writers. _busy_timeout=5000 rides out transient lock
	// contention during concurrent writes.
	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("ping sqlite: %w", err)
	}
	notes, err := runMigrations(db)
	if err != nil {
		_ = db.Close()
		return nil, notes, err
	}
	return db, notes, nil
}

// runMigrations applies every embedded migration via goose and runs
// one-off backfills. Captures all informational output (goose's
// "applied vN", "no migrations to run", our backfill count) into an
// in-memory slice the caller routes into systemLine entries inside
// the messages pane — nothing ever touches stderr, so the TUI owns
// the whole terminal window.
func runMigrations(db *sql.DB) ([]string, error) {
	var notes []string
	goose.SetBaseFS(embedMigrations)
	goose.SetLogger(noticesLogger{notes: &notes})
	if err := goose.SetDialect("sqlite3"); err != nil {
		return notes, fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return notes, fmt.Errorf("goose up: %w", err)
	}
	// One-off retroactive fix — rows written before saveMessage
	// learned about from_num still have it set to 0. Parse the
	// "node 0x<hex>" sender placeholder and back-fill the real
	// node num so ghost-peer replay resolves those senders to a
	// lookup-able nodeItem on next launch. Idempotent: only
	// touches rows where from_num == 0 AND sender matches the
	// placeholder shape.
	n, err := backfillFromNum(db)
	if err != nil {
		return notes, fmt.Errorf("backfill from_num: %w", err)
	}
	if n > 0 {
		notes = append(notes, fmt.Sprintf("back-filled from_num on %d historical rows", n))
	}
	return notes, nil
}

// backfillFromNum parses the historical "node 0x<hex>" sender
// placeholder out of pre-from_num rows and writes the decoded
// number into from_num. Rows that already have a non-zero
// from_num are skipped. Anything that doesn't match the
// placeholder shape (real callsigns from peers we did resolve)
// is skipped too — we'd have no id to recover for them.
func backfillFromNum(db *sql.DB) (int, error) {
	rows, err := db.Query(
		`SELECT id, sender FROM messages WHERE from_num = 0 AND sender LIKE 'node 0x%'`,
	)
	if err != nil {
		return 0, fmt.Errorf("query placeholder rows: %w", err)
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
			return 0, fmt.Errorf("scan placeholder row: %w", err)
		}
		hex := strings.TrimPrefix(sender, "node 0x")
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			continue // sender looked close but didn't actually parse — skip
		}
		updates = append(updates, update{id: id, fromNum: n})
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iter placeholder rows: %w", err)
	}
	if len(updates) == 0 {
		return 0, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin backfill tx: %w", err)
	}
	stmt, err := tx.Prepare(`UPDATE messages SET from_num = ? WHERE id = ?`)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("prepare backfill update: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, u := range updates {
		if _, err := stmt.Exec(u.fromNum, u.id); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("exec backfill update id=%d: %w", u.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit backfill tx: %w", err)
	}
	return len(updates), nil
}

// expireStalePendingMessages finds every row whose status is still
// "pending" after a prior session crashed or exited mid-flight, and
// flips it to "fail" when its created_at is older than ttl. Called
// from newModel before loadMessages replays, so the user sees the
// stale rows as `✗` (and can hit `R` to resend them) rather than `…`
// ghosts that nothing will ever ack. Returns the count updated so
// the caller can surface it as a systemLine. Safe on nil db.
func expireStalePendingMessages(db *sql.DB, ttl time.Duration) (int, error) {
	if db == nil {
		return 0, nil
	}
	cutoff := time.Now().Add(-ttl)
	res, err := db.Exec(
		`UPDATE messages SET status = 'fail'
         WHERE status = 'pending' AND created_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("expire stale pending: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expire stale pending rows affected: %w", err)
	}
	return int(n), nil
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
        SELECT time, sender, text, mine, bang, status, hops, snr, packet_id, reply_id, from_num, created_at
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
			&msg.sentAt,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msg.mine = mine != 0
		// Historic rows may have been written before sanitizeMessageText
		// landed in applyTextMessage. Clean on read so old end-of-day
		// reports don't smear the pane border on replay.
		msg.text = sanitizeMessageText(msg.text)
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

// bleDevice is the slim persisted shape of a Bluetooth-paired
// Meshtastic radio. Populated from the ble_devices table and
// consumed by the `meshx ble` subcommand tree + bare-meshx fallback
// resolution.
type bleDevice struct {
	UUID      string
	LongName  string
	ShortName string
	HWModel   string
	Favorite  bool
}

// DisplayName returns the human-facing label the CLI prints in
// `meshx ble list` and in "connecting to …" messages. Prefers the
// longname (the name printed on the radio's OLED), falls back to
// shortname, then the raw uuid. Always non-empty.
func (d bleDevice) DisplayName() string {
	switch {
	case d.LongName != "":
		return d.LongName
	case d.ShortName != "":
		return d.ShortName
	default:
		return d.UUID
	}
}

// saveBLEDevice inserts a newly-paired device (or updates its
// metadata on re-pair). Does NOT touch the favorite flag —
// setBLEFavorite is the single entrypoint for that so we don't
// accidentally change which device is auto-connected.
func saveBLEDevice(db *sql.DB, d bleDevice) error {
	if db == nil {
		return nil
	}
	if d.UUID == "" {
		return fmt.Errorf("save ble device: uuid required")
	}
	_, err := db.Exec(`
        INSERT INTO ble_devices (uuid, long_name, short_name, hw_model, paired_at)
        VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
        ON CONFLICT(uuid) DO UPDATE SET
            long_name  = excluded.long_name,
            short_name = excluded.short_name,
            hw_model   = excluded.hw_model`,
		d.UUID, d.LongName, d.ShortName, d.HWModel,
	)
	if err != nil {
		return fmt.Errorf("save ble device: %w", err)
	}
	return nil
}

// loadBLEDevices returns every saved Bluetooth device ordered by
// favorite DESC, paired_at DESC so `meshx ble list` naturally
// surfaces the auto-connect target at the top. Empty slice when
// no devices are paired yet.
func loadBLEDevices(db *sql.DB) ([]bleDevice, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.Query(`
        SELECT uuid, long_name, short_name, hw_model, favorite
        FROM ble_devices
        ORDER BY favorite DESC, paired_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query ble devices: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []bleDevice
	for rows.Next() {
		var (
			d   bleDevice
			fav int
		)
		if err := rows.Scan(&d.UUID, &d.LongName, &d.ShortName, &d.HWModel, &fav); err != nil {
			return nil, fmt.Errorf("scan ble device: %w", err)
		}
		d.Favorite = fav != 0
		out = append(out, d)
	}
	return out, rows.Err()
}

// lookupBLEDevice finds a saved device by exact uuid OR by longname
// / shortname match (case-insensitive). Returns nil if no match,
// error only on DB failure. Accepting the friendly name is what
// makes `meshx ble connect T-Beam-Mobile` work alongside the
// hex-uuid form.
func lookupBLEDevice(db *sql.DB, needle string) (*bleDevice, error) {
	devs, err := loadBLEDevices(db)
	if err != nil {
		return nil, err
	}
	lowered := strings.ToLower(needle)
	for _, d := range devs {
		if d.UUID == needle {
			return &d, nil
		}
		if strings.EqualFold(d.LongName, needle) || strings.EqualFold(d.ShortName, needle) {
			return &d, nil
		}
		// Allow case-insensitive UUID match too — macOS sometimes
		// uppercases, Linux sometimes lowercases, user shouldn't
		// have to care.
		if strings.ToLower(d.UUID) == lowered {
			return &d, nil
		}
	}
	return nil, nil
}

// setBLEFavorite marks exactly one device as the auto-connect
// fallback for bare `meshx`. Clears the flag on every other row in
// the same transaction so we never end up with two favorites. Empty
// uuid clears the flag entirely (no favorite set).
func setBLEFavorite(db *sql.DB, uuid string) error {
	if db == nil {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin set favorite: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE ble_devices SET favorite = 0`); err != nil {
		return fmt.Errorf("clear favorites: %w", err)
	}
	if uuid != "" {
		if _, err := tx.Exec(`UPDATE ble_devices SET favorite = 1 WHERE uuid = ?`, uuid); err != nil {
			return fmt.Errorf("set favorite: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit favorite: %w", err)
	}
	return nil
}

// forgetBLEDevice removes a paired device from persistence. The
// caller is responsible for any OS-level unpair call (macOS doesn't
// expose one programmatically; Linux uses `bluetoothctl remove`).
// Missing uuids return nil (idempotent forget).
func forgetBLEDevice(db *sql.DB, uuid string) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`DELETE FROM ble_devices WHERE uuid = ?`, uuid)
	if err != nil {
		return fmt.Errorf("forget ble device: %w", err)
	}
	return nil
}

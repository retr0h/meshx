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
	"errors"
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

// legacyRadioID is the placeholder UUID migration 009 used to seed
// pre-existing single-radio data. Migration 010 rewrites it to
// `radioIDFromNodeNum(my_node_num)` for any radio whose handshake
// has populated my_node_num; the application-level claimRadioIdentity
// covers the remaining case (placeholder still present because the
// radio hasn't finished a handshake yet). We keep the constant so
// claimRadioIdentity can recognize the legacy placeholder and
// upgrade it on first MyNodeInfo arrival.
const legacyRadioID = "00000000-0000-0000-0000-000000000001"

// radioIDFromNodeNum returns the canonical Meshtastic identity string
// for a radio: "0x" + lower-cased 8-hex-digit zero-padded node num.
// Matches what the Meshtastic phone app, Python CLI, and the firmware
// itself all surface (e.g. "0x103e034d"). Used as the primary key in
// the radios table once a handshake reveals my_node_num.
func radioIDFromNodeNum(myNodeNum uint32) string {
	return fmt.Sprintf("0x%08x", myNodeNum)
}

// pendingRadioID returns the placeholder ID for a connection whose
// my_node_num isn't known yet — the gap between transport.Dial
// returning and MyNodeInfo arriving. The form is self-describing
// ("pending:<transport>:<addr>") so a developer poking at the DB
// or the daemon's API mid-handshake can tell at a glance that the
// row is unresolved. Replaced by `radioIDFromNodeNum(my_node_num)`
// the moment MyNodeInfo arrives — see claimRadioIdentity.
func pendingRadioID(transport, addr string) string {
	return fmt.Sprintf("pending:%s:%s", transport, addr)
}

// isPlaceholderRadioID reports whether id is one of the placeholder
// shapes claimRadioIdentity should rewrite on first handshake — the
// legacy migration-009 seed UUID, or any "pending:…" string minted
// pre-handshake by resolveRadioByConnection. Real radio IDs ("0xNN…")
// fall through unchanged.
func isPlaceholderRadioID(id string) bool {
	return id == legacyRadioID || strings.HasPrefix(id, "pending:")
}

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
func expireStalePendingMessages(db *sql.DB, radioID string, ttl time.Duration) (int, error) {
	if db == nil {
		return 0, nil
	}
	cutoff := time.Now().Add(-ttl)
	res, err := db.Exec(
		`UPDATE messages SET status = 'fail'
         WHERE radio_id = ? AND status = 'pending' AND created_at < ?`,
		radioID, cutoff,
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
func saveMessage(db *sql.DB, radioID, channel string, msg messageItem) error {
	if db == nil {
		return nil
	}
	// statusSystem (and statusNotice) are locally generated rows that
	// regenerate from live state on every launch — don't waste a
	// SQLite write on them.
	if msg.status == statusSystem || msg.status == statusNotice {
		return nil
	}
	mine := 0
	if msg.mine {
		mine = 1
	}
	// ON CONFLICT(packet_id) DO UPDATE — when a replay lands for a
	// packet we already have, refresh the mutable state (status,
	// signal telemetry) in place instead of failing the unique
	// index added in migration 006. The WHERE excluded.packet_id > 0
	// guard mirrors the partial index: system rows / local-only
	// entries carry packet_id = 0 and the constraint doesn't apply
	// to them, so those still append freely.
	_, err := db.Exec(`
        INSERT INTO messages
        (radio_id, channel, time, sender, text, mine, bang, status, hops, snr, packet_id, reply_id, from_num)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(packet_id) WHERE packet_id > 0 DO UPDATE SET
            status = excluded.status,
            hops   = excluded.hops,
            snr    = excluded.snr`,
		radioID, channel, msg.time, msg.from, msg.text, mine, msg.bang, msg.status.String(),
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
func saveNode(
	db *sql.DB,
	radioID string,
	nodeNum uint32,
	longName, shortName, hwModel string,
) error {
	if db == nil {
		return nil
	}
	if longName == "" && shortName == "" {
		return nil
	}
	// ON CONFLICT(node_num) — the existing unique index is on node_num
	// alone, which means TWO radios reporting the same peer share one
	// row. That's intentional for now: same Meshtastic peer => same
	// identity regardless of which of the user's radios heard it. If
	// per-radio peer state ever matters (different RSSI per radio,
	// per-radio mute), the unique index becomes (node_num, radio_id)
	// in a future migration.
	_, err := db.Exec(`
        INSERT INTO nodes (radio_id, node_num, long_name, short_name, hw_model, last_seen)
        VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
        ON CONFLICT(node_num) DO UPDATE SET
            radio_id   = excluded.radio_id,
            long_name  = excluded.long_name,
            short_name = excluded.short_name,
            hw_model   = excluded.hw_model,
            last_seen  = CURRENT_TIMESTAMP`,
		radioID, nodeNum, longName, shortName, hwModel,
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
func loadNodes(db *sql.DB, radioID string) ([]cachedNode, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.Query(
		`SELECT node_num, long_name, short_name, hw_model, favorite, muted
         FROM nodes WHERE radio_id = ?`,
		radioID,
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
func saveNodePrefs(
	db *sql.DB, radioID string, nodeNum uint32, favorite, muted bool,
) error {
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
        INSERT INTO nodes (radio_id, node_num, favorite, muted)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(node_num) DO UPDATE SET
            radio_id = excluded.radio_id,
            favorite = excluded.favorite,
            muted    = excluded.muted`,
		radioID, nodeNum, fav, mu,
	)
	if err != nil {
		return fmt.Errorf("save node prefs: %w", err)
	}
	return nil
}

// getSetting returns the persisted value for `key` or ("", false) when
// the row is missing. radioID scopes the lookup: pass "" for global
// (meshx-client) prefs like /mute's "ding_muted"; pass a radio UUID
// for per-radio prefs (none today; reserved for things like per-
// radio default channel or per-radio nicknames). Treats nil db as
// "no row" so demo mode and storage-open failures don't branch at
// the call site.
func getSetting(db *sql.DB, radioID, key string) (string, bool) {
	if db == nil {
		return "", false
	}
	var v string
	var err error
	if radioID == "" {
		err = db.QueryRow(
			`SELECT value FROM settings WHERE key = ? AND radio_id IS NULL`,
			key,
		).Scan(&v)
	} else {
		err = db.QueryRow(
			`SELECT value FROM settings WHERE key = ? AND radio_id = ?`,
			key, radioID,
		).Scan(&v)
	}
	if err != nil {
		return "", false
	}
	return v, true
}

// putSetting writes `value` under `(key, radioID)`, upserting when the
// row already exists. Pass "" for radioID to write a global pref.
//
// The settings PRIMARY KEY today is just (key) — see migration 007.
// Multi-radio per-key support requires migration to a composite PK
// (key, COALESCE(radio_id, '__global__')); deferred until a per-
// radio setting actually exists. For now, attempting a per-radio
// setting + a global setting under the same key would conflict on
// the existing PK; not a problem because no caller does that today.
func putSetting(db *sql.DB, radioID, key, value string) error {
	if db == nil {
		return nil
	}
	var rid any
	if radioID != "" {
		rid = radioID
	}
	_, err := db.Exec(`
        INSERT INTO settings (key, value, radio_id) VALUES (?, ?, ?)
        ON CONFLICT(key) DO UPDATE SET
            value    = excluded.value,
            radio_id = excluded.radio_id`,
		key, value, rid,
	)
	if err != nil {
		return fmt.Errorf("put setting %s: %w", key, err)
	}
	return nil
}

// loadMessages reads the most recent `limit` rows, oldest-first (so
// callers can append directly to m.messages and selectedMsg = len-1
// lands on the newest). An empty channel string means "every
// channel" — used at boot before the handshake resolves which
// channel the user is on. A limit of 0 returns nothing; negative
// means "no cap".
func loadMessages(
	db *sql.DB, radioID, channel string, limit int,
) ([]messageItem, error) {
	if db == nil {
		return nil, nil
	}
	query := `
        SELECT time, sender, text, mine, bang, status, hops, snr, packet_id, reply_id, from_num, created_at
        FROM messages
        WHERE radio_id = ?`
	args := []any{radioID}
	if channel != "" {
		query += " AND channel = ?"
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
		var statusStr string
		if err := rows.Scan(
			&msg.time, &msg.from, &msg.text, &mine, &msg.bang, &statusStr,
			&msg.hops, &msg.snr, &msg.packetID, &msg.replyID, &msg.fromNum,
			&msg.sentAt,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msg.status = parseMessageStatus(statusStr)
		msg.mine = mine != 0
		// Historic rows may have been written before sanitizeMessageText
		// landed in applyTextMessage. Clean on read so old end-of-day
		// reports don't smear the pane border on replay AND so the
		// corruption flag gets stamped onto historic rows that have
		// invalid bytes baked into SQLite — they pick up the ⚠ marker
		// + dim styling on next launch without any migration.
		msg.text, msg.corrupted = sanitizeMessageText(msg.text)
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

// parseRadioDest splits a meshx Dial dest string into transport +
// addr components for radio identity lookup. Handles the three
// schemes transport.Dial recognizes:
//
//	"ble:<uuid>"            → ("ble", "<uuid>")
//	"host:port"             → ("tcp", "host:port")    (port is numeric)
//	"/dev/cu.usbserial-…"   → ("usb", "/dev/cu.usbserial-…")  (anything else)
//
// Empty dest returns ("unknown", "unknown") — same placeholder the
// 009 migration seeds, so demo-mode / never-connected radios still
// have a stable identity.
func parseRadioDest(dest string) (transport, addr string) {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "unknown", "unknown"
	}
	if rest, ok := strings.CutPrefix(dest, "ble:"); ok {
		return "ble", rest
	}
	if i := strings.LastIndex(dest, ":"); i > 0 {
		// Numeric tail = TCP host:port; non-numeric tail (e.g. a
		// Windows COM path containing ":") falls through to usb.
		if _, err := strconv.Atoi(dest[i+1:]); err == nil {
			return "tcp", dest
		}
	}
	return "usb", dest
}

// resolveRadioByConnection returns the radio_id for the given
// (transport, addr) connection — either the canonical
// `radioIDFromNodeNum` form for radios we've handshaken with before
// (cache hit on radios.my_node_num), or a `pendingRadioID` placeholder
// for fresh connections whose handshake hasn't completed yet. The
// placeholder gets rewritten to the canonical form by
// claimRadioIdentity once MyNodeInfo arrives.
//
// Steady state: a radio that's connected at least once before sits in
// radios with id="0xNNNNNNNN" and an exact (transport, addr) match.
// Cache lookup returns instantly; history loads against the real
// identity before the handshake even starts.
//
// First connect to a never-seen-before radio: no exact match. We
// insert a placeholder row keyed on `pendingRadioID(transport, addr)`
// so the storage calls this session issues land somewhere consistent;
// when MyNodeInfo lands, claimRadioIdentity rewrites the placeholder
// across radios + every FK column in one transaction.
//
// Demo mode (db == nil): returns ("", nil) — callers treat empty
// radioID as "no persistence" and skip storage writes entirely.
func resolveRadioByConnection(
	db *sql.DB, transport, addr string,
) (string, error) {
	if db == nil {
		return "", nil
	}
	// Exact (transport, addr) hit — return whatever id is on the row.
	// For an upgraded DB that's "0xNNNNNNNN"; for an in-flight pending
	// connection that's the same placeholder we'd otherwise mint.
	var id string
	err := db.QueryRow(
		`SELECT id FROM radios WHERE transport = ? AND addr = ?`,
		transport, addr,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("lookup radio: %w", err)
	}

	// No row for this (transport, addr). Two sub-cases to handle:
	//
	// 1. Legacy seeded radios.id='00…01' from migration 009 still
	//    sitting in placeholder state (transport='unknown'). Claim it
	//    by rewriting transport+addr in place — keeps the seed row
	//    pointing at the radio that connects first, so historical FKs
	//    (which all reference 00…01 until claimRadioIdentity runs)
	//    stay correctly attributed.
	//
	// 2. Genuine new radio. Insert a fresh placeholder row keyed on
	//    pendingRadioID(transport, addr); claimRadioIdentity rewrites
	//    it to the canonical 0xNN form on MyNodeInfo arrival.
	var legacyAddr string
	err = db.QueryRow(
		`SELECT addr FROM radios WHERE id = ?`, legacyRadioID,
	).Scan(&legacyAddr)
	if err == nil && legacyAddr == "unknown" {
		_, err = db.Exec(
			`UPDATE radios SET transport = ?, addr = ?, last_seen = CURRENT_TIMESTAMP
             WHERE id = ?`,
			transport, addr, legacyRadioID,
		)
		if err != nil {
			return "", fmt.Errorf("claim legacy radio: %w", err)
		}
		return legacyRadioID, nil
	}

	pending := pendingRadioID(transport, addr)
	_, err = db.Exec(
		`INSERT INTO radios (id, name, transport, addr, last_seen)
         VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		pending, "radio", transport, addr,
	)
	if err != nil {
		return "", fmt.Errorf("insert pending radio: %w", err)
	}
	return pending, nil
}

// claimRadioIdentity rewrites a placeholder radio_id (the legacy 009
// seed UUID, or any "pending:…" string) to the canonical
// `radioIDFromNodeNum(myNodeNum)` form, propagating the change across
// every foreign-key column (messages, nodes, settings) and the radios
// row itself. Returns the new canonical id.
//
// Called from the radioMyInfoMsg handler the moment the radio's own
// node num arrives. No-op when oldID is already canonical (steady
// state — we've handshaken with this radio before; nothing to claim).
//
// All four UPDATEs run in one transaction so a crash mid-rewrite
// can't leave dangling FKs. Idempotent on retry: if oldID has
// already been rewritten, every WHERE clause matches zero rows.
func claimRadioIdentity(
	db *sql.DB, oldID string, myNodeNum uint32,
) (string, error) {
	newID := radioIDFromNodeNum(myNodeNum)
	if !isPlaceholderRadioID(oldID) {
		// Already canonical (we've connected to this radio before, or
		// migration 010 already rewrote it). Just refresh my_node_num
		// + last_seen so the radios row reflects the current handshake.
		if db != nil {
			_, _ = db.Exec(
				`UPDATE radios SET my_node_num = ?, last_seen = CURRENT_TIMESTAMP
                 WHERE id = ?`,
				myNodeNum, oldID,
			)
		}
		return newID, nil
	}
	if db == nil {
		return newID, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("claim begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// If the canonical row already exists (extremely unlikely — would
	// require a previous claim from a different placeholder for the
	// same node num, e.g. user paired the same radio over BLE then
	// USB without restart in between), merge by deleting the
	// placeholder before reassigning FKs. Without this guard the
	// UPDATE radios SET id=newID would PK-conflict.
	var canonExists int
	err = tx.QueryRow(
		`SELECT COUNT(*) FROM radios WHERE id = ?`, newID,
	).Scan(&canonExists)
	if err != nil {
		return "", fmt.Errorf("claim probe: %w", err)
	}
	if canonExists > 0 {
		if _, err := tx.Exec(`DELETE FROM radios WHERE id = ?`, oldID); err != nil {
			return "", fmt.Errorf("claim drop placeholder: %w", err)
		}
	} else {
		if _, err := tx.Exec(
			`UPDATE radios
             SET id = ?, my_node_num = ?, last_seen = CURRENT_TIMESTAMP
             WHERE id = ?`,
			newID, myNodeNum, oldID,
		); err != nil {
			return "", fmt.Errorf("claim radios: %w", err)
		}
	}

	// Cascade to the FK columns. SQLite doesn't enforce these as real
	// foreign keys (the migration ALTER ADDs can't declare them as FKs
	// once columns already exist), so we do the cascade in app code.
	for _, table := range []string{"messages", "nodes", "settings"} {
		if _, err := tx.Exec(
			fmt.Sprintf(`UPDATE %s SET radio_id = ? WHERE radio_id = ?`, table),
			newID, oldID,
		); err != nil {
			return "", fmt.Errorf("claim %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("claim commit: %w", err)
	}
	return newID, nil
}

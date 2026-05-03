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

// Package storage owns meshx's SQLite persistence layer. The concrete
// type is `*Sqlite`, returned by New(path); consumers in the meshx
// package cast it to their own `Store` interface (defined where it's
// consumed, per the osapi-io pattern) so each call site only sees the
// methods it actually needs. The future `meshx serve` daemon casts to
// its own (likely larger) interface.
//
// All public methods take/return types from internal/meshx/model so
// the package has no dependency on the meshx renderer state. That's
// the contract that lets a future huma-based HTTP server reuse the
// same persistence layer without dragging the TUI's row envelope
// types over the wire.
package storage

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pressly/goose/v3"

	// SQLite driver registration — we talk to it via database/sql.
	_ "github.com/mattn/go-sqlite3"

	"github.com/retr0h/meshx/internal/meshx/model"
)

// LegacyRadioID is the placeholder UUID migration 009 used to seed
// pre-existing single-radio data. Migration 010 rewrites it to the
// canonical "0x{my_node_num}" form for any DB whose handshake has
// populated my_node_num; ClaimRadioIdentity covers the remaining case
// (placeholder still present because the radio hasn't finished a
// handshake yet). We expose the constant so callers can recognize
// the legacy placeholder if they need to render it differently.
const LegacyRadioID = "00000000-0000-0000-0000-000000000001"

// RadioIDFromNodeNum returns the canonical Meshtastic identity string
// for a radio: "0x" + lower-cased 8-hex-digit zero-padded node num.
// Matches what the Meshtastic phone app, Python CLI, and the firmware
// itself all surface (e.g. "0x103e034d"). Used as the primary key in
// the radios table once a handshake reveals my_node_num.
func RadioIDFromNodeNum(myNodeNum uint32) string {
	return fmt.Sprintf("0x%08x", myNodeNum)
}

// PendingRadioID returns the placeholder ID for a connection whose
// my_node_num isn't known yet — the gap between transport.Dial
// returning and MyNodeInfo arriving. The form is self-describing
// ("pending:<transport>:<addr>") so a developer poking at the DB or
// the daemon's API mid-handshake can tell at a glance that the row
// is unresolved. Replaced by RadioIDFromNodeNum the moment MyNodeInfo
// arrives — see ClaimRadioIdentity.
func PendingRadioID(transport, addr string) string {
	return fmt.Sprintf("pending:%s:%s", transport, addr)
}

// IsPlaceholderRadioID reports whether id is one of the placeholder
// shapes ClaimRadioIdentity should rewrite on first handshake — the
// legacy migration-009 seed UUID, or any "pending:…" string minted
// pre-handshake by ResolveRadioByConnection. Real radio IDs ("0xNN…")
// fall through unchanged.
func IsPlaceholderRadioID(id string) bool {
	return id == LegacyRadioID || strings.HasPrefix(id, "pending:")
}

// ParseRadioDest splits a meshx Dial dest string into transport +
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
func ParseRadioDest(dest string) (transport, addr string) {
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

// embedMigrations ships every SQL file under migrations/ into the
// binary so goose can apply them against a user's ~/.meshx/meshx.db
// without needing the source tree at runtime.
//
//go:embed migrations/*.sql
var embedMigrations embed.FS

// DefaultPath returns "$HOME/.meshx/meshx.db" with the parent
// directory created on demand. Used by live-radio mode (RunRadio) to
// persist chat history across restarts.
func DefaultPath() (string, error) {
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

// Sqlite is the concrete SQLite-backed storage implementation. Hold
// it via *Sqlite, cast to your consumer's interface at construction.
type Sqlite struct {
	db *sql.DB
}

// New opens (creating if needed) the SQLite file at path, runs the
// goose migration chain, and returns a *Sqlite ready for use. The
// migration trace lands in the process-level boot buffer (see
// ConsumeBootNotes) so any frontend (TUI, daemon, CLI) can surface
// migration apply lines on startup.
func New(path string) (*Sqlite, error) {
	// WAL journal mode survives power loss better than the default
	// rollback journal and also lets a startup reader not block
	// writers. _busy_timeout=5000 rides out transient lock contention
	// during concurrent writes.
	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	notes, err := runMigrations(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	recordBootNotes(notes)
	return &Sqlite{db: db}, nil
}

// Close releases the underlying SQLite handle. Idempotent on nil
// receiver so callers don't have to guard demo-mode (never-opened)
// stores.
func (s *Sqlite) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB returns the underlying *sql.DB for callers (BLE CLI helpers)
// that haven't been migrated to the methods API yet. New code should
// use the typed methods instead.
//
// TODO: drop once the BLE CLI helpers in the meshx package consume
// the typed Storage interface.
func (s *Sqlite) DB() *sql.DB {
	if s == nil {
		return nil
	}
	return s.db
}

// bootNotes is the process-level diagnostics buffer for the very
// first storage.New call (which is the ONE call that actually runs
// goose migrations — every subsequent call sees a fully-migrated DB
// and produces nothing interesting). Notes captured here include
// every "OK 010_xxx.sql (Xms)" apply line plus the trailing
// "successfully migrated" summary that goose emits during the upgrade
// path on a freshly-pulled binary.
//
// Capturing globally is what lets a frontend like the TUI surface the
// migration trace via systemLine even when the migration itself was
// triggered upstream by a CLI helper that opened storage first. Drained
// by ConsumeBootNotes — slice resets on every consume so the same
// notes don't get displayed twice across multiple frontends in the
// same process.
var bootNotes struct {
	mu    sync.Mutex
	notes []string
}

func recordBootNotes(notes []string) {
	if len(notes) == 0 {
		return
	}
	bootNotes.mu.Lock()
	defer bootNotes.mu.Unlock()
	bootNotes.notes = append(bootNotes.notes, notes...)
}

// ConsumeBootNotes drains and returns the captured migration trace.
// Returns nil when nothing is buffered. Callers display the result
// however they like (systemLine for the TUI, fmt.Fprintln(stderr) for
// CLI subcommands, an SSE startup event for the future daemon). The
// buffer is single-consumer — once drained the slice is gone,
// preventing double-display.
func (s *Sqlite) ConsumeBootNotes() []string {
	bootNotes.mu.Lock()
	defer bootNotes.mu.Unlock()
	out := bootNotes.notes
	bootNotes.notes = nil
	return out
}

// noticesLogger captures goose's log.Printf output into the notes
// slice runMigrations passes in. Implements goose.Logger (Printf +
// Fatalf). Trailing newlines are trimmed so each line lands as a
// clean systemLine without the literal "\n" in display.
type noticesLogger struct{ notes *[]string }

func (l noticesLogger) Printf(format string, v ...any) {
	s := strings.TrimRight(fmt.Sprintf(format, v...), "\n")
	if s == "" {
		return
	}
	*l.notes = append(*l.notes, s)
}

// Fatalf preserves goose's "I can't continue" semantics — fall back
// to stdlib log.Fatalf which writes to stderr and os.Exit(1)s. The
// TUI's alt-screen would swallow stderr but at this point storage
// has already failed; staying silent + exiting is preferable to a
// hang.
func (l noticesLogger) Fatalf(format string, v ...any) {
	log.Fatalf(format, v...)
}

// runMigrations applies every embedded migration via goose and runs
// one-off backfills. When goose actually applies migrations, the
// captured trace ("OK 010_xxx.sql (Xms)" + post-apply summary) gets
// returned for the caller to surface; when goose finds nothing to
// do (steady state — autoconnect alone re-opens storage two-to-three
// times per launch, every open re-runs goose), we drop the captured
// notes entirely so the log doesn't spam the same status line N
// times.
//
// The "did goose apply anything" check uses goose.GetDBVersion
// before and after rather than parsing log output — that's robust
// against any future goose log-format change.
func runMigrations(db *sql.DB) ([]string, error) {
	var notes []string
	goose.SetBaseFS(embedMigrations)
	goose.SetLogger(noticesLogger{notes: &notes})
	if err := goose.SetDialect("sqlite3"); err != nil {
		return notes, fmt.Errorf("goose dialect: %w", err)
	}
	beforeVersion, err := goose.GetDBVersion(db)
	if err != nil {
		// Brand-new DB — goose_db_version doesn't exist yet, but
		// goose.Up below creates it on first run. Treat that as
		// version 0; any applied migration bumps the version above
		// 0 and the trace surfaces normally.
		beforeVersion = 0
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return notes, fmt.Errorf("goose up: %w", err)
	}
	afterVersion, _ := goose.GetDBVersion(db)
	if afterVersion == beforeVersion {
		// Steady state — goose had nothing to apply. Drop captured
		// notes so subsequent opens in the same process (autoconnect
		// → RunBLE → newModel each open storage) don't flood the
		// log with duplicate status lines. Apply traces and post-
		// apply summaries only ever land here when afterVersion >
		// beforeVersion, and those pass through unchanged.
		notes = nil
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
// placeholder out of pre-from_num rows and writes the decoded number
// into from_num. Rows that already have a non-zero from_num are
// skipped. Anything that doesn't match the placeholder shape (real
// callsigns from peers we did resolve) is skipped too — we'd have
// no id to recover for them.
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
			continue
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

// ResolveRadioByConnection returns the radio_id for the given
// (transport, addr) connection — either the canonical
// RadioIDFromNodeNum form for radios we've handshaken with before, or
// a PendingRadioID placeholder for fresh connections whose handshake
// hasn't completed yet. The placeholder gets rewritten to the
// canonical form by ClaimRadioIdentity once MyNodeInfo arrives.
//
// Demo mode (s == nil): returns ("", nil) — callers treat empty
// radioID as "no persistence" and skip storage writes entirely.
func (s *Sqlite) ResolveRadioByConnection(transport, addr string) (string, error) {
	if s == nil || s.db == nil {
		return "", nil
	}
	var id string
	err := s.db.QueryRow(
		`SELECT id FROM radios WHERE transport = ? AND addr = ?`,
		transport, addr,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("lookup radio: %w", err)
	}

	// Try to claim the legacy seeded row before minting a fresh
	// pending placeholder. See migration 009 for why the seed exists.
	var legacyAddr string
	err = s.db.QueryRow(
		`SELECT addr FROM radios WHERE id = ?`, LegacyRadioID,
	).Scan(&legacyAddr)
	if err == nil && legacyAddr == "unknown" {
		_, err = s.db.Exec(
			`UPDATE radios SET transport = ?, addr = ?, last_seen = CURRENT_TIMESTAMP
             WHERE id = ?`,
			transport, addr, LegacyRadioID,
		)
		if err != nil {
			return "", fmt.Errorf("claim legacy radio: %w", err)
		}
		return LegacyRadioID, nil
	}

	pending := PendingRadioID(transport, addr)
	_, err = s.db.Exec(
		`INSERT INTO radios (id, name, transport, addr, last_seen)
         VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		pending, "radio", transport, addr,
	)
	if err != nil {
		return "", fmt.Errorf("insert pending radio: %w", err)
	}
	return pending, nil
}

// ClaimRadioIdentity rewrites a placeholder radio_id (the legacy 009
// seed UUID, or any "pending:…" string) to the canonical
// RadioIDFromNodeNum(myNodeNum) form, propagating the change across
// every foreign-key column (messages, nodes, settings) and the radios
// row itself. Returns the new canonical id.
//
// Called from the radioMyInfoMsg handler the moment the radio's own
// node num arrives. No-op when oldID is already canonical (steady
// state — we've handshaken with this radio before; nothing to claim).
//
// All UPDATEs run in one transaction so a crash mid-rewrite can't
// leave dangling FKs. Idempotent on retry: if oldID has already been
// rewritten, every WHERE clause matches zero rows.
func (s *Sqlite) ClaimRadioIdentity(oldID string, myNodeNum uint32) (string, error) {
	newID := RadioIDFromNodeNum(myNodeNum)
	if !IsPlaceholderRadioID(oldID) {
		// Already canonical. Refresh my_node_num + last_seen so the
		// row reflects the current handshake.
		if s != nil && s.db != nil {
			_, _ = s.db.Exec(
				`UPDATE radios SET my_node_num = ?, last_seen = CURRENT_TIMESTAMP
                 WHERE id = ?`,
				myNodeNum, oldID,
			)
		}
		return newID, nil
	}
	if s == nil || s.db == nil {
		return newID, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("claim begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Guard against the (extremely rare) case where the canonical row
	// already exists — would happen if the same radio paired over BLE
	// then USB without restart in between, leaving two placeholders for
	// the same node num. Merge by deleting the placeholder so the
	// UPDATE radios SET id=newID below doesn't PK-conflict.
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

// ExpireStalePendingMessages flips every "pending" row whose
// created_at is older than ttl to "fail". Called at startup so the
// user sees stale rows as `✗` (and can hit `R` to resend them) rather
// than `…` ghosts that will never ack. Returns the count updated.
// Safe on nil receiver.
func (s *Sqlite) ExpireStalePendingMessages(radioID string, ttl time.Duration) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	cutoff := time.Now().Add(-ttl)
	res, err := s.db.Exec(
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

// SaveMessage persists one model.Message under a channel. System and
// notice rows are skipped — they regenerate from live state on every
// launch. Failure is returned so callers can route through their
// "persistence degraded" warning.
func (s *Sqlite) SaveMessage(radioID, channel string, msg model.Message) error {
	if s == nil || s.db == nil {
		return nil
	}
	if msg.Status == model.StatusSystem || msg.Status == model.StatusNotice {
		return nil
	}
	mine := 0
	if msg.Mine {
		mine = 1
	}
	// ON CONFLICT(packet_id) DO UPDATE — when a replay lands for a
	// packet we already have, refresh the mutable state (status,
	// signal telemetry) in place instead of failing the unique index
	// added in migration 006. The WHERE excluded.packet_id > 0 guard
	// mirrors the partial index: system rows / local-only entries
	// carry packet_id = 0 and the constraint doesn't apply to them,
	// so those still append freely.
	_, err := s.db.Exec(`
        INSERT INTO messages
        (radio_id, channel, time, sender, text, mine, bang, status, hops, snr, packet_id, reply_id, from_num)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(packet_id) WHERE packet_id > 0 DO UPDATE SET
            status = excluded.status,
            hops   = excluded.hops,
            snr    = excluded.snr`,
		radioID, channel, msg.Time, msg.From, msg.Text, mine, msg.Bang, msg.Status.String(),
		msg.Hops, msg.SNR, msg.PacketID, msg.ReplyID, msg.FromNum,
	)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

// LoadMessages reads the most recent `limit` rows, oldest-first (so
// callers can append directly to their state and selectedMsg = len-1
// lands on the newest). Empty channel = "every channel" (used at
// boot before the handshake resolves which channel the user is on).
// Limit 0 returns nothing; negative means "no cap".
//
// Loaded text is NOT sanitized here — sanitization is the caller's
// concern. The renderer's sanitizeMessageText runs on every load so
// a sanitizer change automatically re-evaluates historic rows.
func (s *Sqlite) LoadMessages(
	radioID, channel string, limit int,
) ([]model.Message, error) {
	if s == nil || s.db == nil {
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
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []model.Message
	for rows.Next() {
		var (
			msg  model.Message
			mine int
		)
		var statusStr string
		if err := rows.Scan(
			&msg.Time, &msg.From, &msg.Text, &mine, &msg.Bang, &statusStr,
			&msg.Hops, &msg.SNR, &msg.PacketID, &msg.ReplyID, &msg.FromNum,
			&msg.SentAt,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msg.Status = model.ParseMessageStatus(statusStr)
		msg.Mine = mine != 0
		out = append(out, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iter: %w", err)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// SaveNode persists a peer's current User info. Placeholder
// "node 0x…" callsigns (both names empty) are skipped. Failure is
// returned so callers can route through their "persistence degraded"
// warning.
func (s *Sqlite) SaveNode(radioID string, n model.CachedNode) error {
	if s == nil || s.db == nil {
		return nil
	}
	if n.LongName == "" && n.ShortName == "" {
		return nil
	}
	// ON CONFLICT(node_num) — the unique index is on node_num alone,
	// which means TWO radios reporting the same peer share one row.
	// Intentional for now: same Meshtastic peer => same identity
	// regardless of which radio heard it.
	_, err := s.db.Exec(`
        INSERT INTO nodes (radio_id, node_num, long_name, short_name, hw_model, last_seen)
        VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
        ON CONFLICT(node_num) DO UPDATE SET
            radio_id   = excluded.radio_id,
            long_name  = excluded.long_name,
            short_name = excluded.short_name,
            hw_model   = excluded.hw_model,
            last_seen  = CURRENT_TIMESTAMP`,
		radioID, n.NodeNum, n.LongName, n.ShortName, n.HwModel,
	)
	if err != nil {
		return fmt.Errorf("insert node: %w", err)
	}
	return nil
}

// LoadNodes reads every persisted node for radioID. Used at startup
// to pre-populate live state with real callsigns + sticky favorite /
// muted preferences.
func (s *Sqlite) LoadNodes(radioID string) ([]model.CachedNode, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT node_num, long_name, short_name, hw_model, favorite, muted
         FROM nodes WHERE radio_id = ?`,
		radioID,
	)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.CachedNode
	for rows.Next() {
		var (
			n   model.CachedNode
			fav int
			mu  int
		)
		if err := rows.Scan(&n.NodeNum, &n.LongName, &n.ShortName, &n.HwModel, &fav, &mu); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		n.Favorite = fav != 0
		n.Muted = mu != 0
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iter: %w", err)
	}
	return out, nil
}

// SaveNodePrefs writes just the sticky UX preferences (favorite /
// muted) for a single node num. INSERT-on-conflict so this works even
// when the NodeInfo identity row isn't saved yet (user stars a still-
// ghost peer). The identity fields stay empty until SaveNode fills
// them in later; SaveNode's ON CONFLICT UPDATE explicitly does NOT
// touch favorite / muted so this pref never gets clobbered.
func (s *Sqlite) SaveNodePrefs(
	radioID string, nodeNum uint32, favorite, muted bool,
) error {
	if s == nil || s.db == nil {
		return nil
	}
	fav, mu := 0, 0
	if favorite {
		fav = 1
	}
	if muted {
		mu = 1
	}
	_, err := s.db.Exec(`
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

// GetSetting returns the persisted value for `key` or ("", false) if
// absent. radioID scopes the lookup: pass "" for global (meshx-client)
// prefs like /mute's "ding_muted"; pass a radio UUID for per-radio
// prefs.
func (s *Sqlite) GetSetting(radioID, key string) (string, bool) {
	if s == nil || s.db == nil {
		return "", false
	}
	var v string
	var err error
	if radioID == "" {
		err = s.db.QueryRow(
			`SELECT value FROM settings WHERE key = ? AND radio_id IS NULL`,
			key,
		).Scan(&v)
	} else {
		err = s.db.QueryRow(
			`SELECT value FROM settings WHERE key = ? AND radio_id = ?`,
			key, radioID,
		).Scan(&v)
	}
	if err != nil {
		return "", false
	}
	return v, true
}

// PutSetting writes `value` under `(key, radioID)`, upserting if the
// row already exists. Pass "" for radioID to write a global pref.
func (s *Sqlite) PutSetting(radioID, key, value string) error {
	if s == nil || s.db == nil {
		return nil
	}
	var rid any
	if radioID != "" {
		rid = radioID
	}
	_, err := s.db.Exec(`
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

// SaveBLEDevice inserts a newly-paired device (or updates its
// metadata on re-pair). Does NOT touch the favorite flag —
// SetBLEFavorite is the single entrypoint for that so we don't
// accidentally change which device is auto-connected.
func (s *Sqlite) SaveBLEDevice(d model.BLEDevice) error {
	if s == nil || s.db == nil {
		return nil
	}
	if d.UUID == "" {
		return fmt.Errorf("save ble device: uuid required")
	}
	_, err := s.db.Exec(`
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

// LoadBLEDevices returns every saved Bluetooth device ordered by
// favorite DESC, paired_at DESC so `meshx ble list` naturally
// surfaces the auto-connect target at the top.
func (s *Sqlite) LoadBLEDevices() ([]model.BLEDevice, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`
        SELECT uuid, long_name, short_name, hw_model, favorite
        FROM ble_devices
        ORDER BY favorite DESC, paired_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query ble devices: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []model.BLEDevice
	for rows.Next() {
		var (
			d   model.BLEDevice
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

// LookupBLEDevice finds a saved device by exact uuid OR by longname /
// shortname match (case-insensitive). Returns nil if no match, error
// only on DB failure.
func (s *Sqlite) LookupBLEDevice(needle string) (*model.BLEDevice, error) {
	devs, err := s.LoadBLEDevices()
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
		if strings.ToLower(d.UUID) == lowered {
			return &d, nil
		}
	}
	return nil, nil
}

// SetBLEFavorite marks exactly one device as the auto-connect
// fallback for bare `meshx`. Clears the flag on every other row in
// the same transaction so we never end up with two favorites. Empty
// uuid clears the flag entirely.
func (s *Sqlite) SetBLEFavorite(uuid string) error {
	if s == nil || s.db == nil {
		return nil
	}
	tx, err := s.db.Begin()
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

// ForgetBLEDevice removes a paired device from persistence. The caller
// is responsible for any OS-level unpair call. Missing uuids return
// nil (idempotent forget).
func (s *Sqlite) ForgetBLEDevice(uuid string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM ble_devices WHERE uuid = ?`, uuid)
	if err != nil {
		return fmt.Errorf("forget ble device: %w", err)
	}
	return nil
}

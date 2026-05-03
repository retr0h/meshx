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

// Package model holds the domain data types meshx persists, surfaces
// in its renderer, and (eventually) ships over the daemon's HTTP+SSE
// API. Types here are the canonical wire/disk shape — exported fields,
// JSON-friendly, no render concerns mixed in. The meshx renderer
// wraps these in its own row/cell types when it needs UI-only state
// like fade timers or pin status.
package model

import "time"

// MessageStatus is a typed enum for Message.Status. Strings on the
// wire / disk (the SQLite messages.status column stays TEXT for
// debuggability — `sqlite3 meshx.db "select status from messages"`
// reads as "ack" / "pending" / etc., not opaque ints) but typed in
// the model so a typo like StatusPendng becomes a compile error
// instead of a row that silently never matches a switch case. The
// boundary lives in the storage package's saveMessage/loadMessages
// using String() and ParseMessageStatus() for the conversion.
type MessageStatus int

const (
	// StatusOK is the zero value — used for inbound chat that doesn't
	// carry a delivery indicator. Persisted as the empty string "".
	StatusOK MessageStatus = iota
	// StatusAck — outbound message the radio confirmed delivery for
	// (Routing.NONE on a routing reply OR a real REPLY_APP echo).
	// Renders the trailing ✓ glyph.
	StatusAck
	// StatusPending — outbound message we've enqueued but haven't
	// heard back on. Renders the trailing … glyph; the stale-pending
	// sweep at startup flips any row stuck here past the cutoff to
	// StatusFail.
	StatusPending
	// StatusFail — outbound message the radio actively rejected OR
	// that aged out without an ack. Renders the trailing ✗ glyph;
	// the `R` nav-key resends.
	StatusFail
	// StatusSystem — locally generated row (`-!-` notice, /whois /
	// /info / /config blocks, etc.). NOT persisted to SQLite — the
	// storage layer's saveMessage early-returns on these since they
	// regenerate from live state on every launch.
	StatusSystem
	// StatusNotice — TTL-expiring `-!-` row from notices.go. Same
	// rendering as StatusSystem but with a fade + reap path; NOT
	// persisted for the same reason.
	StatusNotice
)

// String returns the wire/disk form. Kept stable so historic SQLite
// rows replay correctly across refactors (column stays TEXT, no
// migration needed).
func (s MessageStatus) String() string {
	switch s {
	case StatusAck:
		return "ack"
	case StatusPending:
		return "pending"
	case StatusFail:
		return "fail"
	case StatusSystem:
		return "system"
	case StatusNotice:
		return "notice"
	default:
		return ""
	}
}

// ParseMessageStatus is the inverse — used by the storage layer when
// reading the messages.status TEXT column off disk. Unknown values
// fall back to StatusOK rather than panicking; an invalid row is
// surface-level wrong (no glyph) but doesn't crash the UI.
func ParseMessageStatus(s string) MessageStatus {
	switch s {
	case "ack":
		return StatusAck
	case "pending":
		return StatusPending
	case "fail":
		return StatusFail
	case "system":
		return StatusSystem
	case "notice":
		return StatusNotice
	default:
		return StatusOK
	}
}

// Message is the persistence/wire shape of one chat row — every
// field maps to a SQLite messages column AND (eventually) a JSON
// field on the daemon's HTTP+SSE API. Render-only state (fade
// timers, pin status, group binding for visual stripe continuity)
// lives on the meshx package's row envelope, NOT here.
//
// Field naming follows Go convention (exported). The future huma
// daemon adds `json:"…"` tags to set the wire-format names and
// `doc:"…"` tags to seed the OpenAPI spec; not adding them now to
// keep this package zero-dependency.
type Message struct {
	// Time is the rendered timestamp ("09:47") shown in the chat
	// row. Captured at receive (live) or restored from
	// messages.time (replay).
	Time string

	// From is the sender callsign as resolved at receive time. For
	// peers we hadn't seen NodeInfo for yet this is the placeholder
	// "node 0x<hex>"; the renderer backfills the live callsign at
	// display time using FromNum + the in-memory NodeDB so a
	// mid-session NodeInfo arrival doesn't leave history stuck on
	// the placeholder.
	From string

	// Text is the message body, post-sanitization (CRLF trim,
	// non-printable rune drop). Multi-line bodies preserve internal
	// `\n`; edge whitespace is trimmed.
	Text string

	// Mine flags rows the local user composed (true) vs incoming
	// peer chat (false). Drives the right-aligned styling + ack
	// glyph rendering.
	Mine bool

	// Bang carries the leading verb for ham-bang messages — "!cq",
	// "!qth", "!cqr", etc. Empty for plain chat.
	Bang string

	// Status is the delivery-state enum — see MessageStatus.
	Status MessageStatus

	// Hops is the mesh hop count; 0 means direct (no relay) or
	// self.
	Hops int

	// SNR is the signal-to-noise ratio at receive ("-8.5"), empty
	// to hide.
	SNR string

	// PacketID is MeshPacket.id as seen on the wire. Zero for
	// in-memory-only entries (system lines, demo seeds, pre-feature
	// rows). Used as the unique key for ack matching + replay
	// dedup.
	PacketID uint32

	// ReplyID is Data.reply_id pointing at the message this one is
	// answering. Zero when the message isn't a reply.
	ReplyID uint32

	// FromNum is the Meshtastic node num of the sender, captured at
	// ingest. Persisted so the renderer can backfill the displayed
	// callsign from the in-memory NodeDB live (the From field is
	// only a snapshot at receive time — if NodeInfo arrives later
	// we'd otherwise be stuck showing "node 0xabc" forever). Zero
	// for "me" / system lines / demo seeds.
	FromNum uint32

	// SentAt is the absolute timestamp the message was received
	// (live) or originally persisted (replay). Populated from
	// messages.created_at on replay; set to time.Now() on live
	// inbound/outbound.
	SentAt time.Time

	// Corrupted is true when sanitization replaced bad bytes or
	// dropped non-printable runes from the body. Drives the ⚠
	// marker + dim italic styling on the row so the user knows
	// the content isn't trustworthy without throwing away the
	// salvageable printable bits. Recomputed on every replay so
	// a future sanitizer change automatically re-evaluates
	// historic rows.
	Corrupted bool
}

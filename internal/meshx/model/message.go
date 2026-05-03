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

// MarshalJSON emits the string form so HTTP API responses carry
// "ack" / "pending" / etc. rather than opaque integer values.
func (s MessageStatus) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// UnmarshalJSON is the inverse for client-side round-tripping.
func (s *MessageStatus) UnmarshalJSON(data []byte) error {
	v := string(data)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
	}
	*s = ParseMessageStatus(v)
	return nil
}

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
	Time      string        `json:"time"                doc:"display timestamp like '09:47'"`
	From      string        `json:"from"                doc:"sender callsign at receive time"`
	Text      string        `json:"text"                doc:"message body, post-sanitization"`
	Mine      bool          `json:"mine"                doc:"true when local user composed this row"`
	Bang      string        `json:"bang,omitempty"      doc:"leading verb for ham-bang messages"`
	Status    MessageStatus `json:"status"              doc:"ok | ack | pending | fail | system | notice"`
	Hops      int           `json:"hops"                doc:"mesh hop count; 0 = direct"`
	SNR       string        `json:"snr,omitempty"       doc:"signal-to-noise ratio at receive"`
	PacketID  uint32        `json:"packet_id"           doc:"MeshPacket.id; 0 for system / demo rows"`
	ReplyID   uint32        `json:"reply_id,omitempty"  doc:"PacketID this message answers"`
	FromNum   uint32        `json:"from_num"            doc:"sender node num"`
	ToNum     uint32        `json:"to_num"              doc:"addressee node num; 0xFFFFFFFF = broadcast"`
	SentAt    time.Time     `json:"sent_at"             doc:"absolute time of receive / persist"`
	Corrupted bool          `json:"corrupted,omitempty" doc:"sanitization replaced/dropped bytes"`
}

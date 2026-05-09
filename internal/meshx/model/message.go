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

// MessageStatus is a string-typed enum for Message.Status — the wire
// form (HTTP/SSE JSON), the persistence form (SQLite messages.status
// TEXT column), and the in-memory form are all the same string. Using
// a named string type rather than a raw string keeps typo'd values
// (StatusPendng, "pendnig") a compile error instead of a row that
// silently never matches a switch case, and lets OpenAPI codegen emit
// proper typed enums in every consumer language.
type MessageStatus string

// Each constant's value is the canonical wire/disk string. Stable —
// historic SQLite rows replay correctly across refactors. Order
// matches the lifecycle: empty ↔ inbound, ack/pending/fail are
// outbound, system/notice are TUI-local.
const (
	// StatusOK is the zero value — inbound chat without a delivery
	// indicator. Persists as the empty string in SQLite.
	StatusOK MessageStatus = ""
	// StatusAck — outbound message the radio confirmed delivery for
	// (Routing.NONE on a routing reply OR a real REPLY_APP echo).
	// Renders the trailing ✓ glyph.
	StatusAck MessageStatus = "ack"
	// StatusPending — outbound message we've enqueued but haven't
	// heard back on. Renders the trailing … glyph; the stale-pending
	// sweep at startup flips any row stuck here past the cutoff to
	// StatusFail.
	StatusPending MessageStatus = "pending"
	// StatusFail — outbound message the radio actively rejected OR
	// that aged out without an ack. Renders the trailing ✗ glyph;
	// the `R` nav-key resends.
	StatusFail MessageStatus = "fail"
	// StatusSystem — locally generated row (`-!-` notice, /whois /
	// /info / /config blocks, etc.). NOT persisted to SQLite — the
	// storage layer's saveMessage early-returns on these since they
	// regenerate from live state on every launch.
	StatusSystem MessageStatus = "system"
	// StatusNotice — TTL-expiring `-!-` row from notices.go. Same
	// rendering as StatusSystem but with a fade + reap path; NOT
	// persisted for the same reason.
	StatusNotice MessageStatus = "notice"
)

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
	Bang      string        `json:"-"`
	Status    MessageStatus `json:"status"              doc:"row delivery state. (empty) = inbound chat (no delivery indicator). 'ack' = local radio confirmed transmission — fires within ~1s of send for both broadcasts and DMs (the everyday 'did it leave my radio?' signal). 'pending' = queued locally, ack not yet received. 'fail' = radio rejected the send or the ack timed out. 'system' / 'notice' = synthetic rows the TUI generates for status banners; never persisted to SQLite. For per-peer mesh acks (DMs only), see the 'acks' field." enum:",ack,pending,fail,system,notice"`
	Hops      int           `json:"hops"                doc:"mesh hop count; 0 = direct"`
	SNR       string        `json:"snr,omitempty"       doc:"signal-to-noise ratio at receive"`
	PacketID  uint32        `json:"packet_id"           doc:"MeshPacket.id; 0 for system / demo rows"                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              format:"int64" minimum:"0"`
	ReplyID   uint32        `json:"reply_id,omitempty"  doc:"PacketID this message answers"                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        format:"int64" minimum:"0"`
	FromNum   uint32        `json:"from_num"            doc:"sender node num"                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      format:"int64" minimum:"0"`
	ToNum     uint32        `json:"to_num"              doc:"addressee node num; 0xFFFFFFFF = broadcast"                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           format:"int64" minimum:"0"`
	SentAt    time.Time     `json:"sent_at"             doc:"absolute time of receive / persist"`
	Corrupted bool          `json:"corrupted,omitempty" doc:"sanitization replaced/dropped bytes"`
}

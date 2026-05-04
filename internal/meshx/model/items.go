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

package model

import "time"

// ChannelItem is a Meshtastic channel slot. JSON tags shape the
// HTTP API response — generated clients deserialize into a struct
// with this exact wire shape. PSK is redacted with json:"-" so the
// raw key bytes never cross the network; the daemon keeps them in
// memory for /channel share round-tripping only.
type ChannelItem struct {
	Name    string `json:"name"    doc:"channel display name; empty for the unnamed primary"`
	Private bool   `json:"private" doc:"true when the channel uses a non-default PSK"`
	Unread  int    `json:"unread"  doc:"unread message count for the tab badge"`
	Index   int    `json:"index"   doc:"radio slot 0..7"`
	Role    string `json:"role"    doc:"PRIMARY | SECONDARY | DISABLED"`
	PSK     []byte `json:"-"`
	HasPSK  bool   `json:"has_psk" doc:"true when this slot is encrypted (computed from PSK presence)"`
}

// NodeItem is the canonical peer state — identity, favorites/mute
// prefs (persisted), and most recent telemetry (live). JSON tags
// shape the HTTP API response.
type NodeItem struct {
	Callsign    string    `json:"callsign"      doc:"long-form callsign as set on the radio"`
	ShortName   string    `json:"short_name"    doc:"4-char badge"`
	NodeNum     uint32    `json:"node_num"      doc:"radio identity, derived from MAC"                        format:"int64" minimum:"0"`
	Unresolved  bool      `json:"unresolved"    doc:"identity is a synthesized placeholder; no NodeInfo yet"`
	State       NodeState `json:"state"         doc:"online | offline | failed | muted | (empty for unknown)"`
	Fav         bool      `json:"fav"           doc:"user marked this peer with *"`
	LastHeard   string    `json:"last_heard"    doc:"display string ('2m', '14:02', '3h')"`
	LastHeardAt time.Time `json:"last_heard_at" doc:"absolute time of last decoded packet"`
	HeardRank   int       `json:"heard_rank"    doc:"sort stability — lower = more recent"`
	LastSNR     string    `json:"last_snr"      doc:"most recent SNR (dB), e.g. -8.5"`
	LastRSSI    string    `json:"last_rssi"     doc:"most recent RSSI (dBm), e.g. -92"`
	LastHops    int       `json:"last_hops"     doc:"hop count of last received packet (0 = direct)"`
	HwModel     string    `json:"hw_model"      doc:"e.g. T-Beam v1.1, HELTEC_V3"`
	Firmware    string    `json:"firmware"      doc:"firmware version string"`
}

// CurrentState derives the peer's effective state at call time so
// every read reflects real elapsed duration since LastHeardAt.
// Muted always wins (sticky preference). Heard within 15m = online.
func (n *NodeItem) CurrentState() NodeState {
	if n == nil {
		return StateUnknown
	}
	if n.State == StateMuted {
		return StateMuted
	}
	if n.LastHeardAt.IsZero() {
		return n.State
	}
	if time.Since(n.LastHeardAt) < 15*time.Minute {
		return StateOnline
	}
	return StateOffline
}

// MessageItem embeds Message (the wire/persisted shape) and adds
// runtime overlay fields. Group / ExpireAt / Pinned / PinnedRemaining
// / Style are TUI-only state — daemon and remote clients have no
// use for them, so all five are redacted from the wire shape via
// `json:"-"`. Style is `any` because the noticeStyle type lives in
// the TUI package; encoding it as a typed slot here would invert
// the package dependency.
type MessageItem struct {
	Message

	Acks            string        `json:"acks,omitempty"      doc:"child line ('↳ 3 acks — ...') under outgoing messages"`
	Group           uint64        `json:"-"`
	Style           any           `json:"-"`
	ExpireAt        *time.Time    `json:"-"`
	Pinned          bool          `json:"-"`
	PinnedRemaining time.Duration `json:"-"`
}

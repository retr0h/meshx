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

// Package session holds the canonical mesh-radio session state — the
// data the meshx Bubble Tea TUI consumes today and the future meshx
// serve daemon will expose over HTTP+SSE. Nothing in this package
// imports bubbletea / lipgloss / bubbles, so a headless caller can
// construct a Session, feed it pump events, and stand up its own
// surface (HTTP, RPC, TUI client) without dragging the renderer in.
//
// Scope (MR-3.5a): a focused first slice that pulls cleanly-typed
// primitives and the apply* handlers' transient bookkeeping
// (PendingPing / PendingTraceroute / PeerPosition / PeerEnvMetrics /
// ReconnectState) into this package. The richer collections
// (channels / messages / nodes) still live in the meshx package
// because their types embed renderer-only fields (notice styling, pin
// state); a follow-up MR-3.5b will detangle and move those plus the
// apply handlers themselves.
package session

import (
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// Session is the canonical per-radio session state. The TUI's model
// embeds *Session via Go field promotion so existing references to
// `m.MyNodeNum` etc. continue to work; a future daemon constructs a
// Session directly and pumps events into it.
type Session struct {
	// Identity + transport binding -----------------------------------

	// ConnectDest — the transport target string (`/dev/cu.usbmodem*`,
	// `host:port`, `ble:<uuid>`). Empty in demo mode.
	ConnectDest string
	// Connected flips true once mdl.ConfigComplete arrives, marking
	// the radio's NodeDB handshake as finished. Renderers gate
	// "connecting…" placeholders on this.
	Connected bool
	// MyNodeNum is populated by mdl.MyInfo. Used to mark messages as
	// `Mine` and to detect DMs (a future MR will route packets whose
	// ToNum matches into per-peer threads).
	MyNodeNum uint32
	// RadioID is the canonical Meshtastic identity ("0x" + hex of
	// my_node_num) once handshake reveals it; "pending:<transport>:
	// <addr>" beforehand. Every storage write is scoped by RadioID so
	// multi-radio history stays partitioned.
	RadioID string

	// Lookup indices --------------------------------------------------

	// NodesByNum maps radio node id → meshx-side nodes slice index.
	// Lets upsertNode + apply* paths do O(1) NodeDB lookups instead of
	// scanning the slice on every text/telemetry packet.
	NodesByNum map[uint32]int
	// MessagesByPacketID dedupes radio replays after a reconnect — when
	// the firmware re-drains its RAM queue after WantConfigId, an
	// already-applied packet upgrades the existing row instead of
	// appending a duplicate. PacketID==0 entries (system rows, demo
	// seeds) deliberately don't go in this map.
	MessagesByPacketID map[uint32]int

	// Active focus + filter -------------------------------------------

	// CurrentChannel is the channel name the user is composing into.
	// Outbound /command messages and bare-text composes target this
	// channel. Set by `/join`, `Alt+n`, and `/channel new`.
	CurrentChannel string

	// Radio metadata snapshot (zero = "not yet received") -------------
	// These mirror the FromRadio.Metadata + Config dumps the radio
	// sends during handshake. Renderers show "—" for zero values
	// rather than branching on a separate "have I seen this yet" flag.

	RadioFirmware    string
	RadioDeviceState uint32
	RadioHasWifi     bool
	RadioHasBT       bool
	RadioTxPower     int32
	RadioRegion      string
	RadioModemPreset string
	RadioRole        string

	// DeviceMetrics snapshot ------------------------------------------

	BatteryLevel   uint32
	BatteryVoltage float32
	ChannelUtil    float32
	AirUtilTx      float32
	HasTelemetry   bool

	// Own position ----------------------------------------------------

	MyLatitude  float64
	MyLongitude float64
	MyAltitude  int32
	// MyGrid is the Maidenhead grid square computed from My{Latitude,
	// Longitude} — surfaced in the status bar's "☖ <grid>" segment.
	MyGrid string

	// Per-peer position + environment metrics. Keyed by node num,
	// matching NodesByNum, so /qth and /env can look these up in O(1).
	PeerPositions map[uint32]PeerPosition
	PeerEnv       map[uint32]PeerEnvMetrics

	// In-flight request bookkeeping -----------------------------------

	// PendingTraceroute tracks an outbound /tr awaiting its
	// TRACEROUTE_APP reply (or timeout); same shape as PendingPing.
	PendingTraceroute *PendingTraceroute
	// PendingPing tracks an outbound /ping awaiting its REPLY_APP echo.
	PendingPing *PendingPing

	// NodeDB sync progress --------------------------------------------

	// SyncPendingGhosts snapshots unresolved-peer count at /sync time;
	// the next mdl.ConfigComplete diffs against the live count to
	// emit "sync complete — N peers identified". Zero = no /sync in
	// flight. -1 = pending with initial count zero (so we can tell
	// "started" from the start-of-day handshake's ConfigComplete).
	SyncPendingGhosts int
	// SyncReceived backs the live peer counter shown in the chanRow
	// flash during the NodeDB handshake. Bumps once per mdl.NodeInfo
	// between MyInfo and ConfigComplete; reset at both endpoints.
	SyncReceived int

	// Buzzer / external-notification config ---------------------------

	// RadioBuzzerEnabled is the derived "will the buzzer beep on a
	// text message" — true only when ExternalNotification.Enabled AND
	// AlertMessageBuzzer are set on the radio. Updated when
	// mdl.ModuleBuzzer lands during handshake.
	RadioBuzzerEnabled bool
	// RadioBuzzerKnown distinguishes "haven't heard the live config
	// yet" from "config is actually false."
	RadioBuzzerKnown bool
	// RadioBuzzerSnapshot retains the full ExternalNotification config
	// the radio reported, so commitConfigDraft can round-trip every
	// field other than Enabled / AlertMessageBuzzer when toggling.
	RadioBuzzerSnapshot mdl.ExternalNotification

	// Storage degradation flag ----------------------------------------

	// StorageAlerted gates the "persistence degraded" system line —
	// flips true on the FIRST save error so the messages pane gets
	// one notice; subsequent failures stay silent so a bad db doesn't
	// machine-gun the log. In-memory operation continues regardless.
	StorageAlerted bool

	// User preferences ------------------------------------------------

	// DingMuted gates the terminal BEL emit on inbound text. Toggled
	// by /mute, persisted under settings.ding_muted so the preference
	// rides across restarts.
	DingMuted bool
	// Ignored is the set of lowercase callsigns whose chat messages
	// get filtered from the messages pane. Toggled by /ignore +
	// /unignore. RAM-only by design — restart clears the set.
	Ignored map[string]bool

	// Reconnect banner ------------------------------------------------

	// Reconnect is non-nil while the pump is in its retry loop. Each
	// mdl.Reconnecting refreshes the struct; the noticeTick handler
	// uses it to repaint the flash with a live "in Ns" countdown.
	// Cleared the moment a radio frame proves we're back.
	Reconnect *ReconnectState
}

// New returns a fresh Session with all maps initialized. Callers
// (newModel for the TUI, future daemon Session() builder) populate
// the rest from configuration / fixture / handshake.
func New() *Session {
	return &Session{
		NodesByNum:         map[uint32]int{},
		MessagesByPacketID: map[uint32]int{},
		PeerPositions:      map[uint32]PeerPosition{},
		PeerEnv:            map[uint32]PeerEnvMetrics{},
		Ignored:            map[string]bool{},
	}
}

// PeerPosition is the most-recent position fix we've heard from a
// peer. Populated by the apply* path when a POSITION_APP packet
// arrives. Used by /qth + /nearby + /radar.
type PeerPosition struct {
	Latitude  float64
	Longitude float64
	Altitude  int32
	// Grid is the Maidenhead grid square derived from Lat/Lon — kept
	// pre-computed so renderers don't recompute on every frame.
	Grid string
	// At is when the peer's most recent position fix arrived, used to
	// age out "last seen 3h ago" labels on /nearby + /radar.
	At time.Time
}

// PeerEnvMetrics is the most-recent environmental telemetry we've
// heard from a peer (TELEMETRY_APP / EnvironmentMetrics). Used by
// /env <call>.
type PeerEnvMetrics struct {
	Temperature float32
	Humidity    float32
	Pressure    float32
	Gas         float32
	At          time.Time
}

// PendingTraceroute is the in-flight /tr request the session tracks
// from outbound enqueue until the matching TRACEROUTE_APP reply (or
// timeout) lands. PacketID is the MeshPacket.id we stamped on the
// request; matching against mdl.Traceroute.RequestID ignores foreign
// traceroutes that happen to be on the air. TargetCall + TargetNum
// are kept around so the reply card can render "traceroute <call> —
// N hops" without re-resolving. RequestedAt drives the round-trip-
// time readout in the result block + the timeout deadline.
type PendingTraceroute struct {
	PacketID    uint32
	TargetNum   uint32
	TargetCall  string
	RequestedAt time.Time
}

// PendingPing is the in-flight /ping request — same correlation
// shape as PendingTraceroute. PacketID matches the inbound REPLY_APP
// echo's RequestID; FromNum match against TargetNum is the fallback
// for older firmware that doesn't echo RequestID.
type PendingPing struct {
	PacketID    uint32
	TargetNum   uint32
	TargetCall  string
	RequestedAt time.Time
}

// ReconnectState backs the persistent "reconnecting · attempt N · in
// Ns" status banner the TUI shows while the pump is retrying a dead
// transport. The banner survives across noticeTickMsg ticks (each
// tick recomputes the live remaining-time portion without losing the
// original attempt count or error). ReadyAt is when the pump's
// backoff sleep is expected to end (the next dial attempt fires) —
// renderers display the diff against now.
type ReconnectState struct {
	// Initial is true for the very first connect at app startup —
	// the TUI renders this as "connecting" instead of "reconnecting"
	// and skips the attempt counter (no retries have happened yet).
	// Cleared the moment the pump emits its first mdl.Reconnecting or
	// the radio sends its first frame.
	Initial bool
	Attempt int
	Err     error
	ReadyAt time.Time
}

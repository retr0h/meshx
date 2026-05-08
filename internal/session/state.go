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

package session

import (
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// State is the canonical per-radio runtime state — collections,
// identity, telemetry, and in-flight request bookkeeping the apply*
// path mutates and the HTTP API surfaces. The TUI's model embeds
// *State via field promotion so existing m.MyNodeNum etc. resolve
// without an explicit accessor.
type State struct {
	Channels []mdl.ChannelItem
	Nodes    []mdl.NodeItem
	Messages []mdl.MessageItem

	ConnectDest string
	Connected   bool
	MyNodeNum   uint32
	// RadioID is the canonical Meshtastic identity ("0x" + hex of
	// my_node_num) post-handshake; "pending:<transport>:<addr>"
	// beforehand. Storage writes are scoped by RadioID so multi-radio
	// history stays partitioned.
	RadioID string

	// NodesByNum gives O(1) NodeDB lookups instead of scanning the
	// slice on every text/telemetry packet.
	NodesByNum map[uint32]int
	// MessagesByPacketID dedupes radio replays after a reconnect.
	// PacketID==0 entries (system rows) deliberately don't go in here.
	MessagesByPacketID map[uint32]int

	CurrentChannel string

	RadioFirmware    string
	RadioDeviceState uint32
	RadioHasWifi     bool
	RadioHasBT       bool
	RadioTxPower     int32
	RadioRegion      string
	RadioModemPreset string
	RadioRole        string

	BatteryLevel   uint32
	BatteryVoltage float32
	ChannelUtil    float32
	AirUtilTx      float32
	HasTelemetry   bool

	MyLatitude  float64
	MyLongitude float64
	MyAltitude  int32
	// MyGrid is the Maidenhead grid computed from My{Latitude,Longitude}.
	MyGrid string

	PeerPositions map[uint32]PeerPosition
	PeerEnv       map[uint32]PeerEnvMetrics

	PendingTraceroute *PendingTraceroute
	PendingPing       *PendingPing

	// SyncPendingGhosts snapshots unresolved-peer count at /sync
	// time; the next mdl.ConfigComplete diffs against the live count
	// to emit "sync complete — N peers identified". -1 = pending
	// with initial count zero so we can distinguish "started" from
	// the start-of-day handshake's ConfigComplete.
	SyncPendingGhosts int
	SyncReceived      int

	// RadioBuzzerEnabled is true only when both ExternalNotification.Enabled
	// and AlertMessageBuzzer are set on the radio.
	RadioBuzzerEnabled  bool
	RadioBuzzerKnown    bool
	RadioBuzzerSnapshot mdl.ExternalNotification

	// StorageAlerted gates the "persistence degraded" notice — flips
	// true on the first save error so subsequent failures stay silent.
	StorageAlerted bool

	DingMuted bool
	// Ignored is the lowercase-callsign filter set. RAM-only.
	Ignored map[string]bool

	// Reconnect is non-nil while the pump is in its retry loop.
	Reconnect *ReconnectState
}

// NewState returns an empty State with all maps initialized.
func NewState() *State {
	return &State{
		NodesByNum:         map[uint32]int{},
		MessagesByPacketID: map[uint32]int{},
		PeerPositions:      map[uint32]PeerPosition{},
		PeerEnv:            map[uint32]PeerEnvMetrics{},
		Ignored:            map[string]bool{},
	}
}

// PeerPosition is the most-recent position fix from a peer.
type PeerPosition struct {
	Latitude  float64
	Longitude float64
	Altitude  int32
	Grid      string
	At        time.Time
}

// PeerEnvMetrics is the most-recent environmental telemetry from a
// peer (TELEMETRY_APP / EnvironmentMetrics).
type PeerEnvMetrics struct {
	Temperature float32
	Humidity    float32
	Pressure    float32
	Gas         float32
	At          time.Time
}

// PendingTraceroute correlates an outbound /tr with its eventual
// TRACEROUTE_APP reply (or timeout). PacketID matches
// mdl.Traceroute.RequestID; TargetCall + TargetNum let the reply
// card render without re-resolving identity.
type PendingTraceroute struct {
	PacketID    uint32
	TargetNum   uint32
	TargetCall  string
	RequestedAt time.Time
}

// PendingPing correlates an outbound /ping with its REPLY_APP echo.
// FromNum match against TargetNum is the fallback for firmware that
// doesn't echo request_id.
type PendingPing struct {
	PacketID    uint32
	TargetNum   uint32
	TargetCall  string
	RequestedAt time.Time
}

// ReconnectState backs the persistent "reconnecting · attempt N · in
// Ns" banner. ReadyAt is when the pump's backoff sleep is expected
// to end. Initial is true for the first connect at startup so the
// renderer shows "connecting" instead of "reconnecting".
type ReconnectState struct {
	Initial bool
	Attempt int
	Err     error
	ReadyAt time.Time
}

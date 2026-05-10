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

// Event types — the canonical wire shapes the pump emits and that
// every consumer (the meshx renderer today, the future `meshx serve`
// daemon's SSE stream tomorrow) consumes. Each is a plain Go struct
// with exported fields so the daemon's huma JSON marshaller can
// serialize it unmodified.
//
// In-process consumers (meshx) receive these as tea.Msg — Bubble
// Tea's runtime dispatches by Go type identity, so every type below
// satisfies tea.Msg trivially. Out-of-process consumers will get
// them wrapped in an SSE envelope (kind + body) the daemon
// constructs at marshal time; the wrap is server concern, not
// model concern.

// Text is one TEXT_MESSAGE_APP arrival projected onto a partial
// Message body plus the routing context the consumer needs to
// dispatch into the right channel pane. Body's wire fields (FromNum,
// Text, SNR, Hops, Time/SentAt, PacketID, ReplyID) come from the
// packet; render fields (From, Mine, Status, Bang) are zero —
// the consumer enriches them. Channel/ToNum/RSSI stay off Body
// because Message has no columns for them today; channel is the
// storage row's foreign key, RSSI threads to the sender's nodeItem
// cache, ToNum is unused but preserved for future DM detection.
type Text struct {
	Channel int
	ToNum   uint32 `format:"int64" minimum:"0"`
	RSSI    string
	Body    Message
}

// MyInfo delivers MyNodeInfo — our own node number, used to resolve
// self vs peer and to claim the radio identity in storage.
type MyInfo struct {
	NodeNum uint32 `format:"int64" minimum:"0"`
}

// NodeInfo delivers one peer NodeInfo. Multiple arrive during the
// initial handshake, one per known peer in the NodeDB. Mid-session
// updates also flow through here (NODEINFO_APP packets). Persisted
// projection lives in CachedNode (NodeDB-cache subset of the wire
// fields).
type NodeInfo struct {
	NodeNum     uint32 `format:"int64" minimum:"0"`
	LongName    string
	ShortName   string
	HwModel     string
	SNR         string
	RSSI        string
	Hops        int
	LastHeardAt time.Time
}

// ChannelInfo delivers one channel slot — index, name, role, PSK
// presence, and the PSK bytes themselves. Empty Name + PRIMARY role
// is the default LongFast channel. PSK rides along so /channel
// share can round-trip the channel into a meshtastic:// URL without
// a second GetChannel roundtrip; the bytes stay RAM-only (no SQLite
// column).
//
// ID is ChannelSettings.id — a 32-bit collision-avoidance value the
// firmware uses to disambiguate same-named channels across meshes.
// /channel new mints a fresh random one; /channel add carries
// whatever the URL had.
type ChannelInfo struct {
	Index  int
	Name   string
	Role   ChannelRole
	ID     uint32
	HasPSK bool
	PSK    []byte
}

// Ping arrives when a REPLY_APP packet lands — the firmware echoes
// whatever payload it received back to the sender. We use it as a
// real ping: send a REPLY_APP packet, measure the round trip when
// the echo lands. RequestID correlates back to the consumer's
// pending ping; FromNum is the fallback when the firmware doesn't
// echo request_id.
type Ping struct {
	RequestID uint32 `format:"int64" minimum:"0"`
	FromNum   uint32 `format:"int64" minimum:"0"`
	Hops      int
	SNR       string
	RSSI      string
	At        time.Time
}

// ModuleBuzzer arrives when the radio sends a ModuleConfig envelope
// carrying its ExternalNotification submodule, either during the
// WantConfigId handshake or in response to an explicit AdminMessage
// GetModuleConfigRequest. "On" for the user means BOTH Enabled AND
// AlertMessageBuzzer are true. Snapshot is the FULL ExternalNotification
// the radio reported (modeled into a flat Go struct in
// model/buzzer.go — no proto leaks past pump), kept on the model so
// a buzzer-toggle save can flip those two fields while preserving
// everything else verbatim.
type ModuleBuzzer struct {
	Enabled            bool
	AlertMessageBuzzer bool
	Snapshot           ExternalNotification
}

// Traceroute arrives when a TRACEROUTE_APP reply lands —
// the result of an outbound /tr that issued a RouteDiscovery
// request. RequestID matches MeshPacket.Data.request_id; Route is
// the ordered list of intermediate node nums the discovery walked
// through (does NOT include source or dest per the firmware
// convention).
type Traceroute struct {
	RequestID uint32   `format:"int64" minimum:"0"`
	FromNum   uint32   `format:"int64" minimum:"0"`
	ToNum     uint32   `format:"int64" minimum:"0"`
	Route     []uint32 `format:"int64"`
	At        time.Time
}

// Routing is the Meshtastic delivery receipt — the radio echoes a
// Routing packet with request_id matching our outbound packetID
// once it finishes the send (or gives up). Reason == RoutingNone
// means the packet made it onto the mesh; anything else (Timeout,
// MaxRetransmit, …) is a delivery failure. OK is the convenience
// summary; ErrorName is the human-readable string for the flash
// message.
type Routing struct {
	RequestID uint32 `format:"int64" minimum:"0"`
	Reason    RoutingError
	ErrorName string
	OK        bool
	// FromNum is the NodeNum of the peer that sent this routing
	// reply. The local radio's own ack-of-send carries FromNum =
	// MyNodeNum (the radio identifies itself in the response); a
	// mesh-relay or destination ack carries FromNum = the relaying
	// or receiving peer. ApplyRouting uses this to aggregate per-
	// peer acks into MessageItem.Acks without double-counting our
	// own local ack.
	FromNum uint32 `format:"int64" minimum:"0"`
	// Hops is the hop count the routing reply traversed back to us
	// (HopStart - HopLimit on the wire). Zero for the local ack;
	// non-zero for replies that crossed at least one repeater.
	Hops int
	// At is the time the routing reply was applied locally — i.e.
	// when the ack landed in our process. Surfaces on the
	// MessageItem.Ackers entry so consumers can render "ack from X
	// 14s ago" without re-timing it themselves.
	At time.Time
}

// Metadata delivers firmware_version + hw identity details from the
// one-shot FromRadio.Metadata envelope.
type Metadata struct {
	FirmwareVersion string
	DeviceStateVer  uint32 `format:"int64" minimum:"0"`
	HasWifi         bool
	HasBluetooth    bool
}

// LoraConfig delivers the LoRa config — surfaces tx_power, region,
// and modem preset in the status bar.
type LoraConfig struct {
	TxPowerDBm  int32
	Region      Region
	ModemPreset ModemPreset
}

// DeviceMetrics delivers the latest DeviceMetrics telemetry packet
// — battery, voltage, channel utilization, TX airtime. Arrives
// periodically (default every 30 min) from the radio.
type DeviceMetrics struct {
	FromNodeNum  uint32  `format:"int64" minimum:"0"`
	BatteryLevel uint32  `format:"int64" minimum:"0"` // 0-100; >100 = powered
	Voltage      float32 // volts
	ChannelUtil  float32 // percent
	AirUtilTx    float32 // percent
}

// EnvMetrics delivers a peer's environmental telemetry —
// temperature / humidity / pressure / gas. Reported by nodes with
// an attached BME280 / SHT3x etc. sensor. Rare on most meshes.
type EnvMetrics struct {
	FromNodeNum uint32  `format:"int64" minimum:"0"`
	Temperature float32 // °C
	Humidity    float32 // %
	Pressure    float32 // hPa
	Gas         float32 // ohms
}

// DeviceConfig delivers Config.device — surfaces the node's role
// (Client / Router / Repeater / Tracker) in the status bar.
type DeviceConfig struct {
	Role DeviceRole
}

// Position delivers a node's position (from NodeInfo or a
// POSITION_APP packet). Applied to the sender's nodeItem and
// surfaced via /qth <call> or the top-bar grid square for self.
type Position struct {
	FromNodeNum uint32  `format:"int64" minimum:"0"`
	Latitude    float64 // degrees
	Longitude   float64 // degrees
	Altitude    int32   // meters
	At          time.Time
}

// ConfigComplete fires when the initial config dump finishes — the
// consumer leaves "connecting" state and shows live data populated
// with whatever the radio dumped.
type ConfigComplete struct{}

// Reconnecting fires after a transport error while the pump is in
// its retry loop. The consumer parks a persistent banner keyed off
// this so a transient BLE / serial drop reads as "we noticed,
// retrying every Ns" instead of a silent hang followed by a stale
// error.
type Reconnecting struct {
	Attempt int
	After   time.Duration
	Err     error
}

// Disconnected fires when the stream ends without error — the radio
// was unplugged or rebooted cleanly. The pump treats this as
// terminal (no retry) on the assumption the user pulled the plug
// deliberately.
type Disconnected struct{}

// TransportError carries a fatal transport error. With the
// indefinite-retry policy the pump never emits one of these on its
// own — kept on the type set for future use (transport errors the
// pump explicitly classifies as unrecoverable, e.g. the dest string
// can't be parsed). Named TransportError (not Error) so it doesn't
// shadow the builtin error interface in switch arms.
type TransportError struct {
	Err error
}

// MessageStatusUpdate fires when ApplyRouting flips an outbound
// row's terminal Status — the radio confirmed delivery (StatusAck)
// or gave up (StatusFail). Lets SSE consumers track per-packet
// outcome without polling GET /messages. Ackers replays the
// structured per-peer mesh-relay echoes accumulated for this row at
// the time the status flipped (DMs only — broadcasts don't
// generate Routing echoes from peers).
//
// Named "Update" rather than just "Status" because mdl.MessageStatus
// is already the string-typed enum on Message rows; the event type
// has to use a different name to avoid the collision.
type MessageStatusUpdate struct {
	PacketID uint32        `json:"packet_id"        format:"int64" minimum:"0"`
	Status   MessageStatus `json:"status"                                      enum:"ack,fail" doc:"new status — only the terminal transitions (ack | fail) fire this event"`
	Ackers   []Acker       `json:"ackers,omitempty"`
	At       time.Time     `json:"at"`
}

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

package server

import (
	"context"
	"time"
)

// handlers.go — concrete request → driver-state → response logic.
// One handler per registered route. Each handler is a thin
// translator: read the relevant slice off Driver.Sess, project
// it through a wire-safe DTO, return.
//
// PSK bytes never appear in any DTO emitted from this file — the
// model layer carries them for round-tripping share URLs, but the
// HTTP API redacts them. Future "/channels/{idx}/psk" endpoint
// (gated by auth) is the supervised exception.
//
// NOTE: Channel / Node / Message data lives on the TUI's *model*
// today — moving those collections onto the Driver's *Session* is
// the next-MR refactor (the "data wiring" follow-up). Until then,
// the list-* handlers return empty arrays so the API surface +
// OpenAPI spec are real and usable; clients see a contract that
// matches the eventual schema, not a placeholder shape.

// Channel is the wire-shape the API emits for a channel slot. Mirrors
// the future driver state with PSK redacted to a HasPSK bool —
// clients never see raw key bytes over the network.
type Channel struct {
	Index   int    `json:"index" doc:"radio slot 0..7 — stable across reconnects"`
	Name    string `json:"name" doc:"channel display name; empty for the unnamed primary"`
	Role    string `json:"role" doc:"PRIMARY | SECONDARY | DISABLED"`
	HasPSK  bool   `json:"has_psk" doc:"true when this slot is encrypted"`
	Private bool   `json:"private" doc:"true when the channel uses a non-default PSK"`
}

// Node is the wire-shape the API emits for a mesh peer. Combines
// the persisted NodeDB cache fields with the most recent telemetry
// the driver has seen.
type Node struct {
	NodeNum     uint32    `json:"node_num" doc:"radio identity, derived from MAC"`
	LongName    string    `json:"long_name" doc:"user callsign as set on the radio"`
	ShortName   string    `json:"short_name" doc:"4-char badge"`
	HwModel     string    `json:"hw_model" doc:"e.g. T-Beam v1.1, HELTEC_V3"`
	Firmware    string    `json:"firmware" doc:"firmware version string"`
	Favorite    bool      `json:"favorite" doc:"user marked this peer with *"`
	Muted       bool      `json:"muted" doc:"user muted this peer with m"`
	Unresolved  bool      `json:"unresolved" doc:"identity is a synthesized placeholder; no NodeInfo yet"`
	State       string    `json:"state" doc:"online | offline | failed | muted | (empty for unknown)"`
	LastSNR     string    `json:"last_snr" doc:"most recent SNR (dB), e.g. -8.5"`
	LastRSSI    string    `json:"last_rssi" doc:"most recent RSSI (dBm), e.g. -92"`
	LastHops    int       `json:"last_hops" doc:"hop count of last received packet (0 = direct)"`
	LastHeardAt time.Time `json:"last_heard_at" doc:"absolute time of last decoded packet from this peer"`
}

// Message is the wire-shape for a chat row.
type Message struct {
	PacketID  uint32    `json:"packet_id" doc:"MeshPacket.id; 0 for system / demo rows"`
	From      string    `json:"from" doc:"sender callsign at receive time"`
	FromNum   uint32    `json:"from_num" doc:"sender node num"`
	ToNum     uint32    `json:"to_num" doc:"addressee node num; 0xFFFFFFFF = broadcast"`
	Text      string    `json:"text" doc:"message body, post-sanitization"`
	Time      string    `json:"time" doc:"display timestamp like '09:47'"`
	SentAt    time.Time `json:"sent_at" doc:"absolute time of receive / persist"`
	Status    string    `json:"status" doc:"ok | ack | pending | fail | system | notice"`
	Mine      bool      `json:"mine" doc:"true when local user composed this row"`
	Bang      string    `json:"bang,omitempty" doc:"leading verb for ham-bang messages"`
	Hops      int       `json:"hops" doc:"mesh hop count; 0 = direct"`
	SNR       string    `json:"snr,omitempty" doc:"signal-to-noise ratio at receive"`
	ReplyID   uint32    `json:"reply_id,omitempty" doc:"PacketID this message answers"`
	Corrupted bool      `json:"corrupted,omitempty" doc:"sanitization replaced/dropped bytes"`
}

// SessionSnapshot is the wire-shape for the GET /session response —
// identity, connection status, telemetry highlights pulled directly
// from driver.Sess.
type SessionSnapshot struct {
	RadioID        string  `json:"radio_id"`
	MyNodeNum      uint32  `json:"my_node_num"`
	Connected      bool    `json:"connected"`
	CurrentChannel string  `json:"current_channel"`
	ConnectDest    string  `json:"connect_dest"`
	RadioFirmware  string  `json:"radio_firmware,omitempty"`
	RadioRegion    string  `json:"radio_region,omitempty"`
	RadioModem     string  `json:"radio_modem,omitempty"`
	RadioRole      string  `json:"radio_role,omitempty"`
	BatteryLevel   uint32  `json:"battery_level,omitempty"`
	BatteryVoltage float32 `json:"battery_voltage,omitempty"`
	ChannelUtil    float32 `json:"channel_util,omitempty"`
	AirUtilTx      float32 `json:"air_util_tx,omitempty"`
	MyLatitude     float64 `json:"my_latitude,omitempty"`
	MyLongitude    float64 `json:"my_longitude,omitempty"`
	MyAltitude     int32   `json:"my_altitude,omitempty"`
	MyGrid         string  `json:"my_grid,omitempty"`
}

// ----------------------------------------------------------------------
// Handlers
// ----------------------------------------------------------------------

type healthInput struct{}

type healthOutput struct {
	Body struct {
		Status string `json:"status" doc:"always 'ok' when the server is responsive"`
	}
}

func (s *Server) handleHealth(_ context.Context, _ *healthInput) (*healthOutput, error) {
	out := &healthOutput{}
	out.Body.Status = "ok"
	return out, nil
}

type sessionInput struct{}

type sessionOutput struct {
	Body SessionSnapshot
}

func (s *Server) handleSession(_ context.Context, _ *sessionInput) (*sessionOutput, error) {
	out := &sessionOutput{}
	if s.drv == nil || s.drv.Session() == nil {
		return out, nil
	}
	sess := s.drv.Session()
	out.Body = SessionSnapshot{
		RadioID:        sess.RadioID,
		MyNodeNum:      sess.MyNodeNum,
		Connected:      sess.Connected,
		CurrentChannel: sess.CurrentChannel,
		ConnectDest:    sess.ConnectDest,
		RadioFirmware:  sess.RadioFirmware,
		RadioRegion:    sess.RadioRegion,
		RadioModem:     sess.RadioModemPreset,
		RadioRole:      sess.RadioRole,
		BatteryLevel:   sess.BatteryLevel,
		BatteryVoltage: sess.BatteryVoltage,
		ChannelUtil:    sess.ChannelUtil,
		AirUtilTx:      sess.AirUtilTx,
		MyLatitude:     sess.MyLatitude,
		MyLongitude:    sess.MyLongitude,
		MyAltitude:     sess.MyAltitude,
		MyGrid:         sess.MyGrid,
	}
	return out, nil
}

type listChannelsInput struct{}

type listChannelsOutput struct {
	Body struct {
		Channels []Channel `json:"channels"`
	}
}

func (s *Server) handleListChannels(_ context.Context, _ *listChannelsInput) (*listChannelsOutput, error) {
	out := &listChannelsOutput{}
	// TODO(MR-data-wiring): channels currently live on the TUI's
	// model.channels slice — moving them onto driver.Sess (or a new
	// driver.State) is the next refactor. Until then, return [].
	out.Body.Channels = []Channel{}
	return out, nil
}

type listNodesInput struct{}

type listNodesOutput struct {
	Body struct {
		Nodes []Node `json:"nodes"`
	}
}

func (s *Server) handleListNodes(_ context.Context, _ *listNodesInput) (*listNodesOutput, error) {
	out := &listNodesOutput{}
	// TODO(MR-data-wiring): nodes currently live on the TUI's
	// model.nodes slice; same migration as channels.
	out.Body.Nodes = []Node{}
	return out, nil
}

type listMessagesInput struct {
	Limit int `query:"limit" doc:"max rows to return; 0 = no limit" default:"0"`
}

type listMessagesOutput struct {
	Body struct {
		Messages []Message `json:"messages"`
	}
}

func (s *Server) handleListMessages(_ context.Context, _ *listMessagesInput) (*listMessagesOutput, error) {
	out := &listMessagesOutput{}
	// TODO(MR-data-wiring): messages currently live on the TUI's
	// model.messages slice; same migration as channels.
	out.Body.Messages = []Message{}
	return out, nil
}

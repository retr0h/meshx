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
	"sort"

	"github.com/danielgtaylor/huma/v2"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/radio"
)

// Handlers translate HTTP requests into driver-state reads. Every
// per-radio handler resolves {radio_id} → Driver via the Registry,
// then projects through model types directly — no DTO duplication.
// JSON tags on model types shape the OpenAPI spec; generated client
// SDKs deserialize into structurally identical structs.

// RadioSummary is one entry in GET /radios.
type RadioSummary struct {
	RadioID     string `json:"radio_id"     doc:"canonical radio identifier — 0x<hex node_num> post-handshake, pending:<transport>:<addr> beforehand"`
	MyNodeNum   uint32 `json:"my_node_num"  doc:"radio's own node num; zero before MyInfo arrives"`
	Connected   bool   `json:"connected"    doc:"true once ConfigComplete has fired"`
	ConnectDest string `json:"connect_dest" doc:"transport target string (/dev/cu.*, host:port, ble:<uuid>)"`
}

// SessionSnapshot is the GET /radios/{radio_id} response.
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

// SendMessageRequest is the inbound POST body for sending text.
// ToNum=0 sends a broadcast on the named channel — every peer
// listening on that channel slot decodes it. ToNum=peer.NodeNum
// sends a unicast DM to that peer; the channel still selects the
// PSK keyset the firmware uses to encrypt the packet, so DMs
// remain scoped to peers who share that channel's key.
type SendMessageRequest struct {
	Channel int    `json:"channel"            doc:"target channel slot index (0..7); the current channel's slot is the default"`
	Text    string `json:"text"               doc:"message body"                                                                                                                                                                              minLength:"1"`
	ReplyID uint32 `json:"reply_id,omitempty" doc:"PacketID this message replies to"`
	ToNum   uint32 `json:"to_num,omitempty"   doc:"recipient NodeNum for a DM (peer-addressed unicast); 0 = broadcast on the channel. Look up the numeric NodeNum via GET /radios/{radio_id}/nodes — callsigns are not resolved server-side."               format:"int64" minimum:"0"`
}

// SendMessageResult echoes the allocated PacketID so clients can
// correlate with ack / fail events on the SSE stream. (Named
// "Result" not "Response" so the OpenAPI schema name doesn't collide
// with oapi-codegen's auto-generated <OpId>Response wrapper.)
type SendMessageResult struct {
	PacketID uint32 `json:"packet_id" doc:"MeshPacket.id allocated by the radio (zero if pump rejected the send)"`
	OK       bool   `json:"ok"        doc:"false when the pump's outbound buffer was full or no radio is attached"`
}

// resolveRadio looks up the Driver for an inbound {radio_id} and
// returns 404 when the radio isn't registered.
func (s *Server) resolveRadio(radioID string) (Driver, error) {
	if s == nil || s.radios == nil {
		return nil, huma.Error503ServiceUnavailable("registry uninitialized")
	}
	d, ok := s.radios.Get(radioID)
	if !ok {
		return nil, huma.Error404NotFound("radio not registered: " + radioID)
	}
	return d, nil
}

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

type listRadiosInput struct{}

type listRadiosOutput struct {
	Body struct {
		Radios []RadioSummary `json:"radios"`
	}
}

func (s *Server) handleListRadios(
	_ context.Context,
	_ *listRadiosInput,
) (*listRadiosOutput, error) {
	out := &listRadiosOutput{}
	out.Body.Radios = []RadioSummary{}
	if s.radios == nil {
		return out, nil
	}
	ids := s.radios.IDs()
	sort.Strings(ids)
	for _, id := range ids {
		d, ok := s.radios.Get(id)
		if !ok {
			continue
		}
		st := d.Snapshot()
		if st == nil {
			out.Body.Radios = append(out.Body.Radios, RadioSummary{RadioID: id})
			continue
		}
		out.Body.Radios = append(out.Body.Radios, RadioSummary{
			RadioID:     id,
			MyNodeNum:   st.MyNodeNum,
			Connected:   st.Connected,
			ConnectDest: st.ConnectDest,
		})
	}
	return out, nil
}

type getRadioInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
}

type getRadioOutput struct {
	Body SessionSnapshot
}

func (s *Server) handleGetRadio(_ context.Context, in *getRadioInput) (*getRadioOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	out := &getRadioOutput{}
	st := d.Snapshot()
	if st == nil {
		return out, nil
	}
	out.Body = SessionSnapshot{
		RadioID:        st.RadioID,
		MyNodeNum:      st.MyNodeNum,
		Connected:      st.Connected,
		CurrentChannel: st.CurrentChannel,
		ConnectDest:    st.ConnectDest,
		RadioFirmware:  st.RadioFirmware,
		RadioRegion:    st.RadioRegion,
		RadioModem:     st.RadioModemPreset,
		RadioRole:      st.RadioRole,
		BatteryLevel:   st.BatteryLevel,
		BatteryVoltage: st.BatteryVoltage,
		ChannelUtil:    st.ChannelUtil,
		AirUtilTx:      st.AirUtilTx,
		MyLatitude:     st.MyLatitude,
		MyLongitude:    st.MyLongitude,
		MyAltitude:     st.MyAltitude,
		MyGrid:         st.MyGrid,
	}
	return out, nil
}

type listChannelsInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
}

type listChannelsOutput struct {
	Body struct {
		Channels []mdl.ChannelItem `json:"channels"`
	}
}

func (s *Server) handleListChannels(
	_ context.Context,
	in *listChannelsInput,
) (*listChannelsOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	out := &listChannelsOutput{}
	out.Body.Channels = []mdl.ChannelItem{}
	st := d.Snapshot()
	if st == nil {
		return out, nil
	}
	for _, c := range st.Channels {
		c.HasPSK = len(c.PSK) > 0
		c.PSK = nil
		out.Body.Channels = append(out.Body.Channels, c)
	}
	return out, nil
}

type listNodesInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
}

type listNodesOutput struct {
	Body struct {
		Nodes []mdl.NodeItem `json:"nodes"`
	}
}

func (s *Server) handleListNodes(_ context.Context, in *listNodesInput) (*listNodesOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	out := &listNodesOutput{}
	out.Body.Nodes = []mdl.NodeItem{}
	st := d.Snapshot()
	if st == nil {
		return out, nil
	}
	for i := range st.Nodes {
		n := st.Nodes[i]
		n.State = n.CurrentState()
		out.Body.Nodes = append(out.Body.Nodes, n)
	}
	return out, nil
}

type listMessagesInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	Limit   int    `                doc:"max rows to return; 0 = no limit"                                                                                                                                   query:"limit" default:"0"`
	DM      string `                doc:"DM filter; '' = all rows, '1' = peer-addressed DMs (either direction), 'mine' = DMs to/from my_node_num. Use to skip channel-firehose filtering on the client side" query:"dm"                enum:",1,mine"`
}

type listMessagesOutput struct {
	Body struct {
		Messages []mdl.MessageItem `json:"messages"`
	}
}

// matchesDMFilter reports whether a row passes the ?dm= query
// filter. "" = no filter (every row). "1" = any peer-addressed DM
// (ToNum is a specific peer, not broadcast / unset). "mine" = DMs
// I'm a participant in (incoming-to-me OR outgoing-by-me to a
// specific peer). Other values are rejected by the input enum so
// this falls through to a permissive default.
func matchesDMFilter(m mdl.MessageItem, mode string, myNodeNum uint32) bool {
	switch mode {
	case "":
		return true
	case "1":
		return m.ToNum != 0 && m.ToNum != mdl.BroadcastNum
	case "mine":
		if myNodeNum != 0 && m.ToNum == myNodeNum {
			return true
		}
		return m.Mine && m.ToNum != 0 && m.ToNum != mdl.BroadcastNum
	default:
		return true
	}
}

func (s *Server) handleListMessages(
	_ context.Context,
	in *listMessagesInput,
) (*listMessagesOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	out := &listMessagesOutput{}
	out.Body.Messages = []mdl.MessageItem{}
	st := d.Snapshot()
	if st == nil {
		return out, nil
	}
	msgs := st.Messages
	if in.DM != "" {
		filtered := make([]mdl.MessageItem, 0, len(msgs))
		for _, m := range msgs {
			if matchesDMFilter(m, in.DM, st.MyNodeNum) {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}
	if in.Limit > 0 && len(msgs) > in.Limit {
		msgs = msgs[len(msgs)-in.Limit:]
	}
	out.Body.Messages = append(out.Body.Messages, msgs...)
	return out, nil
}

type sendMessageInput struct {
	RadioID        string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	IdempotencyKey string `                doc:"opaque request key (typically a UUID) for retry dedupe; identical key on the same radio within 60s returns the original result without re-dispatching to the radio" header:"Idempotency-Key"`
	Body           SendMessageRequest
}

type sendMessageOutput struct {
	Body SendMessageResult
}

func (s *Server) handleSendMessage(
	_ context.Context,
	in *sendMessageInput,
) (*sendMessageOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	// Idempotency-Key dedupe — return the original result for retries
	// of the same logical send within the TTL window so a network
	// blip + client retry doesn't double-broadcast on RF. Key is
	// per-radio so independent radios don't share a key namespace.
	if cached, ok := s.idempotency.Get(in.RadioID, in.IdempotencyKey); ok {
		out := &sendMessageOutput{}
		out.Body = cached
		return out, nil
	}
	pid, ok := d.Send(mdl.SendText{
		Channel: in.Body.Channel,
		Text:    in.Body.Text,
		ReplyID: in.Body.ReplyID,
		ToNum:   in.Body.ToNum,
	})
	// Record the outbound row in State.Messages + persist + publish
	// even when ok=false (demo mode / pump disconnected): the row
	// still belongs in the user's chat log as a pending entry, and
	// the Routing handler flips it to Fail when no ack arrives.
	d.RecordOutbound(radio.RecordOutboundOptions{
		Channel:  in.Body.Channel,
		Text:     in.Body.Text,
		ReplyID:  in.Body.ReplyID,
		PacketID: pid,
		ToNum:    in.Body.ToNum,
	})
	out := &sendMessageOutput{}
	out.Body = SendMessageResult{PacketID: pid, OK: ok}
	s.idempotency.Put(in.RadioID, in.IdempotencyKey, out.Body)
	return out, nil
}

// eventsInput is the SSE registration's typed input shape — Huma's
// sse.Register reads the path tag to populate the spec's parameters
// block. There's no body / response shape here; sse.Register provides
// the streaming response itself.
//
// LastEventID and Since are the two surfaces clients use to resume
// after a reconnect. Browser EventSource auto-emits Last-Event-ID
// from the most recent SSE id: line; curl / hand-written clients
// generally find ?since= more ergonomic. The handler accepts either
// — see resolveEventCursor for the priority rules.
type eventsInput struct {
	RadioID     string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	LastEventID string `                doc:"resumption cursor auto-emitted by EventSource clients on reconnect; the daemon replays buffered events with id > LastEventID" header:"Last-Event-ID"`
	Since       string `                doc:"explicit resumption cursor (decimal event_id); takes priority over Last-Event-ID. Use 0 to replay the entire ring buffer"                            query:"since"`
}

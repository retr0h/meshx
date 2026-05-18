// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

package radio

// apply.go is the canonical inbound-event handler — the single place
// State mutates in response to a translated FromRadio event. Both
// the TUI (local mode) and the daemon's pump-driven sink (server
// mode + remote mode TUI consuming SSE) call into these methods so
// State stays consistent regardless of who's driving.
//
// Design split:
//   - Session.ApplyX  — STATE truth: collection mutations, persistence,
//                      Publish fan-out. Idempotent enough for replay.
//   - TUI react*     — TUI-only consequences: flash text, scrollback
//                      cursor follow-tail, terminal ding, systemBlock /
//                      systemLine emissions, modeFlash transitions.
//
// Methods return a small result struct when the TUI needs to know
// what changed (ghost-peer upgrade, message-was-tail, status flip)
// so the react layer can decide its presentation work without
// re-deriving from State.

import (
	"fmt"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// ApplyMyInfoResult tells the caller whether this MyInfo claimed a
// new canonical RadioID — the daemon uses this to rekey its Registry
// (so /radios/0xNNNNNNNN works after handshake).
type ApplyMyInfoResult struct {
	OldRadioID string
	NewRadioID string
	Changed    bool
}

// ApplyMyInfo locks in the radio's identity. First MyInfo of a session
// transitions State.RadioID from "pending:..." (or a stale cache key)
// to "0x" + hex(my_node_num) — the same canonical form the
// Meshtastic phone app + Python CLI use. ClaimRadioIdentity rewrites
// the storage row and every FK column atomically; on storage failure
// we keep the old RadioID so apply* paths don't crash later trying
// to scope by an empty key.
func (s *Session) ApplyMyInfo(msg mdl.MyInfo) ApplyMyInfoResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.Publish(Event{Kind: EventMyInfo, Data: msg})
	res := ApplyMyInfoResult{OldRadioID: s.State.RadioID}
	s.State.MyNodeNum = msg.NodeNum
	if s.store != nil {
		if newID, err := s.store.ClaimRadioIdentity(s.State.RadioID, msg.NodeNum); err == nil {
			s.State.RadioID = newID
		}
	}
	res.NewRadioID = s.State.RadioID
	res.Changed = res.OldRadioID != res.NewRadioID
	return res
}

// ApplyMetadata stamps firmware + hw flags from the radio's one-shot
// Metadata envelope. Surfaces in the status bar.
func (s *Session) ApplyMetadata(msg mdl.Metadata) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.Publish(Event{Kind: EventMetadata, Data: msg})
	s.State.RadioFirmware = msg.FirmwareVersion
	s.State.RadioDeviceState = msg.DeviceStateVer
	s.State.RadioHasWifi = msg.HasWifi
	s.State.RadioHasBT = msg.HasBluetooth
}

// ApplyLoraConfig stamps the radio's tx_power, region, and modem
// preset.
func (s *Session) ApplyLoraConfig(msg mdl.LoraConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.Publish(Event{Kind: EventLoRaConfig, Data: msg})
	s.State.RadioTxPower = msg.TxPowerDBm
	s.State.RadioRegion = string(msg.Region)
	s.State.RadioModemPreset = string(msg.ModemPreset)
}

// ApplyDeviceConfig stamps the radio's role (Client / Router /
// Repeater / Tracker).
func (s *Session) ApplyDeviceConfig(msg mdl.DeviceConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.Publish(Event{Kind: EventDeviceConfig, Data: msg})
	s.State.RadioRole = string(msg.Role)
}

// ApplyDeviceMetrics applies our-own-radio battery / channel / TX
// telemetry. Peer metrics are ignored here for now (peer metrics
// land in PeerEnv via ApplyEnvMetrics).
func (s *Session) ApplyDeviceMetrics(msg mdl.DeviceMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.Publish(Event{Kind: EventDeviceMetrics, Data: msg})
	if msg.FromNodeNum == s.State.MyNodeNum || msg.FromNodeNum == 0 {
		s.State.BatteryLevel = msg.BatteryLevel
		s.State.BatteryVoltage = msg.Voltage
		s.State.ChannelUtil = msg.ChannelUtil
		s.State.AirUtilTx = msg.AirUtilTx
		s.State.HasTelemetry = true
	}
}

// ApplyEnvMetrics records a peer's environmental telemetry —
// temperature / humidity / pressure / gas. Indexed by FromNodeNum
// so /env or per-peer dashboards can render the freshest reading.
func (s *Session) ApplyEnvMetrics(msg mdl.EnvMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.Publish(Event{Kind: EventEnvMetrics, Data: msg})
	if s.State.PeerEnv == nil {
		s.State.PeerEnv = make(map[uint32]PeerEnvMetrics)
	}
	s.State.PeerEnv[msg.FromNodeNum] = PeerEnvMetrics{
		Temperature: msg.Temperature,
		Humidity:    msg.Humidity,
		Pressure:    msg.Pressure,
		Gas:         msg.Gas,
		At:          time.Now(),
	}
}

// ApplyPositionResult reports whether the position belonged to our
// own node — the TUI uses this to refresh the top-bar Maidenhead
// grid when self-position changes.
type ApplyPositionResult struct {
	IsSelf bool
}

// ApplyPosition mutates PeerPositions and (for self) MyLatitude /
// MyLongitude / MyAltitude.
func (s *Session) ApplyPosition(msg mdl.Position, grid string) ApplyPositionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.PublishPosition(msg)
	if s.State.PeerPositions == nil {
		s.State.PeerPositions = make(map[uint32]PeerPosition)
	}
	s.State.PeerPositions[msg.FromNodeNum] = PeerPosition{
		Latitude:  msg.Latitude,
		Longitude: msg.Longitude,
		Altitude:  msg.Altitude,
		Grid:      grid,
		At:        msg.At,
	}
	res := ApplyPositionResult{IsSelf: msg.FromNodeNum == s.State.MyNodeNum}
	if res.IsSelf {
		s.State.MyLatitude = msg.Latitude
		s.State.MyLongitude = msg.Longitude
		s.State.MyAltitude = msg.Altitude
		s.State.MyGrid = grid
	}
	return res
}

// ApplyChannelInfo sets or replaces a channel slot, growing the
// slice if needed. DISABLED slots are kept (with role="DISABLED")
// so a future /channel new can find the first free slot. PSK rides
// along (RAM-only — never persisted) so /channel share can build a
// meshtastic:// URL without a second roundtrip. Preserves unread
// counts across re-apply. Publishes after mutation so SSE
// subscribers see the event in lockstep with State.
func (s *Session) ApplyChannelInfo(msg mdl.ChannelInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.PublishChannelInfo(msg)
	const roleDisabled = "DISABLED"
	for len(s.State.Channels) <= msg.Index {
		s.State.Channels = append(s.State.Channels, mdl.ChannelItem{Role: roleDisabled})
	}
	if string(msg.Role) == roleDisabled {
		prevUnread := s.State.Channels[msg.Index].Unread
		s.State.Channels[msg.Index] = mdl.ChannelItem{
			Index:  msg.Index,
			Role:   roleDisabled,
			Unread: prevUnread,
		}
		return
	}
	name := msg.Name
	switch {
	case name == "":
		name = "#default"
	case msg.HasPSK:
		name = "*" + msg.Name + "*"
	default:
		name = "#" + msg.Name
	}
	c := mdl.ChannelItem{
		Name:    name,
		Private: msg.HasPSK,
		Index:   msg.Index,
		Role:    string(msg.Role),
		PSK:     msg.PSK,
	}
	c.Unread = s.State.Channels[msg.Index].Unread
	s.State.Channels[msg.Index] = c
	if s.State.CurrentChannel == "" {
		s.State.CurrentChannel = name
	}
}

// ApplyNodeInfoResult reports observable changes for the TUI's
// presentation layer. GhostUpgrade is true when a previously-
// unresolved peer just got real User info.
type ApplyNodeInfoResult struct {
	GhostUpgrade bool
	PrevCallsign string
	NewCallsign  string
}

// ApplyNodeInfo upserts a peer NodeInfo. Synthesizes firmware-default
// callsigns when the wire payload is content-free (peer the radio has
// only forwarded for). Preserves user prefs (Fav) and the freshest
// LastHeardAt across updates. Publishes after mutation.
func (s *Session) ApplyNodeInfo(msg mdl.NodeInfo) ApplyNodeInfoResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.PublishNodeInfo(msg)
	unresolved := false
	if msg.LongName == "" && msg.ShortName == "" {
		long, short := mdl.DefaultCallsign(msg.NodeNum)
		msg.LongName = long
		msg.ShortName = short
		unresolved = true
	}
	callsign := msg.LongName
	if callsign == "" {
		callsign = msg.ShortName
	}
	state := mdl.StateOffline
	if !msg.LastHeardAt.IsZero() && time.Since(msg.LastHeardAt) < 15*time.Minute {
		state = mdl.StateOnline
	}
	lastHeard := "never"
	if !msg.LastHeardAt.IsZero() {
		lastHeard = humanDuration(time.Since(msg.LastHeardAt))
	}
	item := mdl.NodeItem{
		Callsign:    callsign,
		ShortName:   msg.ShortName,
		NodeNum:     msg.NodeNum,
		Unresolved:  unresolved,
		State:       state,
		LastHeard:   lastHeard,
		LastHeardAt: msg.LastHeardAt,
		HeardRank:   int(time.Since(msg.LastHeardAt).Seconds()),
		LastSNR:     msg.SNR,
		LastRSSI:    msg.RSSI,
		LastHops:    msg.Hops,
		HwModel:     msg.HwModel,
	}
	if s.store != nil {
		s.storeError(s.store.SaveNode(s.State.RadioID, mdl.CachedNode{
			NodeNum:   msg.NodeNum,
			LongName:  msg.LongName,
			ShortName: msg.ShortName,
			HwModel:   msg.HwModel,
		}))
	}
	res := ApplyNodeInfoResult{}
	if idx, ok := s.State.NodesByNum[msg.NodeNum]; ok {
		item.Fav = s.State.Nodes[idx].Fav
		if s.State.Nodes[idx].LastHeardAt.After(item.LastHeardAt) {
			item.LastHeardAt = s.State.Nodes[idx].LastHeardAt
		}
		wasUnresolved := s.State.Nodes[idx].Unresolved
		prevCallsign := s.State.Nodes[idx].Callsign
		s.State.Nodes[idx] = item
		if wasUnresolved && !item.Unresolved && prevCallsign != item.Callsign {
			res.GhostUpgrade = true
			res.PrevCallsign = prevCallsign
			res.NewCallsign = item.Callsign
		}
		return res
	}
	s.State.NodesByNum[msg.NodeNum] = len(s.State.Nodes)
	s.State.Nodes = append(s.State.Nodes, item)
	return res
}

// ApplyPing records a REPLY_APP echo's telemetry against the
// matching peer's NodeItem (LastHops / LastSNR / LastRSSI /
// LastHeardAt) and publishes the event so SSE consumers see the
// ping land. The TUI side correlates the ping against its
// PendingPing slot and renders the systemBlock — that lives in
// the TUI because the report shape is presentation, not state.
//
// The state mutation here mirrors the per-Node telemetry refresh
// that happens on every text packet, so a remote client doing
// /whois on a peer that just answered a /ping reads the freshest
// signal numbers without needing the TUI's correlation logic.
func (s *Session) ApplyPing(msg mdl.Ping) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.PublishPing(msg)
	if idx, ok := s.State.NodesByNum[msg.FromNum]; ok && idx < len(s.State.Nodes) {
		s.State.Nodes[idx].LastHops = msg.Hops
		if msg.SNR != "" {
			s.State.Nodes[idx].LastSNR = msg.SNR
		}
		if msg.RSSI != "" {
			s.State.Nodes[idx].LastRSSI = msg.RSSI
		}
		s.State.Nodes[idx].LastHeardAt = time.Now()
	}
}

// ApplyTraceroute records a TRACEROUTE_APP reply's hop count on
// the source peer's NodeItem and publishes the event. Path
// rendering (systemBlock with the resolved hop chain) lives in
// the TUI — it's presentation, and the daemon doesn't know which
// callsigns the consumer wants to see.
func (s *Session) ApplyTraceroute(msg mdl.Traceroute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.PublishTraceroute(msg)
	if idx, ok := s.State.NodesByNum[msg.FromNum]; ok && idx < len(s.State.Nodes) {
		s.State.Nodes[idx].LastHops = len(msg.Route)
	}
}

// RecordOutboundOptions describes a locally-originated text packet
// the caller has just handed to the pump (or POSTed to the daemon).
// Fields mirror mdl.SendText with the Session-allocated PacketID
// included so the row's MessagesByPacketID index lines up with the
// later Routing receipt.
type RecordOutboundOptions struct {
	Channel  int
	Text     string
	Bang     string // empty for plain chat; "/cq", "/73", etc. for ham bangs
	ReplyID  uint32
	PacketID uint32 // returned by Send; 0 in demo mode
	ToNum    uint32 // 0 for broadcast
}

// RecordOutbound appends a "mine" row for a just-sent text packet
// into State.Messages, persists it, indexes by PacketID, and
// publishes the synthesized mdl.Text event so SSE consumers (remote
// TUIs in particular) see their own outbound message reflected in
// the chat log immediately rather than waiting for the radio to
// echo a packet that's never coming.
//
// State mutation is single-source: TUI's sendPlainReply /
// sendBangReply and the daemon's handleSendMessage both call this.
// Without it, remote-mode TUIs would type a message, see it
// disappear, and never know the daemon actually accepted it.
func (s *Session) RecordOutbound(opts RecordOutboundOptions) ApplyTextResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordOutbound(opts)
}

// recordOutbound is the lock-free core of RecordOutbound. Caller must
// hold s.mu. Split out so SendMessage (which already holds the lock)
// can call this without re-entering the mutex.
func (s *Session) recordOutbound(opts RecordOutboundOptions) ApplyTextResult {
	channelName := s.State.CurrentChannel
	if opts.Channel >= 0 && opts.Channel < len(s.State.Channels) {
		if name := s.State.Channels[opts.Channel].Name; name != "" {
			channelName = name
		}
	}
	now := time.Now()
	item := mdl.MessageItem{Message: mdl.Message{
		Time:     timeHHMM(now),
		From:     "me",
		Mine:     true,
		Bang:     opts.Bang,
		Text:     opts.Text,
		Status:   mdl.StatusPending,
		PacketID: opts.PacketID,
		ReplyID:  opts.ReplyID,
		FromNum:  s.State.MyNodeNum,
		ToNum:    opts.ToNum,
		SentAt:   now,
	}}
	s.State.Messages = append(s.State.Messages, item)
	idx := len(s.State.Messages) - 1
	if opts.PacketID != 0 {
		s.State.MessagesByPacketID[opts.PacketID] = idx
	}
	if s.store != nil {
		s.storeError(s.store.SaveMessage(s.State.RadioID, channelName, item.Message))
	}
	// Publish a synthesized mdl.Text so SSE subscribers get a
	// live "new outbound message" event in lockstep with the
	// State.Messages append. Mirrors the inbound ApplyText path.
	s.Publish(Event{Kind: EventText, Data: mdl.Text{
		Body:    item.Message,
		Channel: opts.Channel,
		ToNum:   opts.ToNum,
	}})
	return ApplyTextResult{Index: idx, FromMine: true}
}

// timeHHMM is the HH:MM formatter Message.Time uses; lifted here
// so RecordOutbound doesn't reach into the TUI package for the
// format. Standard Go reference layout (15 = 24-hour hour, 04 =
// zero-padded minute).
func timeHHMM(t time.Time) string {
	return t.Format("15:04")
}

// ApplyReconnecting reflects the pump's retry banner onto State and
// publishes the event so SSE subscribers (remote TUIs, monitoring
// dashboards) see the dialing/retry status in lockstep with the
// daemon's own State. Without publishing, a remote TUI watching the
// daemon would only learn the radio dropped when text packets stop
// arriving.
func (s *Session) ApplyReconnecting(ev mdl.Reconnecting) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.Publish(Event{Kind: EventReconnecting, Data: ev})
	s.State.Reconnect = &ReconnectState{
		Attempt: ev.Attempt,
		ReadyAt: time.Now().Add(ev.After),
		Err:     ev.Err,
	}
}

// ApplyDisconnected flips Connected = false and publishes. The
// reconnect banner is left intact; ApplyReconnecting owns its
// lifecycle (clear happens on the next ApplyConfigComplete).
func (s *Session) ApplyDisconnected(ev mdl.Disconnected) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.Publish(Event{Kind: EventDisconnected, Data: ev})
	s.State.Connected = false
}

// ApplyConfigComplete marks the handshake as complete — Connected
// flips true, the reconnect banner clears. Returns whether this was
// the first ConfigComplete (TUI uses it to decide whether to emit
// the "sync complete" system line).
func (s *Session) ApplyConfigComplete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	wasDisconnected := !s.State.Connected
	s.State.Connected = true
	s.State.Reconnect = nil
	s.State.SyncReceived = 0
	s.Publish(Event{Kind: EventConfigComplete, Data: mdl.ConfigComplete{}})
	return wasDisconnected
}

// humanDuration is a tiny formatter for the LastHeard display column
// — minutes for the last hour, hours for the last day, days beyond.
// Falls back to ":mm" wall-clock-ish shape to keep the column narrow.
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

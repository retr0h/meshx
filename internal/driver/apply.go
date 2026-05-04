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

package driver

// apply.go is the canonical inbound-event handler — the single place
// State mutates in response to a translated FromRadio event. Both
// the TUI (local mode) and the daemon's pump-driven sink (server
// mode + remote mode TUI consuming SSE) call into these methods so
// State stays consistent regardless of who's driving.
//
// Design split:
//   - Driver.ApplyX  — STATE truth: collection mutations, persistence,
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
func (d *Driver) ApplyMyInfo(msg mdl.MyInfo) ApplyMyInfoResult {
	defer d.Publish(Event{Kind: EventMyInfo, Data: msg})
	res := ApplyMyInfoResult{OldRadioID: d.State.RadioID}
	d.State.MyNodeNum = msg.NodeNum
	if d.Store != nil {
		if newID, err := d.Store.ClaimRadioIdentity(d.State.RadioID, msg.NodeNum); err == nil {
			d.State.RadioID = newID
		}
	}
	res.NewRadioID = d.State.RadioID
	res.Changed = res.OldRadioID != res.NewRadioID
	return res
}

// ApplyMetadata stamps firmware + hw flags from the radio's one-shot
// Metadata envelope. Surfaces in the status bar.
func (d *Driver) ApplyMetadata(msg mdl.Metadata) {
	defer d.Publish(Event{Kind: EventMetadata, Data: msg})
	d.State.RadioFirmware = msg.FirmwareVersion
	d.State.RadioDeviceState = msg.DeviceStateVer
	d.State.RadioHasWifi = msg.HasWifi
	d.State.RadioHasBT = msg.HasBluetooth
}

// ApplyLoraConfig stamps the radio's tx_power, region, and modem
// preset.
func (d *Driver) ApplyLoraConfig(msg mdl.LoraConfig) {
	defer d.Publish(Event{Kind: EventLoRaConfig, Data: msg})
	d.State.RadioTxPower = msg.TxPowerDBm
	d.State.RadioRegion = string(msg.Region)
	d.State.RadioModemPreset = string(msg.ModemPreset)
}

// ApplyDeviceConfig stamps the radio's role (Client / Router /
// Repeater / Tracker).
func (d *Driver) ApplyDeviceConfig(msg mdl.DeviceConfig) {
	defer d.Publish(Event{Kind: EventDeviceConfig, Data: msg})
	d.State.RadioRole = string(msg.Role)
}

// ApplyDeviceMetrics applies our-own-radio battery / channel / TX
// telemetry. Peer metrics are ignored here for now (peer metrics
// land in PeerEnv via ApplyEnvMetrics).
func (d *Driver) ApplyDeviceMetrics(msg mdl.DeviceMetrics) {
	defer d.Publish(Event{Kind: EventDeviceMetrics, Data: msg})
	if msg.FromNodeNum == d.State.MyNodeNum || msg.FromNodeNum == 0 {
		d.State.BatteryLevel = msg.BatteryLevel
		d.State.BatteryVoltage = msg.Voltage
		d.State.ChannelUtil = msg.ChannelUtil
		d.State.AirUtilTx = msg.AirUtilTx
		d.State.HasTelemetry = true
	}
}

// ApplyEnvMetrics records a peer's environmental telemetry —
// temperature / humidity / pressure / gas. Indexed by FromNodeNum
// so /env or per-peer dashboards can render the freshest reading.
func (d *Driver) ApplyEnvMetrics(msg mdl.EnvMetrics) {
	defer d.Publish(Event{Kind: EventEnvMetrics, Data: msg})
	if d.State.PeerEnv == nil {
		d.State.PeerEnv = make(map[uint32]PeerEnvMetrics)
	}
	d.State.PeerEnv[msg.FromNodeNum] = PeerEnvMetrics{
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
func (d *Driver) ApplyPosition(msg mdl.Position, grid string) ApplyPositionResult {
	defer d.PublishPosition(msg)
	if d.State.PeerPositions == nil {
		d.State.PeerPositions = make(map[uint32]PeerPosition)
	}
	d.State.PeerPositions[msg.FromNodeNum] = PeerPosition{
		Latitude:  msg.Latitude,
		Longitude: msg.Longitude,
		Altitude:  msg.Altitude,
		Grid:      grid,
		At:        msg.At,
	}
	res := ApplyPositionResult{IsSelf: msg.FromNodeNum == d.State.MyNodeNum}
	if res.IsSelf {
		d.State.MyLatitude = msg.Latitude
		d.State.MyLongitude = msg.Longitude
		d.State.MyAltitude = msg.Altitude
		d.State.MyGrid = grid
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
func (d *Driver) ApplyChannelInfo(msg mdl.ChannelInfo) {
	defer d.PublishChannelInfo(msg)
	const roleDisabled = "DISABLED"
	for len(d.State.Channels) <= msg.Index {
		d.State.Channels = append(d.State.Channels, mdl.ChannelItem{Role: roleDisabled})
	}
	if string(msg.Role) == roleDisabled {
		prevUnread := d.State.Channels[msg.Index].Unread
		d.State.Channels[msg.Index] = mdl.ChannelItem{
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
	c.Unread = d.State.Channels[msg.Index].Unread
	d.State.Channels[msg.Index] = c
	if d.State.CurrentChannel == "" {
		d.State.CurrentChannel = name
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
func (d *Driver) ApplyNodeInfo(msg mdl.NodeInfo) ApplyNodeInfoResult {
	defer d.PublishNodeInfo(msg)
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
	if d.Store != nil {
		_ = d.Store.SaveNode(d.State.RadioID, mdl.CachedNode{
			NodeNum:   msg.NodeNum,
			LongName:  msg.LongName,
			ShortName: msg.ShortName,
			HwModel:   msg.HwModel,
		})
	}
	res := ApplyNodeInfoResult{}
	if idx, ok := d.State.NodesByNum[msg.NodeNum]; ok {
		item.Fav = d.State.Nodes[idx].Fav
		if d.State.Nodes[idx].LastHeardAt.After(item.LastHeardAt) {
			item.LastHeardAt = d.State.Nodes[idx].LastHeardAt
		}
		wasUnresolved := d.State.Nodes[idx].Unresolved
		prevCallsign := d.State.Nodes[idx].Callsign
		d.State.Nodes[idx] = item
		if wasUnresolved && !item.Unresolved && prevCallsign != item.Callsign {
			res.GhostUpgrade = true
			res.PrevCallsign = prevCallsign
			res.NewCallsign = item.Callsign
		}
		return res
	}
	d.State.NodesByNum[msg.NodeNum] = len(d.State.Nodes)
	d.State.Nodes = append(d.State.Nodes, item)
	return res
}

// ApplyTextResult tells the TUI whether to advance selectedMsg
// (caller observed wasAtTail before calling) and whether the
// message is from a peer (so the TUI can ring the bell).
type ApplyTextResult struct {
	Index    int  // index in State.Messages where the row landed (-1 if dedupe-skipped)
	Skipped  bool // true when an existing PacketID was upgraded in place
	FromMine bool
}

// ApplyText handles an inbound TEXT_MESSAGE_APP packet. Updates the
// sender's NodeDB telemetry (lastSNR/RSSI/hops, ghost-creates if the
// peer hasn't sent NodeInfo yet), dedupes against a packet-ID replay,
// appends or refreshes the message row, bumps unread on non-active
// channels, and persists if a Store is wired. Sanitization of the
// text body is the caller's concern (lives in TUI today; daemon
// passes pre-sanitized text or ignores cleanup).
func (d *Driver) ApplyText(ev mdl.Text, sanitizedText string, corrupted bool) ApplyTextResult {
	defer d.PublishText(ev)
	body := ev.Body
	defaultLong, _ := mdl.DefaultCallsign(body.FromNum)
	from := defaultLong
	if idx, ok := d.State.NodesByNum[body.FromNum]; ok {
		from = d.State.Nodes[idx].Callsign
		d.State.Nodes[idx].LastHeardAt = time.Now()
		d.State.Nodes[idx].HeardRank = 0
		if body.SNR != "" {
			d.State.Nodes[idx].LastSNR = body.SNR
		}
		if ev.RSSI != "" {
			d.State.Nodes[idx].LastRSSI = ev.RSSI
		}
		if body.Hops > 0 {
			d.State.Nodes[idx].LastHops = body.Hops
		}
	} else if body.FromNum != 0 {
		long, short := mdl.DefaultCallsign(body.FromNum)
		d.State.Nodes = append(d.State.Nodes, mdl.NodeItem{
			Callsign:    long,
			ShortName:   short,
			NodeNum:     body.FromNum,
			Unresolved:  true,
			LastHeardAt: time.Now(),
			LastSNR:     body.SNR,
			LastRSSI:    ev.RSSI,
			LastHops:    body.Hops,
		})
		d.State.NodesByNum[body.FromNum] = len(d.State.Nodes) - 1
		from = long
	}
	mine := body.FromNum == d.State.MyNodeNum
	item := mdl.MessageItem{Message: mdl.Message{
		Time:      body.Time,
		From:      from,
		Mine:      mine,
		Text:      sanitizedText,
		Corrupted: corrupted,
		Status:    mdl.StatusAck,
		Hops:      body.Hops,
		SNR:       body.SNR,
		PacketID:  body.PacketID,
		ReplyID:   body.ReplyID,
		FromNum:   body.FromNum,
		ToNum:     ev.ToNum,
		SentAt:    body.SentAt,
	}}
	channelName := d.State.CurrentChannel
	if ev.Channel < len(d.State.Channels) {
		channelName = d.State.Channels[ev.Channel].Name
	}
	if body.PacketID != 0 {
		if existing, ok := d.State.MessagesByPacketID[body.PacketID]; ok &&
			existing >= 0 && existing < len(d.State.Messages) {
			prev := &d.State.Messages[existing]
			prev.Hops = body.Hops
			prev.SNR = body.SNR
			if prev.Status == mdl.StatusPending {
				prev.Status = mdl.StatusAck
			}
			if d.Store != nil {
				_ = d.Store.SaveMessage(d.State.RadioID, channelName, prev.Message)
			}
			return ApplyTextResult{Index: existing, Skipped: true, FromMine: mine}
		}
	}
	d.State.Messages = append(d.State.Messages, item)
	idx := len(d.State.Messages) - 1
	if body.PacketID != 0 {
		d.State.MessagesByPacketID[body.PacketID] = idx
	}
	if d.Store != nil {
		_ = d.Store.SaveMessage(d.State.RadioID, channelName, item.Message)
	}
	if ev.Channel < len(d.State.Channels) &&
		d.State.Channels[ev.Channel].Name != d.State.CurrentChannel && !mine {
		d.State.Channels[ev.Channel].Unread++
	}
	return ApplyTextResult{Index: idx, FromMine: mine}
}

// ApplyRoutingResult reports whether a Routing reply matched one of
// our outbound rows and what status it landed on, so the TUI can
// surface "ack received" / "delivery failed" flashes.
type ApplyRoutingResult struct {
	Matched   bool
	Index     int
	OK        bool
	ErrorName string
}

// ApplyRouting flips the matching outbound message row's status —
// NONE → ack, anything else → fail. Persists the flip so the row
// stays correct across restarts (without this, expireStalePending
// would re-mark a delivered row "fail" on next launch).
//
// Foreign Routing replies (request_id matches no row of ours) drop
// silently. Ping-correlation lives in the TUI's reactRouting; this
// path only handles the message-status flip.
func (d *Driver) ApplyRouting(msg mdl.Routing) ApplyRoutingResult {
	defer d.PublishRouting(msg)
	if msg.RequestID == 0 {
		return ApplyRoutingResult{}
	}
	for i := range d.State.Messages {
		if d.State.Messages[i].PacketID != msg.RequestID || !d.State.Messages[i].Mine {
			continue
		}
		if msg.OK {
			d.State.Messages[i].Status = mdl.StatusAck
		} else {
			d.State.Messages[i].Status = mdl.StatusFail
		}
		if d.Store != nil {
			_ = d.Store.SaveMessage(
				d.State.RadioID,
				d.State.CurrentChannel,
				d.State.Messages[i].Message,
			)
		}
		return ApplyRoutingResult{
			Matched:   true,
			Index:     i,
			OK:        msg.OK,
			ErrorName: msg.ErrorName,
		}
	}
	return ApplyRoutingResult{}
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
func (d *Driver) ApplyPing(msg mdl.Ping) {
	defer d.PublishPing(msg)
	if idx, ok := d.State.NodesByNum[msg.FromNum]; ok && idx < len(d.State.Nodes) {
		d.State.Nodes[idx].LastHops = msg.Hops
		if msg.SNR != "" {
			d.State.Nodes[idx].LastSNR = msg.SNR
		}
		if msg.RSSI != "" {
			d.State.Nodes[idx].LastRSSI = msg.RSSI
		}
		d.State.Nodes[idx].LastHeardAt = time.Now()
	}
}

// ApplyTraceroute records a TRACEROUTE_APP reply's hop count on
// the source peer's NodeItem and publishes the event. Path
// rendering (systemBlock with the resolved hop chain) lives in
// the TUI — it's presentation, and the daemon doesn't know which
// callsigns the consumer wants to see.
func (d *Driver) ApplyTraceroute(msg mdl.Traceroute) {
	defer d.PublishTraceroute(msg)
	if idx, ok := d.State.NodesByNum[msg.FromNum]; ok && idx < len(d.State.Nodes) {
		d.State.Nodes[idx].LastHops = len(msg.Route)
	}
}

// RecordOutboundOptions describes a locally-originated text packet
// the caller has just handed to the pump (or POSTed to the daemon).
// Fields mirror mdl.SendText with the Driver-allocated PacketID
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
func (d *Driver) RecordOutbound(opts RecordOutboundOptions) ApplyTextResult {
	if d == nil {
		return ApplyTextResult{Index: -1}
	}
	channelName := d.State.CurrentChannel
	if opts.Channel >= 0 && opts.Channel < len(d.State.Channels) {
		if name := d.State.Channels[opts.Channel].Name; name != "" {
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
		FromNum:  d.State.MyNodeNum,
		ToNum:    opts.ToNum,
		SentAt:   now,
	}}
	d.State.Messages = append(d.State.Messages, item)
	idx := len(d.State.Messages) - 1
	if opts.PacketID != 0 {
		d.State.MessagesByPacketID[opts.PacketID] = idx
	}
	if d.Store != nil {
		_ = d.Store.SaveMessage(d.State.RadioID, channelName, item.Message)
	}
	// Publish a synthesized mdl.Text so SSE subscribers get a
	// live "new outbound message" event in lockstep with the
	// State.Messages append. Mirrors the inbound ApplyText path.
	d.Publish(Event{Kind: EventText, Data: mdl.Text{
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

// ApplyConfigComplete marks the handshake as complete — Connected
// flips true, the reconnect banner clears. Returns whether this was
// the first ConfigComplete (TUI uses it to decide whether to emit
// the "sync complete" system line).
func (d *Driver) ApplyConfigComplete() bool {
	wasDisconnected := !d.State.Connected
	d.State.Connected = true
	d.State.Reconnect = nil
	d.State.SyncReceived = 0
	d.Publish(Event{Kind: EventConfigComplete, Data: mdl.ConfigComplete{}})
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

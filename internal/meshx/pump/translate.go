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

//
// Honest summary of what this program does and doesn't do with
// location data. Read this before editing any position-related code.
//
// WHAT MESHX DOES:
//
//   • Reads peer positions broadcast to the mesh (NodeInfo.position,
//     POSITION_APP packets) and caches them in-memory. Peers put this
//     on the wire by configuring their own radio — meshx doesn't
//     make the radio broadcast, the radio's firmware does.
//
//   • Displays the user's OWN grid square in the top status bar
//     (local terminal only — nothing goes out the radio). This
//     reflects whatever the radio already knows about its position.
//
//   • Transmits grid square when the user explicitly types /qth or
//     /grid. These are opt-in commands; meshx never runs them
//     automatically.
//
//   • Shows peer grids on explicit /qth <call> and /whois <call>
//     lookup. Output is limited to coarse Maidenhead grid
//     (≈20 km precision), never exact lat/long or altitude.
//
// WHAT MESHX DOES NOT DO:
//
//   • Never auto-transmits position (no beacon, no ping, no timer).
//   • Never writes position data to disk (in-memory only).
//   • Never shows exact lat/long coords to the user (grid-only).
//   • Never forwards position data off-device (no HTTP, no MQTT,
//     no logging).
//
// IMPORTANT CAVEATS:
//
//   • The RADIO may broadcast its own position independently of
//     meshx — controlled by `position.*` config in the Meshtastic
//     firmware. If you don't want your radio sending position,
//     disable it on the radio side (official Meshtastic app/CLI).
//     meshx cannot stop the radio from broadcasting; it can only
//     choose not to relay what the radio sends.
//
//   • /qth and /grid DO transmit your grid over LoRa when you run
//     them — that's the command's purpose. If you don't want your
//     location shared, don't run those commands.
//
//   • Peer positions are cached because /qth <call> lookup needs
//     them. If you don't want meshx to even READ peer positions,
//     we'd need a config flag (not yet wired) to drop them at
//     ingress. Open an issue if you want that.
//
// Changes to these behaviors require review — they're load-bearing
// for anyone using meshx on a public mesh.

package pump

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"

	"github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/transport"
)

// translate converts a FromRadio envelope to zero or more model
// event values, in the order the consumer should observe them. A
// single FromRadio frame may produce multiple events — e.g. a
// NodeInfo envelope with an embedded Position fans out to BOTH a
// model.NodeInfo AND a model.Position, and the NodeInfo has to land
// first so the position update applies to an existing node row
// instead of creating a stub. Returning a slice (instead of firing
// a goroutine for the side-channel msg) keeps that ordering
// deterministic — runSession iterates and Sends in slice order on
// the same goroutine that receives frames, so the NodeInfo always
// reaches the consumer before the Position. Returns nil/empty for
// housekeeping variants the consumer doesn't care about.
//
// This file is the ONE place in the codebase where pb.* types meet
// model.* types — every projection lives here so model can stay
// proto-free.
func (p *Pump) translate(msg *pb.FromRadio) []tea.Msg {
	switch v := msg.GetPayloadVariant().(type) {
	case *pb.FromRadio_MyInfo:
		p.myNum = v.MyInfo.GetMyNodeNum()
		return []tea.Msg{model.MyInfo{NodeNum: p.myNum}}

	case *pb.FromRadio_NodeInfo:
		n := v.NodeInfo
		u := n.GetUser()

		out := []tea.Msg{model.NodeInfo{
			NodeNum:   n.GetNum(),
			LongName:  u.GetLongName(),
			ShortName: u.GetShortName(),
			HwModel:   transport.HwModelName(int(u.GetHwModel())),
			SNR:       fmt.Sprintf("%.1f", n.GetSnr()),
			// NodeInfo doesn't carry RSSI directly in this vendored
			// proto; leave blank — MeshPacket telemetry carries it
			// per-message.
			RSSI:        "",
			Hops:        0,
			LastHeardAt: time.Unix(int64(n.GetLastHeard()), 0),
		}}
		// NodeInfo.Position is populated for peers whose radios
		// broadcast their location. Append a Position AFTER the
		// NodeInfo so the consumer has already created the node row
		// by the time the position update applies. Zero lat+lon →
		// no fix, skip.
		if pos := n.GetPosition(); pos != nil &&
			(pos.GetLatitudeI() != 0 || pos.GetLongitudeI() != 0) {
			out = append(out, model.Position{
				FromNodeNum: n.GetNum(),
				Latitude:    float64(pos.GetLatitudeI()) / 1e7,
				Longitude:   float64(pos.GetLongitudeI()) / 1e7,
				Altitude:    pos.GetAltitude(),
				At:          time.Unix(int64(pos.GetTime()), 0),
			})
		}
		return out

	case *pb.FromRadio_Channel:
		s := v.Channel.GetSettings()
		// Defensive copy: GetPsk() returns the proto's underlying
		// byte slice without a copy. Aliasing that across the
		// goroutine boundary into a tea.Msg means a future caller of
		// Reset() on the proto (or any pooling gomesh adds later)
		// could mutate our channelItem.psk in place. Cheap to copy
		// 16-32 bytes.
		var pskCopy []byte
		if psk := s.GetPsk(); len(psk) > 0 {
			pskCopy = append([]byte(nil), psk...)
		}
		return []tea.Msg{model.ChannelInfo{
			Index:  int(v.Channel.GetIndex()),
			Name:   s.GetName(),
			Role:   model.ChannelRole(v.Channel.GetRole().String()),
			HasPSK: len(pskCopy) > 0,
			PSK:    pskCopy,
		}}

	case *pb.FromRadio_Packet:
		return p.translatePacket(v.Packet)

	case *pb.FromRadio_Metadata:
		md := v.Metadata
		return []tea.Msg{model.Metadata{
			FirmwareVersion: md.GetFirmwareVersion(),
			DeviceStateVer:  md.GetDeviceStateVersion(),
			HasWifi:         md.GetHasWifi(),
			HasBluetooth:    md.GetHasBluetooth(),
		}}

	case *pb.FromRadio_Config:
		switch c := v.Config.GetPayloadVariant().(type) {
		case *pb.Config_Lora:
			if c == nil || c.Lora == nil {
				return nil
			}
			return []tea.Msg{model.LoraConfig{
				TxPowerDBm:  c.Lora.GetTxPower(),
				Region:      model.Region(c.Lora.GetRegion().String()),
				ModemPreset: model.ModemPreset(c.Lora.GetModemPreset().String()),
			}}
		case *pb.Config_Device:
			if c == nil || c.Device == nil {
				return nil
			}
			return []tea.Msg{model.DeviceConfig{
				Role: model.DeviceRole(c.Device.GetRole().String()),
			}}
		}
		return nil

	case *pb.FromRadio_ConfigCompleteId:
		return []tea.Msg{model.ConfigComplete{}}

	case *pb.FromRadio_ModuleConfig:
		// Module configs ship during the same WantConfigId NodeDB
		// replay phase Config envelopes do. We only consume the
		// ExternalNotification variant — that's what governs the
		// radio buzzer /config exposes. Other modules (mqtt,
		// store-and-forward, range_test, …) don't have a meshx
		// surface yet, so we drop them silently.
		mc := v.ModuleConfig
		if mc == nil {
			return nil
		}
		switch pl := mc.GetPayloadVariant().(type) {
		case *pb.ModuleConfig_ExternalNotification:
			ext := pl.ExternalNotification
			if ext == nil {
				return nil
			}
			return []tea.Msg{model.ModuleBuzzer{
				Enabled:            ext.GetEnabled(),
				AlertMessageBuzzer: ext.GetAlertMessageBuzzer(),
				Snapshot:           ExternalNotificationFromProto(ext),
			}}
		}
		return nil
	}
	// Other FromRadio variants we don't consume yet — drop.
	return nil
}

// translatePacket handles the FromRadio_Packet variant — split out
// because the inner switch is long and translate() reads more
// cleanly when each top-level FromRadio arm gets a single line.
func (p *Pump) translatePacket(pkt *pb.MeshPacket) []tea.Msg {
	dec := pkt.GetDecoded()
	if dec == nil {
		return nil
	}
	switch dec.GetPortnum() {
	case pb.PortNum_TEXT_MESSAGE_APP:
		// Build the partial model.Message body — wire fields
		// populated from the packet, render fields (From, Mine,
		// Bang, Status) left zero for the consumer to enrich.
		// Time ("15:04") is pre-formatted off SentAt so the
		// renderer doesn't need to re-format on every paint.
		at := time.Unix(int64(pkt.GetRxTime()), 0)
		return []tea.Msg{model.Text{
			Channel: int(pkt.GetChannel()),
			ToNum:   pkt.GetTo(),
			RSSI:    fmt.Sprintf("%d", pkt.GetRxRssi()),
			Body: model.Message{
				Time:     at.Format("15:04"),
				Text:     string(dec.GetPayload()),
				Status:   model.StatusAck,
				Hops:     int(pkt.GetHopStart()) - int(pkt.GetHopLimit()),
				SNR:      fmt.Sprintf("%.1f", pkt.GetRxSnr()),
				PacketID: pkt.GetId(),
				ReplyID:  dec.GetReplyId(),
				FromNum:  pkt.GetFrom(),
				SentAt:   at,
			},
		}}
	case pb.PortNum_TELEMETRY_APP:
		// TELEMETRY_APP payload is a Telemetry protobuf whose
		// `variant` oneof is DeviceMetrics / EnvironmentMetrics /
		// etc. Branch on which variant arrived.
		tel := &pb.Telemetry{}
		if err := proto.Unmarshal(dec.GetPayload(), tel); err != nil {
			return nil
		}
		switch v := tel.GetVariant().(type) {
		case *pb.Telemetry_DeviceMetrics:
			if v == nil || v.DeviceMetrics == nil {
				return nil
			}
			return []tea.Msg{model.DeviceMetrics{
				FromNodeNum:  pkt.GetFrom(),
				BatteryLevel: v.DeviceMetrics.GetBatteryLevel(),
				Voltage:      v.DeviceMetrics.GetVoltage(),
				ChannelUtil:  v.DeviceMetrics.GetChannelUtilization(),
				AirUtilTx:    v.DeviceMetrics.GetAirUtilTx(),
			}}
		case *pb.Telemetry_EnvironmentMetrics:
			if v == nil || v.EnvironmentMetrics == nil {
				return nil
			}
			return []tea.Msg{model.EnvMetrics{
				FromNodeNum: pkt.GetFrom(),
				Temperature: v.EnvironmentMetrics.GetTemperature(),
				Humidity:    v.EnvironmentMetrics.GetRelativeHumidity(),
				Pressure:    v.EnvironmentMetrics.GetBarometricPressure(),
				Gas:         v.EnvironmentMetrics.GetGasResistance(),
			}}
		}
		return nil
	case pb.PortNum_POSITION_APP:
		// Standalone position update — a peer broadcasting a fresh
		// fix. Decode the Position payload and apply it.
		pos := &pb.Position{}
		if err := proto.Unmarshal(dec.GetPayload(), pos); err != nil {
			return nil
		}
		if pos.GetLatitudeI() == 0 && pos.GetLongitudeI() == 0 {
			return nil
		}
		return []tea.Msg{model.Position{
			FromNodeNum: pkt.GetFrom(),
			Latitude:    float64(pos.GetLatitudeI()) / 1e7,
			Longitude:   float64(pos.GetLongitudeI()) / 1e7,
			Altitude:    pos.GetAltitude(),
			At:          time.Unix(int64(pos.GetTime()), 0),
		}}
	case pb.PortNum_NODEINFO_APP:
		// Live NodeInfo broadcast — a peer announcing their User
		// (longname + shortname + hw). The FromRadio_NodeInfo
		// envelope handles config-time NodeDB dumps; THIS is how we
		// pick up NodeInfo updates that arrive mid-session. Without
		// this case we'd stay stuck on "node 0x…" for any peer whose
		// text packet arrived before the radio happened to see their
		// NodeInfo.
		u := &pb.User{}
		if err := proto.Unmarshal(dec.GetPayload(), u); err != nil {
			return nil
		}
		return []tea.Msg{model.NodeInfo{
			NodeNum:     pkt.GetFrom(),
			LongName:    u.GetLongName(),
			ShortName:   u.GetShortName(),
			HwModel:     transport.HwModelName(int(u.GetHwModel())),
			SNR:         fmt.Sprintf("%.1f", pkt.GetRxSnr()),
			RSSI:        fmt.Sprintf("%d", pkt.GetRxRssi()),
			Hops:        int(pkt.GetHopStart()) - int(pkt.GetHopLimit()),
			LastHeardAt: time.Unix(int64(pkt.GetRxTime()), 0),
		}}
	case pb.PortNum_ADMIN_APP:
		// AdminMessage replies — the radio answers our
		// GetModuleConfigRequest with a MeshPacket carrying an
		// AdminMessage whose oneof is GetModuleConfigResponse.
		// Same decode shape the WantConfigId dump uses, just
		// arriving via the data port instead of FromRadio_*. We
		// only consume the ExternalNotification variant; other
		// AdminMessage replies (Owner, Channel, … responses we
		// didn't request) drop silently.
		adm := &pb.AdminMessage{}
		if err := proto.Unmarshal(dec.GetPayload(), adm); err != nil {
			return nil
		}
		resp := adm.GetGetModuleConfigResponse()
		if resp == nil {
			return nil
		}
		ext := resp.GetExternalNotification()
		if ext == nil {
			return nil
		}
		return []tea.Msg{model.ModuleBuzzer{
			Enabled:            ext.GetEnabled(),
			AlertMessageBuzzer: ext.GetAlertMessageBuzzer(),
			Snapshot:           ExternalNotificationFromProto(ext),
		}}
	case pb.PortNum_REPLY_APP:
		// Echo of an outbound /ping. The firmware's REPLY_APP
		// service bounces whatever payload it received back to the
		// sender — we don't care about the body; we only need the
		// round-trip metadata. request_id correlates back to the
		// consumer's pendingPing when firmware echoes it; the
		// FromNum fallback in the consumer covers the (older) case
		// where it doesn't.
		return []tea.Msg{model.Ping{
			RequestID: dec.GetRequestId(),
			FromNum:   pkt.GetFrom(),
			Hops:      int(pkt.GetHopStart()) - int(pkt.GetHopLimit()),
			SNR:       fmt.Sprintf("%.1f", pkt.GetRxSnr()),
			RSSI:      fmt.Sprintf("%d", pkt.GetRxRssi()),
			At:        time.Unix(int64(pkt.GetRxTime()), 0),
		}}
	case pb.PortNum_TRACEROUTE_APP:
		// Reply to a /tr request. Payload is a RouteDiscovery proto
		// whose Route is the ordered list of node nums the packet
		// traversed (intermediate hops only; the source and dest
		// are implicit at MeshPacket.From / .To). request_id
		// correlates back to the outbound packetID stashed at
		// request time. Foreign traceroutes (replies to someone
		// else's request) silently drop in the consumer because
		// their request_id won't match.
		rd := &pb.RouteDiscovery{}
		if err := proto.Unmarshal(dec.GetPayload(), rd); err != nil {
			return nil
		}
		return []tea.Msg{model.Traceroute{
			RequestID: dec.GetRequestId(),
			FromNum:   pkt.GetFrom(),
			ToNum:     pkt.GetTo(),
			Route:     append([]uint32(nil), rd.GetRoute()...),
			At:        time.Unix(int64(pkt.GetRxTime()), 0),
		}}
	case pb.PortNum_ROUTING_APP:
		// Routing payload carries the radio's verdict on a packet
		// we (or someone else) sent. For our own outbound: the
		// MeshPacket's request_id == our stashed packetID and the
		// Routing.error_reason says NONE (ack) or a failure code
		// (TIMEOUT, MAX_RETRANSMIT, ...). For foreign packets the
		// request_id won't match any of ours so the consumer
		// simply drops it.
		r := &pb.Routing{}
		if err := proto.Unmarshal(dec.GetPayload(), r); err != nil {
			return nil
		}
		reason := r.GetErrorReason().String()
		return []tea.Msg{model.Routing{
			RequestID: dec.GetRequestId(),
			Reason:    model.RoutingError(reason),
			ErrorName: reason,
			OK:        reason == string(model.RoutingNone),
		}}
	}
	return nil
}

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

package meshx

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"

	"github.com/retr0h/meshx/internal/meshx/transport"
)

// ─── PII / POSITION HANDLING ─────────────────────────────────────────
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
// ─────────────────────────────────────────────────────────────────────

// Bubble Tea messages pushed from the transport pump into the
// running program via p.Send(). The model's Update handler has a
// case for each.
type (
	// radioConnectingMsg fires as soon as Dial succeeds — before any
	// protocol traffic. Carries the destination string for UI display.
	radioConnectingMsg struct{ dest string }

	// radioMyInfoMsg delivers MyNodeInfo — our own node number,
	// firmware / hardware details used to populate the top status bar.
	radioMyInfoMsg struct {
		nodeNum uint32
	}

	// radioNodeInfoMsg delivers one peer NodeInfo. Multiple arrive
	// during the initial handshake, one per known peer in the NodeDB.
	radioNodeInfoMsg struct {
		nodeNum     uint32
		longName    string
		shortName   string
		hwModel     string
		snr         string
		rssi        string
		hops        int
		lastHeardAt time.Time
	}

	// radioChannelMsg delivers one channel slot — index, name, role,
	// and PSK presence. Empty name + PRIMARY role is "default LongFast."
	radioChannelMsg struct {
		index   int
		name    string
		role    string
		hasPSK  bool
	}

	// radioTextMsg arrives whenever a text packet lands on any
	// channel. From and To are node nums; channel is the channel index.
	// packetID is MeshPacket.id — we capture it so outgoing replies
	// can reference it via Data.reply_id. replyID, when non-zero,
	// is the parent packet this one is answering.
	radioTextMsg struct {
		fromNum  uint32
		toNum    uint32
		channel  int
		text     string
		snr      string
		rssi     string
		hops     int
		at       time.Time
		packetID uint32
		replyID  uint32
	}

	// radioMetadataMsg delivers firmware_version + hw identity
	// details from the one-shot FromRadio.Metadata envelope.
	radioMetadataMsg struct {
		firmwareVersion string
		deviceStateVer  uint32
		hasWifi         bool
		hasBluetooth    bool
	}

	// radioLoraConfigMsg delivers the LoRa config — we surface
	// tx_power, region, and modem preset in the status bar.
	radioLoraConfigMsg struct {
		txPowerDBm  int32
		region      string
		modemPreset string
	}

	// radioDeviceMetricsMsg delivers the latest DeviceMetrics telemetry
	// packet — battery, voltage, channel utilization, TX airtime.
	// Arrives periodically (default every 30 min) from the radio.
	radioDeviceMetricsMsg struct {
		fromNodeNum   uint32
		batteryLevel  uint32  // 0-100; >100 = powered
		voltage       float32 // volts
		channelUtil   float32 // percent
		airUtilTx     float32 // percent
	}

	// radioEnvMetricsMsg delivers a peer's environmental telemetry —
	// temperature / humidity / pressure / gas. Reported by nodes with
	// an attached BME280 / SHT3x etc. sensor. Rare on most meshes.
	radioEnvMetricsMsg struct {
		fromNodeNum uint32
		temperature float32 // °C
		humidity    float32 // %
		pressure    float32 // hPa
		gas         float32 // ohms
	}

	// radioDeviceConfigMsg delivers Config.device — right now we just
	// surface the node's role (CLIENT / ROUTER / REPEATER / TRACKER
	// etc.) in the status bar.
	radioDeviceConfigMsg struct {
		role string
	}

	// radioPositionMsg delivers a node's position (from NodeInfo or
	// a POSITION_APP packet). Applied to the sender's nodeItem and
	// surfaced via /qth <call> or the top-bar grid square for us.
	radioPositionMsg struct {
		fromNodeNum uint32
		latitude    float64 // degrees
		longitude   float64 // degrees
		altitude    int32   // meters
		at          time.Time
	}

	// radioConfigCompleteMsg fires when the initial config dump
	// finishes — the UI leaves "connecting" state and shows the main
	// log populated with the node list received so far.
	radioConfigCompleteMsg struct{}

	// radioErrorMsg carries a fatal transport error. The UI shows it
	// and the app exits cleanly.
	radioErrorMsg struct{ err error }

	// radioDisconnectedMsg fires when the stream ends without error —
	// the radio was unplugged or rebooted cleanly.
	radioDisconnectedMsg struct{}
)

// pump is the glue between a transport.Client and the Bubble Tea
// runtime. One goroutine reads FromRadio frames and publishes them
// as tea.Msg via program.Send(); another drains outboundToRadio and
// writes them to the device.
type pump struct {
	client  transport.Client
	program *tea.Program

	// Our own node num, learned from MyNodeInfo. Used to filter
	// outbound-echo MeshPackets.
	myNum uint32

	// Outbound ToRadio envelopes — model code enqueues to send.
	outbound chan *pb.ToRadio

	// Cancellation for the running goroutines.
	cancel context.CancelFunc
}

// startPump opens a transport to the given destination, spawns the
// pump goroutines, and returns a handle. The caller hands the
// returned pump to the model via an option so the UI can enqueue
// outbound messages.
//
// Call p.Stop() to tear down.
func startPump(dest string, program *tea.Program) (*pump, error) {
	client, err := transport.Dial(dest)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())

	p := &pump{
		client:   client,
		program:  program,
		outbound: make(chan *pb.ToRadio, 16),
		cancel:   cancel,
	}

	// Bubble Tea isn't up yet when cmd.Execute calls this. We stash
	// the program pointer so the goroutine can start publishing as
	// soon as the Bubble Tea loop begins.
	go p.run(ctx)

	return p, nil
}

func (p *pump) Stop() {
	p.cancel()
	_ = p.client.Close()
}

// Enqueue is how model code sends a ToRadio from the Update goroutine.
// Non-blocking — drops the message (flashing a hint is the caller's
// responsibility) if the outbound buffer is full, which should never
// happen in practice.
func (p *pump) Enqueue(msg *pb.ToRadio) bool {
	select {
	case p.outbound <- msg:
		return true
	default:
		return false
	}
}

// run is the main pump loop. Spawns the transport Run goroutine,
// pumps outbound messages, translates inbound FromRadio frames into
// tea.Msg and ships them via program.Send().
func (p *pump) run(ctx context.Context) {
	inbound := make(chan *pb.FromRadio, 64)

	// Kick off the transport read+write goroutines.
	runErr := make(chan error, 1)
	go func() {
		runErr <- p.client.Run(ctx, inbound, p.outbound)
	}()

	// Fire the config handshake — prompts the radio to dump its NodeDB,
	// channels, configs, and ConfigComplete.
	_ = transport.SendWantConfig(p.outbound)

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-runErr:
			if err != nil {
				p.program.Send(radioErrorMsg{err: fmt.Errorf("transport: %w", err)})
			} else {
				p.program.Send(radioDisconnectedMsg{})
			}
			return
		case msg := <-inbound:
			if msg == nil {
				continue
			}
			if tm := p.translate(msg); tm != nil {
				p.program.Send(tm)
			}
		}
	}
}

// translate converts a FromRadio envelope to the corresponding
// tea.Msg. Returns nil for housekeeping variants the UI doesn't
// care about (module config, raw config dumps, etc.) — those get
// silently dropped so the Bubble Tea Update loop isn't spammed.
func (p *pump) translate(msg *pb.FromRadio) tea.Msg {
	switch v := msg.GetPayloadVariant().(type) {
	case *pb.FromRadio_MyInfo:
		p.myNum = v.MyInfo.GetMyNodeNum()
		return radioMyInfoMsg{nodeNum: p.myNum}

	case *pb.FromRadio_NodeInfo:
		n := v.NodeInfo
		u := n.GetUser()
		snr := fmt.Sprintf("%.1f", n.GetSnr())
		// NodeInfo doesn't carry RSSI directly in this vendored proto;
		// leave blank — MeshPacket telemetry carries it per-message.
		rssi := ""

		// NodeInfo.Position is populated for peers whose radios
		// broadcast their location. Piggy-back a radioPositionMsg so
		// the model can stash the coordinates for /qth + grid-square
		// rendering. Zero lat+lon → no fix, skip.
		if pos := n.GetPosition(); pos != nil &&
			(pos.GetLatitudeI() != 0 || pos.GetLongitudeI() != 0) {
			go p.program.Send(radioPositionMsg{
				fromNodeNum: n.GetNum(),
				latitude:    float64(pos.GetLatitudeI()) / 1e7,
				longitude:   float64(pos.GetLongitudeI()) / 1e7,
				altitude:    pos.GetAltitude(),
				at:          time.Unix(int64(pos.GetTime()), 0),
			})
		}
		return radioNodeInfoMsg{
			nodeNum:     n.GetNum(),
			longName:    u.GetLongName(),
			shortName:   u.GetShortName(),
			hwModel:     transport.HwModelName(int(u.GetHwModel())),
			snr:         snr,
			rssi:        rssi,
			hops:        0,
			lastHeardAt: time.Unix(int64(n.GetLastHeard()), 0),
		}

	case *pb.FromRadio_Channel:
		s := v.Channel.GetSettings()
		return radioChannelMsg{
			index:  int(v.Channel.GetIndex()),
			name:   s.GetName(),
			role:   v.Channel.GetRole().String(),
			hasPSK: len(s.GetPsk()) > 0,
		}

	case *pb.FromRadio_Packet:
		p := v.Packet
		dec := p.GetDecoded()
		if dec == nil {
			return nil
		}
		switch dec.GetPortnum() {
		case pb.PortNum_TEXT_MESSAGE_APP:
			return radioTextMsg{
				fromNum:  p.GetFrom(),
				toNum:    p.GetTo(),
				channel:  int(p.GetChannel()),
				text:     string(dec.GetPayload()),
				snr:      fmt.Sprintf("%.1f", p.GetRxSnr()),
				rssi:     fmt.Sprintf("%d", p.GetRxRssi()),
				hops:     int(p.GetHopStart()) - int(p.GetHopLimit()),
				at:       time.Unix(int64(p.GetRxTime()), 0),
				packetID: p.GetId(),
				replyID:  dec.GetReplyId(),
			}
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
				return radioDeviceMetricsMsg{
					fromNodeNum:  p.GetFrom(),
					batteryLevel: v.DeviceMetrics.GetBatteryLevel(),
					voltage:      v.DeviceMetrics.GetVoltage(),
					channelUtil:  v.DeviceMetrics.GetChannelUtilization(),
					airUtilTx:    v.DeviceMetrics.GetAirUtilTx(),
				}
			case *pb.Telemetry_EnvironmentMetrics:
				if v == nil || v.EnvironmentMetrics == nil {
					return nil
				}
				return radioEnvMetricsMsg{
					fromNodeNum: p.GetFrom(),
					temperature: v.EnvironmentMetrics.GetTemperature(),
					humidity:    v.EnvironmentMetrics.GetRelativeHumidity(),
					pressure:    v.EnvironmentMetrics.GetBarometricPressure(),
					gas:         v.EnvironmentMetrics.GetGasResistance(),
				}
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
			return radioPositionMsg{
				fromNodeNum: p.GetFrom(),
				latitude:    float64(pos.GetLatitudeI()) / 1e7,
				longitude:   float64(pos.GetLongitudeI()) / 1e7,
				altitude:    pos.GetAltitude(),
				at:          time.Unix(int64(pos.GetTime()), 0),
			}
		}
		return nil

	case *pb.FromRadio_Metadata:
		md := v.Metadata
		return radioMetadataMsg{
			firmwareVersion: md.GetFirmwareVersion(),
			deviceStateVer:  md.GetDeviceStateVersion(),
			hasWifi:         md.GetHasWifi(),
			hasBluetooth:    md.GetHasBluetooth(),
		}

	case *pb.FromRadio_Config:
		switch c := v.Config.GetPayloadVariant().(type) {
		case *pb.Config_Lora:
			if c == nil || c.Lora == nil {
				return nil
			}
			return radioLoraConfigMsg{
				txPowerDBm:  c.Lora.GetTxPower(),
				region:      c.Lora.GetRegion().String(),
				modemPreset: c.Lora.GetModemPreset().String(),
			}
		case *pb.Config_Device:
			if c == nil || c.Device == nil {
				return nil
			}
			return radioDeviceConfigMsg{role: c.Device.GetRole().String()}
		}
		return nil

	case *pb.FromRadio_ConfigCompleteId:
		return radioConfigCompleteMsg{}
	}
	// ModuleConfig and other variants — ignore.
	return nil
}


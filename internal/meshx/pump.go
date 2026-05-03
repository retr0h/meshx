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
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"

	"github.com/retr0h/meshx/internal/meshx/transport"
)

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

// Bubble Tea messages pushed from the transport pump into the
// running program via p.Send(). The model's Update handler has a
// case for each.
type (
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
		index  int
		name   string
		role   string
		hasPSK bool
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

	// radioTracerouteMsg arrives when a TRACEROUTE_APP reply lands —
	// the result of an outbound /tr that issued a RouteDiscovery
	// request. requestID matches MeshPacket.Data.request_id and
	// correlates back to the in-flight pendingTraceroute the model
	// recorded when the user pressed /tr. fromNum is the responder's
	// node num; route is the ordered list of intermediate node nums
	// the discovery walked through (does NOT include source or dest
	// per the Meshtastic firmware convention). hops = len(route) is
	// the actual mesh hop count for the round-trip.
	radioTracerouteMsg struct {
		requestID uint32
		fromNum   uint32
		toNum     uint32
		route     []uint32
		at        time.Time
	}

	// radioRoutingMsg is the Meshtastic delivery receipt — the radio
	// echoes a Routing packet with request_id == our packetID once it
	// finishes the send (or gives up). errorReason == NONE means the
	// packet made it onto the mesh; anything else (TIMEOUT,
	// MAX_RETRANSMIT, NO_INTERFACE, etc.) is a delivery failure.
	// The UI flips the matching local row's status to "ack" / "fail"
	// so the `…` in-flight marker becomes ✓ or ✗.
	radioRoutingMsg struct {
		requestID uint32
		errorName string // e.g. "NONE", "TIMEOUT", "MAX_RETRANSMIT"
		ok        bool   // true when errorName == "NONE"
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
		fromNodeNum  uint32
		batteryLevel uint32  // 0-100; >100 = powered
		voltage      float32 // volts
		channelUtil  float32 // percent
		airUtilTx    float32 // percent
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

	// radioErrorMsg carries a fatal transport error. With the
	// indefinite-retry policy the pump never emits one of these on
	// its own — kept on the type set for future use (transport errors
	// the pump explicitly classifies as unrecoverable, e.g. the dest
	// string can't be parsed).
	radioErrorMsg struct{ err error }

	// radioReconnectingMsg fires after a transport error while the
	// pump is in its retry loop. The UI parks a persistent banner
	// (model.reconnect) keyed off this so a transient BLE / serial
	// drop reads as "we noticed, retrying every Ns" instead of a
	// silent hang followed by a stale error.
	radioReconnectingMsg struct {
		attempt int
		after   time.Duration
		err     error
	}

	// radioDisconnectedMsg fires when the stream ends without error —
	// the radio was unplugged or rebooted cleanly. The pump treats this
	// as terminal (no retry) on the assumption the user pulled the plug
	// deliberately.
	radioDisconnectedMsg struct{}
)

// pump is the glue between a transport.Client and the Bubble Tea
// runtime. One goroutine reads FromRadio frames and publishes them
// as tea.Msg via program.Send(); another drains outboundToRadio and
// writes them to the device.
//
// The pump owns the reconnect policy. When `client.Run` returns an
// error the pump closes the dead client, sleeps with exponential
// backoff, re-dials `dest`, and resumes pumping — all transparent
// to the model. If a session manages to receive at least one frame
// the attempt counter resets, so a long-stable connection that
// hiccups gets a fresh budget.
type pump struct {
	client  transport.Client
	program *tea.Program

	// Destination string from the original Dial — re-used by the
	// reconnect loop. Stash it so the pump doesn't need to plumb the
	// dest back through the model.
	dest string

	// Our own node num, learned from MyNodeInfo. Used to filter
	// outbound-echo MeshPackets.
	myNum uint32

	// Outbound ToRadio envelopes — model code enqueues to send.
	// Survives across reconnects: the channel itself is stable while
	// the underlying transport.Client gets swapped out.
	outbound chan *pb.ToRadio

	// Cancellation for the running goroutines.
	cancel context.CancelFunc
}

// Reconnect policy: truncated exponential backoff with no jitter,
// retried indefinitely. Schedule is 1s,2s,4s,8s,16s,30s,30s,30s,…
// (every step beyond 30s stays at 30s). The pump only stops on ctx
// cancellation (Ctrl+X / process exit) or a clean transport-side
// disconnect. Real-world BLE re-pair (radio in a drawer, walked out
// of range, OS Bluetooth hiccup) routinely takes more than two
// minutes, and the user told us they'd rather see "retry 47/∞ in
// 30s" forever than have meshx silently give up. Jitter is a no-op
// here — single client, single radio, no thundering-herd risk.
const (
	minReconnectBackoff = 1 * time.Second
	maxReconnectBackoff = 30 * time.Second
)

// reconnectBackoff returns the delay before the Nth retry. attempt is
// 1-indexed (the first retry uses attempt=1 → 1s).
func reconnectBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	// Cap the shift so 1<<(attempt-1) can't overflow on absurd inputs.
	if attempt > 30 {
		attempt = 30
	}
	d := minReconnectBackoff * time.Duration(1<<(attempt-1))
	if d > maxReconnectBackoff || d <= 0 {
		d = maxReconnectBackoff
	}
	return d
}

// startPump spins up a pump goroutine and returns the handle
// immediately. Dialing happens inside the goroutine — that way an
// 8-second BLE scan at startup doesn't block the Bubble Tea Update
// loop, and a doomed dest (radio off, bad UUID) flows through the
// same indefinite-retry path as a mid-session drop. Call p.Stop()
// to tear down.
func startPump(dest string, program *tea.Program) *pump {
	ctx, cancel := context.WithCancel(context.Background())

	p := &pump{
		// client is intentionally nil; the run loop's "if p.client
		// == nil" branch performs the first dial. That keeps initial
		// connect and reconnect on the same code path.
		client:   nil,
		program:  program,
		dest:     dest,
		outbound: make(chan *pb.ToRadio, 16),
		cancel:   cancel,
	}

	go p.run(ctx)

	return p
}

func (p *pump) Stop() {
	p.cancel()
	// p.client is nil between startPump and the first successful dial,
	// so a Ctrl+X during the initial BLE scan would panic here without
	// the guard. The run loop owns subsequent client mutations and will
	// observe ctx cancellation on its next iteration to clean up.
	if p.client != nil {
		_ = p.client.Close()
	}
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
//
// When $MESHX_DEBUG is set (to a file path, or to "1" for a default
// path), every pump event is appended to that file — the TUI's
// alt-screen swallows stderr so this is the only way to see what's
// flowing when a BLE / serial session appears to hang. Pipe-friendly
// single-line records so `tail -f` reads cleanly.
func (p *pump) run(ctx context.Context) {
	dbg := openPumpDebugLog()
	defer func() {
		if dbg != nil {
			_ = dbg.Close()
		}
	}()
	dbgf := func(format string, args ...any) {
		if dbg == nil {
			return
		}
		line := fmt.Sprintf(format, args...)
		_, _ = fmt.Fprintf(dbg, "[%s] %s\n", time.Now().Format("15:04:05.000"), line)
	}

	dbgf("pump.run start dest=%s", p.dest)

	attempt := 0
	for {
		if ctx.Err() != nil {
			dbgf("ctx done before next session — exiting")
			return
		}

		// Re-dial if we're between sessions. First time through, the
		// client supplied by startPump is already live so we skip.
		var sessErr error
		if p.client == nil {
			dbgf("re-dial attempt %d", attempt+1)
			client, derr := transport.Dial(p.dest)
			if derr != nil {
				dbgf("re-dial failed: %v", derr)
				sessErr = derr
			} else {
				p.client = client
			}
		}

		if sessErr == nil && p.client != nil {
			established, err := p.runSession(ctx, dbgf)
			if ctx.Err() != nil {
				dbgf("ctx done after session — exiting")
				return
			}
			// A session that produced any inbound frames counts as a
			// successful connect — reset the budget so a long-running
			// link that hiccups gets a fresh 8 attempts.
			if established {
				attempt = 0
			}
			_ = p.client.Close()
			p.client = nil

			if err == nil {
				// Clean disconnect — radio rebooted or got unplugged.
				// Don't auto-redial; the user probably did this on
				// purpose and the pump goroutine should exit.
				dbgf("clean disconnect — not retrying")
				p.program.Send(radioDisconnectedMsg{})
				return
			}
			sessErr = err
		}

		// Either dial or session failed. Bump attempt, sleep, and
		// loop. We never give up — the only exits from this loop are
		// ctx cancel (user quit) or clean disconnect (radio rebooted).
		attempt++
		backoff := reconnectBackoff(attempt)
		dbgf("retry %d in %s after: %v", attempt, backoff, sessErr)
		p.program.Send(radioReconnectingMsg{
			attempt: attempt,
			after:   backoff,
			err:     sessErr,
		})
		select {
		case <-ctx.Done():
			dbgf("ctx done during backoff — exiting")
			return
		case <-time.After(backoff):
		}
	}
}

// runSession drives one connection's lifetime: kick off the transport
// reader, request the config dump, and forward translated frames to
// Bubble Tea until the transport drops or ctx cancels. Returns
// `established=true` when at least one inbound frame arrived (so the
// caller can reset its retry budget) and the underlying error from
// transport.Run if any.
func (p *pump) runSession(
	ctx context.Context,
	dbgf func(string, ...any),
) (bool, error) {
	inbound := make(chan *pb.FromRadio, 64)

	// Each session runs under a child ctx so cancelling it (e.g. when
	// runSession returns early on ctx.Done) yanks transport.Run out of
	// any blocking read without affecting the parent.
	sessCtx, sessCancel := context.WithCancel(ctx)
	defer sessCancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- p.client.Run(sessCtx, inbound, p.outbound)
	}()
	dbgf("transport.Run goroutine started")

	// Fire the config handshake — prompts the radio to dump its NodeDB,
	// channels, configs, and ConfigComplete. We do this on every
	// (re)connect; the model's dedup logic absorbs the replay.
	nonce := transport.SendWantConfig(p.outbound)
	dbgf("SendWantConfig nonce=0x%08x", nonce)

	totalIn := 0
	for {
		select {
		case <-ctx.Done():
			dbgf("ctx.Done in session")
			return totalIn > 0, nil
		case err := <-runErr:
			if err != nil {
				dbgf("transport.Run returned error: %v", err)
				return totalIn > 0, err
			}
			dbgf("transport.Run returned cleanly (radio disconnect?)")
			return totalIn > 0, nil
		case msg := <-inbound:
			if msg == nil {
				dbgf("inbound nil — skipping")
				continue
			}
			totalIn++
			tms := p.translate(msg)
			if len(tms) == 0 {
				dbgf("[%d] inbound translated to nil (housekeeping)", totalIn)
				continue
			}
			for _, tm := range tms {
				dbgf("[%d] sending %T to tea", totalIn, tm)
				p.program.Send(tm)
			}
		}
	}
}

// openPumpDebugLog opens the pump debug log file when $MESHX_DEBUG
// is set. Value is interpreted as a file path; special value "1"
// expands to /tmp/meshx-pump.log for convenience. Returns nil (no
// error) when the env var is unset — pump.run no-ops its logging
// in that case. Safe to call at process startup; the file is
// opened in append mode so repeated sessions accumulate.
func openPumpDebugLog() *os.File {
	path := os.Getenv("MESHX_DEBUG")
	if path == "" {
		return nil
	}
	if path == "1" {
		path = "/tmp/meshx-pump.log"
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		// Silently fall back to no logging. Anything else (stderr
		// write) would get eaten by the TUI's alt-screen anyway.
		return nil
	}
	return f
}

// translate converts a FromRadio envelope to zero or more tea.Msg
// values, in the order the model should observe them. A single
// FromRadio frame may produce multiple messages — e.g. a NodeInfo
// envelope with an embedded Position fans out to BOTH a
// radioNodeInfoMsg AND a radioPositionMsg, and the NodeInfoMsg has
// to land first so the position update applies to an existing node
// row instead of creating a stub. Returning a slice (instead of
// firing a goroutine for the side-channel msg) keeps that ordering
// deterministic — runSession iterates and Sends in slice order on
// the same goroutine that receives frames, so the NodeInfo always
// reaches Update before the Position. Returns nil/empty for
// housekeeping variants the UI doesn't care about.
func (p *pump) translate(msg *pb.FromRadio) []tea.Msg {
	switch v := msg.GetPayloadVariant().(type) {
	case *pb.FromRadio_MyInfo:
		p.myNum = v.MyInfo.GetMyNodeNum()
		return []tea.Msg{radioMyInfoMsg{nodeNum: p.myNum}}

	case *pb.FromRadio_NodeInfo:
		n := v.NodeInfo
		u := n.GetUser()
		snr := fmt.Sprintf("%.1f", n.GetSnr())
		// NodeInfo doesn't carry RSSI directly in this vendored proto;
		// leave blank — MeshPacket telemetry carries it per-message.
		rssi := ""

		out := []tea.Msg{radioNodeInfoMsg{
			nodeNum:     n.GetNum(),
			longName:    u.GetLongName(),
			shortName:   u.GetShortName(),
			hwModel:     transport.HwModelName(int(u.GetHwModel())),
			snr:         snr,
			rssi:        rssi,
			hops:        0,
			lastHeardAt: time.Unix(int64(n.GetLastHeard()), 0),
		}}
		// NodeInfo.Position is populated for peers whose radios
		// broadcast their location. Append a radioPositionMsg AFTER
		// the NodeInfoMsg so the model has already created the node
		// row by the time the position update applies. Zero lat+lon
		// → no fix, skip.
		if pos := n.GetPosition(); pos != nil &&
			(pos.GetLatitudeI() != 0 || pos.GetLongitudeI() != 0) {
			out = append(out, radioPositionMsg{
				fromNodeNum: n.GetNum(),
				latitude:    float64(pos.GetLatitudeI()) / 1e7,
				longitude:   float64(pos.GetLongitudeI()) / 1e7,
				altitude:    pos.GetAltitude(),
				at:          time.Unix(int64(pos.GetTime()), 0),
			})
		}
		return out

	case *pb.FromRadio_Channel:
		s := v.Channel.GetSettings()
		return []tea.Msg{radioChannelMsg{
			index:  int(v.Channel.GetIndex()),
			name:   s.GetName(),
			role:   v.Channel.GetRole().String(),
			hasPSK: len(s.GetPsk()) > 0,
		}}

	case *pb.FromRadio_Packet:
		p := v.Packet
		dec := p.GetDecoded()
		if dec == nil {
			return nil
		}
		switch dec.GetPortnum() {
		case pb.PortNum_TEXT_MESSAGE_APP:
			return []tea.Msg{radioTextMsg{
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
				return []tea.Msg{radioDeviceMetricsMsg{
					fromNodeNum:  p.GetFrom(),
					batteryLevel: v.DeviceMetrics.GetBatteryLevel(),
					voltage:      v.DeviceMetrics.GetVoltage(),
					channelUtil:  v.DeviceMetrics.GetChannelUtilization(),
					airUtilTx:    v.DeviceMetrics.GetAirUtilTx(),
				}}
			case *pb.Telemetry_EnvironmentMetrics:
				if v == nil || v.EnvironmentMetrics == nil {
					return nil
				}
				return []tea.Msg{radioEnvMetricsMsg{
					fromNodeNum: p.GetFrom(),
					temperature: v.EnvironmentMetrics.GetTemperature(),
					humidity:    v.EnvironmentMetrics.GetRelativeHumidity(),
					pressure:    v.EnvironmentMetrics.GetBarometricPressure(),
					gas:         v.EnvironmentMetrics.GetGasResistance(),
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
			return []tea.Msg{radioPositionMsg{
				fromNodeNum: p.GetFrom(),
				latitude:    float64(pos.GetLatitudeI()) / 1e7,
				longitude:   float64(pos.GetLongitudeI()) / 1e7,
				altitude:    pos.GetAltitude(),
				at:          time.Unix(int64(pos.GetTime()), 0),
			}}
		case pb.PortNum_NODEINFO_APP:
			// Live NodeInfo broadcast — a peer announcing their User
			// (longname + shortname + hw). The FromRadio_NodeInfo
			// envelope handles config-time NodeDB dumps; THIS is how
			// we pick up NodeInfo updates that arrive mid-session.
			// Without this case we'd stay stuck on "node 0x…" for
			// any peer whose text packet arrived before the radio
			// happened to see their NodeInfo.
			u := &pb.User{}
			if err := proto.Unmarshal(dec.GetPayload(), u); err != nil {
				return nil
			}
			return []tea.Msg{radioNodeInfoMsg{
				nodeNum:     p.GetFrom(),
				longName:    u.GetLongName(),
				shortName:   u.GetShortName(),
				hwModel:     transport.HwModelName(int(u.GetHwModel())),
				snr:         fmt.Sprintf("%.1f", p.GetRxSnr()),
				rssi:        fmt.Sprintf("%d", p.GetRxRssi()),
				hops:        int(p.GetHopStart()) - int(p.GetHopLimit()),
				lastHeardAt: time.Unix(int64(p.GetRxTime()), 0),
			}}
		case pb.PortNum_TRACEROUTE_APP:
			// Reply to a /tr request. Payload is a RouteDiscovery
			// proto whose Route is the ordered list of node nums the
			// packet traversed (intermediate hops only; the source
			// and dest are implicit at MeshPacket.From / .To).
			// request_id correlates back to the outbound packetID we
			// stashed in m.pendingTraceroute. Foreign traceroutes
			// (replies to someone else's request) silently drop in
			// the UI handler because their request_id won't match.
			rd := &pb.RouteDiscovery{}
			if err := proto.Unmarshal(dec.GetPayload(), rd); err != nil {
				return nil
			}
			return []tea.Msg{radioTracerouteMsg{
				requestID: dec.GetRequestId(),
				fromNum:   p.GetFrom(),
				toNum:     p.GetTo(),
				route:     append([]uint32(nil), rd.GetRoute()...),
				at:        time.Unix(int64(p.GetRxTime()), 0),
			}}
		case pb.PortNum_ROUTING_APP:
			// Routing payload carries the radio's verdict on a packet
			// we (or someone else) sent. For our own outbound: the
			// MeshPacket's request_id == our stashed packetID and the
			// Routing.error_reason says NONE (ack) or a failure code
			// (TIMEOUT, MAX_RETRANSMIT, ...). For foreign packets the
			// request_id won't match any of ours so the UI handler
			// simply drops it.
			r := &pb.Routing{}
			if err := proto.Unmarshal(dec.GetPayload(), r); err != nil {
				return nil
			}
			reason := r.GetErrorReason().String()
			return []tea.Msg{radioRoutingMsg{
				requestID: dec.GetRequestId(),
				errorName: reason,
				ok:        reason == "NONE",
			}}
		}
		return nil

	case *pb.FromRadio_Metadata:
		md := v.Metadata
		return []tea.Msg{radioMetadataMsg{
			firmwareVersion: md.GetFirmwareVersion(),
			deviceStateVer:  md.GetDeviceStateVersion(),
			hasWifi:         md.GetHasWifi(),
			hasBluetooth:    md.GetHasBluetooth(),
		}}

	case *pb.FromRadio_Config:
		switch c := v.Config.GetPayloadVariant().(type) {
		case *pb.Config_Lora:
			if c == nil || c.Lora == nil {
				return nil
			}
			return []tea.Msg{radioLoraConfigMsg{
				txPowerDBm:  c.Lora.GetTxPower(),
				region:      c.Lora.GetRegion().String(),
				modemPreset: c.Lora.GetModemPreset().String(),
			}}
		case *pb.Config_Device:
			if c == nil || c.Device == nil {
				return nil
			}
			return []tea.Msg{radioDeviceConfigMsg{role: c.Device.GetRole().String()}}
		}
		return nil

	case *pb.FromRadio_ConfigCompleteId:
		return []tea.Msg{radioConfigCompleteMsg{}}
	}
	// ModuleConfig and other variants — ignore.
	return nil
}

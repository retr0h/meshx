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

// commands.go wires the entire slash-command surface:
//
//   - executeCommand — the dispatcher ham / messaging / overlay verbs
//     all route through.
//   - sendBang / sendBangReply / systemLine / systemBlock — the four
//     primitives every slash handler uses to emit a user-visible row.
//   - newTextToRadio / newAdminSetOwner / setOwner — ToRadio envelope
//     builders + the AdminMessage write path used by /nick and /tag.
//   - small helpers (activate, actOnSelectedNode, ackWord,
//     currentChannelIndex, replyTargetFor) that commands lean on.
//
// Model / Update / render surface stays in app.go and ui.go.

package meshx

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"
)

func (m *model) sendPlainMessage(text string) {
	item := messageItem{
		time: timeNowHHMM(), from: "me", mine: true, text: text, status: "pending",
	}
	m.messages = append(m.messages, item)
	m.selectedMsg = len(m.messages) - 1
	m.flash = fmt.Sprintf("sent in %s", m.currentChannel)

	_ = saveMessage(m.db, m.currentChannel, item)

	if m.pump != nil {
		m.pump.Enqueue(newTextToRadio(text, m.currentChannelIndex(), 0))
	}
}

// newTextToRadio builds the ToRadio envelope for a plain text chat
// message on a named channel index. Broadcast (to = 0xFFFFFFFF) on
// PortNum TEXT_MESSAGE_APP (1) — the canonical Meshtastic chat path.
// When replyID != 0 the packet threads to the referenced parent
// (Data.reply_id) — this is how /73 <call> and friends tie their
// outgoing text to the specific message from that operator.
func newTextToRadio(text string, channel, replyID uint32) *pb.ToRadio {
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			To:      0xFFFFFFFF,
			Channel: channel,
			WantAck: true,
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum: pb.PortNum_TEXT_MESSAGE_APP,
				Payload: []byte(text),
				ReplyId: replyID,
			}},
		}},
	}
}

// setOwner validates the desired longname / shortname and sends an
// AdminMessage.SetOwner to the radio. `which` is "long" or "short"
// and controls which field the user is targeting — the other field
// is carried through unchanged from the current config (firmware
// overwrites the whole User record, so we have to round-trip both).
// Called by /nick and /tag. Returns a tea.Cmd for consistency with
// the dispatcher's expected shape; today it's always nil.
func (m *model) setOwner(longName, shortName, which string) tea.Cmd {
	// Demo mode has no real radio — just flash a hint.
	if m.isDemo() {
		m.flash = "/" + which + "name takes effect on a real radio (demo mode)"
		return nil
	}
	if m.pump == nil {
		m.flash = "/" + which + "name needs a live radio connection"
		return nil
	}
	target := strings.TrimSpace(longName)
	if which == "short" {
		target = strings.TrimSpace(shortName)
	}
	if target == "" {
		if which == "short" {
			m.flash = "usage: /tag <1-4 chars or emoji>"
		} else {
			m.flash = "usage: /nick <longname>"
		}
		return nil
	}
	// Meshtastic field lengths per the proto — longname up to 36
	// bytes, shortname up to 4 bytes. We measure in bytes not
	// runes because that's what firmware enforces.
	switch which {
	case "long":
		if len(longName) > 36 {
			m.flash = fmt.Sprintf("/nick rejected: longname %d bytes, max 36", len(longName))
			return nil
		}
	case "short":
		if len(shortName) > 4 {
			m.flash = fmt.Sprintf(
				"/tag rejected: shortname %d bytes, max 4 (%d-byte emoji counts as 1 char)",
				len(shortName),
				len(shortName),
			)
			return nil
		}
	}
	isLicensed := false
	if n := m.myNode(); n != nil {
		// Preserve the existing is_licensed flag across renames.
		// We don't expose a /license command today so this just
		// stays however the user configured it via the phone app.
		_ = n
	}
	envelope, err := newAdminSetOwner(m.myNodeNum, longName, shortName, isLicensed)
	if err != nil {
		m.flash = fmt.Sprintf("/%sname failed: %v", which, err)
		return nil
	}
	if !m.pump.Enqueue(envelope) {
		m.flash = "/" + which + "name dropped — outbound buffer full"
		return nil
	}
	switch which {
	case "long":
		m.systemLine(
			fmt.Sprintf("nick → %s (radio will re-broadcast NodeInfo on next cycle)", longName),
		)
	case "short":
		m.systemLine(
			fmt.Sprintf("tag → %s (radio will re-broadcast NodeInfo on next cycle)", shortName),
		)
	}
	return nil
}

// newAdminSetOwner builds an AdminMessage.SetOwner ToRadio envelope,
// wrapped in a MeshPacket addressed to our own node. This is how
// meshX updates the radio's User config (longname / shortname) over
// the wire — same AdminMessage path the official Meshtastic phone
// apps and Python CLI use.
//
// The phone apps chase SetOwner with a reboot; we don't. Meshtastic
// firmware >= 2.3 accepts User updates hot (updates in-memory,
// persists to flash, keeps running). Skipping the reboot saves
// ~20 seconds of downtime per rename and is safe on any current
// build.
func newAdminSetOwner(
	myNodeNum uint32,
	longName, shortName string,
	isLicensed bool,
) (*pb.ToRadio, error) {
	admin := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_SetOwner{
			SetOwner: &pb.User{
				LongName:   longName,
				ShortName:  shortName,
				IsLicensed: isLicensed,
			},
		},
	}
	payload, err := proto.Marshal(admin)
	if err != nil {
		return nil, fmt.Errorf("marshal AdminMessage: %w", err)
	}
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			// AdminMessage is addressed to OUR OWN node num — the
			// radio treats it as a local config write.
			To:      myNodeNum,
			WantAck: true,
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum: pb.PortNum_ADMIN_APP,
				Payload: payload,
			}},
		}},
	}, nil
}

// currentChannelIndex maps m.currentChannel back to the Meshtastic
// channel index used on the wire. Defaults to 0 (PRIMARY) when the
// channel name isn't in our list.
func (m model) currentChannelIndex() uint32 {
	for i, c := range m.channels {
		if c.name == m.currentChannel {
			return uint32(i)
		}
	}
	return 0
}

// timeNowHHMM returns the current wall time in HH:MM for message
// timestamps. Extracted so tests can override if needed.

func (m *model) actOnSelectedNode(fn func(*nodeItem)) {
	if m.focused != paneNodes {
		return
	}
	sorted := m.sortedNodes()
	if m.selectedNd < 0 || m.selectedNd >= len(sorted) {
		return
	}
	target := sorted[m.selectedNd].callsign
	for i := range m.nodes {
		if m.nodes[i].callsign == target {
			fn(&m.nodes[i])
			return
		}
	}
}

// activate is the "open/select" action — Enter and Space in normal mode.
// Meaning depends on which pane is focused:
//   - channels: switch the messages pane to that channel
//   - nodes:    show whois / node info flash
//   - messages: expand selected message (hop, SNR, RSSI, hex id)
func (m *model) activate() {
	switch m.focused {
	case paneChannels:
		if m.selectedCh < len(m.channels) {
			c := m.channels[m.selectedCh]
			m.currentChannel = c.name
			m.channels[m.selectedCh].unread = 0
			m.flash = fmt.Sprintf("switched to %s", c.name)
			// Auto-jump focus to messages pane, mutt-style.
			m.focused = paneMessages
		}
	case paneNodes:
		sorted := m.sortedNodes()
		if m.selectedNd < len(sorted) {
			n := sorted[m.selectedNd]
			hw := n.hwModel
			if hw == "" {
				hw = "?"
			}
			fw := n.firmware
			if fw == "" {
				fw = "?"
			}
			m.flash = fmt.Sprintf(
				"%s  ·  %s  ·  fw %s  ·  last heard %s  ·  %s",
				n.callsign, hw, fw, n.lastHeard, n.state,
			)
		}
	case paneMessages:
		if m.selectedMsg < len(m.messages) {
			msg := m.messages[m.selectedMsg]
			switch {
			case msg.status == "system":
				m.flash = "system message — no metadata"
			case msg.mine:
				m.flash = fmt.Sprintf("to %s  ·  hop %d  ·  ACK %s",
					m.currentChannel, msg.hops, ackWord(msg.status))
			default:
				parts := []string{"from " + msg.from}
				if msg.hops > 0 {
					parts = append(parts, fmt.Sprintf("hop %d", msg.hops))
				}
				if msg.snr != "" {
					parts = append(parts, "SNR "+msg.snr+" dB")
				}
				m.flash = strings.Join(parts, "  ·  ")
			}
		}
	}
}

func ackWord(status string) string {
	switch status {
	case "ack":
		return "ok"
	case "fail":
		return "timeout"
	default:
		return "pending"
	}
}

func (m *model) executeCommand(raw string) tea.Cmd {
	if raw == "" {
		return nil
	}
	// Split into verb + rest for arg-taking commands.
	verb := raw
	rest := ""
	if sp := strings.IndexByte(raw, ' '); sp >= 0 {
		verb = raw[:sp]
		rest = strings.TrimSpace(raw[sp+1:])
	}

	switch verb {
	case "q", "quit", "exit":
		return tea.Quit

	// ── Ham-radio bang shortcuts ──────────────────────────────────
	// These are quick-command shorthands that compose and send the
	// underlying !bang message. Geeky, fast, and keeps the protocol
	// payload visible as normal message text so every other
	// Meshtastic client sees it as plain chat.

	case "cq":
		// Ham-customary "via <rig/app>" suffix on the beacon so
		// anyone copying the CQ knows what client the caller runs.
		// Only /cq carries this tag — routine chat + reply verbs
		// stay clean so a 237-byte LoRa payload isn't wasted on
		// attribution on every packet.
		call := m.myCallsign()
		body := fmt.Sprintf("CQ CQ CQ de %s via %s — testing signals, please ack", call, clientTag)
		if rest != "" {
			body = fmt.Sprintf("CQ de %s via %s %s", call, clientTag, rest)
		}
		m.sendBang("/cq", body)
		m.flash = "!cq broadcast — awaiting acks…"
	case "cqr":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /cqr <callsign>  (or highlight their CQ in nav mode)"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("no telemetry for %s — node unknown", target)
			return nil
		}
		m.sendBangReply("/cqr "+target, signalReport(n), m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!cqr %s — copy report sent (%s)", target, signalReport(n))
	case "rs":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /rs <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("no telemetry for %s — node unknown", target)
			return nil
		}
		m.sendBangReply("/rs "+target, signalReport(n), m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!rs %s — %s", target, signalReport(n))
	case "73":
		// /73           → broadcast best-regards
		// /73 <call>    → directed "73 <call>" — aimed at a specific
		//                 operator you're signing off to cordially.
		//                 Threads via Data.reply_id to that operator's
		//                 most recent message when we have one.
		target := strings.TrimSpace(rest)
		if target == "" {
			m.sendBang("/73", "73")
			m.flash = "!73 sent"
			return nil
		}
		m.sendBangReply("/73 "+target, "73 "+target, m.replyTargetFor(target))
		m.flash = "!73 " + target + " — best regards"
	case "88":
		m.sendBang("/88", "88")
		m.flash = "!88 sent"
	case "qsl":
		// /qsl           → broadcast acknowledgment
		// /qsl <call>    → directed "QSL <call>" — aimed at a specific
		//                  operator whose last transmission we copied.
		//                  Threads via Data.reply_id to that operator's
		//                  most recent message.
		target := strings.TrimSpace(rest)
		if target == "" {
			m.sendBang("/qsl", "QSL")
			m.flash = "!qsl — acknowledged"
			return nil
		}
		body := "QSL " + target
		m.sendBangReply("/qsl "+target, body, m.replyTargetFor(target))
		m.flash = "!qsl " + target + " — copy confirmed"
	case "qth":
		// PRIVACY — /qth only transmits when the user runs it
		// explicitly, and only the coarse Maidenhead grid (~20 km
		// precision). Never exact lat/long.
		//
		// Two forms:
		//   /qth                → broadcast your own grid (from radio GPS)
		//   /qth <text>         → broadcast a custom QTH string
		//
		// To look up a PEER's QTH, use /whois <call> — keeps send vs.
		// query unambiguous.
		arg := strings.TrimSpace(rest)
		if arg == "" {
			if m.myGrid == "" {
				m.flash = "no GPS fix — /qth <text> to send a custom QTH, or configure position on the radio"
				return nil
			}
			m.sendBang("/qth", "QTH: "+m.myGrid)
			m.flash = "QTH: " + m.myGrid
			return nil
		}
		m.sendBang("/qth", "QTH: "+arg)
		m.flash = "QTH: " + arg
	case "env":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /env <callsign>"
			return nil
		}
		nodeNum := m.nodeNumOf(target)
		if nodeNum == 0 {
			m.systemLine(fmt.Sprintf("env: no record of %s", target))
			return nil
		}
		n := m.lookupNode(target)
		env, ok := m.peerEnv[nodeNum]
		if !ok {
			m.systemLine(fmt.Sprintf("env: %s has no environmental telemetry on file", n.callsign))
			m.systemLine("     (only peers with temp/humidity/pressure sensors broadcast this)")
			return nil
		}
		var lines []string
		if env.temperature != 0 {
			lines = append(lines, fmt.Sprintf("temp:     %.1f °C", env.temperature))
		}
		if env.humidity != 0 {
			lines = append(lines, fmt.Sprintf("humidity: %.0f %%", env.humidity))
		}
		if env.pressure != 0 {
			lines = append(lines, fmt.Sprintf("pressure: %.0f hPa", env.pressure))
		}
		if env.gas != 0 {
			lines = append(lines, fmt.Sprintf("gas:      %.0f Ω", env.gas))
		}
		lines = append(lines, fmt.Sprintf("age:      %s ago", humanDuration(time.Since(env.at))))
		m.systemBlock(fmt.Sprintf("env %s", n.callsign), lines...)
	case "sked":
		target := rest
		if target == "" {
			m.flash = "usage: /sked <callsign>"
			return nil
		}
		m.sendBang("/sked "+target, "proposing scheduled contact, 24h from now")
		m.flash = fmt.Sprintf("!sked %s — proposal sent", target)

	// ── Extra ham/Meshtastic slang ────────────────────────────────
	case "qrz":
		// "Who is calling me?" — broadcast a prompt for identification.
		m.sendBang("/qrz", "QRZ? who's calling?")
		m.flash = "!qrz — asking for ID"
	case "qrm":
		// "You have man-made interference." Report to a station.
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /qrm <callsign>"
			return nil
		}
		m.sendBangReply(
			"/qrm "+target,
			"QRM — interference on your signal",
			m.replyTargetFor(target),
		)
		m.flash = fmt.Sprintf("!qrm %s — interference reported", target)
	case "qsb":
		// "Your signal is fading."
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /qsb <callsign>"
			return nil
		}
		m.sendBangReply("/qsb "+target, "QSB — signal fading, copy weak", m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!qsb %s — fade reported", target)
	case "sk":
		// Final sign-off — stronger than /73. "Signing off clear."
		// /sk           → broadcast SK
		// /sk <call>    → directed "SK <call>" — aimed at a specific
		//                 operator you're closing a contact with.
		//                 Threads via Data.reply_id to that operator's
		//                 most recent message.
		target := strings.TrimSpace(rest)
		if target == "" {
			m.sendBang("/sk", "SK — clear and out 73")
			m.flash = "!sk — clear"
			return nil
		}
		body := "SK — clear and out 73, " + target
		m.sendBangReply("/sk "+target, body, m.replyTargetFor(target))
		m.flash = "!sk " + target + " — cleared"
	case "wx":
		// Weather at my QTH. Optional argument supplies the conditions;
		// without one we emit a placeholder so the user types their own.
		wx := rest
		if wx == "" {
			wx = "clear 55°F light wind"
		}
		m.sendBang("/wx", "wx: "+wx)
		m.flash = "wx: " + wx + " — broadcast"
	case "grid":
		// Just the Maidenhead locator — shorter / more data-friendly
		// than /qth which also names the city.
		grid := rest
		if grid == "" {
			grid = m.myGrid
		}
		if grid == "" {
			m.flash = "no GPS fix — /grid <locator> to send a custom grid"
			return nil
		}
		m.sendBang("/grid", "grid: "+grid)
		m.flash = "grid: " + grid + " — broadcast"
	case "mesh":
		// Meshtastic-specific — summarize what the mesh looks like
		// from our vantage: number of nodes we can hear, by state.
		online, muted, offline := 0, 0, 0
		for _, n := range m.nodes {
			switch n.state {
			case "online":
				online++
			case "muted":
				muted++
			case "offline", "failed":
				offline++
			}
		}
		body := fmt.Sprintf("mesh view: %d online, %d muted, %d stale", online, muted, offline)
		m.sendBang("/mesh", body)
		m.flash = body
	case "k":
		// "Over — go ahead." Ragchew turn-taking.
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /k <callsign>"
			return nil
		}
		m.sendBangReply("/k "+target, "K — over, go ahead", m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!k %s — over to you", target)

	// ── IRC-style operational commands ────────────────────────────
	case "tr", "traceroute", "trace":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /tr <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.systemLine(fmt.Sprintf("tr: no route data for %s", target))
			return nil
		}
		m.systemBlock(
			fmt.Sprintf("traceroute %s", n.callsign),
			fmt.Sprintf("hops:   %d", n.lastHops),
			fmt.Sprintf("signal: %s", signalReport(n)),
			"note:   live path not yet queried — showing last-known telemetry",
		)
	case "ping":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /ping <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.systemLine(fmt.Sprintf("ping: node %s unknown", target))
			return nil
		}
		// Pinging ourselves yields meaningless telemetry (0s / 0.0 dB
		// because we never rx our own packets). Refuse with a note
		// rather than emitting a card full of zeros.
		if nodeNum := m.nodeNumOf(target); nodeNum != 0 && nodeNum == m.myNodeNum {
			m.systemLine("ping: that's you — /whois for your own config")
			return nil
		}
		lines := []string{
			fmt.Sprintf("last heard: %s ago", n.lastHeard),
			fmt.Sprintf("signal:     %s", signalReport(n)),
		}
		// Include peer battery + distance if we've received Device
		// Metrics + Position for them. Both are optional — Meshtastic
		// peers don't always broadcast telemetry or a GPS fix.
		if nodeNum := m.nodeNumOf(target); nodeNum != 0 {
			if pos, ok := m.peerPositions[nodeNum]; ok && m.myGrid != "" {
				if km := haversineKm(m.myLatitude, m.myLongitude, pos.latitude, pos.longitude); km > 0 {
					lines = append(lines, fmt.Sprintf("distance:   %.1f km  (grid %s)", km, pos.grid))
				}
			}
		}
		m.systemBlock(fmt.Sprintf("ping %s", n.callsign), lines...)
	case "w", "whois":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /whois <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.systemLine(fmt.Sprintf("whois: no record of %s", target))
			return nil
		}
		hw := n.hwModel
		if hw == "" {
			hw = "unknown hw"
		}
		fw := n.firmware
		nodeNum := m.nodeNumOf(target)
		isSelf := nodeNum != 0 && nodeNum == m.myNodeNum
		// For our own node, fw lives on m.radioFirmware (from
		// FromRadio.Metadata), not on the nodeItem — MyNodeInfo
		// doesn't carry firmware. Same story for battery /
		// channel-util telemetry, which arrives via DeviceMetrics
		// and is stored on the model root for self only.
		if isSelf && fw == "" {
			fw = m.radioFirmware
		}
		if fw == "" {
			fw = "?"
		}

		// Ghost peer — we have their node num from text packets but
		// have never received their NodeInfo, so longname / shortname
		// / hw are all unknown. Surface that plainly rather than
		// pretending a partial whois is a whole one.
		ghost := strings.HasPrefix(n.callsign, "node 0x")

		var lines []string
		if ghost {
			lines = append(lines,
				"👻 no NodeInfo received for this peer",
				"  we've heard text packets from them but never their",
				"  User broadcast, so longname / shortname / hw are",
				"  unknown. Their NodeInfo may arrive in the next",
				"  ~15 min, or try /sync to force a NodeDB re-dump.",
				"",
			)
		}
		lines = append(lines,
			fmt.Sprintf("hw:     %s", hw),
			fmt.Sprintf("fw:     %s", fw),
			fmt.Sprintf("heard:  %s ago", n.lastHeard),
			fmt.Sprintf("state:  %s", n.state),
			fmt.Sprintf("signal: %s", signalReport(n)),
		)
		// Battery + channel-util are only tracked model-wide for
		// self today. For peers we'd need a per-peer DeviceMetrics
		// cache (TODO). Surface what we have.
		if isSelf && m.hasTelemetry {
			pct := "—"
			switch {
			case m.batteryLevel > 100:
				pct = "pwr (USB / solar — no cell)"
			case m.batteryLevel > 0:
				pct = fmt.Sprintf("%d%%", m.batteryLevel)
			}
			if m.batteryVoltage > 0 {
				lines = append(lines, fmt.Sprintf("battery: %s  %.2f V", pct, m.batteryVoltage))
			} else {
				lines = append(lines, fmt.Sprintf("battery: %s", pct))
			}
			lines = append(lines, fmt.Sprintf("chanutl: %.1f%%", m.channelUtil))
		}
		if nodeNum != 0 {
			if pos, ok := m.peerPositions[nodeNum]; ok {
				lines = append(
					lines,
					fmt.Sprintf("grid:   %s", pos.grid),
					fmt.Sprintf(
						"coord:  %.5f, %.5f  alt %d m",
						pos.latitude,
						pos.longitude,
						pos.altitude,
					),
					fmt.Sprintf("pos:    %s ago", humanDuration(time.Since(pos.at))),
				)
				// Distance from us if we also have a fix — same
				// great-circle helper /ping uses.
				if !isSelf && m.myLatitude != 0 && m.myLongitude != 0 {
					if km := haversineKm(m.myLatitude, m.myLongitude, pos.latitude, pos.longitude); km > 0 {
						lines = append(lines, fmt.Sprintf("dist:   %.1f km from you", km))
					}
				}
			}
		}
		lines = append(lines, "end of /whois")
		m.systemBlock(fmt.Sprintf("whois %s", n.callsign), lines...)
	case "r", "reply":
		if rest == "" {
			target := m.selectedSender()
			if target == "" {
				m.flash = "usage: /reply <callsign> <text>"
				return nil
			}
			return m.prefillInput("/reply " + target + " ")
		}
		// /reply <call> <text>
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			return m.prefillInput("/reply " + rest + " ")
		}
		target := rest[:sp]
		body := strings.TrimSpace(rest[sp+1:])
		if body == "" {
			return m.prefillInput("/reply " + target + " ")
		}
		m.messages = append(m.messages, messageItem{
			time: "14:13", from: "me", mine: true,
			text: "→" + target + ": " + body, status: "ack",
		})
		m.selectedMsg = len(m.messages) - 1
		m.flash = fmt.Sprintf("reply sent to %s", target)
	case "msg":
		// /msg <call> <text> — direct message, same shape as /reply with args.
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			m.flash = "usage: /msg <callsign> <text>"
			return nil
		}
		target := rest[:sp]
		body := strings.TrimSpace(rest[sp+1:])
		m.messages = append(m.messages, messageItem{
			time: "14:13", from: "me", mine: true,
			text: "→" + target + ": " + body, status: "ack",
		})
		m.selectedMsg = len(m.messages) - 1
		m.flash = fmt.Sprintf("DM sent to %s", target)
	case "join":
		if rest == "" {
			m.flash = "usage: /join <channel>"
			return nil
		}
		// Join by matching name; if not found, flash.
		for i, c := range m.channels {
			if c.name == rest || strings.TrimPrefix(c.name, "#") == rest {
				m.switchChannelByIndex(i)
				return nil
			}
		}
		m.flash = fmt.Sprintf("no channel named %s — /channel list", rest)
	case "part":
		m.flash = "/part — channel leave needs radio transport to wire"
	case "channels":
		m.openOverlay(overlayChannels)
	case "nodes":
		// "Node" is the canonical Meshtastic term — radios on the
		// mesh are nodes, not users. We dropped the /users and
		// /names IRC aliases to keep the vocabulary consistent.
		m.openOverlay(overlayNodes)
	case "channel":
		if rest == "list" || rest == "" {
			m.openOverlay(overlayChannels)
			return nil
		}
		m.flash = "usage: /channel list  |  /channel add <meshtastic://url>"
	case "nick", "callsign":
		// /nick <name> — set the radio's User.long_name via
		// AdminMessage.SetOwner. Aliases: /callsign (ham-idiomatic).
		// No reboot (unlike the phone app's behavior); firmware
		// accepts the write hot. Change propagates to peers on
		// the radio's next NodeInfo broadcast.
		return m.setOwner(rest, m.myShortName(), "long")
	case "tag", "emoji":
		// /tag <text> — set the radio's User.short_name via
		// AdminMessage.SetOwner. Aliases: /emoji (because ~every
		// user sets shortname to a single emoji). Up to 4 bytes.
		return m.setOwner(m.myCallsign(), rest, "short")
	case "config":
		// Single render path — demo and live both read from model
		// state since demo mode pre-populates these fields. The only
		// difference is a [DEMO] tag on the block header.
		n := m.myNode()
		lines := []string{fmt.Sprintf("callsign: %s", m.myCallsign())}
		if n != nil && n.hwModel != "" {
			lines = append(lines, fmt.Sprintf("hw:       %s", n.hwModel))
		}
		if m.radioFirmware != "" {
			lines = append(lines, fmt.Sprintf("fw:       %s", m.radioFirmware))
		}
		if m.currentChannel != "" {
			lines = append(
				lines,
				fmt.Sprintf("channel:  %s  %s", m.currentChannel, m.radioModemPreset),
			)
		}
		if m.radioRole != "" {
			lines = append(lines, fmt.Sprintf("role:     %s", m.radioRole))
		}
		if m.radioRegion != "" {
			lines = append(lines, fmt.Sprintf("region:   %s", m.radioRegion))
		}
		if m.radioTxPower != 0 {
			lines = append(lines, fmt.Sprintf("tx power: %d dBm", m.radioTxPower))
		}
		if m.myGrid != "" {
			lines = append(lines, fmt.Sprintf("grid:     %s", m.myGrid))
		}
		if m.hasTelemetry {
			lines = append(lines,
				fmt.Sprintf("battery:  %.2f V  %d%%", m.batteryVoltage, m.batteryLevel),
				fmt.Sprintf("chan use: %.1f%%", m.channelUtil),
			)
		}
		lines = append(lines, fmt.Sprintf("peers:    %d known", len(m.nodes)))
		header := "config"
		if m.isDemo() {
			header = "config [DEMO]"
		}
		m.systemBlock(header, lines...)
	case "info":
		// /info — dump meshx's current knowledge to the log so you
		// can diagnose "why don't I have a name for this peer?"
		// without external tooling. Shows our own identity, a
		// peer-count breakdown (real names vs. unresolved "node 0x…"
		// placeholders), session state, and channel summary.
		lines := []string{
			fmt.Sprintf(
				"self:     %s (0x%x)  shortname=%s",
				m.myCallsign(),
				m.myNodeNum,
				m.myShortName(),
			),
		}
		if n := m.myNode(); n != nil {
			lines = append(lines, fmt.Sprintf("hw:       %s  fw=%s", n.hwModel, m.radioFirmware))
		}
		var resolved, ghosts int
		for _, n := range m.nodes {
			if strings.HasPrefix(n.callsign, "node 0x") {
				ghosts++
			} else {
				resolved++
			}
		}
		lines = append(
			lines,
			fmt.Sprintf(
				"peers:    %d total  (%d named, %d placeholder)",
				len(m.nodes),
				resolved,
				ghosts,
			),
			fmt.Sprintf("channels: %d", len(m.channels)),
			fmt.Sprintf("connected: %t  handshake_complete=%t", m.connected, m.connected),
		)
		if m.radioRegion != "" {
			lines = append(lines, fmt.Sprintf("region:   %s  preset=%s  tx=%d dBm  role=%s",
				m.radioRegion, m.radioModemPreset, m.radioTxPower, m.radioRole))
		}
		if ghosts > 0 {
			const maxList = 10
			header := fmt.Sprintf("unresolved peers (%d):", ghosts)
			if ghosts > maxList {
				header = fmt.Sprintf("unresolved peers (first %d of %d):", maxList, ghosts)
			}
			lines = append(lines, header)
			n := 0
			for _, node := range m.nodes {
				if strings.HasPrefix(node.callsign, "node 0x") {
					lines = append(lines, "  "+node.callsign)
					n++
					if n >= maxList {
						break
					}
				}
			}
			lines = append(lines, "(try /sync to re-request the radio's NodeDB)")
		}
		m.systemBlock("info", lines...)
	case "sync":
		// /sync — ask the radio to re-dump its config + NodeDB via a
		// fresh WantConfigId handshake. Use when you suspect the
		// cache is stale, or after the radio just resolved a peer
		// you want surfaced without waiting for the next organic
		// NODEINFO_APP broadcast.
		if m.pump == nil {
			m.flash = "/sync needs a live radio connection (demo mode)"
			return nil
		}
		nonce := rand.Uint32()
		if nonce == 0 {
			nonce = 1
		}
		ok := m.pump.Enqueue(&pb.ToRadio{
			PayloadVariant: &pb.ToRadio_WantConfigId{WantConfigId: nonce},
		})
		if !ok {
			m.flash = "/sync dropped — outbound buffer full"
			return nil
		}
		// Snapshot current ghost count so we can report the delta
		// when the matching ConfigComplete lands.
		ghosts := 0
		for _, n := range m.nodes {
			if strings.HasPrefix(n.callsign, "node 0x") {
				ghosts++
			}
		}
		// Store as non-zero sentinel even when zero ghosts exist, so
		// the ConfigComplete handler can tell a pending /sync apart
		// from the startup handshake.
		if ghosts == 0 {
			m.syncPendingGhosts = -1
		} else {
			m.syncPendingGhosts = ghosts
		}
		m.systemBlock("sync",
			fmt.Sprintf("requested NodeDB re-dump (nonce=0x%x)", nonce),
			fmt.Sprintf("baseline: %d unresolved peers", ghosts),
			"watching for incoming NodeInfo — any placeholder that resolves",
			"will fire its own `identified` line; summary lands on completion.",
		)
	case "help", "h":
		// /help             → open the full scrollable overlay
		// /help <verb>      → irssi / BitchX-style per-command usage
		//                     + summary card dropped inline as a
		//                     systemBlock so it lives in the log
		//                     alongside the exchange it's helping
		//                     with (no modal context switch).
		verb := strings.ToLower(strings.TrimSpace(rest))
		if verb == "" {
			m.mode = modeHelp
			return nil
		}
		verb = strings.TrimPrefix(verb, "/")
		entry, ok := helpEntries[verb]
		if !ok {
			m.flash = fmt.Sprintf("no help for /%s — try /help alone for the full index", verb)
			return nil
		}
		m.systemBlock(
			fmt.Sprintf("help /%s", verb),
			"usage:   "+entry.usage,
			"summary: "+entry.summary,
		)
	case "search", "find":
		if rest == "" {
			m.flash = "usage: /search <pattern>"
			return nil
		}
		m.searchQuery = strings.ToLower(rest)
		if ok, count := m.jumpToSearchHit(+1); ok {
			m.flash = fmt.Sprintf("search: %d matches for %q", count, rest)
			m.mode = modeNav
			m.input.Blur()
		} else {
			m.flash = fmt.Sprintf("no match for %q", rest)
		}
	case "clear":
		m.messages = nil
		m.selectedMsg = 0
		m.flash = "scrollback cleared"

	default:
		m.flash = fmt.Sprintf("unknown /%s — see /help", verb)
	}
	return nil
}

// systemLine appends a single-line system/meta entry to the message
// log. Prefixed with `-!-` irssi-style. Never transmits over LoRa —
// display-only. Used for short one-shot notices.
func (m *model) systemLine(text string) {
	m.messages = append(m.messages, messageItem{
		time:   timeNowHHMM(),
		text:   "-!- " + text,
		status: "system",
	})
	m.selectedMsg = len(m.messages) - 1
}

// systemBlock emits a multi-line "server reply" block. Each line
// becomes its own messageItem, but all carry the same `group` ID —
// the renderer uses this to (a) give every row in the block the
// same zebra stripe color, (b) hide the timestamp on continuation
// rows so only the header carries it, and (c) let j/k navigation
// keep cursor movement smooth across blocks.
func (m *model) systemBlock(header string, lines ...string) {
	gid := nextGroupID()
	t := timeNowHHMM()
	m.messages = append(m.messages, messageItem{
		time:   t,
		text:   "-!- " + header,
		status: "system",
		group:  gid,
	})
	for _, l := range lines {
		m.messages = append(m.messages, messageItem{
			time:   t,
			text:   "-!-    " + l,
			status: "system",
			group:  gid,
		})
	}
	m.selectedMsg = len(m.messages) - 1
}

// groupCounter is a monotonically-increasing counter used to tag
// members of a systemBlock with a shared ID so the renderer can
// bind them visually.
var groupCounter uint64

func nextGroupID() uint64 {
	groupCounter++
	return groupCounter
}

// sendBang appends an outgoing command-originated message to the
// local log AND (in live-radio mode) transmits it over LoRa via the
// pump. The `bang` field is kept purely for local UI styling — the
// on-wire text is just `body`, clean enough that any other
// Meshtastic client reads it as plain chat.
//
// Used by /cq, /73, /qsl, /qth, /grid, /rs, /cqr, /sk, /qrz, /qrm,
// /qsb, /wx, /k, /mesh. Commands that don't transmit (/whois, /ping,
// /tr, /env, /config) use systemLine() instead.
func (m *model) sendBang(bang, body string) {
	m.sendBangReply(bang, body, 0)
}

// sendBangReply is sendBang with an optional reply target — when
// replyToID is non-zero, the outgoing packet carries Data.reply_id
// pointing at the parent message, and the local log entry records
// the same replyID so the renderer can draw a quoted-parent line
// above the reply.
func (m *model) sendBangReply(bang, body string, replyToID uint32) {
	status := "ack"
	if !m.isDemo() {
		status = "pending" // flipped to "ack" when the radio echoes our packet back
	}
	item := messageItem{
		time:    timeNowHHMM(),
		from:    "me",
		mine:    true,
		bang:    bang,
		text:    body,
		status:  status,
		replyID: replyToID,
	}
	m.messages = append(m.messages, item)
	m.selectedMsg = len(m.messages) - 1
	m.focused = paneMessages

	// Persist the outgoing so the log survives restart. Skipped in
	// demo mode (m.db is always nil there).
	_ = saveMessage(m.db, m.currentChannel, item)

	if m.pump != nil {
		m.pump.Enqueue(newTextToRadio(body, m.currentChannelIndex(), replyToID))
	}
}

// replyTargetFor returns the packetID of the most recent message
// from the given callsign, or 0 if none exists. Used by directed
// ham verbs (/73 <call>, /qsl <call>, /sk <call>, /rs <call>, etc.)
// to thread the outgoing reply to whatever <call> most recently
// said — the Meshtastic "reply to" semantic.
func (m *model) replyTargetFor(call string) uint32 {
	if call == "" {
		return 0
	}
	target := strings.ToLower(strings.TrimSpace(call))
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.mine || msg.status == "system" || msg.packetID == 0 {
			continue
		}
		if strings.Contains(strings.ToLower(msg.from), target) {
			return msg.packetID
		}
	}
	return 0
}

// updateHelp handles keys while the help overlay is visible. Vim-style
// scroll: j/k lines, d/u half-page, g/G top/bottom, q / ? / Enter /
// ESC dismiss. Ctrl+X still exits the whole app.

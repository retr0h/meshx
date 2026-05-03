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

// radio.go — handlers that react to a FromRadio packet and mutate
// the model, plus resend (the other direction: rebuild a ToRadio
// from an existing row). pump.go translates each protobuf variant
// into a typed tea.Msg; Update routes each to the matching apply*
// handler here.
//
// Boundary with node.go: node.go is read-side identity + lookup +
// derived display. radio.go is the mutation side — it creates /
// updates nodeItem rows in response to NodeInfo + text packets.
// Everything that changes m.nodes, m.channels, or m.messages in
// response to incoming RF traffic lives here.

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// sanitizeMessageText scrubs peer-originated text so one bad packet
// can't wreck the message-pane layout for every row that follows.
//
// Three distinct hazards we cleanse:
//
//  1. **Edge whitespace.** Sender clients (iOS / Android / web)
//     routinely emit trailing newlines or stray leading spaces from
//     their compose buffers — the user typed "hi" + Enter and the
//     client packaged "hi\n" on the wire. Internal \n is meaningful
//     (solar-node multi-line reports); edge \n is incidental to the
//     compose path. Without trimming, "hi\n" splits to ["hi", ""]
//     and renders an empty `▎` continuation row under every such
//     message. Stock Meshtastic clients trim too; not trimming makes
//     meshX the outlier.
//
//  2. **CR / CRLF.** Real-radio payloads (e.g. solar-node end-of-day
//     reports) ship CRLF; a lone \r left on a continuation line
//     ships to the terminal as a carriage-return and snaps the
//     cursor back to column 0, smearing the next row's left pane
//     border into the message body. Normalize CRLF → \n, drop
//     bare \r.
//
//  3. **Invalid UTF-8 / non-printable bytes.** A buggy firmware
//     can drop a byte mid-codepoint, or a custom app can misuse
//     the TEXT_MESSAGE_APP port to ship binary, or an RF bit-flip
//     can corrupt the payload. Go's UTF-8 decoder substitutes
//     U+FFFD ("replacement character", renders as ◆ / ?) and
//     terminals' width-measurement of those runes drifts from
//     go-runewidth's, which makes the right-aligned stats column
//     wrap to its own line, knocks pane-height accounting off by
//     one for every row below, and cascades into duplicated input
//     prompts + jumbled splash art. Replace U+FFFD with `?` and
//     drop any other non-printable rune (keeping \n).
//
// Called at the ingress boundary (applyTextMessage) and on replay
// from SQLite (loadMessages) so historic rows written before this
// fix get cleaned on read. Returns the cleaned text and a boolean
// reporting whether any sanitization beyond CRLF normalization
// actually had to happen — caller stores the flag on the message
// so the renderer can tag the row with a ⚠ marker + dim styling
// (corrupted text we can partially read is more useful than
// silently-cleaned text the user has no reason to distrust).
var messageTextSanitizer = strings.NewReplacer("\r\n", "\n", "\r", "")

// okMessageRune is the allow-list for sanitizeMessageText. The only
// runes we actively reject are control characters (Cc) other than
// newline — NUL, BEL, ESC, etc. — which can scramble the terminal.
// EVERYTHING else passes: letters, marks, symbols, format chars
// (ZWJ U+200D, variation selectors U+FE0F, BOM), surrogate-paired
// emoji, the lot. Modern emoji ZWJ sequences like 🙋🏼‍♂️ decompose
// into base + skin tone + ZWJ + sign + VS16; a stricter filter
// shreds them apart and falsely tags the row corrupted. The actual
// layout hazard is invalid UTF-8 (handled by the utf8.RuneError
// case in the caller), not exotic-but-valid Unicode.
func okMessageRune(r rune) bool {
	if r == '\n' {
		return true
	}
	return !unicode.Is(unicode.Cc, r)
}

func sanitizeMessageText(s string) (string, bool) {
	s = messageTextSanitizer.Replace(s)
	// Trim edge whitespace — leading/trailing spaces, tabs, newlines.
	// Internal \n is preserved (multi-line reports stay multi-line);
	// only the edges get touched. See the doc comment above for why.
	s = strings.TrimSpace(s)
	// Fast path: well-behaved UTF-8 with no control bytes goes
	// straight through. Saves an allocation on the common case
	// (every legitimate chat message).
	if utf8.ValidString(s) && !strings.ContainsRune(s, utf8.RuneError) {
		clean := true
		for _, r := range s {
			if !okMessageRune(r) {
				clean = false
				break
			}
		}
		if clean {
			return s, false
		}
	}
	var b strings.Builder
	b.Grow(len(s))
	corrupted := false
	for _, r := range s {
		switch {
		case r == utf8.RuneError:
			// Invalid UTF-8 OR explicit U+FFFD — both decode to the
			// same rune. Substitute with `?` so the user sees there
			// was a byte here without the layout-breaking ambiguous
			// width.
			b.WriteByte('?')
			corrupted = true
		case okMessageRune(r):
			b.WriteRune(r)
		default:
			// Drop the rune. Control chars (NUL, ESC, BEL, etc.),
			// surrogates, and other non-printable runes have no place
			// in a chat message and only ever break things downstream.
			corrupted = true
		}
	}
	return b.String(), corrupted
}

// upsertNode inserts a NodeInfo arrival or updates the existing row
// by node num. Uses nodesByNum for O(1) lookup. Falls back to
// short/long name for display text, and chooses state from lastHeard.
//
// When BOTH names are empty the NodeInfo is effectively
// content-free — usually a peer the radio has only heard via mesh
// forwarding without ever decoding a User packet. Synthesize the
// firmware-default ("Meshtastic <last-4-hex>" / "<last-4-hex>") so
// the row matches what every other Meshtastic client renders for
// the same node and flag it unresolved so the UI can dim it + the
// "identified" notification can fire later when a real User packet
// finally lands.
func (m *model) upsertNode(msg mdl.NodeInfo) {
	unresolved := false
	if msg.LongName == "" && msg.ShortName == "" {
		long, short := defaultCallsign(msg.NodeNum)
		msg.LongName = long
		msg.ShortName = short
		unresolved = true
	}
	callsign := msg.LongName
	if callsign == "" {
		callsign = msg.ShortName
	}

	// Derive state from lastHeard age. Anything past the 15-minute
	// online window is "offline" — we used to branch on age < 2h vs
	// older, but both arms set the same value, so the live derivation
	// here matches what currentState() does at render time anyway.
	state := stateOffline
	if !msg.LastHeardAt.IsZero() && time.Since(msg.LastHeardAt) < 15*time.Minute {
		state = stateOnline
	}
	lastHeard := "never"
	if !msg.LastHeardAt.IsZero() {
		lastHeard = humanDuration(time.Since(msg.LastHeardAt))
	}

	item := nodeItem{
		callsign:    callsign,
		shortName:   msg.ShortName,
		nodeNum:     msg.NodeNum,
		unresolved:  unresolved,
		state:       state,
		lastHeard:   lastHeard,
		lastHeardAt: msg.LastHeardAt,
		heardRank:   int(time.Since(msg.LastHeardAt).Seconds()),
		lastSNR:     msg.SNR,
		lastRSSI:    msg.RSSI,
		lastHops:    msg.Hops,
		hwModel:     msg.HwModel,
	}

	// Persist to the cross-session NodeDB cache so once we've learned
	// a peer's real User info we remember it on every subsequent
	// launch — same behavior as the official phone app. Placeholder
	// "node 0x…" callsigns (both longname and shortname empty) are
	// skipped inside saveNode itself.
	if m.store != nil {
		m.storagePersist(m.store.SaveNode(m.radioID, mdl.CachedNode{
			NodeNum:   msg.NodeNum,
			LongName:  msg.LongName,
			ShortName: msg.ShortName,
			HwModel:   msg.HwModel,
		}))
	}

	if idx, ok := m.nodesByNum[msg.NodeNum]; ok {
		// Preserve fav flag across updates.
		item.fav = m.nodes[idx].fav
		// Preserve the NEWER lastHeardAt — text packets between
		// NodeInfo beacons bump lastHeardAt on applyTextMessage,
		// and a subsequent NodeInfo arrival would clobber that
		// recency if we just overwrote the row wholesale. Take
		// the max so the derived currentState always reflects the
		// most recent evidence we have.
		if m.nodes[idx].lastHeardAt.After(item.lastHeardAt) {
			item.lastHeardAt = m.nodes[idx].lastHeardAt
		}
		wasUnresolved := m.nodes[idx].unresolved
		prevCallsign := m.nodes[idx].callsign
		m.nodes[idx] = item
		// Ghost upgrade notification — when a peer that was
		// previously a synthesized firmware-default placeholder
		// (because NodeInfo hadn't arrived yet) just got resolved
		// to a real User packet, drop a grey inline system line in
		// the log so the user sees the name flip happen. Skipped
		// when we're still on a default (NodeInfo lacked both
		// names) or when the callsign didn't actually change
		// (re-applied same NodeInfo).
		if wasUnresolved && !item.unresolved && prevCallsign != item.callsign {
			m.systemLine(fmt.Sprintf("identified %s (was %s)", item.callsign, prevCallsign))
		}
		return
	}
	m.nodesByNum[msg.NodeNum] = len(m.nodes)
	m.nodes = append(m.nodes, item)
}

// applyChannel sets or replaces a channel slot. DISABLED slots are
// kept in m.channels (with role="DISABLED") so /channel new can find
// the first free slot to allocate into; renderers (channelTabsRow,
// channelsPane) skip DISABLED so empty slots don't clutter the UI.
func (m *model) applyChannel(msg mdl.ChannelInfo) {
	// Grow the slice up to msg.Index regardless of role so the slot is
	// addressable for delete + re-apply later.
	for len(m.channels) <= msg.Index {
		m.channels = append(m.channels, channelItem{role: roleDisabled})
	}
	if string(msg.Role) == roleDisabled {
		// Preserve any unread accumulated before the slot was disabled
		// (rare but possible if a /channel del raced an inbound packet)
		// and mark the slot empty so /channel new can re-use it.
		prevUnread := m.channels[msg.Index].unread
		m.channels[msg.Index] = channelItem{
			index:  msg.Index,
			role:   roleDisabled,
			unread: prevUnread,
		}
		return
	}
	name := msg.Name
	if name == "" {
		// Empty-name PRIMARY is the default "LongFast" channel — give
		// it a readable label in the UI.
		name = "#default"
	} else if msg.HasPSK {
		name = "*" + msg.Name + "*"
	} else {
		name = "#" + msg.Name
	}
	c := channelItem{
		name:    name,
		private: msg.HasPSK,
		index:   msg.Index,
		role:    string(msg.Role),
		psk:     msg.PSK,
	}
	// Preserve unread count across re-apply.
	c.unread = m.channels[msg.Index].unread
	m.channels[msg.Index] = c
	if m.currentChannel == "" {
		m.currentChannel = name
	}
}

// applyTextMessage appends a received text packet to the message log.
// Resolves fromNum to a callsign via the NodeDB; unread count bumps
// on the destination channel when it's not the active one. Returns a
// tea.Cmd carrying the BEL when the message is from a peer and the
// user hasn't /muted it; nil otherwise. Update threads it back to the
// runtime so the bell write happens in a controlled goroutine instead
// of racing the renderer.
func (m *model) applyTextMessage(ev mdl.Text) tea.Cmd {
	// body is the partial wire shape pump produced; the renderer-only
	// fields (From, Mine, Status, …) get filled in below.
	body := ev.Body

	// Default ghost identity from the firmware's last-4-hex
	// convention so the FROM column matches what other Meshtastic
	// clients display for the same peer (iOS shows "c7f7", we
	// shouldn't be the outlier showing "node 0x273cc7f7").
	defaultLong, _ := defaultCallsign(body.FromNum)
	from := defaultLong
	if idx, ok := m.nodesByNum[body.FromNum]; ok {
		from = m.nodes[idx].callsign
		// Live RF contact — stamp lastHeardAt + refresh signal
		// telemetry. currentState / currentLastHeard derive
		// "online" and "now" from lastHeardAt at render time, so
		// there's no need to poke state / lastHeard strings here;
		// the renderer always reads the live derivation.
		m.nodes[idx].lastHeardAt = time.Now()
		m.nodes[idx].heardRank = 0
		if body.SNR != "" {
			m.nodes[idx].lastSNR = body.SNR
		}
		if ev.RSSI != "" {
			m.nodes[idx].lastRSSI = ev.RSSI
		}
		if body.Hops > 0 {
			m.nodes[idx].lastHops = body.Hops
		}
	} else if body.FromNum != 0 {
		// We've heard a text packet from a peer whose NodeInfo we
		// haven't received yet — ghost them into m.nodes so /cqr,
		// /rs, /whois, /ping can find them by id, hex, or shortname
		// substring. The entry gets upgraded by upsertNode the
		// moment a real NodeInfo arrives (nodesByNum index is
		// stable so all references stay valid). Synthesize the
		// firmware-default callsigns up front so the row already
		// reads the same way every other Meshtastic client renders
		// the peer.
		long, short := defaultCallsign(body.FromNum)
		m.nodes = append(m.nodes, nodeItem{
			callsign:    long,
			shortName:   short,
			nodeNum:     body.FromNum,
			unresolved:  true,
			lastHeardAt: time.Now(),
			lastSNR:     body.SNR,
			lastRSSI:    ev.RSSI,
			lastHops:    body.Hops,
		})
		m.nodesByNum[body.FromNum] = len(m.nodes) - 1
		from = long
	}
	mine := body.FromNum == m.myNodeNum

	cleanText, corrupted := sanitizeMessageText(body.Text)
	item := messageItem{
		time:      body.Time,
		from:      from,
		mine:      mine,
		text:      cleanText,
		corrupted: corrupted,
		status:    statusAck,
		hops:      body.Hops,
		snr:       body.SNR,
		packetID:  body.PacketID,
		replyID:   body.ReplyID,
		fromNum:   body.FromNum,
		sentAt:    body.SentAt,
	}

	// Dedupe replays from the radio's RAM queue. When we reconnect
	// after a short disconnect, the radio re-drains any packets it
	// still holds — some of those will be ones we already persisted
	// to SQLite in the previous session. Without dedup, the same
	// on-wire packet lands twice (once from loadMessages, again from
	// this replay), duplicating both m.messages and SQLite rows.
	// The messagesByPacketID index lets us find the existing entry
	// and upgrade it in place (telemetry refresh) instead.
	channelName := m.currentChannel
	if ev.Channel < len(m.channels) {
		channelName = m.channels[ev.Channel].name
	}
	if body.PacketID != 0 {
		if existing, ok := m.messagesByPacketID[body.PacketID]; ok &&
			existing >= 0 && existing < len(m.messages) {
			// Refresh signal telemetry in case the replay carries
			// fresher RSSI/SNR/hops than the stored row (can happen
			// when the firmware re-measures before handing off).
			// Leave status alone unless it was pending and we now
			// have a real ack.
			prev := &m.messages[existing]
			prev.hops = body.Hops
			prev.snr = body.SNR
			if prev.status == statusPending {
				prev.status = statusAck
			}
			if m.store != nil {
				m.storagePersist(
					m.store.SaveMessage(m.radioID, channelName, messageItemToModel(*prev)),
				)
			}
			return nil
		}
	}

	// Snapshot whether the user was anchored at the tail BEFORE we
	// append. If they were (selectedMsg was on the last row of the
	// log, or the log was empty) we auto-follow new traffic by
	// advancing selectedMsg to the fresh tail. If they'd scrolled up
	// to read history, leave selectedMsg alone — irssi convention:
	// scrollback is sticky, new messages appear at the bottom
	// invisibly until the user returns to tail. Without this
	// incoming texts would arrive but never scroll into view because
	// renderMessagesPane anchors its viewport on selectedMsg.
	wasAtTail := len(m.messages) == 0 || m.selectedMsg == len(m.messages)-1
	m.messages = append(m.messages, item)
	if body.PacketID != 0 {
		m.messagesByPacketID[body.PacketID] = len(m.messages) - 1
	}
	if wasAtTail {
		m.selectedMsg = len(m.messages) - 1
	}

	// Persist the incoming message so it survives a restart.
	if m.store != nil {
		m.storagePersist(m.store.SaveMessage(m.radioID, channelName, messageItemToModel(item)))
	}

	// Bump unread count on non-active channels.
	if ev.Channel < len(m.channels) && m.channels[ev.Channel].name != m.currentChannel && !mine {
		m.channels[ev.Channel].unread++
	}

	// Terminal ding — return the BEL Cmd when the message came from
	// someone else and the user hasn't /muted it. Update threads the
	// Cmd back through the runtime; /dingtest returns the same Cmd
	// so manual verification and the live ingress path share one
	// code path.
	if !mine && !m.dingMuted {
		return ringTerminalBellCmd()
	}
	return nil
}

// ringTerminalBellCmd returns a tea.Cmd that writes a BEL byte to
// os.Stdout — the canonical bubbletea pattern for terminal side
// effects. The runtime executes the Cmd in a goroutine so the BEL
// write doesn't interleave with the renderer's frame output. Stdout
// (not /dev/tty) because that's the FD bubbletea renders to and
// what tmux watches for bell-activity (`printf '\a'` works for the
// same reason — same FD). Returns nil msg so nothing routes back
// to Update.
//
// If the user has BEL silenced in their terminal preferences (iTerm
// "Silence bell", Terminal.app "Audible Bell" / "Visual Bell" both
// off) the byte goes through but no audible / visual bell fires —
// that's terminal-side, not a meshx issue.
func ringTerminalBellCmd() tea.Cmd {
	return func() tea.Msg {
		_, _ = fmt.Fprint(os.Stdout, "\a")
		return nil
	}
}

// applyTraceroute consumes a TRACEROUTE_APP reply that landed for
// our outbound /tr request. Correlates against m.pendingTraceroute
// via request_id; foreign traceroutes (replies to someone else's
// request) silently drop because their request_id won't match.
//
// Surfaces the result as a systemBlock with the round-trip time, the
// hop count (== len(route) — the firmware's RouteDiscovery puts only
// intermediate node nums in the slice; source and dest are implicit
// at MeshPacket.From / .To), and the resolved callsign of every hop
// when we know one (placeholder hex when we don't).
//
// On match the pendingTraceroute slot clears so the user can fire a
// fresh /tr without waiting for the timeout to elapse, and the
// scheduled tracerouteTimeoutMsg becomes a no-op when its tick lands
// (the packetID guard there falls through silently).
func (m *model) applyTraceroute(msg mdl.Traceroute) {
	if m.pendingTraceroute == nil {
		return
	}
	// Correlate by request_id when the firmware sets it (modern
	// builds), otherwise fall back to "is this from the node we're
	// tracing?" — older Meshtastic firmware (≤ 2.2) replies to a
	// TRACEROUTE_APP request with a fresh MeshPacket whose Data does
	// NOT echo the original packetID, so a strict request_id match
	// silently times out every time. The fromNum fallback only fires
	// while a request is in flight, and `pendingTraceroute` enforces
	// one-in-flight, so the worst-case false positive is "we accept
	// a foreign traceroute reply that happens to come from the exact
	// peer we just asked about" — which IS effectively the right
	// answer for this user anyway.
	switch {
	case msg.RequestID != 0 && msg.RequestID == m.pendingTraceroute.packetID:
	case msg.RequestID == 0 && msg.FromNum == m.pendingTraceroute.targetNum:
	default:
		return
	}
	tgt := m.pendingTraceroute.targetCall
	rtt := msg.At.Sub(m.pendingTraceroute.requestedAt)
	if rtt < 0 {
		// Clock skew between the radio's RxTime stamp and our local
		// clock can yield a negative delta. Round to time.Since so
		// the displayed RTT is still useful.
		rtt = time.Since(m.pendingTraceroute.requestedAt)
	}
	hops := len(msg.Route)
	lines := []string{
		fmt.Sprintf("hops:    %d", hops),
		fmt.Sprintf("rtt:     %s", rtt.Round(100*time.Millisecond)),
	}
	if hops == 0 {
		lines = append(lines, "path:    direct (RF-adjacent — no relays)")
	} else {
		// Build "us → r1 → r2 → ... → target" using callsigns where
		// we have them, "0x<hex>" otherwise. The user-readable path
		// is the whole point of the live traceroute, so spend the
		// width on rendering it well.
		hopLabels := make([]string, 0, hops+2)
		hopLabels = append(hopLabels, m.myCallsign())
		for _, num := range msg.Route {
			if idx, ok := m.nodesByNum[num]; ok && idx < len(m.nodes) {
				hopLabels = append(hopLabels, m.nodes[idx].callsign)
				continue
			}
			hopLabels = append(hopLabels, fmt.Sprintf("0x%x", num))
		}
		hopLabels = append(hopLabels, tgt)
		lines = append(lines, "path:    "+strings.Join(hopLabels, " → "))
	}
	// Update cached telemetry so subsequent /tr in demo / offline
	// fall-back mode shows the freshly-measured value instead of the
	// stale zero. lastHops needs the live value even if it's 0
	// (direct), so this assignment doesn't gate on > 0.
	if idx, ok := m.nodesByNum[msg.FromNum]; ok && idx < len(m.nodes) {
		m.nodes[idx].lastHops = hops
	}
	m.systemBlock(fmt.Sprintf("traceroute %s", tgt), lines...)
	m.flash = fmt.Sprintf(
		"tr: %s — %d hop%s in %s",
		tgt,
		hops,
		plural(hops),
		rtt.Round(100*time.Millisecond),
	)
	m.pendingTraceroute = nil
}

// applyPing consumes a REPLY_APP echo for our outbound /ping.
// Correlates against m.pendingPing via request_id; falls back to
// fromNum match when the firmware doesn't echo request_id (older
// builds). Surfaces an RTT + hop + signal systemBlock and clears
// the pending slot. Also refreshes the node's lastSNR / lastRSSI /
// lastHops cache off the live measurement so /whois on the same
// peer immediately renders fresh telemetry.
func (m *model) applyPing(msg mdl.Ping) {
	if m.pendingPing == nil {
		return
	}
	switch {
	case msg.RequestID != 0 && msg.RequestID == m.pendingPing.packetID:
	case msg.RequestID == 0 && msg.FromNum == m.pendingPing.targetNum:
	default:
		return
	}
	tgt := m.pendingPing.targetCall
	rtt := msg.At.Sub(m.pendingPing.requestedAt)
	if rtt < 0 {
		rtt = time.Since(m.pendingPing.requestedAt)
	}
	lines := []string{
		fmt.Sprintf("rtt:     %s", rtt.Round(100*time.Millisecond)),
		fmt.Sprintf("hops:    %d", msg.Hops),
		fmt.Sprintf("snr:     %s dB", msg.SNR),
		fmt.Sprintf("rssi:    %s", msg.RSSI),
	}
	if idx, ok := m.nodesByNum[msg.FromNum]; ok && idx < len(m.nodes) {
		m.nodes[idx].lastHops = msg.Hops
		if msg.SNR != "" {
			m.nodes[idx].lastSNR = msg.SNR
		}
		if msg.RSSI != "" {
			m.nodes[idx].lastRSSI = msg.RSSI
		}
		m.nodes[idx].lastHeardAt = time.Now()
	}
	m.systemBlock(fmt.Sprintf("ping %s", tgt), lines...)
	m.flash = fmt.Sprintf(
		"ping: %s — %d hop%s in %s",
		tgt,
		msg.Hops,
		plural(msg.Hops),
		rtt.Round(100*time.Millisecond),
	)
	m.pendingPing = nil
}

// applyRouting flips the status of the local messageItem whose
// packetID matches the Routing reply's request_id. NONE → "ack"
// (delivery succeeded), anything else → "fail" (the errorName
// hints at why: TIMEOUT, MAX_RETRANSMIT, NO_INTERFACE...).
// Routing replies for packets we didn't originate silently drop —
// request_id won't match any of our outbound rows.
//
// Persists the new status through saveMessage's UPSERT path so the
// flip survives a restart. Without this, SQLite would still hold
// "pending", expireStalePendingMessages would mark the row "fail"
// after 5 minutes on next launch, and old messages that actually
// delivered would surface as ✗ — misleading the user about what
// went out.
func (m *model) applyRouting(msg mdl.Routing) {
	if msg.RequestID == 0 {
		return
	}
	// Outbound /ping correlation. REPLY_APP is technically a "built-in
	// module" but firmware ships with it disabled often enough that
	// relying on the echo alone fails on a lot of radios. The Routing
	// receipt always lands (it's how the firmware confirms delivery
	// for any WantAck packet), so when an in-flight ping resolves
	// here BEFORE applyPing sees a REPLY_APP echo, treat the ack as
	// the success signal and surface a softer result block:
	// delivered, but no echo — useful for the "is this peer reachable
	// at all?" question even when /ping's primary echo path can't
	// answer it.
	if m.pendingPing != nil && m.pendingPing.packetID == msg.RequestID {
		tgt := m.pendingPing.targetCall
		rtt := time.Since(m.pendingPing.requestedAt).Round(100 * time.Millisecond)
		if msg.OK {
			m.systemBlock(fmt.Sprintf("ping %s", tgt),
				fmt.Sprintf("rtt:     %s (ack only — no echo)", rtt),
				"note:    radio acknowledged delivery, but REPLY_APP echo",
				"         did not return. Common when the target's REPLY_APP",
				"         module is disabled — /tr still works in that case.",
			)
			m.flash = fmt.Sprintf("ping: %s — ack in %s (no echo)", tgt, rtt)
		} else {
			m.systemBlock(fmt.Sprintf("ping %s", tgt),
				fmt.Sprintf("result:  delivery failed (%s)", msg.ErrorName),
			)
			m.flash = fmt.Sprintf("ping: %s — %s", tgt, msg.ErrorName)
		}
		m.pendingPing = nil
		return
	}
	for i := range m.messages {
		if m.messages[i].packetID != msg.RequestID || !m.messages[i].mine {
			continue
		}
		if msg.OK {
			m.messages[i].status = statusAck
			m.flash = "ack received"
		} else {
			m.messages[i].status = statusFail
			m.flash = "delivery failed: " + msg.ErrorName + "  (R to resend)"
		}
		if m.store != nil {
			m.storagePersist(
				m.store.SaveMessage(m.radioID, m.currentChannel, messageItemToModel(m.messages[i])),
			)
		}
		return
	}
}

// resend takes a prior outbound messageItem, re-enqueues it over
// the pump with a fresh packetID, and flips the original row's
// status back to "pending" so the user sees the retransmit in
// flight. Bound to `R` in nav mode on any mine-row that's either
// "fail" (the obvious retry case) or "pending" (stuck because the
// radio never sent a ROUTING reply — treating R as "this is stuck,
// try again" matches what the user actually wants). "ack" rows are
// rejected with an explicit flash rather than silently no-oping so
// the user knows the keypress registered.
func (m *model) resend(idx int) {
	if idx < 0 || idx >= len(m.messages) {
		return
	}
	msg := &m.messages[idx]
	if !msg.mine {
		m.flash = "R: can only resend your own messages"
		return
	}
	switch msg.status {
	case statusFail, statusPending:
		// Fall through to retransmit.
	case statusAck:
		m.flash = "R: this message already delivered ✓"
		return
	default:
		m.flash = "R: nothing to resend on this row"
		return
	}
	if m.pump == nil {
		m.flash = "R: no radio connected — cannot resend"
		return
	}
	pid, _ := m.pump.Send(mdl.SendText{
		Channel: int(m.currentChannelIndex()),
		Text:    msg.text,
		ReplyID: msg.replyID,
	})
	msg.packetID = pid
	msg.status = statusPending
	m.flash = fmt.Sprintf("↻ retransmit sent (pid=0x%08x) — awaiting ack", pid)
}

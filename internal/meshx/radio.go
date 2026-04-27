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
	"strings"
	"time"
)

// sanitizeMessageText normalizes line endings in peer-originated text.
// Real-radio payloads (e.g. solar-node end-of-day reports) ship CRLF;
// a lone \r left on a continuation line ships to the terminal as
// carriage-return and snaps the cursor back to column 0, smearing
// the next row's left pane border into the message body. Called at
// the ingress boundary (applyTextMessage) and on replay from SQLite
// (loadMessages) so historic rows written before this fix get
// sanitized on read.
var messageTextSanitizer = strings.NewReplacer("\r\n", "\n", "\r", "")

func sanitizeMessageText(s string) string {
	return messageTextSanitizer.Replace(s)
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
func (m *model) upsertNode(msg radioNodeInfoMsg) {
	unresolved := false
	if msg.longName == "" && msg.shortName == "" {
		long, short := defaultCallsign(msg.nodeNum)
		msg.longName = long
		msg.shortName = short
		unresolved = true
	}
	callsign := msg.longName
	if callsign == "" {
		callsign = msg.shortName
	}

	// Derive state from lastHeard age.
	state := "offline"
	if !msg.lastHeardAt.IsZero() {
		age := time.Since(msg.lastHeardAt)
		switch {
		case age < 15*time.Minute:
			state = "online"
		case age < 2*time.Hour:
			state = "offline"
		default:
			state = "offline"
		}
	}
	lastHeard := "never"
	if !msg.lastHeardAt.IsZero() {
		lastHeard = humanDuration(time.Since(msg.lastHeardAt))
	}

	item := nodeItem{
		callsign:    callsign,
		shortName:   msg.shortName,
		nodeNum:     msg.nodeNum,
		unresolved:  unresolved,
		state:       state,
		lastHeard:   lastHeard,
		lastHeardAt: msg.lastHeardAt,
		heardRank:   int(time.Since(msg.lastHeardAt).Seconds()),
		lastSNR:     msg.snr,
		lastRSSI:    msg.rssi,
		lastHops:    msg.hops,
		hwModel:     msg.hwModel,
	}

	// Persist to the cross-session NodeDB cache so once we've learned
	// a peer's real User info we remember it on every subsequent
	// launch — same behavior as the official phone app. Placeholder
	// "node 0x…" callsigns (both longname and shortname empty) are
	// skipped inside saveNode itself.
	m.storagePersist(saveNode(m.db, msg.nodeNum, msg.longName, msg.shortName, msg.hwModel))

	if idx, ok := m.nodesByNum[msg.nodeNum]; ok {
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
	m.nodesByNum[msg.nodeNum] = len(m.nodes)
	m.nodes = append(m.nodes, item)
}

// applyChannel sets or replaces a channel slot. Skips DISABLED
// channels so they don't clutter the tab strip.
func (m *model) applyChannel(msg radioChannelMsg) {
	if msg.role == "DISABLED" {
		return
	}
	name := msg.name
	if name == "" {
		// Empty-name PRIMARY is the default "LongFast" channel — give
		// it a readable label in the UI.
		name = "#default"
	} else if msg.hasPSK {
		name = "*" + msg.name + "*"
	} else {
		name = "#" + msg.name
	}
	c := channelItem{name: name, private: msg.hasPSK}
	// Upsert by index; grow the slice if needed.
	for len(m.channels) <= msg.index {
		m.channels = append(m.channels, channelItem{})
	}
	// Preserve unread count across re-apply.
	c.unread = m.channels[msg.index].unread
	m.channels[msg.index] = c
	if m.currentChannel == "" {
		m.currentChannel = name
	}
}

// applyTextMessage appends a received text packet to the message log.
// Resolves fromNum to a callsign via the NodeDB; unread count bumps
// on the destination channel when it's not the active one.
func (m *model) applyTextMessage(msg radioTextMsg) {
	// Default ghost identity from the firmware's last-4-hex
	// convention so the FROM column matches what other Meshtastic
	// clients display for the same peer (iOS shows "c7f7", we
	// shouldn't be the outlier showing "node 0x273cc7f7").
	defaultLong, _ := defaultCallsign(msg.fromNum)
	from := defaultLong
	if idx, ok := m.nodesByNum[msg.fromNum]; ok {
		from = m.nodes[idx].callsign
		// Live RF contact — stamp lastHeardAt + refresh signal
		// telemetry. currentState / currentLastHeard derive
		// "online" and "now" from lastHeardAt at render time, so
		// there's no need to poke state / lastHeard strings here;
		// the renderer always reads the live derivation.
		m.nodes[idx].lastHeardAt = time.Now()
		m.nodes[idx].heardRank = 0
		if msg.snr != "" {
			m.nodes[idx].lastSNR = msg.snr
		}
		if msg.rssi != "" {
			m.nodes[idx].lastRSSI = msg.rssi
		}
		if msg.hops > 0 {
			m.nodes[idx].lastHops = msg.hops
		}
	} else if msg.fromNum != 0 {
		// We've heard a text packet from a peer whose NodeInfo we
		// haven't received yet — ghost them into m.nodes so /cqr,
		// /rs, /whois, /ping can find them by id, hex, or shortname
		// substring. The entry gets upgraded by upsertNode the
		// moment a real NodeInfo arrives (nodesByNum index is
		// stable so all references stay valid). Synthesize the
		// firmware-default callsigns up front so the row already
		// reads the same way every other Meshtastic client renders
		// the peer.
		long, short := defaultCallsign(msg.fromNum)
		m.nodes = append(m.nodes, nodeItem{
			callsign:    long,
			shortName:   short,
			nodeNum:     msg.fromNum,
			unresolved:  true,
			lastHeardAt: time.Now(),
			lastSNR:     msg.snr,
			lastRSSI:    msg.rssi,
			lastHops:    msg.hops,
		})
		m.nodesByNum[msg.fromNum] = len(m.nodes) - 1
		from = long
	}
	mine := msg.fromNum == m.myNodeNum

	item := messageItem{
		time:     msg.at.Format("15:04"),
		from:     from,
		mine:     mine,
		text:     sanitizeMessageText(msg.text),
		status:   "ack",
		hops:     msg.hops,
		snr:      msg.snr,
		packetID: msg.packetID,
		replyID:  msg.replyID,
		fromNum:  msg.fromNum,
		sentAt:   msg.at,
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
	if msg.channel < len(m.channels) {
		channelName = m.channels[msg.channel].name
	}
	if msg.packetID != 0 {
		if existing, ok := m.messagesByPacketID[msg.packetID]; ok &&
			existing >= 0 && existing < len(m.messages) {
			// Refresh signal telemetry in case the replay carries
			// fresher RSSI/SNR/hops than the stored row (can happen
			// when the firmware re-measures before handing off).
			// Leave status alone unless it was pending and we now
			// have a real ack.
			prev := &m.messages[existing]
			prev.hops = msg.hops
			prev.snr = msg.snr
			if prev.status == "pending" {
				prev.status = "ack"
			}
			m.storagePersist(saveMessage(m.db, channelName, *prev))
			return
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
	if msg.packetID != 0 {
		m.messagesByPacketID[msg.packetID] = len(m.messages) - 1
	}
	if wasAtTail {
		m.selectedMsg = len(m.messages) - 1
	}

	// Persist the incoming message so it survives a restart.
	m.storagePersist(saveMessage(m.db, channelName, item))

	// Bump unread count on non-active channels.
	if msg.channel < len(m.channels) && m.channels[msg.channel].name != m.currentChannel && !mine {
		m.channels[msg.channel].unread++
	}
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
func (m *model) applyRouting(msg radioRoutingMsg) {
	if msg.requestID == 0 {
		return
	}
	for i := range m.messages {
		if m.messages[i].packetID != msg.requestID || !m.messages[i].mine {
			continue
		}
		if msg.ok {
			m.messages[i].status = "ack"
			m.flash = "ack received"
		} else {
			m.messages[i].status = "fail"
			m.flash = "delivery failed: " + msg.errorName + "  (R to resend)"
		}
		m.storagePersist(saveMessage(m.db, m.currentChannel, m.messages[i]))
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
	case "fail", "pending":
		// Fall through to retransmit.
	case "ack":
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
	envelope, pid := newTextToRadio(msg.text, m.currentChannelIndex(), msg.replyID)
	msg.packetID = pid
	msg.status = "pending"
	m.pump.Enqueue(envelope)
	m.flash = fmt.Sprintf("↻ retransmit sent (pid=0x%08x) — awaiting ack", pid)
}

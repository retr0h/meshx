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

package tui

// radio.go — handlers that react to a FromRadio packet and mutate
// the model, plus resend (the other direction: rebuild a ToRadio
// from an existing row). pump.go translates each protobuf variant
// into a typed tea.Msg; Update routes each to the matching apply*
// handler here.
//
// Boundary with node.go: node.go is read-side identity + lookup +
// derived display. radio.go is the mutation side — it creates /
// updates nodeItem rows in response to NodeInfo + text packets.
// Everything that changes m.Nodes, m.Channels, or m.Messages in
// response to incoming RF traffic lives here.

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/retr0h/meshx/internal/driver"
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

// applyTextMessage appends a received text packet to the message log.
// State mutation (ghost-create the sender, append the message,
// dedupe by PacketID, persist, bump unread) lives in
// Driver.ApplyText so daemon and TUI write the exact same row.
// This wrapper sanitizes the body (TUI is the boundary that decides
// what survives the layout invariants), snapshots whether the user
// was reading at the tail (TUI-only autoscroll concern), delegates
// to ApplyText, then layers the autoscroll cursor advance + the
// terminal-bell ding Cmd.
func (m *model) applyTextMessage(ev mdl.Text) tea.Cmd {
	cleanText, corrupted := sanitizeMessageText(ev.Body.Text)

	// Snapshot the autoscroll anchor BEFORE Apply — if the user was
	// at the bottom of the log, advance the cursor to the new tail
	// so live traffic stays in view. Scrolled-up readers stay where
	// they are (irssi convention: scrollback is sticky).
	wasAtTail := len(m.Messages) == 0 || m.selectedMsg == len(m.Messages)-1

	res := m.driver.ApplyText(ev, cleanText, corrupted)

	if !res.Skipped && wasAtTail && res.Index >= 0 {
		m.selectedMsg = res.Index
	}

	if !res.FromMine && !m.DingMuted {
		return ringTerminalBellCmd()
	}
	return nil
}

// reactRouting layers TUI-only side effects on top of
// Driver.ApplyRouting's status flip. Two concerns:
//
//   - PendingPing fallback. Some firmware acks WantAck packets via
//     Routing without ever sending REPLY_APP, so a /ping that times
//     out on echo can still resolve here when the routing receipt
//     matches its packet id.
//   - User-visible flash for our own outbound messages — "ack
//     received" / "delivery failed: ...". State stays in Driver.
func (m *model) reactRouting(msg mdl.Routing, res driver.ApplyRoutingResult) {
	if msg.RequestID == 0 {
		return
	}
	if m.PendingPing != nil && m.PendingPing.PacketID == msg.RequestID {
		tgt := m.PendingPing.TargetCall
		rtt := time.Since(m.PendingPing.RequestedAt).Round(100 * time.Millisecond)
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
		m.PendingPing = nil
		return
	}
	if res.Matched {
		if res.OK {
			m.flash = "ack received"
		} else {
			m.flash = "delivery failed: " + res.ErrorName + "  (R to resend)"
		}
	}
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
// our outbound /tr request. Correlates against m.PendingTraceroute
// via request_id; foreign traceroutes (replies to someone else's
// request) silently drop because their request_id won't match.
//
// Surfaces the result as a systemBlock with the round-trip time, the
// hop count (== len(route) — the firmware's RouteDiscovery puts only
// intermediate node nums in the slice; source and dest are implicit
// at MeshPacket.From / .To), and the resolved callsign of every hop
// when we know one (placeholder hex when we don't).
//
// On match the driver.PendingTraceroute slot clears so the user can fire a
// fresh /tr without waiting for the timeout to elapse, and the
// scheduled tracerouteTimeoutMsg becomes a no-op when its tick lands
// (the packetID guard there falls through silently).
func (m *model) applyTraceroute(msg mdl.Traceroute) {
	if m.PendingTraceroute == nil {
		return
	}
	// Correlate by request_id when the firmware sets it (modern
	// builds), otherwise fall back to "is this from the node we're
	// tracing?" — older Meshtastic firmware (≤ 2.2) replies to a
	// TRACEROUTE_APP request with a fresh MeshPacket whose Data does
	// NOT echo the original packetID, so a strict request_id match
	// silently times out every time. The fromNum fallback only fires
	// while a request is in flight, and `driver.PendingTraceroute` enforces
	// one-in-flight, so the worst-case false positive is "we accept
	// a foreign traceroute reply that happens to come from the exact
	// peer we just asked about" — which IS effectively the right
	// answer for this user anyway.
	switch {
	case msg.RequestID != 0 && msg.RequestID == m.PendingTraceroute.PacketID:
	case msg.RequestID == 0 && msg.FromNum == m.PendingTraceroute.TargetNum:
	default:
		return
	}
	tgt := m.PendingTraceroute.TargetCall
	rtt := msg.At.Sub(m.PendingTraceroute.RequestedAt)
	if rtt < 0 {
		// Clock skew between the radio's RxTime stamp and our local
		// clock can yield a negative delta. Round to time.Since so
		// the displayed RTT is still useful.
		rtt = time.Since(m.PendingTraceroute.RequestedAt)
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
			if idx, ok := m.NodesByNum[num]; ok && idx < len(m.Nodes) {
				hopLabels = append(hopLabels, m.Nodes[idx].Callsign)
				continue
			}
			hopLabels = append(hopLabels, fmt.Sprintf("0x%x", num))
		}
		hopLabels = append(hopLabels, tgt)
		lines = append(lines, "path:    "+strings.Join(hopLabels, " → "))
	}
	// Per-node LastHops refresh happens in Driver.ApplyTraceroute —
	// we only render the report here.
	m.systemBlock(fmt.Sprintf("traceroute %s", tgt), lines...)
	m.flash = fmt.Sprintf(
		"tr: %s — %d hop%s in %s",
		tgt,
		hops,
		plural(hops),
		rtt.Round(100*time.Millisecond),
	)
	m.PendingTraceroute = nil
}

// applyPing consumes a REPLY_APP echo for our outbound /ping.
// Correlates against m.PendingPing via request_id; falls back to
// fromNum match when the firmware doesn't echo request_id (older
// builds). Surfaces an RTT + hop + signal systemBlock and clears
// the pending slot. Also refreshes the node's lastSNR / lastRSSI /
// lastHops cache off the live measurement so /whois on the same
// peer immediately renders fresh telemetry.
func (m *model) applyPing(msg mdl.Ping) {
	if m.PendingPing == nil {
		return
	}
	switch {
	case msg.RequestID != 0 && msg.RequestID == m.PendingPing.PacketID:
	case msg.RequestID == 0 && msg.FromNum == m.PendingPing.TargetNum:
	default:
		return
	}
	tgt := m.PendingPing.TargetCall
	rtt := msg.At.Sub(m.PendingPing.RequestedAt)
	if rtt < 0 {
		rtt = time.Since(m.PendingPing.RequestedAt)
	}
	lines := []string{
		fmt.Sprintf("rtt:     %s", rtt.Round(100*time.Millisecond)),
		fmt.Sprintf("hops:    %d", msg.Hops),
		fmt.Sprintf("snr:     %s dB", msg.SNR),
		fmt.Sprintf("rssi:    %s", msg.RSSI),
	}
	// Per-node telemetry refresh happens in Driver.ApplyPing —
	// we only render the report + flash here.
	m.systemBlock(fmt.Sprintf("ping %s", tgt), lines...)
	m.flash = fmt.Sprintf(
		"ping: %s — %d hop%s in %s",
		tgt,
		msg.Hops,
		plural(msg.Hops),
		rtt.Round(100*time.Millisecond),
	)
	m.PendingPing = nil
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
	if idx < 0 || idx >= len(m.Messages) {
		return
	}
	msg := &m.Messages[idx]
	if !msg.Mine {
		m.flash = "R: can only resend your own messages"
		return
	}
	switch msg.Status {
	case mdl.StatusFail, mdl.StatusPending:
		// Fall through to retransmit.
	case mdl.StatusAck:
		m.flash = "R: this message already delivered ✓"
		return
	default:
		m.flash = "R: nothing to resend on this row"
		return
	}
	if m.driver.PumpHandle() == nil {
		m.flash = "R: no radio connected — cannot resend"
		return
	}
	pid, _ := m.driver.Send(mdl.SendText{
		Channel: int(m.currentChannelIndex()),
		Text:    msg.Text,
		ReplyID: msg.ReplyID,
	})
	msg.PacketID = pid
	msg.Status = mdl.StatusPending
	m.flash = fmt.Sprintf("↻ retransmit sent (pid=0x%08x) — awaiting ack", pid)
}

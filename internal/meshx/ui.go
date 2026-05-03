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

// Package meshx rendering surface.
//
// ui.go holds everything View-side: the top-level View() dispatcher,
// every pane renderer (messages / channels / nodes), the status-bar
// family (top status, channel status, input row, top divider), the
// help overlay, plus the small styling primitives (paneStyle,
// paneHeader, nickColor, zebraBg, wrapSelection, padOrTruncate) that
// the renderers share. No state mutation lives here — all mutation
// happens in app.go (model + Update + message handlers), input.go
// (nav / mode transitions), or commands.go (slash dispatch).
package meshx

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// View composes the screen as a Component tree: a VStack of the chrome
// regions wrapped around a body region. The frame box is m.w-1 wide
// (NOT m.w) to dodge terminal pending-wrap (DECAWM auto-margin) — no
// component is ever asked to render content into the very last column,
// which is the architectural fix for the duplicate-input-row bug class.
//
// Each child of the VStack is sized explicitly:
//
//   - status:   1 row
//   - divider:  1 row
//   - body:     -1 (flex; takes whatever's left after the others)
//   - chanRow:  1 row
//   - inputBar: 1 row
//
// Components are responsible for filling their allocated Box exactly.
// Row + Cell + padCells in box.go enforce that contract; nothing here
// has to compute m.w-N math by hand.
func (m model) View() string {
	if m.w == 0 || m.h == 0 {
		return ""
	}
	out := frameView(m).Render(Box{Width: m.w - 1, Height: m.h})
	if path := os.Getenv("MESHX_DEBUG_VIEW"); path != "" {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
			lines := strings.Split(out, "\n")
			for i, l := range lines {
				_, _ = fmt.Fprintf(f, "[%2d] w=%d %q\n", i, ansi.StringWidth(l), ansi.Strip(l))
			}
			_ = f.Close()
		}
		_ = os.Unsetenv("MESHX_DEBUG_VIEW")
	}
	return out
}

// findMessageByPacketID returns a pointer to the m.messages entry
// whose packetID matches, or nil. Used by the renderer to resolve
// reply_id → parent message for threaded quote rendering.
func (m model) findMessageByPacketID(id uint32) *messageItem {
	if id == 0 {
		return nil
	}
	for i := range m.messages {
		if m.messages[i].PacketID == id {
			return &m.messages[i]
		}
	}
	return nil
}

// displayFrom returns the callsign to render for a message, preferring
// the CURRENT NodeDB entry over the `from` snapshot taken at ingest.
// This is what backfills "node 0xdeadbeef" → real callsign once the
// corresponding NodeInfo arrives — without it, the ingest-time
// fallback is baked into the row forever. Falls back to msg.from
// when the node isn't in nodesByNum (demo seeds with no fromNum,
// or peers we never learned about).
//
// Own ("mine") rows go through myCallsign() so rows sent BEFORE
// MyNodeInfo arrived (persisted with from="—" or from="me") also
// upgrade to the real callsign as soon as we learn it. Without
// this the first BLE session's outbound history would stay stuck
// on the placeholder forever.
// senderUnresolved reports whether the message's sender is a peer
// we've only synthesized a firmware-default callsign for (no real
// NodeInfo has arrived). Used by the row renderer to dim the FROM
// column + accent tick and prepend the 👻 marker. Own messages and
// rows with no fromNum (demo seeds, system rows) are never flagged.
func (m model) senderUnresolved(msg messageItem) bool {
	if msg.Mine || msg.FromNum == 0 {
		return false
	}
	idx, ok := m.nodesByNum[msg.FromNum]
	if !ok || idx < 0 || idx >= len(m.nodes) {
		return false
	}
	return m.nodes[idx].unresolved
}

func (m model) displayFrom(msg messageItem) string {
	if msg.Mine {
		if cs := m.myCallsign(); cs != "" && cs != "—" {
			return cs
		}
		return msg.From
	}
	if msg.FromNum == 0 {
		return msg.From
	}
	if idx, ok := m.nodesByNum[msg.FromNum]; ok && idx < len(m.nodes) {
		if cs := m.nodes[idx].callsign; cs != "" {
			return cs
		}
	}
	// Belt-and-suspenders for any path that bypassed ghost backfill
	// (race, future code) — synthesize the firmware default from
	// fromNum so the row never shows the legacy "node 0x<hex>"
	// string we used to bake into the SQLite from column.
	long, _ := defaultCallsign(msg.FromNum)
	return long
}

// truncateRunes clamps s to at most n display runes, appending …
// when the source was longer. Used for parent-message quote lines
// so a long reply target doesn't blow out the width budget.
func truncateRunes(s string, n int) string {
	count := 0
	for i := range s {
		count++
		if count > n {
			return s[:i] + "…"
		}
	}
	return s
}

// padOrTruncate forces a string to exactly width w display cells:
// right-pads with spaces if short, truncates with an ellipsis if long.
//
// Uses charmbracelet/x/ansi.StringWidth for measurement — that's the
// SAME library bubbletea's diff renderer, lipgloss/cellbuf word-wrap,
// and the standard-renderer's EraseLineRight check all use, so our
// padding stays self-consistent with everyone downstream. uniseg
// (and runewidth) under-count VS16-promoted keycaps "2️⃣ 6️⃣ ⚠️"
// which the terminal AND ansi render as 2 cells — using uniseg here
// shaved one cell off every keycap row, sliding the right-aligned
// metrics column left by one and breaking the bordered box layout.
//
// Iterates by grapheme cluster so a single emoji is never split: no
// chopping the digit out of a 2️⃣ keycap, no severing skin-tone
// modifiers, no halving 🙋🏼‍♂️ ZWJ sequences.
func padOrTruncate(s string, w int) string {
	// Funnel through padCells so every measurement applies the keycap
	// correction (VS16 / U+20E3 promote to 2 cells per Unicode TR51).
	// Without it, "7️⃣" measures 1 by ansi.StringWidth but renders as
	// 2 in every modern terminal, so padded rows visually overflow by
	// 1 column on keycap messages and the right ║ frame walks left.
	return padCells(s, w)
}

// isMsgSearchHit is true when search mode has a committed query and
// the message (from or text) contains it (case-insensitive).
func (m model) isMsgSearchHit(msg messageItem) bool {
	if m.searchQuery == "" {
		return false
	}
	return strings.Contains(strings.ToLower(msg.From+" "+msg.Text), m.searchQuery)
}

// isStringSearchHit is the plain-string variant — used for channels
// and node callsigns.
func (m model) isStringSearchHit(s string) bool {
	if m.searchQuery == "" {
		return false
	}
	return strings.Contains(strings.ToLower(s), m.searchQuery)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func resolveJump(to, n int) int {
	if to < 0 {
		return n - 1
	}
	return clamp(to, 0, n-1)
}

func toggleFlash(on bool, whenOn, whenOff string) string {
	if on {
		return whenOn
	}
	return whenOff
}

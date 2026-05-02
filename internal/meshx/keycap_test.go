package meshx

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// TestMessagesPane_KeycapRowFitsBox runs renderMessagesPane with a
// keycap-bodied message in the log and asserts every line of the
// resulting bordered pane is EXACTLY width cells wide per ansiCells.
//
// The regression: lipgloss-based paneStyle measured with runewidth,
// which under-counts keycap emoji ("7️⃣" reads as 1 cell while iTerm2
// renders 2). lipgloss then padded its inner content to its declared
// Width using its own count, leaving keycap rows 1 cell wider than
// the box's cell budget. fitToBox saw the over-budget row and chopped
// it with "…", clobbering the styled metrics column on the right.
//
// The fix replaces paneStyle with the Bordered Component, which uses
// our keycap-aware ansiCells everywhere and never asks lipgloss to do
// width math, so a keycap row lands the right ║ in the same column
// as a plain-text row.
func TestMessagesPane_KeycapRowFitsBox(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, termW := range []int{80, 120, 200, 205, 250} {
		m := newModel(DefaultDemo(), "")
		m.w = termW
		m.h = 30
		m.messages = []messageItem{
			{
				time: "12:16", from: "RumpleDumpleAuto", fromNum: 0xdead0001,
				text: "6️⃣", hops: 6, snr: "4.5", status: "ack",
			},
			{
				time: "12:17", from: "RumpleDumpleAuto", fromNum: 0xdead0001,
				text: "7️⃣", hops: 6, snr: "6.8", status: "ack",
			},
			{
				time: "12:24", from: "Node",
				text: "Heard diamond bar", hops: 2, snr: "6.0", status: "ack",
			},
			{
				time: "12:25", from: "Node",
				text: "2️⃣ 6️⃣ 7️⃣", hops: 4, snr: "5.5", status: "ack",
			},
		}
		boxW := termW - 1 // mirrors View()'s safeW frame
		paneH := 20
		out := m.renderMessagesPane(boxW, paneH)
		for i, line := range strings.Split(out, "\n") {
			if w := ansiCells(line); w != boxW {
				t.Errorf("termW=%d line %d: ansiCells=%d, want %d\nstripped=%q",
					termW, i, w, boxW, ansi.Strip(line))
			}
		}
	}
}

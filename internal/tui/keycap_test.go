package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
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
			{Message: mdl.Message{
				Time: "12:16", From: "RumpleDumpleAuto", FromNum: 0xdead0001,
				Text: "6️⃣", Hops: 6, SNR: "4.5", Status: mdl.StatusAck,
			}},
			{Message: mdl.Message{
				Time: "12:17", From: "RumpleDumpleAuto", FromNum: 0xdead0001,
				Text: "7️⃣", Hops: 6, SNR: "6.8", Status: mdl.StatusAck,
			}},
			{Message: mdl.Message{
				Time: "12:24", From: "Node",
				Text: "Heard diamond bar", Hops: 2, SNR: "6.0", Status: mdl.StatusAck,
			}},
			{Message: mdl.Message{
				Time: "12:25", From: "Node",
				Text: "2️⃣ 6️⃣ 7️⃣", Hops: 4, SNR: "5.5", Status: mdl.StatusAck,
			}},
		}
		boxW := termW - 1 // mirrors View()'s safeW frame
		paneH := 20
		out := messagesPane{m: m}.Render(Box{Width: boxW, Height: paneH})
		for i, line := range strings.Split(out, "\n") {
			if w := ansiCells(line); w != boxW {
				t.Errorf("termW=%d line %d: ansiCells=%d, want %d\nstripped=%q",
					termW, i, w, boxW, ansi.Strip(line))
			}
		}
	}
}

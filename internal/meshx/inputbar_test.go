package meshx

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// TestInputBar_PrefixKeepsColorWhenTyping is the regression for the
// "[#default] › goes white when I start typing" bug. The bubbles
// textinput.View() emits Width+1 visible cells once a value is set
// (cursor block lands AFTER the typed char rather than over it). If
// the inputBar doesn't reserve that 1-cell margin, the row overflows,
// the truncation path kicks in, and the styled prefix collapses to
// plain text on the wire.
//
// Asserts the rendered row is exactly box.Width cells wide AND
// preserves multiple ANSI SGR sequences (the dim brackets, mesh-green
// channel, amber chevron) regardless of whether the input is empty
// or has been typed into.
func TestInputBar_PrefixKeepsColorWhenTyping(t *testing.T) {
	// Force lipgloss to emit truecolor escapes — without an actual
	// TTY it auto-detects "no color" and returns plain text, which
	// would mask the bug we're guarding against.
	lipgloss.SetColorProfile(termenv.TrueColor)

	const boxW = 99
	for _, sample := range []string{"", "h", "hello", strings.Repeat("a", 200)} {
		m := newModel(DefaultDemo(), "")
		m.w = boxW + 1
		m.h = 30
		m.input.Focus()
		m.input.SetValue(sample)
		out := inputBar{m: m}.Render(Box{Width: boxW, Height: 1})

		if w := ansiCells(out); w != boxW {
			t.Errorf("sample=%q width=%d, want %d (visible=%q)",
				sample, w, boxW, ansi.Strip(out))
		}
		// Three styled spans in the prefix (dim '[', green chan,
		// dim '] ', amber '› ') = at least 4 SGR sequences. Plus
		// resets after each = 8. A truncation event would drop
		// every escape and leave the row entirely plain, which is
		// what the user reported.
		const minEscapes = 8
		if n := strings.Count(out, "\x1b["); n < minEscapes {
			t.Errorf("sample=%q ANSI-escape-count=%d, want >= %d (truncation stripped styling?)",
				sample, n, minEscapes)
		}
	}
}

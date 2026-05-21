// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestPaneHeaderCell(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		focused  bool
		wantText string
	}{
		{
			name:     "uppercases-text",
			text:     "channels",
			focused:  false,
			wantText: "CHANNELS",
		},
		{
			name:     "already-uppercase-unchanged",
			text:     "NODES",
			focused:  true,
			wantText: "NODES",
		},
		{
			name:     "mixed-case-uppercased",
			text:     "nearby",
			focused:  false,
			wantText: "NEARBY",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := paneHeaderCell(tt.text, tt.focused)
			plain := ansi.Strip(got)
			if plain != tt.wantText {
				t.Errorf(
					"paneHeaderCell(%q, %v) plain text = %q, want %q",
					tt.text,
					tt.focused,
					plain,
					tt.wantText,
				)
			}
		})
	}
}

func TestPaneCountSuffix(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string // expected plain text substring
	}{
		{
			name: "empty-returns-empty",
			text: "",
			want: "",
		},
		{
			name: "non-empty-preserved",
			text: "  (304 msgs)",
			want: "304 msgs",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := paneCountSuffix(tt.text)
			if tt.text == "" && got != "" {
				t.Errorf("paneCountSuffix(%q) = %q, want empty", tt.text, got)
				return
			}
			plain := ansi.Strip(got)
			if tt.want != "" && !strings.Contains(plain, tt.want) {
				t.Errorf(
					"paneCountSuffix(%q) plain = %q, want contains %q",
					tt.text,
					plain,
					tt.want,
				)
			}
		})
	}
}

func TestPaneLegendLine(t *testing.T) {
	got := paneLegendLine("legend:  @online  +pinned")
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "@online") {
		t.Errorf("paneLegendLine() plain = %q, want to contain '@online'", plain)
	}
}

func TestDimRow(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "plain-text-preserved",
			input: "hello world",
		},
		{
			name:  "ansi-stripped-and-re-applied",
			input: "\x1b[31mred text\x1b[0m",
		},
		{
			name:  "empty-string",
			input: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dimRow(tt.input)
			// Result must be a non-empty styled string (except for empty input).
			if tt.input == "" {
				// dimRow of empty should still be a styled empty string — no panic.
				return
			}
			plain := ansi.Strip(got)
			inputPlain := ansi.Strip(tt.input)
			if plain != inputPlain {
				t.Errorf(
					"dimRow() stripped text = %q, want %q (original stripped)",
					plain, inputPlain,
				)
			}
		})
	}
}

func TestDistanceBarCell(t *testing.T) {
	tests := []struct {
		name   string
		filled int
		barMax int
		rowBg  string
	}{
		{name: "zero-filled", filled: 0, barMax: 24, rowBg: rowBgOdd},
		{name: "full-bar", filled: 24, barMax: 24, rowBg: rowBgOdd},
		{name: "half-bar", filled: 12, barMax: 24, rowBg: rowBgOdd},
		{name: "negative-clamped", filled: -5, barMax: 10, rowBg: rowBgOdd},
		{name: "over-max-clamped", filled: 100, barMax: 10, rowBg: rowBgOdd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := distanceBarCell(tt.filled, tt.barMax, tt.rowBg)
			plain := ansi.Strip(got)
			// Plain text must be exactly barMax chars of '▓' and '░'.
			if len([]rune(plain)) != tt.barMax {
				t.Errorf(
					"distanceBarCell(%d, %d) plain length = %d, want %d",
					tt.filled, tt.barMax, len([]rune(plain)), tt.barMax,
				)
			}
		})
	}
}

func TestDistanceBarUnknownCell(t *testing.T) {
	got := distanceBarUnknownCell(10, rowBgOdd)
	plain := ansi.Strip(got)
	if len([]rune(plain)) != 10 {
		t.Errorf("distanceBarUnknownCell(10) plain length = %d, want 10", len([]rune(plain)))
	}
	if !strings.Contains(plain, "·") {
		t.Errorf("distanceBarUnknownCell() plain = %q, want '·' characters", plain)
	}
}

func TestEarlierCountLine(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want string
	}{
		{name: "one-earlier", n: 1, want: "1 earlier"},
		{name: "many-earlier", n: 42, want: "42 earlier"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := earlierCountLine(tt.n)
			plain := ansi.Strip(got)
			if !strings.Contains(plain, tt.want) {
				t.Errorf("earlierCountLine(%d) = %q, want to contain %q", tt.n, plain, tt.want)
			}
		})
	}
}

func TestHelpScrollIndicator(t *testing.T) {
	tests := []struct {
		name    string
		scroll  int
		total   int
		visible int
		wantFit bool // true if content fits — expect simple "close" hint
	}{
		{
			name:    "fits-shows-close-hint",
			scroll:  0,
			total:   10,
			visible: 20,
			wantFit: true,
		},
		{
			name:    "scrollable-shows-position",
			scroll:  5,
			total:   30,
			visible: 10,
			wantFit: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := helpScrollIndicator(tt.scroll, tt.total, tt.visible)
			plain := ansi.Strip(got)
			if tt.wantFit {
				if !strings.Contains(plain, "close") {
					t.Errorf("helpScrollIndicator (fits) = %q, want 'close'", plain)
				}
			} else {
				if !strings.Contains(plain, "line") {
					t.Errorf("helpScrollIndicator (scrollable) = %q, want 'line'", plain)
				}
			}
		})
	}
}

func TestTabCompletionFlashCell(t *testing.T) {
	matches := []matchItem{
		{display: "alpha", insert: "alpha"},
		{display: "beta", insert: "beta"},
		{display: "gamma", insert: "gamma"},
	}
	tests := []struct {
		name   string
		active int
		want   string
	}{
		{name: "first-active", active: 0, want: "alpha"},
		{name: "second-active", active: 1, want: "beta"},
		{name: "third-active", active: 2, want: "gamma"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tabCompletionFlashCell(matches, tt.active)
			plain := ansi.Strip(got)
			if !strings.Contains(plain, tt.want) {
				t.Errorf(
					"tabCompletionFlashCell(active=%d) plain = %q, want to contain %q",
					tt.active, plain, tt.want,
				)
			}
			// Counter "N/M" must be present.
			if !strings.Contains(plain, "/3") {
				t.Errorf(
					"tabCompletionFlashCell(active=%d) plain = %q, want '/3' counter",
					tt.active, plain,
				)
			}
		})
	}
}

func TestNodePresentationFor(t *testing.T) {
	recentTime := func() nodeItem {
		// Set LastHeardAt to now so CurrentState() returns StateOnline.
		n := nodeItem{Callsign: "KC7ABC"}
		n.LastHeardAt = time.Now()
		return n
	}
	tests := []struct {
		name       string
		node       nodeItem
		isSelf     bool
		isSelected bool
		wantSigil  string
	}{
		{
			name:      "online-node-has-at-sigil",
			node:      recentTime(),
			wantSigil: "@",
		},
		{
			name: "fav-node-overrides-to-plus",
			node: func() nodeItem {
				n := recentTime()
				n.Fav = true
				return n
			}(),
			wantSigil: "+",
		},
		{
			name:      "self-overrides-to-at",
			node:      recentTime(),
			isSelf:    true,
			wantSigil: "@",
		},
		{
			name: "offline-node-has-dot-sigil",
			node: nodeItem{
				Callsign: "KC7ABC",
				// Zero LastHeardAt + StateOffline → stateOffline presentation.
				State: stateOffline,
			},
			wantSigil: "·",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := nodePresentationFor(tt.node, tt.isSelf, tt.isSelected)
			if p.Sigil != tt.wantSigil {
				t.Errorf(
					"nodePresentationFor().Sigil = %q, want %q",
					p.Sigil, tt.wantSigil,
				)
			}
		})
	}
}

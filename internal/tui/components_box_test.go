// Copyright (c) 2026 John Dewey
//
// Boundary tests for the core layout primitives — Box / Text / Row /
// Cell + padCells. Shared fixtures + helpers (nastyFixtures,
// rangeOfWidths, assertExactBox) live here and are reused by
// components_stack_test.go for the higher-level composition primitives.
//
// The component contract is: Render(box) returns exactly box.Height
// lines of exactly box.Width cells. These tests feed every primitive
// nasty fixtures (compound emoji, ZWJ sequences, keycaps, control
// chars, multi-thousand-character strings) at every realistic terminal
// width and assert the contract holds.
//
// Any new component must add a fixture row here so its overflow
// behavior is covered before a render-time bug ever ships.
//
package tui
//
import (
	"strings"
	"testing"
//
	"github.com/charmbracelet/x/ansi"
)
//
// nastyFixtures is the canonical list of Unicode + length pathologies
// the layout primitives must handle without overflow.
var nastyFixtures = []string{
	"",
	"hello",
	"hello world",
	strings.Repeat("X", 5000),
	"line1\nline2\nline3",
	"👋",                            // 4-byte emoji (1 grapheme, 2 cells)
	"👋🏼",                           // skin-tone modifier (1 grapheme, 2 cells)
	"🙋🏼‍♂️",                        // ZWJ sequence (1 grapheme, 2 cells)
	"2️⃣",                          // keycap (1 grapheme, 2 cells per ansi)
	"⚠️",                           // VS16-promoted (1 grapheme, 2 cells per ansi)
	"🇺🇸",                           // regional indicator flag (1 grapheme, 2 cells)
	"á",                            // combining accent (1 grapheme, 1 cell)
	"\x1b[31m red \x1b[0m",         // ANSI escape — counted as 0 visible cells
	"AAA\nBBB\nCCC\nDDD\nEEE\nFFF", // many lines
}
//
// rangeOfWidths covers all the realistic terminal widths the UI will
// see plus the pending-wrap-edge values where bugs cluster.
var rangeOfWidths = []int{1, 2, 5, 10, 20, 40, 80, 120, 200, 206, 500}
//
func assertExactBox(t *testing.T, label, out string, box Box) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != box.Height {
		t.Errorf("%s: got %d lines, want %d (box=%+v)\nout=%q",
			label, len(lines), box.Height, box, out)
		return
	}
	for i, l := range lines {
		w := ansiCells(l)
		if w != box.Width {
			t.Errorf("%s: line %d width=%d, want %d (box=%+v)\nstripped=%q",
				label, i, w, box.Width, box, ansi.Strip(l))
		}
	}
}
//
// TestText_Render — Text's Render(box) contract: every (content × w ×
// h) combination from the nasty-fixture matrix must return exactly
// box.Height lines of exactly box.Width cells. Single mechanic
// (sweep + assert) so the matrix is one tight loop rather than rows.
func TestText_Render(t *testing.T) {
	for _, content := range nastyFixtures {
		for _, w := range rangeOfWidths {
			for _, h := range []int{1, 2, 5, 10, 50} {
				box := Box{Width: w, Height: h}
				out := Text{Content: content}.Render(box)
				assertExactBox(t,
					"Text content="+truncForLabel(content)+
						" w="+itoa(w)+" h="+itoa(h),
					out, box)
			}
		}
	}
}
//
// TestRow_Render — Row's Render(box) contract across the three
// distinct mechanics it must satisfy: mixed-cell layouts fill the box
// exactly, oversize content truncates, and flex children share the
// leftover budget. Sub-tests because mechanics genuinely diverge
// (different fixture shapes) but the surface is the one Render method.
func TestRow_Render(t *testing.T) {
	t.Run("fills-box-exactly-with-mixed-cells", func(t *testing.T) {
		for _, w := range rangeOfWidths {
			row := Row{Cells: []Cell{
				{Content: "👻", Width: 5},
				{Content: "node 0xabcdef", Width: 20},
				{Content: "2️⃣", Width: 5},
				{Content: "body text", Width: -1},
				{Content: "↝ 4h", Width: 7, Align: AlignRight},
				{Content: "5.0dB", Width: 8, Align: AlignRight},
			}}
			out := row.Render(Box{Width: w, Height: 1})
			got := ansiCells(out)
			if got != w {
				t.Errorf("Row w=%d got %d cells\nstripped=%q", w, got, ansi.Strip(out))
			}
		}
	})
//
	t.Run("oversize-content-truncates-to-box", func(t *testing.T) {
		row := Row{Cells: []Cell{
			{Content: strings.Repeat("X", 1000), Width: -1},
		}}
		for _, w := range rangeOfWidths {
			out := row.Render(Box{Width: w, Height: 1})
			got := ansiCells(out)
			if got != w {
				t.Errorf("w=%d got %d cells; long content must truncate to box",
					w, got)
			}
		}
	})
//
	t.Run("flex-children-share-leftover-budget", func(t *testing.T) {
		// 3 flex children + 1 fixed of width 6 in a box of 32.
		// Leftover = 26, split as 9+9+8 (first 2 get the +1 remainder).
		row := Row{Cells: []Cell{
			{Content: "fixed1", Width: 6},
			{Content: "flexA", Width: -1},
			{Content: "flexB", Width: -1},
			{Content: "flexC", Width: -1},
		}}
		out := row.Render(Box{Width: 32, Height: 1})
		if got := ansiCells(out); got != 32 {
			t.Fatalf("want 32 cells, got %d (%q)", got, ansi.Strip(out))
		}
	})
}
//
// TestPadCells — padCells is a package-level function, so the test
// name has no underscore. Scenarios are uniform (truncate-or-pad to
// width) so they land as table rows.
func TestPadCells(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		w     int
		wantW int
	}{
		{name: "shorter-than-budget-pads-out", in: "hello", w: 10, wantW: 10},
		{name: "longer-than-budget-truncates", in: "hello world", w: 5, wantW: 5},
		{name: "truncates-on-grapheme-boundary", in: "👋🏼 hello", w: 4, wantW: 4},
		{name: "keycap-cluster-stays-intact", in: "2️⃣ 6️⃣ 7️⃣", w: 7, wantW: 7},
		{name: "long-ascii-truncates-cleanly", in: strings.Repeat("X", 100), w: 20, wantW: 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := padCells(c.in, c.w)
			if w := ansiCells(got); w != c.wantW {
				t.Errorf("padCells(%q, %d) → width %d, want %d (got=%q)",
					c.in, c.w, w, c.wantW, got)
			}
		})
	}
}
//
// itoa is a test-helper to keep error messages noise-free without
// pulling fmt.Sprintf at call sites.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
//
func truncForLabel(s string) string {
	if len(s) > 20 {
		return s[:20] + "..."
	}
	return s
}

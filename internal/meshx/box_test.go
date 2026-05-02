// Copyright (c) 2026 John Dewey
//
// Boundary tests for the layout primitives.
//
// The component contract is: Render(box) returns exactly box.Height
// lines of exactly box.Width cells. These tests feed every primitive
// nasty fixtures (compound emoji, ZWJ sequences, keycaps, control
// chars, multi-thousand-character strings) at every realistic terminal
// width and assert the contract holds.
//
// Any new component must add a fixture row here so its overflow
// behavior is covered before a render-time bug ever ships.

package meshx

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

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
	"á",                           // combining accent (1 grapheme, 1 cell)
	"\x1b[31m red \x1b[0m",         // ANSI escape — counted as 0 visible cells
	"AAA\nBBB\nCCC\nDDD\nEEE\nFFF", // many lines
}

// rangeOfWidths covers all the realistic terminal widths the UI will
// see plus the pending-wrap-edge values where bugs cluster.
var rangeOfWidths = []int{1, 2, 5, 10, 20, 40, 80, 120, 200, 206, 500}

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

func TestText_FillsBoxExactly(t *testing.T) {
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

func TestRow_FillsBoxExactly(t *testing.T) {
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
}

func TestRow_ContentOverflowTruncates(t *testing.T) {
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
}

func TestRow_FlexDistribution(t *testing.T) {
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
}

func TestVStack_StackedHeightsExact(t *testing.T) {
	stack := VStack{Children: []SizedChild{
		{Comp: Text{Content: "top"}, Size: 1},
		{Comp: Text{Content: "middle"}, Size: -1},
		{Comp: Text{Content: "bottom"}, Size: 1},
	}}
	for _, w := range rangeOfWidths {
		for _, h := range []int{3, 5, 10, 50} {
			box := Box{Width: w, Height: h}
			out := stack.Render(box)
			assertExactBox(t,
				"VStack w="+itoa(w)+" h="+itoa(h), out, box)
		}
	}
}

func TestHStack_SideBySideExact(t *testing.T) {
	stack := HStack{Children: []SizedChild{
		{Comp: Text{Content: "L"}, Size: 10},
		{Comp: Text{Content: "M"}, Size: -1},
		{Comp: Text{Content: "R"}, Size: 10},
	}}
	for _, w := range []int{30, 80, 200} {
		for _, h := range []int{1, 5, 20} {
			box := Box{Width: w, Height: h}
			out := stack.Render(box)
			assertExactBox(t, "HStack", out, box)
		}
	}
}

func TestBordered_InnerBudgetSubtracted(t *testing.T) {
	inner := Text{Content: strings.Repeat("X\n", 50)}
	for _, w := range []int{10, 80, 200, 206} {
		for _, h := range []int{5, 20, 50} {
			box := Box{Width: w, Height: h}
			out := Bordered{
				Inner: inner,
				Chars: DoubleBorder,
			}.Render(box)
			assertExactBox(t, "Bordered", out, box)
		}
	}
}

func TestBordered_ContractEnforced_NoOverflow(t *testing.T) {
	// Inner that emits a 5000-char line: Bordered must absorb it via
	// padCells truncation; outer rows still match Box.Width.
	inner := Text{Content: strings.Repeat("Z", 5000)}
	box := Box{Width: 80, Height: 10}
	out := Bordered{Inner: inner, Chars: NormalBorder}.Render(box)
	assertExactBox(t, "Bordered+long-inner", out, box)
}

func TestPadCells_TruncatesEmojiAtClusterBoundary(t *testing.T) {
	cases := []struct {
		in    string
		w     int
		wantW int
	}{
		{"hello", 10, 10},
		{"hello world", 5, 5},
		{"👋🏼 hello", 4, 4},
		{"2️⃣ 6️⃣ 7️⃣", 7, 7},
		{strings.Repeat("X", 100), 20, 20},
	}
	for _, c := range cases {
		got := padCells(c.in, c.w)
		if w := ansiCells(got); w != c.wantW {
			t.Errorf("padCells(%q, %d) → width %d, want %d (got=%q)",
				c.in, c.w, w, c.wantW, got)
		}
	}
}

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

func truncForLabel(s string) string {
	if len(s) > 20 {
		return s[:20] + "..."
	}
	return s
}

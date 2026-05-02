// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining
// a copy of this software and associated documentation files (the
// "Software"), to deal in the Software without restriction, including
// without limitation the rights to use, copy, modify, merge, publish,
// distribute, sublicense, and/or sell copies of the Software, and to
// permit persons to whom the Software is furnished to do so, subject
// to the following conditions:
//
// The above copyright notice and this permission notice shall be
// included in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND.

// Component-tree layout primitives.
//
// Every region of the UI is a Component that, given a Box budget,
// returns output sized EXACTLY to that budget — no row wider than
// box.Width, no taller than box.Height. Parents own the math; children
// fill what they're given.
//
// The contract is one-way: a parent computes per-child boxes and the
// child fills them. There is no upward negotiation, no "give me as
// much as I need," no implicit overflow. A Row that would overflow
// truncates with an ellipsis. A Text that's too short pads with
// spaces. A nested layout that disagrees with its budget panics in
// dev mode (MESHX_LAYOUT_ASSERT=1) so the regression is caught at
// the offending call site rather than as a visible artifact two
// rerenders later.
//
// This is the architectural fix for the pending-wrap class of bugs:
// the top-level frame Box subtracts 1 from terminal width once, in
// View(), so nothing in the tree can be asked to render to the very
// last column. SafeWidth is enforced at the edge, not negotiated by
// every leaf.

package meshx

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// Box is the cell budget a Component is allowed to fill.
//
// Width and Height are exact, not maxima — Component.Render must
// return precisely Height lines, each precisely Width cells (per
// ansi.StringWidth, the same library bubbletea + the terminal use
// for measurement). Any deviation is a bug, caught by the dev-mode
// assertion in renderAndCheck.
type Box struct {
	Width, Height int
}

// Empty returns a box with no area — useful for stack layouts that
// need to drop a child entirely (e.g., width-collapse a sidebar).
func (b Box) Empty() bool { return b.Width <= 0 || b.Height <= 0 }

// Component is the layout-tree contract: given a Box, produce output
// of exactly that size.
//
// Implementations MUST guarantee:
//
//   - The returned string contains exactly box.Height lines (separated
//     by '\n'; no trailing newline).
//   - Each line measures exactly box.Width cells per ansi.StringWidth.
//
// Violations panic in dev mode (MESHX_LAYOUT_ASSERT=1) so regressions
// surface at the offending Render rather than as cumulative drift in
// the rendered terminal.
type Component interface {
	Render(box Box) string
}

// Align selects horizontal alignment within a fixed-width Cell.
type Align int

// Horizontal alignment values for Cell content within its width.
// AlignLeft right-pads the cell content to its declared width.
// AlignRight left-pads. AlignCenter splits the gap evenly.
const (
	AlignLeft Align = iota
	AlignRight
	AlignCenter
)

// alignAssertEnv is the env var that turns on dev-mode invariant
// checks. Set MESHX_LAYOUT_ASSERT=1 to make every component panic
// the moment its Render output doesn't match the requested Box.
const alignAssertEnv = "MESHX_LAYOUT_ASSERT"

// renderAndCheck invokes a Component.Render and, in assert mode,
// validates the output matches the requested Box exactly. Used by
// composition primitives so a buggy child surfaces with a clear
// message at the parent's call site.
func renderAndCheck(c Component, box Box) string {
	out := c.Render(box)
	if os.Getenv(alignAssertEnv) != "1" {
		return out
	}
	if box.Empty() {
		if out != "" {
			panic(fmt.Sprintf("layout: %T returned %q for empty box %+v",
				c, out, box))
		}
		return out
	}
	lines := strings.Split(out, "\n")
	if len(lines) != box.Height {
		panic(fmt.Sprintf(
			"layout: %T returned %d lines, box wants %d (box=%+v)",
			c, len(lines), box.Height, box))
	}
	for i, l := range lines {
		w := ansiCells(l)
		if w != box.Width {
			panic(fmt.Sprintf(
				"layout: %T line %d width=%d, box wants %d (box=%+v)\nline=%q",
				c, i, w, box.Width, box, ansi.Strip(l)))
		}
	}
	return out
}

// ansiCells is the canonical cell-width measurement for the layout
// pipeline — the SAME function the renderer must use everywhere so
// the right ║ pane border lands in the same column on every row.
//
// We start from ansi.StringWidth (handles ANSI CSI escapes + grapheme
// clusters) and apply ONE correction: any grapheme cluster containing
// VS16 (U+FE0F) or COMBINING ENCLOSING KEYCAP (U+20E3) — the keycap
// emoji "1️⃣ 7️⃣ ⚠️" — is promoted to 2 cells. iTerm2 / Ghostty /
// Terminal.app render those clusters as a wide boxed-digit glyph
// (Unicode TR51 emoji-presentation rules), but every Go width library
// in the dependency tree (ansi.StringWidth, uniseg, runewidth) reports
// them as 1 cell. Without this correction a message body of "7️⃣"
// gets padded with one extra space, the row is 1 visual cell wider
// than declared, and the right ║ frame walks out of column.
func ansiCells(s string) int {
	if s == "" {
		return 0
	}
	if !strings.ContainsRune(s, '️') &&
		!strings.ContainsRune(s, '⃣') {
		return ansi.StringWidth(s)
	}
	g := uniseg.NewGraphemes(ansi.Strip(s))
	n := 0
	for g.Next() {
		cluster := g.Str()
		w := ansi.StringWidth(cluster)
		if (strings.ContainsRune(cluster, '️') ||
			strings.ContainsRune(cluster, '⃣')) && w < 2 {
			w = 2
		}
		n += w
	}
	return n
}

// padCells right-pads s to exactly w cells using ansi.StringWidth as
// the measurement. Truncates with an ellipsis if s is too long; pads
// with spaces if too short.
//
// Built on charmbracelet/x/ansi.Truncate which:
//   - skips ANSI CSI escapes from the cell count (so styled content
//     like lipgloss.Render output is measured correctly);
//   - iterates by grapheme cluster, so compound emoji (👋🏼 skin
//     tone, 🙋🏼‍♂️ ZWJ, 2️⃣ keycap, regional-indicator flags) are
//     never split mid-cluster.
//
// ansi.StringWidth is the same measurement bubbletea, lipgloss, and
// the terminal use, so a string padded by padCells fits the budget
// in every layer of the rendering pipeline. This is the critical
// invariant that prevents pending-wrap drift.
func padCells(s string, w int) string {
	if w <= 0 {
		return ""
	}
	cur := ansiCells(s)
	if cur == w {
		return s
	}
	if cur < w {
		return s + strings.Repeat(" ", w-cur)
	}
	// Overlong — ansi.Truncate handles ANSI-aware grapheme-aware
	// cutting, preserving CSI sequences across the cut so styled
	// prefixes (the input-bar's `[#default] ›`) keep their colors
	// instead of dropping to plain white when typing pushes past the
	// row width. The "…" tail is itself 1 cell, so we ask for w cells
	// total INCLUDING the ellipsis.
	out := ansi.Truncate(s, w, "…")
	// ansi.Truncate measures with ansi.StringWidth, which under-counts
	// keycap emoji (Unicode TR51 emoji-presentation sequences). When
	// the input contains keycaps, ansi.Truncate may return a string
	// that ansi.StringWidth says fits in w but ansiCells says is
	// wider. Iteratively drop one grapheme at a time until ansiCells
	// fits, then re-pad to exactly w. Strip ANSI for the walk to
	// avoid splitting an SGR mid-byte; in the pathological keycap-
	// overflow branch losing inline styling is acceptable.
	if ansiCells(out) <= w {
		return out
	}
	stripped := ansi.Strip(out)
	g := uniseg.NewGraphemes(stripped)
	var b strings.Builder
	used := 0
	for g.Next() {
		cluster := g.Str()
		cw := ansiCells(cluster)
		if used+cw > w-1 {
			break
		}
		b.WriteString(cluster)
		used += cw
	}
	b.WriteRune('…')
	used++
	res := b.String()
	if used < w {
		res = res[:len(res)-len("…")] +
			strings.Repeat(" ", w-used) + "…"
	}
	return res
}

// alignCells fits s into exactly w cells with the given horizontal
// alignment. Right-padding for AlignLeft, left-padding for AlignRight,
// equal padding for AlignCenter. Truncation with ellipsis on overflow.
func alignCells(s string, w int, a Align) string {
	if w <= 0 {
		return ""
	}
	cur := ansiCells(s)
	if cur >= w {
		return padCells(s, w)
	}
	gap := w - cur
	switch a {
	case AlignRight:
		return strings.Repeat(" ", gap) + s
	case AlignCenter:
		left := gap / 2
		right := gap - left
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
	default:
		return s + strings.Repeat(" ", gap)
	}
}

// Cell is one segment of a Row — a styled, measured chunk of content
// that will be padded/truncated to its declared Width.
//
// A Width of -1 means "flex" — the parent Row distributes leftover
// cells among all flex children equally. Style is applied to the
// padded result so background fills extend through the whole cell.
type Cell struct {
	Content string
	Width   int // exact cell budget; -1 = flex
	Align   Align
	Style   styler // optional styling wrapper, applied AFTER pad/truncate
}

// styler is a deliberately minimal interface: we accept anything that
// turns a plain string into a styled string. lipgloss.Style satisfies
// this via .Render. Keeping the interface small means we can swap
// styling implementations without rewriting components.
type styler interface {
	Render(strs ...string) string
}

// Row is a horizontal sequence of Cells. Total cell width must be
// fixed (after flex distribution); any leftover is filled with a
// trailing pad cell so the row exactly fills the parent's Box.Width.
//
// Width semantics:
//
//   - Cell.Width >= 0: that cell takes exactly that many cells.
//   - Cell.Width < 0:  flex; remaining width split evenly across all
//     flex cells.
//
// Row is the ONLY place horizontal cell allocation happens. If you
// want overlapping content, two columns side-by-side, or a percent
// split, build it with Row + nested HStack — Row guarantees that the
// final string is exactly Box.Width cells, no exceptions.
type Row struct {
	Cells []Cell
	// FillStyle, when non-nil, styles any trailing pad space added
	// to reach Box.Width. Used by message rows so the zebra-stripe
	// row background extends to the right edge.
	FillStyle styler
}

// Render produces the row at exactly box.Width cells × 1 line.
// Box.Height is ignored (rows are 1 line by definition); use VStack
// to stack rows vertically.
func (r Row) Render(box Box) string {
	if box.Width <= 0 {
		return ""
	}
	// Phase 1: distribute width.
	fixed := 0
	flexN := 0
	for _, c := range r.Cells {
		if c.Width < 0 {
			flexN++
		} else {
			fixed += c.Width
		}
	}
	leftover := box.Width - fixed
	if leftover < 0 {
		leftover = 0
	}
	flexEach := 0
	flexExtra := 0 // distribute remainder among the first N flex cells
	if flexN > 0 {
		flexEach = leftover / flexN
		flexExtra = leftover % flexN
	}
	// Phase 2: render each cell at its allocated width.
	var b strings.Builder
	emitted := 0
	for _, c := range r.Cells {
		w := c.Width
		if w < 0 {
			w = flexEach
			if flexExtra > 0 {
				w++
				flexExtra--
			}
		}
		if w <= 0 {
			continue
		}
		if emitted+w > box.Width {
			w = box.Width - emitted
		}
		piece := alignCells(c.Content, w, c.Align)
		if c.Style != nil {
			piece = c.Style.Render(piece)
		}
		b.WriteString(piece)
		emitted += w
		if emitted >= box.Width {
			break
		}
	}
	// Phase 3: trailing fill if cells didn't add up to the full budget.
	if emitted < box.Width {
		gap := box.Width - emitted
		fill := strings.Repeat(" ", gap)
		if r.FillStyle != nil {
			fill = r.FillStyle.Render(fill)
		}
		b.WriteString(fill)
	}
	return b.String()
}

// Text is a single-string component padded/truncated to fill its Box
// exactly: every line padded to Box.Width, lines added or dropped to
// reach Box.Height.
//
// Multiline content is split on '\n'; lines longer than Box.Width are
// truncated with an ellipsis. If the content has fewer lines than
// Box.Height, blank lines (Box.Width spaces, optionally styled) are
// appended. Callers who want vertical centering or top/bottom anchor
// should wrap Text in a VStack with explicit spacer children.
type Text struct {
	Content string
	Style   styler
	// FillStyle styles the blank pad lines added below short content.
	// When nil, pad lines are unstyled spaces.
	FillStyle styler
}

// Render returns Box.Height lines of Box.Width cells.
func (t Text) Render(box Box) string {
	if box.Empty() {
		return ""
	}
	lines := strings.Split(t.Content, "\n")
	out := make([]string, 0, box.Height)
	for i := 0; i < box.Height; i++ {
		var line string
		if i < len(lines) {
			line = padCells(lines[i], box.Width)
			if t.Style != nil {
				line = t.Style.Render(line)
			}
		} else {
			line = strings.Repeat(" ", box.Width)
			if t.FillStyle != nil {
				line = t.FillStyle.Render(line)
			} else if t.Style != nil {
				line = t.Style.Render(line)
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// Spacer fills a Box with blank cells. Used by stack layouts to
// reserve space (gutters, alignment slots, vertical fillers).
type Spacer struct {
	Style styler
}

// Render returns Box.Height blank lines of Box.Width cells.
func (s Spacer) Render(box Box) string {
	return Text{Content: "", Style: s.Style}.Render(box)
}

// ComponentFunc lets callers build a Component from a closure without
// declaring a struct. Used for one-off renderers (e.g., the active
// pane body, where the rendering depends on overlay state).
type ComponentFunc func(box Box) string

// Render satisfies the Component interface by calling the wrapped
// closure. Lets a function literal stand in anywhere a Component is
// expected — same pattern as net/http's HandlerFunc.
func (f ComponentFunc) Render(box Box) string { return f(box) }

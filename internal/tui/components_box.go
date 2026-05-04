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

package tui

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

// layoutAssertEnabled is read once at process start. renderAndCheck is
// on the per-frame hot path (every pane, every keystroke), so resolving
// the env var via os.Getenv on each call is a real allocation cost for
// the assertion-off case that's the production default.
var layoutAssertEnabled = os.Getenv(alignAssertEnv) == "1"

// renderAndCheck invokes a Component.Render and, in assert mode,
// validates the output matches the requested Box exactly. Used by
// composition primitives so a buggy child surfaces with a clear
// message at the parent's call site.
func renderAndCheck(c Component, box Box) string {
	out := c.Render(box)
	if !layoutAssertEnabled {
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
			c, len(lines), box.Height, box,
		))
	}
	for i, l := range lines {
		w := ansiCells(l)
		if w != box.Width {
			panic(fmt.Sprintf(
				"layout: %T line %d width=%d, box wants %d (box=%+v)\nline=%q",
				c, i, w, box.Width, box, ansi.Strip(l),
			))
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

// renderCell fits a Cell into exactly w cells: content + alignment
// padding, with PadStyle applied to the pad spaces only and Style
// (when set) applied as an outer wrap. Splitting the pad path lets
// callers tint trailing cell-fill in rowBg without disturbing the
// content's existing ANSI codes — the chat/notice row body cells
// rely on this so the zebra background extends past short message
// text out to the metrics columns.
func renderCell(c Cell, w int) string {
	if w <= 0 {
		return ""
	}
	cur := ansiCells(c.Content)
	if cur >= w {
		out := padCells(c.Content, w)
		if c.Style != nil {
			out = c.Style.Render(out)
		}
		return out
	}
	gap := w - cur
	var leftPad, rightPad string
	switch c.Align {
	case AlignRight:
		leftPad = strings.Repeat(" ", gap)
	case AlignCenter:
		l := gap / 2
		r := gap - l
		leftPad = strings.Repeat(" ", l)
		rightPad = strings.Repeat(" ", r)
	default:
		rightPad = strings.Repeat(" ", gap)
	}
	if c.PadStyle != nil {
		if leftPad != "" {
			leftPad = c.PadStyle.Render(leftPad)
		}
		if rightPad != "" {
			rightPad = c.PadStyle.Render(rightPad)
		}
	}
	out := leftPad + c.Content + rightPad
	if c.Style != nil {
		out = c.Style.Render(out)
	}
	return out
}

// Cell is one segment of a Row — a styled, measured chunk of content
// that will be padded/truncated to its declared Width.
//
// A Width of -1 means "flex" — the parent Row distributes leftover
// cells among all flex children equally. Style is applied to the
// padded result so background fills extend through the whole cell.
//
// PadStyle, when set, styles ONLY the spaces renderCell appends to
// fill the cell to its declared width. This is the Right Way to
// extend a row-bg tint past the end of styled content: the content's
// own ANSI codes (lipgloss bake-in fg+bg) stay untouched, and the
// trailing pad spaces get an SGR span tinted to match. Wrapping the
// whole cell with a single bg styler (Style) instead breaks because
// the content's inner `\e[0m` resets terminate the outer span mid-
// cell — the same bug class wrapSelection had to dodge for selection
// highlights.
type Cell struct {
	Content  string
	Width    int // exact cell budget; -1 = flex
	Align    Align
	Style    styler // optional styling wrapper, applied AFTER pad/truncate
	PadStyle styler // optional styler for cell-internal pad spaces only
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
		piece := renderCell(c, w)
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

// Viewport is a scrollable single-pane window over a slice of pre-
// styled lines. Given a list of Lines, a Scroll offset (0-based), and
// the rest of the layout (Reserved rows for chrome, optional Footer
// shown beneath the viewport content), Render returns a block of
// exactly Box.Height lines × Box.Width cells that displays
// Lines[clamped(Scroll) : clamped(Scroll)+visible] padded out.
//
// The clamp is one-way: Scroll is silently pulled back into range if
// it exceeds the content. The caller (model state) doesn't need to
// know the visible-row count; it just bumps Scroll and the Component
// figures out the math from its Box.
//
// Reserved tells the Component how many rows of its Box are already
// committed to non-content chrome (like a "line N/M" indicator
// appended to the output). The visible-row budget is Box.Height -
// Reserved. Set Reserved to len(Footer) when wiring the helper:
// helpPane uses Reserved=2 (a blank row + a one-line scroll indicator).
//
// Footer is rendered AFTER the viewport content and BEFORE any final
// blank padding rows. Each Footer line is padded to Box.Width via
// padCells, so callers can hand pre-styled chrome (italic dim, etc.)
// without knowing the box dimensions.
type Viewport struct {
	Lines    []string
	Scroll   int
	Reserved int
	Footer   []string
}

// Render fills box with the visible window of Lines plus Footer.
// Output has exactly box.Height lines, each exactly box.Width cells
// per ansiCells. Blank rows are appended below if the visible content
// + Footer don't fill the box.
func (v Viewport) Render(box Box) string {
	if box.Empty() {
		return ""
	}
	visible := box.Height - v.Reserved
	if visible < 1 {
		visible = 1
	}
	scroll := v.Scroll
	maxScroll := len(v.Lines) - visible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	end := scroll + visible
	if end > len(v.Lines) {
		end = len(v.Lines)
	}

	out := make([]string, 0, box.Height)
	for i := scroll; i < end; i++ {
		out = append(out, padCells(v.Lines[i], box.Width))
	}
	// Pad the visible-content region with blank rows so the footer
	// always sits at its declared offset (Box.Height - Reserved + 1)
	// rather than floating up against the last content line.
	for i := end - scroll; i < visible; i++ {
		out = append(out, strings.Repeat(" ", box.Width))
	}
	for _, fl := range v.Footer {
		out = append(out, padCells(fl, box.Width))
	}
	for len(out) < box.Height {
		out = append(out, strings.Repeat(" ", box.Width))
	}
	if len(out) > box.Height {
		out = out[:box.Height]
	}
	return strings.Join(out, "\n")
}

// RawBlock wraps a pre-rendered multi-line string and Renders it sized
// to a Box. Each line is right-padded to Box.Width via padCells
// (keycap-aware) and the block is padded out to Box.Height with blank
// rows. Lines wider than Box.Width are truncated with an ellipsis.
//
// This is the bridge between legacy string-emitting renderers and the
// Component layout tree: until every pane is a real composed Component
// tree, the orchestrators (renderChannelsPane / renderMessagesPane /
// etc.) emit pre-formed strings and wrap them in RawBlock at the
// pane → Bordered seam. Replaces the previous fitToBox function and
// the inline ComponentFunc{strings.Split + padCells + strings.Repeat}
// adapters that lived in renderBorderedPane and renderHelpView — same
// behavior, one canonical implementation.
type RawBlock struct {
	Content string
}

// Render satisfies Component for RawBlock. Empty box short-circuits
// to "" so an off-screen pane doesn't allocate. Lines are split on
// '\n'; missing lines are filled with all-space rows so the output
// always meets the box.Height contract.
func (r RawBlock) Render(box Box) string {
	if box.Empty() {
		return ""
	}
	lines := strings.Split(r.Content, "\n")
	out := make([]string, box.Height)
	for i := 0; i < box.Height; i++ {
		if i < len(lines) {
			out[i] = padCells(lines[i], box.Width)
		} else {
			out[i] = strings.Repeat(" ", box.Width)
		}
	}
	return strings.Join(out, "\n")
}

// Centered horizontally centers a single-line or multi-line content
// block within its parent's Box. Each line of Content is measured
// via ansiCells (the canonical keycap-aware width) and padded with
// (Box.Width - lineW)/2 leading spaces so the visible content's
// midpoint lands at the box's midpoint.
//
// This is the "center globally" primitive: hand it a Box of any
// width and it centers the content against THAT width, regardless
// of how deeply nested the parent is. A pane that wants its splash
// art pane-centered just wraps the art in Centered and feeds it the
// pane's inner Box; a modal that wants a single-line title centered
// does the same. Multi-line content centers each line independently
// — the right call for ASCII-art banners where each line has its
// own width and you want every line to share the same midpoint.
//
// FillStyle styles the leading + trailing pad on each line; CellPad
// stays styled by the content's own ANSI codes since lipgloss spans
// reset at \e[0m. Set FillStyle to a row-bg styler when the parent
// expects the row background to extend through the whole Box.
type Centered struct {
	Content   string
	FillStyle styler
	// VAlign: AlignLeft (top), AlignCenter (vertical center),
	// AlignRight (bottom). Defaults to top — extra Box.Height beyond
	// content lines pad below.
	VAlign Align
}

// Render fills box with Content horizontally + vertically centered.
// Content is split on '\n'; each line is padded to box.Width with
// equal leading + trailing space. Vertical alignment positions the
// content block within box.Height; surrounding rows are blank
// (FillStyle-tinted) lines.
func (c Centered) Render(box Box) string {
	if box.Empty() {
		return ""
	}
	lines := strings.Split(c.Content, "\n")
	contentH := len(lines)
	if contentH > box.Height {
		contentH = box.Height
		lines = lines[:contentH]
	}
	topPad, bottomPad := 0, box.Height-contentH
	switch c.VAlign {
	case AlignCenter:
		topPad = (box.Height - contentH) / 2
		bottomPad = box.Height - contentH - topPad
	case AlignRight:
		topPad = box.Height - contentH
		bottomPad = 0
	}
	blank := strings.Repeat(" ", box.Width)
	if c.FillStyle != nil {
		blank = c.FillStyle.Render(blank)
	}
	out := make([]string, 0, box.Height)
	for i := 0; i < topPad; i++ {
		out = append(out, blank)
	}
	for _, line := range lines {
		w := ansiCells(line)
		if w >= box.Width {
			out = append(out, padCells(line, box.Width))
			continue
		}
		gap := box.Width - w
		left := gap / 2
		right := gap - left
		leftPad := strings.Repeat(" ", left)
		rightPad := strings.Repeat(" ", right)
		if c.FillStyle != nil {
			leftPad = c.FillStyle.Render(leftPad)
			rightPad = c.FillStyle.Render(rightPad)
		}
		out = append(out, leftPad+line+rightPad)
	}
	for i := 0; i < bottomPad; i++ {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

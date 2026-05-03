// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining
// a copy of this software and associated documentation files (the
// "Software"), to deal in the Software without restriction.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND.

// Composition primitives for the Component tree.
//
// VStack stacks children vertically with explicit per-child heights;
// HStack does the same horizontally. Bordered wraps a child in a box
// border, subtracting border + padding cells from the inner Box.
//
// All three enforce the contract that no child overflows the parent:
// VStack/HStack distribute the parent's Box exactly, never giving a
// child more cells than fit and never letting a child take what it
// wasn't allocated; Bordered hands its child an inner Box that is
// strictly smaller than its own outer Box.

package tui

import (
	"strings"
)

// SizedChild pairs a Component with its size budget along the stack's
// flow axis (height in VStack, width in HStack).
//
// Size semantics:
//
//   - Size >= 0: exact cell budget along the flow axis.
//   - Size <  0: flex; the remainder of the parent's flow-axis budget
//     is split evenly across all flex children. Multiple flex children
//     each get an equal share with the remainder distributed to the
//     first N children (so total adds up exactly).
//
// The cross-axis (width in VStack, height in HStack) always equals
// the parent's, so children fill the full perpendicular extent.
type SizedChild struct {
	Comp Component
	Size int
}

// VStack stacks Children vertically. Children's widths fill the
// parent's Box.Width; heights are taken from each SizedChild.Size,
// summing to exactly Box.Height after flex distribution.
type VStack struct {
	Children []SizedChild
}

// Render returns Box.Height lines of Box.Width cells, composed from
// the children's outputs in order.
func (s VStack) Render(box Box) string {
	if box.Empty() {
		return ""
	}
	heights := distribute(box.Height, s.Children)
	var rendered []string
	for i, c := range s.Children {
		h := heights[i]
		if h <= 0 {
			continue
		}
		child := renderAndCheck(c.Comp, Box{Width: box.Width, Height: h})
		rendered = append(rendered, child)
	}
	return strings.Join(rendered, "\n")
}

// HStack lays Children left-to-right. Children's heights fill the
// parent's Box.Height; widths are taken from each SizedChild.Size.
type HStack struct {
	Children []SizedChild
}

// Render returns Box.Height lines of Box.Width cells, composed from
// the children's outputs side-by-side.
func (s HStack) Render(box Box) string {
	if box.Empty() {
		return ""
	}
	widths := distribute(box.Width, s.Children)
	// Render each child to a slice of lines.
	rows := make([][]string, len(s.Children))
	for i, c := range s.Children {
		w := widths[i]
		if w <= 0 {
			rows[i] = make([]string, box.Height)
			for j := range rows[i] {
				rows[i][j] = ""
			}
			continue
		}
		out := renderAndCheck(c.Comp, Box{Width: w, Height: box.Height})
		rows[i] = strings.Split(out, "\n")
		// Defensive: if a child returned fewer lines than expected
		// (shouldn't happen — renderAndCheck would have caught it in
		// assert mode), pad to the expected height with blank lines.
		for len(rows[i]) < box.Height {
			rows[i] = append(rows[i], strings.Repeat(" ", w))
		}
	}
	// Stitch lines side-by-side.
	out := make([]string, box.Height)
	for r := 0; r < box.Height; r++ {
		var b strings.Builder
		for i := range s.Children {
			b.WriteString(rows[i][r])
		}
		out[r] = b.String()
	}
	return strings.Join(out, "\n")
}

// distribute divides total cells across len(children) slots based on
// each SizedChild.Size, sharing flex (-1) slots equally with the
// remainder distributed across the first N flex children.
func distribute(total int, children []SizedChild) []int {
	out := make([]int, len(children))
	fixed := 0
	flexN := 0
	for _, c := range children {
		if c.Size < 0 {
			flexN++
		} else {
			fixed += c.Size
		}
	}
	leftover := total - fixed
	if leftover < 0 {
		leftover = 0
	}
	flexEach := 0
	flexExtra := 0
	if flexN > 0 {
		flexEach = leftover / flexN
		flexExtra = leftover % flexN
	}
	for i, c := range children {
		if c.Size >= 0 {
			out[i] = c.Size
			continue
		}
		out[i] = flexEach
		if flexExtra > 0 {
			out[i]++
			flexExtra--
		}
	}
	return out
}

// BorderChars defines the glyph set a Bordered uses for its frame.
// Allows DoubleBorder (╔ ═ ╗ ║ ╚ ╝) and NormalBorder (┌ ─ ┐ │ └ ┘)
// without coupling to lipgloss.
type BorderChars struct {
	TopLeft, Top, TopRight          string
	Left, Right                     string
	BottomLeft, Bottom, BottomRight string
}

// DoubleBorder is the heavy double-line frame used by focused panes.
var DoubleBorder = BorderChars{
	TopLeft: "╔", Top: "═", TopRight: "╗",
	Left: "║", Right: "║",
	BottomLeft: "╚", Bottom: "═", BottomRight: "╝",
}

// NormalBorder is the thin single-line frame used by unfocused panes.
var NormalBorder = BorderChars{
	TopLeft: "┌", Top: "─", TopRight: "┐",
	Left: "│", Right: "│",
	BottomLeft: "└", Bottom: "─", BottomRight: "┘",
}

// Bordered wraps Inner in a frame, subtracting 2 cells (left+right)
// from width and 2 cells (top+bottom) from height before passing the
// remaining budget to Inner. Optional Padding adds extra blank cells
// inside the border but outside the inner content area.
//
// Inner can never overflow the outer Box: it receives a strictly
// smaller box and the renderAndCheck assertion verifies it filled
// exactly that smaller box.
type Bordered struct {
	Inner Component
	Chars BorderChars
	// BorderStyle styles the border glyphs themselves (e.g. mesh-green
	// for focused, dim lavender for unfocused).
	BorderStyle styler
	// Padding adds blank cells inside the border, outside Inner. Order
	// is [top, right, bottom, left].
	Padding [4]int
	// PadStyle styles the padding cells. Useful for full-bleed panel
	// backgrounds; nil leaves padding as plain spaces.
	PadStyle styler
}

// Render produces the bordered frame at exactly box.Width × box.Height.
func (b Bordered) Render(box Box) string {
	if box.Width < 2 || box.Height < 2 {
		// Degenerate: fall back to Spacer so the parent's box budget
		// is still satisfied.
		return Spacer{}.Render(box)
	}
	innerW := box.Width - 2 - b.Padding[1] - b.Padding[3]
	innerH := box.Height - 2 - b.Padding[0] - b.Padding[2]
	if innerW < 0 {
		innerW = 0
	}
	if innerH < 0 {
		innerH = 0
	}
	// Top border row: ╔═══...═══╗
	top := b.Chars.TopLeft +
		strings.Repeat(b.Chars.Top, box.Width-2) +
		b.Chars.TopRight
	if b.BorderStyle != nil {
		top = b.BorderStyle.Render(top)
	}
	// Bottom border row.
	bot := b.Chars.BottomLeft +
		strings.Repeat(b.Chars.Bottom, box.Width-2) +
		b.Chars.BottomRight
	if b.BorderStyle != nil {
		bot = b.BorderStyle.Render(bot)
	}
	// Inner content: render Inner at the smaller box, then wrap each
	// line with side rails and any horizontal padding.
	var innerLines []string
	if innerW > 0 && innerH > 0 {
		out := renderAndCheck(b.Inner, Box{Width: innerW, Height: innerH})
		innerLines = strings.Split(out, "\n")
	}
	// Side rails for content + padding rows.
	leftRail := b.Chars.Left
	rightRail := b.Chars.Right
	if b.BorderStyle != nil {
		leftRail = b.BorderStyle.Render(b.Chars.Left)
		rightRail = b.BorderStyle.Render(b.Chars.Right)
	}
	hpadL := strings.Repeat(" ", b.Padding[3])
	hpadR := strings.Repeat(" ", b.Padding[1])
	if b.PadStyle != nil {
		if b.Padding[3] > 0 {
			hpadL = b.PadStyle.Render(hpadL)
		}
		if b.Padding[1] > 0 {
			hpadR = b.PadStyle.Render(hpadR)
		}
	}
	wrap := func(content string) string {
		return leftRail + hpadL + content + hpadR + rightRail
	}
	// Vertical padding rows (above and below inner content).
	padRow := func() string {
		spaces := strings.Repeat(" ", innerW)
		if b.PadStyle != nil {
			spaces = b.PadStyle.Render(spaces)
		}
		return wrap(spaces)
	}
	rows := []string{top}
	for i := 0; i < b.Padding[0]; i++ {
		rows = append(rows, padRow())
	}
	for _, ln := range innerLines {
		rows = append(rows, wrap(ln))
	}
	for i := 0; i < b.Padding[2]; i++ {
		rows = append(rows, padRow())
	}
	rows = append(rows, bot)
	return strings.Join(rows, "\n")
}

// Styled wraps a Component so its Render output gets passed through a
// styler. Use sparingly — most styling should happen at the Cell or
// Text level so the bytes are styled inside the cell budget. Styled
// is for cases where a whole region needs a uniform background or
// a global ANSI mode (e.g., hyperlink) layered on after composition.
type Styled struct {
	Inner Component
	Style styler
}

// Render delegates to Inner then applies the wrapping style line by
// line so multi-line output keeps its layout (Render's contract is
// preserved — Box.Width and Box.Height are unchanged).
func (s Styled) Render(box Box) string {
	out := renderAndCheck(s.Inner, box)
	if s.Style == nil {
		return out
	}
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = s.Style.Render(l)
	}
	return strings.Join(lines, "\n")
}

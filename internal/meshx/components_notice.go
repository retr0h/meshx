// Copyright (c) 2026 John Dewey
//
// Notice-row Components — leaf decomposition for `-!-` system rows
// (status="notice" / status="system"). Same pattern as
// components_chat.go: pre-compute styled per-cell strings via
// noticeRowFor, stitch with Row{Cells:...} via noticeRowLine.
//
// Notice rows have a different visual structure than chat rows: no
// flag column, no metrics tail. The columns are:
//   accent (2)  time (10)  body (flex)  [pin-tail (1)]
//
// The pin-tail cell is ALWAYS allocated 1 cell wide so rows with and
// without pin corners line up vertically; non-pinned rows just emit
// a bg-styled space there.

package meshx

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// noticeRowParts pre-computes the styled per-cell strings for a
// notice row. Caller picks fast-path (default lavender italic body)
// or styled-path (custom fg / bold / center) and feeds the body cell
// to noticeRowLine.
type noticeRowParts struct {
	accent string // 2 cells: lavender ▎ + space
	time   string // 10 cells: "   HH:MM  " or pin-corner ⌜ + 9
	pinEnd string // 1 cell: ⌟ corner if pinLast, else bg-styled space
	rowBg  string
}

// noticeAccentTimeWidth is the cells consumed by the accent + time
// columns; centered notices pad leading spaces to (inner - bw) / 2 -
// prefixCells where prefixCells = gutter(3) + accent(2) + time(10)
// + "-!- "(4) = 19. Centralizing here keeps that magic in one place.
const (
	noticeAccentW = 2
	noticeTimeW   = 10
	noticePinW    = 1
)

// noticeRowFor styles the chrome cells (accent, time, pin-tail) for
// a notice row. Body content is left to the caller because it varies
// by style.fg / bold / center / corrupted-prefix per call site.
func noticeRowFor(rowBg string, time string, pinFirst, pinLast bool, fade float64) noticeRowParts {
	lav := lerpHex(mhLavender, rowBg, fade)
	drn := lerpHex(mhDrained, rowBg, fade)

	accent := lipgloss.NewStyle().
		Foreground(lipgloss.Color(lav)).
		Background(lipgloss.Color(rowBg)).
		Bold(true).
		Render("▎") +
		lipgloss.NewStyle().Background(lipgloss.Color(rowBg)).Render(" ")

	tstamp := lipgloss.NewStyle().
		Foreground(lipgloss.Color(drn)).
		Background(lipgloss.Color(rowBg))
	timeCol := "   " + time + "  "
	if time == "" {
		timeCol = "          "
	}
	tsRendered := tstamp.Render(timeCol)
	if pinFirst && len(timeCol) > 0 {
		corner := lipgloss.NewStyle().
			Foreground(lipgloss.Color(meshGreen)).
			Background(lipgloss.Color(rowBg)).
			Bold(true).
			Render("⌜")
		tsRendered = corner + tstamp.Render(timeCol[1:])
	}

	pinEnd := lipgloss.NewStyle().Background(lipgloss.Color(rowBg)).Render(" ")
	if pinLast {
		pinEnd = lipgloss.NewStyle().
			Foreground(lipgloss.Color(meshGreen)).
			Background(lipgloss.Color(rowBg)).
			Bold(true).
			Render("⌟")
	}

	return noticeRowParts{
		accent: accent,
		time:   tsRendered,
		pinEnd: pinEnd,
		rowBg:  rowBg,
	}
}

// noticeRowLine renders a notice row at exactly contentW cells via
// Row{Cells:...}. body is the already-styled body content (everything
// after time column); the body cell gets the flex slot.
func noticeRowLine(parts noticeRowParts, body string, contentW int) string {
	bg := lipgloss.NewStyle().Background(lipgloss.Color(parts.rowBg))
	cells := []Cell{
		{Content: parts.accent, Width: noticeAccentW},
		{Content: parts.time, Width: noticeTimeW},
		// Body flex slot — PadStyle tints the trailing space past the
		// end of the message text so the row's lavender-on-rowBg
		// background extends continuously through to the pin column.
		{Content: body, Width: -1, PadStyle: bg},
		{Content: parts.pinEnd, Width: noticePinW},
	}
	return Row{Cells: cells, FillStyle: bg}.Render(Box{Width: contentW, Height: 1})
}

// noticeCenteredRowLine renders a notice row with the body block
// pane-centered against contentW (the pane's inner width), instead
// of within the regular accent/time/body/pin cell layout.
//
// The regular noticeRowLine has 12 cells of left chrome (accent +
// time) and 1 cell of right chrome (pin tail). Centering the body
// within the body cell would offset the visible content ~6 cells
// right of pane center because of that asymmetry. For splash banner
// rows we want the art's visual midpoint to land on the pane's
// midpoint, so we bypass the time cell entirely and emit a row
// shaped as: accent + flex-pad + body + flex-pad + pinEnd. Row's
// flex distribution divides leftover width equally between the two
// pads, which lands the body at exactly contentW/2 - bw/2 cells
// from the gutter regardless of how wide the pane is.
//
// body must be a pre-styled string; this Component owns the layout
// math but not the styling, so the caller can vary the splash
// banner's foreground (the rotating Max-Headroom palette) without
// re-implementing the whole row.
func noticeCenteredRowLine(parts noticeRowParts, body string, contentW int) string {
	bg := lipgloss.NewStyle().Background(lipgloss.Color(parts.rowBg))
	bw := ansiCells(body)
	available := contentW - noticeAccentW - noticePinW
	if bw > available {
		bw = available
	}
	leftPad := (available - bw) / 2
	rightPad := available - bw - leftPad
	if leftPad < 0 {
		leftPad = 0
	}
	if rightPad < 0 {
		rightPad = 0
	}
	cells := []Cell{
		{Content: parts.accent, Width: noticeAccentW},
		{Content: bg.Render(strings.Repeat(" ", leftPad)), Width: leftPad},
		{Content: body, Width: bw, PadStyle: bg},
		{Content: bg.Render(strings.Repeat(" ", rightPad)), Width: rightPad},
		{Content: parts.pinEnd, Width: noticePinW},
	}
	return Row{Cells: cells, FillStyle: bg}.Render(Box{Width: contentW, Height: 1})
}

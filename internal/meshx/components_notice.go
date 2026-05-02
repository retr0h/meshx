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

// noticeCenterPad returns padding to prepend a centered notice
// body so it floats at (bodyW - bw)/2 within the body cell. bw is
// the visible width of the bodyContent (not including the "-!- "
// prefix). Returns "" when no centering is needed (already wider
// than half the body cell).
func noticeCenterPad(bodyW, bw int, sysStyler styler) string {
	padLen := (bodyW - bw) / 2
	if padLen <= 0 {
		return ""
	}
	return sysStyler.Render(strings.Repeat(" ", padLen))
}

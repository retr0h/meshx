// Copyright (c) 2026 John Dewey
//
// Chat-row Components — leaf decomposition of renderMessageRow's
// "regular chat message" path into a Row of named Cells.
//
// The legacy renderMessageRow handles four shapes (system block,
// notice, regular chat, threading quote) all in one ~400-line
// function with inline string concat. This file extracts the most
// common shape (regular chat) into a chatRow Component composed via
// Row{Cells:[]Cell{...}}, which is the React-style decomposition the
// rest of the layout layer was built for. Each Cell is independently
// addressable, independently styled, and reusable: the same hopCell
// / snrCell / statusCell helpers feed both the message log and the
// /whois output card.
//
// System-block and notice shapes still flow through the legacy
// renderNoticeRow path because their visual structure is genuinely
// different (no flag column, no metrics tail) — converting those
// into their own Row{Cells:...} decomposition is straightforward
// follow-up work; this file lays the pattern.

package meshx

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// chatRowParts holds the per-cell content for a regular chat row,
// pre-composed by `chatRowFor` so the Component itself just stitches
// `Row{Cells:...}` and renders. Splitting compute from render makes
// the cells unit-testable in isolation and gives /whois (and any
// future report card) a way to reuse the same SNR / hop / status
// rendering logic without dragging the row layout in.
type chatRowParts struct {
	accent    string // 2 cells: ▎ + space, sender-color tinted
	flag      string // 2 cells: status flag + space
	time      string // 7 cells: "HH:MM  "
	sender    string // fromW cells: "[XXXX] longname"
	hop       string // hopColW cells: "↝Nh  " or "↝  dx  " or spaces
	snr       string // snrColW cells: "  X.XdB" or spaces
	statusGap string // 1 cell: bg-tinted space
	status    string // 1 cell: ✓ ✗ … or space
	rowBg     string // background for the whole row
}

// chatRowFor pre-computes all per-cell strings (already styled) for
// a regular chat row. Mirrors the layout the legacy renderMessageRow
// embeds inline; centralizing here means each field has one
// authoritative source of truth.
func chatRowFor(m model, msg messageItem, rowBg string) chatRowParts {
	tstamp := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Background(lipgloss.Color(rowBg))
	me := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhMagenta)).
		Background(lipgloss.Color(rowBg)).
		Bold(true)
	resolvedName := m.displayFrom(msg)
	peerColor := nickColor(resolvedName)
	if m.senderUnresolved(msg) {
		peerColor = mhDrained
	}
	peer := lipgloss.NewStyle().
		Foreground(lipgloss.Color(peerColor)).
		Background(lipgloss.Color(rowBg)).
		Bold(true)
	hopFg := lipgloss.NewStyle().
		Foreground(lipgloss.Color(meshGreen)).
		Background(lipgloss.Color(rowBg))
	ack := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhGreen)).
		Background(lipgloss.Color(rowBg)).
		Bold(true)
	fail := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhPink)).
		Background(lipgloss.Color(rowBg)).
		Bold(true)
	bang := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhYellow)).
		Background(lipgloss.Color(rowBg)).
		Bold(true)

	// Sender-accent tick (▎). Color follows nickColor by default but
	// flips to magenta for own messages, yellow for /bang commands,
	// pink for failed sends — same priority order the legacy
	// renderer uses.
	accentColor := nickColor(resolvedName)
	if m.senderUnresolved(msg) {
		accentColor = mhDrained
	}
	if msg.mine {
		accentColor = mhMagenta
	}
	if msg.bang != "" {
		accentColor = mhYellow
	}
	if msg.status == "fail" {
		accentColor = mhPink
	}
	accent := lipgloss.NewStyle().
		Foreground(lipgloss.Color(accentColor)).
		Background(lipgloss.Color(rowBg)).
		Bold(true).
		Render("▎") +
		lipgloss.NewStyle().Background(lipgloss.Color(rowBg)).Render(" ")

	// Flag column.
	flagGlyph := " "
	flagStyle := tstamp
	switch {
	case msg.status == "fail":
		flagGlyph = "✗"
		flagStyle = fail
	case msg.bang != "":
		flagGlyph = "*"
		flagStyle = bang
	case msg.mine:
		flagGlyph = "›"
		flagStyle = me
	}
	flag := flagStyle.Render(flagGlyph + " ")

	// Time.
	timeCell := tstamp.Render(msg.time + "  ")

	// Sender.
	fromRaw := m.displayFrom(msg)
	shortName := ""
	if msg.mine {
		fromRaw = m.myCallsign()
		shortName = m.myShortName()
	} else {
		if idx, ok := m.nodesByNum[msg.fromNum]; ok &&
			idx >= 0 && idx < len(m.nodes) {
			shortName = m.nodes[idx].shortName
		}
		if m.senderUnresolved(msg) {
			fromRaw = "👻 " + fromRaw
		}
	}
	if shortName != "" {
		fromRaw = "[" + shortName + "] " + fromRaw
	}
	senderStyle := peer
	if msg.mine {
		senderStyle = me
	}
	sender := senderStyle.Render(padOrTruncate(fromRaw, fromW))

	// Hop column.
	hopText := strings.Repeat(" ", hopColW)
	switch {
	case msg.mine:
		// blank
	case msg.hops > 0:
		hopText = fmt.Sprintf("↝%3dh  ", msg.hops)
	default:
		hopText = "↝  dx  "
	}
	hopCell := hopFg.Render(hopText)

	// SNR column.
	snrText := strings.Repeat(" ", snrColW)
	if msg.snr != "" {
		snrText = fmt.Sprintf("%6sdB", msg.snr)
	}
	snrCell := hopFg.Render(snrText)

	// Status gap + glyph.
	statusGap := lipgloss.NewStyle().Background(lipgloss.Color(rowBg)).Render(" ")
	statusGlyph := " "
	statusRender := tstamp
	switch msg.status {
	case "pending":
		statusGlyph = "…"
	case "ack":
		statusGlyph = "✓"
		statusRender = ack
	case "fail":
		statusGlyph = "✗"
		statusRender = fail
	}
	statusCell := statusRender.Render(statusGlyph)

	return chatRowParts{
		accent:    accent,
		flag:      flag,
		time:      timeCell,
		sender:    sender,
		hop:       hopCell,
		snr:       snrCell,
		statusGap: statusGap,
		status:    statusCell,
		rowBg:     rowBg,
		// body filled by chatRow.Render, since it depends on textW
		// (the flex slot's allocated width).
	}
}

// chatRowMainLine renders the FIRST visible line of a chat row at
// exactly contentW cells per ansiCells via Row{Cells:[]Cell{...}}.
// `body` is msg.text's first line (with optional "(?) " prefix for
// corrupted-byte rows); the body cell is the row's flex slot.
func chatRowMainLine(parts chatRowParts, body string, bodyStyler styler, contentW int) string {
	const (
		accentW = 2 // "▎ "
		flagW   = 2 // glyph + space
		timeW   = 7 // "HH:MM  "
		gapW    = 2 // 2-space gutter between sender and body
	)
	gap := lipgloss.NewStyle().Background(lipgloss.Color(parts.rowBg)).Render("  ")
	cells := []Cell{
		{Content: parts.accent, Width: accentW},
		{Content: parts.flag, Width: flagW},
		{Content: parts.time, Width: timeW},
		{Content: parts.sender, Width: fromW},
		{Content: gap, Width: gapW},
		{Content: bodyStyler.Render(body), Width: -1, // flex slot
			Style: nil},
		{Content: parts.hop, Width: hopColW},
		{Content: parts.snr, Width: snrColW},
		{Content: parts.statusGap, Width: 1},
		{Content: parts.status, Width: 1},
	}
	return Row{Cells: cells}.Render(Box{Width: contentW, Height: 1})
}

// fromW is the cell budget for the sender column. Meshtastic
// longnames cap at 36 bytes per the firmware; 30 display cells
// covers the large majority of real callsigns ("AmputiLayag_…")
// without ellipsis while leaving the body the dominant share of the
// row on typical terminal widths.
const fromW = 30

// hopColW / snrColW are the cell widths for the right-hand metrics
// columns. "↝%3dh" leaves 5 cells of value + 2 cells of trailing
// gap = 7 total; "%6sdB" right-aligns the SNR value in 6 cells +
// "dB" suffix = 8 total.
const (
	hopColW = 7
	snrColW = 8
)

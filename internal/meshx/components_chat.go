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

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// Zebra stripe bgs for message rows — dense mutt-style list where
// every message has a solid bg and adjacent messages alternate
// shade. No blank separators between — the color alternation IS the
// visual separator, which keeps the grid continuous from top to
// bottom of the pane and gives the whole feed a thick, woven feel.
//
// Two complementary mid-charcoal shades from the tokyo-night / max
// headroom family — never pure black. Both read as tinted gray, not
// void, so the zebra reads as soft alternation rather than harsh
// contrast with the pane bg. selectionRowBg is the highlight tint
// the cursor row wears in nav mode — distinct from searchHitRowBg
// (#0e2618) so a selected-AND-hit row still picks one obvious state.
const (
	rowBgEven      = "#1a1b26" // cool tokyo-night base
	rowBgOdd       = "#24283b" // one step lighter + barely-purple
	selectionRowBg = "#2a4a5a"
)

// zebraBg returns the bg tint for the Nth message row in display
// order. Even rows take rowBgEven, odd rowBgOdd; the alternation IS
// the visual separator between rows.
func zebraBg(i int) string {
	if i%2 == 0 {
		return rowBgEven
	}
	return rowBgOdd
}

// nickColorPalette is the accent-color ring used to hash peer
// callsigns into distinct hues — irssi/weechat convention. Avoids
// mesh-green (brand), magenta (reserved for "me"), and any
// drained / lavender-adjacent tones that would blend with the
// quiet labels elsewhere. Bright-saturated hues only so the glitch
// Max Headroom read is loud and nicks pop off the log.
var nickColorPalette = []string{
	mhCyan,    // #00d4ff  neon cyan
	mhYellow,  // #e5c07b  warm amber
	mhOrange,  // #ffb86c  sunset
	mhPink,    // #ff6ec7  hot pink
	"#a78bfa", // electric violet
	"#7dd3fc", // sky blue
	"#facc15", // acid yellow
	"#f472b6", // bubblegum
}

// nickColor deterministically maps a callsign to one of the peer
// accent colors via FNV-1a plus a murmur3-style avalanche mix so
// the low bits carry enough entropy for a good modulo distribution.
// Raw FNV-1a on short near-duplicate strings ("node 0x…") clustered
// several peers into the same bucket; the avalanche step spreads
// them out. Same callsign → same color every time so the eye picks
// each peer out of the log by hue. Empty / system rows fall back
// to drained.
func nickColor(callsign string) string {
	if callsign == "" {
		return mhDrained
	}
	var sum uint32 = 2166136261
	for _, r := range callsign {
		sum ^= uint32(r)
		sum *= 16777619
	}
	sum ^= sum >> 16
	sum *= 0x85ebca6b
	sum ^= sum >> 13
	return nickColorPalette[int(sum)%len(nickColorPalette)]
}

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
	if msg.Mine {
		accentColor = mhMagenta
	}
	if msg.Bang != "" {
		accentColor = mhYellow
	}
	if msg.Status == mdl.StatusFail {
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
	case msg.Status == mdl.StatusFail:
		flagGlyph = "✗"
		flagStyle = fail
	case msg.Bang != "":
		flagGlyph = "*"
		flagStyle = bang
	case msg.Mine:
		flagGlyph = "›"
		flagStyle = me
	}
	flag := flagStyle.Render(flagGlyph + " ")

	// Time.
	timeCell := tstamp.Render(msg.Time + "  ")

	// Sender.
	fromRaw := m.displayFrom(msg)
	shortName := ""
	if msg.Mine {
		fromRaw = m.myCallsign()
		shortName = m.myShortName()
	} else {
		if idx, ok := m.nodesByNum[msg.FromNum]; ok &&
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
	if msg.Mine {
		senderStyle = me
	}
	sender := senderStyle.Render(padOrTruncate(fromRaw, fromW))

	// Hop column.
	hopText := strings.Repeat(" ", hopColW)
	switch {
	case msg.Mine:
		// blank
	case msg.Hops > 0:
		hopText = fmt.Sprintf("↝%3dh  ", msg.Hops)
	default:
		hopText = "↝  dx  "
	}
	hopCell := hopFg.Render(hopText)

	// SNR column.
	snrText := strings.Repeat(" ", snrColW)
	if msg.SNR != "" {
		snrText = fmt.Sprintf("%6sdB", msg.SNR)
	}
	snrCell := hopFg.Render(snrText)

	// Status gap + glyph.
	statusGap := lipgloss.NewStyle().Background(lipgloss.Color(rowBg)).Render(" ")
	statusGlyph := " "
	statusRender := tstamp
	switch msg.Status {
	case mdl.StatusPending:
		statusGlyph = "…"
	case mdl.StatusAck:
		statusGlyph = "✓"
		statusRender = ack
	case mdl.StatusFail:
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
// `body` is msg.Text's first line (with optional "(?) " prefix for
// corrupted-byte rows); the body cell is the row's flex slot.
func chatRowMainLine(parts chatRowParts, body string, bodyStyler styler, contentW int) string {
	const (
		accentW = 2 // "▎ "
		flagW   = 2 // glyph + space
		timeW   = 7 // "HH:MM  "
		gapW    = 2 // 2-space gutter between sender and body
	)
	bg := lipgloss.NewStyle().Background(lipgloss.Color(parts.rowBg))
	gap := bg.Render("  ")
	cells := []Cell{
		{Content: parts.accent, Width: accentW},
		{Content: parts.flag, Width: flagW},
		{Content: parts.time, Width: timeW},
		{Content: parts.sender, Width: fromW},
		{Content: gap, Width: gapW},
		// Body is the flex slot — PadStyle tints the trailing space
		// past the end of the message text so the zebra rowBg extends
		// continuously through to the hop column instead of dropping
		// to the terminal default.
		{Content: bodyStyler.Render(body), Width: -1, PadStyle: bg},
		{Content: parts.hop, Width: hopColW},
		{Content: parts.snr, Width: snrColW},
		{Content: parts.statusGap, Width: 1},
		{Content: parts.status, Width: 1},
	}
	return Row{Cells: cells, FillStyle: bg}.Render(Box{Width: contentW, Height: 1})
}

// fromW is the cell budget for the sender column. Meshtastic
// longnames cap at 36 bytes per the firmware; 30 display cells
// covers the large majority of real callsigns ("AmputiLayag_…")
// without ellipsis while leaving the body the dominant share of the
// row on typical terminal widths.
const fromW = 30

// chatRowLeftFixed is the cell width consumed by accent + flag +
// time + sender + sender-to-body gap on a chat row's first line.
// Continuation lines hang under the body column starting at this
// offset so multi-line messages indent correctly without re-emitting
// the time / sender chrome.
const chatRowLeftFixed = 2 /*accent*/ + 2 /*flag*/ + 7 /*time*/ + fromW + 2 /*gap*/

// chatContinuationLine renders a hanging continuation line for a
// multi-line message body. accent column carries the same sender
// tick as the first line so the color bar spans the whole message
// group; everything between accent and the body column is bg-tinted
// blank so the row reads as a single solid rectangle.
func chatContinuationLine(parts chatRowParts, body string, bodyStyler styler, contentW int) string {
	bg := lipgloss.NewStyle().Background(lipgloss.Color(parts.rowBg))
	indent := bg.Render(strings.Repeat(" ", chatRowLeftFixed-2))
	cells := []Cell{
		{Content: parts.accent, Width: 2},
		{Content: indent, Width: chatRowLeftFixed - 2},
		{Content: bodyStyler.Render(body), Width: -1, PadStyle: bg},
	}
	return Row{Cells: cells, FillStyle: bg}.Render(Box{Width: contentW, Height: 1})
}

// chatAckLine renders the optional acks subline that hangs under a
// chat row, indented to the body column so it reads as commentary
// on the row above. The body cell is rendered with the lavender
// italic system style.
func chatAckLine(parts chatRowParts, acks string, sysStyler styler, contentW int) string {
	bg := lipgloss.NewStyle().Background(lipgloss.Color(parts.rowBg))
	indent := bg.Render(strings.Repeat(" ", chatRowLeftFixed))
	cells := []Cell{
		{Content: indent, Width: chatRowLeftFixed},
		{Content: sysStyler.Render(acks), Width: -1, PadStyle: bg},
	}
	return Row{Cells: cells, FillStyle: bg}.Render(Box{Width: contentW, Height: 1})
}

// chatThreadingQuote renders the dim one-line "┌ from time \"text\""
// quote header above a reply, indented under the row's time column
// so the hook reads as context for the row below.
func chatThreadingQuote(
	parentFrom, parentTime, parentText string,
	rowBg string,
	contentW int,
) string {
	bg := lipgloss.NewStyle().Background(lipgloss.Color(rowBg))
	indent := bg.Render(strings.Repeat(" ", 2+2+7)) // accent+flag+time
	hook := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhPink)).
		Background(lipgloss.Color(rowBg)).
		Bold(true).
		Render("┌ ")
	// Italic + lavender to match the /me action body style — drained
	// (#3b4261) was too faint to read at normal terminal contrast.
	// Lavender (#6272a4) keeps the "this is context, not the row
	// itself" cue while staying legible against the zebra bg.
	quote := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhLavender)).
		Background(lipgloss.Color(rowBg)).
		Italic(true)
	if parentFrom == "" {
		parentFrom = "—"
	}
	quoteBody := fmt.Sprintf("%s %s  %q", parentFrom, parentTime,
		truncateRunes(parentText, 60))
	cells := []Cell{
		{Content: indent, Width: 2 + 2 + 7},
		{Content: hook, Width: 2},
		{Content: quote.Render(quoteBody), Width: -1, PadStyle: bg},
	}
	return Row{Cells: cells, FillStyle: bg}.Render(Box{Width: contentW, Height: 1})
}

// hopColW / snrColW are the cell widths for the right-hand metrics
// columns. "↝%3dh" leaves 5 cells of value + 2 cells of trailing
// gap = 7 total; "%6sdB" right-aligns the SNR value in 6 cells +
// "dB" suffix = 8 total.
const (
	hopColW = 7
	snrColW = 8
)

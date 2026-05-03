// Copyright (c) 2026 John Dewey
//
// Message-row Components — the leaf renderers for the messages pane.
// Each row is a Component with an exact-cell-width contract: given a
// Box, return a string that is precisely box.Height lines of
// box.Width cells per ansiCells. Internally these wrap the legacy
// renderMessageRow / renderNoticeRow string emitters and post-pad
// every line through padCells so the contract holds even when the
// inner renderers' arithmetic drifts (typically: a keycap or VS16
// glyph that runewidth measures differently than ansi.StringWidth).
// The Component layer is what guarantees no row ever pushes the
// right ║ frame out of column — the legacy emitters are still the
// source of truth for message *content*, but this layer is the
// source of truth for message *size*.

package meshx

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// messageRow renders one messageItem at exactly box.Width cells per
// ansiCells, for box.Height visual rows. Render owns the dispatch
// (notice / system → noticeRowRender; regular chat → chatRowRender)
// AND the layout contract (every line padded to box.Width, blank
// lines added to reach box.Height) so a buggy inner renderer cannot
// cascade into pane-level overflow.
type messageRow struct {
	m         model
	msg       messageItem
	selected  bool
	rowBg     string
	pinFirst  bool
	pinLast   bool
	faded     bool
	rowsInner int // inner-width budget the per-row renderer targets
}

// Render returns box.Height lines × box.Width cells. Dispatches by
// msg.status to the right per-row renderer (noticeRowRender for the
// `-!-` colored info lines and the SQLite/whois system blocks;
// chatRowRender for regular chat). Optional dimRow fade is applied
// when the row falls outside the active /F filter.
func (r messageRow) Render(box Box) string {
	if box.Empty() {
		return ""
	}
	innerW := r.rowsInner
	if innerW == 0 {
		innerW = box.Width
	}
	var raw string
	switch r.msg.status {
	case statusNotice, statusSystem:
		raw = noticeRowRender(
			r.m, r.msg, r.selected, innerW, r.rowBg, r.pinFirst, r.pinLast,
		)
	default:
		raw = chatRowRender(
			r.m, r.msg, r.selected, innerW, r.rowBg,
		)
	}
	if r.faded {
		raw = dimRow(raw)
	}
	lines := strings.Split(raw, "\n")
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

// noticeRowRender renders one `-!-` notice or system messageItem.
// Same chrome every system row wears: 3-col wrapSelection gutter,
// lavender ▎ accent, drained `   HH:MM  ` timestamp column. The body
// styling diverges by msg.style — default lavender italic for
// /storage / /whois / etc., custom fg + center + bold for the splash
// banner art.
func noticeRowRender(
	m model,
	msg messageItem,
	selected bool,
	inner int,
	rowBg string,
	pinFirst, pinLast bool,
) string {
	if selected {
		rowBg = selectionRowBg
	}
	style := noticeStyle{}
	if msg.style != nil {
		style = *msg.style
	}
	fade := 0.0
	if m.mode != modeNav {
		fade = noticeFadeAlpha(msg, time.Now())
	}
	bodyFg := style.fg
	if bodyFg == "" {
		bodyFg = mhLavender
	}
	bodyFg = lerpHex(bodyFg, rowBg, fade)
	lav := lerpHex(mhLavender, rowBg, fade)

	parts := noticeRowFor(rowBg, msg.time, pinFirst, pinLast, fade)
	contentW := inner - gutterWidth
	if contentW < 20 {
		contentW = 20
	}

	sys := lipgloss.NewStyle().
		Foreground(lipgloss.Color(lav)).
		Background(lipgloss.Color(rowBg)).
		Italic(true)

	// Fast path — default styling: one sys.Render over the whole
	// msg.text gives the terminal a single uninterrupted ANSI span,
	// painted as one clean lavender-italic band. Every storage /
	// whois / identified line lands here.
	if style.fg == "" && !style.center && !style.bold {
		body := sys.Render(msg.text)
		line := noticeRowLine(parts, body, contentW)
		return wrapSelection(line, selected, false, inner, rowBg)
	}

	// Styled path — body takes a custom fg / bold / center. Split
	// the "-!- " prefix off so it stays flush-left in the standard
	// sys style; only the content after it receives override styling.
	// Keeping the prefix uniform across every notice row is what
	// makes the splash banner visually stack with regular `-!-`.
	const prefix = "-!- "
	bodyContent := strings.TrimPrefix(msg.text, prefix)

	bodyStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(rowBg)).
		Foreground(lipgloss.Color(bodyFg))
	if style.fg == "" {
		bodyStyle = bodyStyle.Italic(true)
	}
	if style.bold {
		bodyStyle = bodyStyle.Bold(true)
	}
	styled := bodyStyle.Render(bodyContent)

	// `-!-` is ALWAYS anchored at the leftmost body chrome column —
	// never floats. style.center only changes the alignment of the
	// content AFTER the prefix: the prefix gets its own fixed-width
	// cell in noticeRowLineSplit, and the content cell takes
	// Align: AlignCenter so the art body-cell-centers in the space
	// to the right of the prefix while the prefix stays put.
	if style.center {
		line := noticeRowLineSplit(
			parts, sys.Render(prefix), styled, AlignCenter, contentW,
		)
		return wrapSelection(line, selected, false, inner, rowBg)
	}
	body := sys.Render(prefix) + styled
	line := noticeRowLine(parts, body, contentW)
	return wrapSelection(line, selected, false, inner, rowBg)
}

// chatRowRender renders one regular chat messageItem. The visual
// structure lives in the chatRow Component family — chatRowFor
// computes the per-cell styled strings (accent, flag, time, sender,
// hop, snr, status); chatRowMainLine stitches them with the body
// cell via Row{Cells:...}. Continuation lines, ack subline, and
// threading-quote header are appended via the chat* helpers in
// components_chat.go.
func chatRowRender(
	m model,
	msg messageItem,
	selected bool,
	inner int,
	rowBg string,
) string {
	// Selection-bg override: every styled span below bakes rowBg into
	// its ANSI escape, so wrapSelection's outer Background() can't
	// win against the nested codes. Swap rowBg for the selection tint
	// at the TOP of the render so every downstream span picks it up
	// natively.
	if selected {
		rowBg = selectionRowBg
	}
	contentW := inner - gutterWidth
	if contentW < 40 {
		contentW = 40
	}

	parts := chatRowFor(m, msg, rowBg)

	// /me action detection — IRC convention: a chat row whose body
	// starts with "* " (and isn't a /bang command) renders without
	// the bracketed sender column and with "* <nick> <action>" as
	// the body in italic. Faithful to irssi's "*  nick waves at the
	// camera" line. The wire format stays "* waves" so peers running
	// other Meshtastic clients see something sensible too — meshx
	// just chooses a richer presentation when we recognize the
	// pattern.
	isAction := msg.bang == "" && msg.status != statusSystem &&
		strings.HasPrefix(msg.text, "* ") && len(msg.text) > 2

	bodyLines := strings.Split(msg.text, "\n")
	if len(bodyLines) == 0 {
		bodyLines = []string{""}
	}
	if isAction {
		// Strip the wire prefix; we rebuild it on the meshx side as
		// "* <displayFrom> <action>".
		actionText := strings.TrimPrefix(bodyLines[0], "* ")
		bodyLines[0] = fmt.Sprintf("* %s %s", m.displayFrom(msg), actionText)
		// Blank out the sender cell — the body now carries the
		// nick. Keep the cell width preserved with a bg-tinted run
		// of spaces so the column alignment past the sender column
		// (gap + body) doesn't shift.
		parts.sender = lipgloss.NewStyle().
			Background(lipgloss.Color(rowBg)).
			Render(strings.Repeat(" ", fromW))
		// Action rows use the "*" flag glyph in dim drained color
		// so the visual marker reads as "this is an action," not as
		// the yellow-bang flag /cq, /73 etc. produce.
		parts.flag = lipgloss.NewStyle().
			Foreground(lipgloss.Color(mhDrained)).
			Background(lipgloss.Color(rowBg)).
			Render("* ")
	}
	// Corrupted bodies — sanitizeMessageText replaced bad bytes with
	// '?' and dropped non-printable runes, so the text is still
	// readable but no longer trustworthy. Re-style in dim lavender
	// italic and prefix "(?) " so the user sees "this row had
	// garbage in it" without us throwing away the salvageable chars.
	bodyText := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhFG)).
		Background(lipgloss.Color(rowBg))
	bodyForFirst := bodyLines[0]
	if msg.corrupted {
		bodyText = lipgloss.NewStyle().
			Foreground(lipgloss.Color(mhLavender)).
			Background(lipgloss.Color(rowBg)).
			Italic(true)
		bodyForFirst = "(?) " + bodyForFirst
	}
	if isAction {
		// Italic + drained color signals "this is an action" the
		// same way irssi renders /me output — distinct enough from
		// regular chat that the eye reads it as narration without
		// being so dim the text becomes unreadable.
		bodyText = lipgloss.NewStyle().
			Foreground(lipgloss.Color(mhLavender)).
			Background(lipgloss.Color(rowBg)).
			Italic(true)
	}
	sys := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhLavender)).
		Background(lipgloss.Color(rowBg)).
		Italic(true)
	row := chatRowMainLine(parts, bodyForFirst, bodyText, contentW)

	if len(bodyLines) > 1 {
		for _, bl := range bodyLines[1:] {
			row += "\n" + chatContinuationLine(parts, bl, bodyText, contentW)
		}
	}
	if msg.acks != "" {
		row += "\n" + chatAckLine(parts, msg.acks, sys, contentW)
	}
	if msg.replyID != 0 {
		if parent := m.findMessageByPacketID(msg.replyID); parent != nil {
			row = chatThreadingQuote(
				m.displayFrom(*parent), parent.time, parent.text,
				rowBg, contentW,
			) + "\n" + row
		}
	}

	return wrapSelection(row, selected, m.isMsgSearchHit(msg), inner, rowBg)
}

// messageRowVisualHeight reports how many terminal rows a given
// messageItem will occupy when rendered. Mirrors the bookkeeping in
// tailStartList / renderMessagesPane — system blocks with embedded
// '\n' are taller, an `acks` sub-line adds 1, and `replyID` threading
// adds the quote line above ONLY when the parent message is still
// in m.messages (renderMessageRow drops the threading quote when
// the parent has been reaped, so claiming +1 height in that case
// would leave the messageRow Component padding a blank row above
// — visible as a phantom gap between two unrelated messages).
func messageRowVisualHeight(m model, msg messageItem) int {
	h := 1 + strings.Count(msg.text, "\n")
	if msg.acks != "" {
		h++
	}
	if msg.replyID != 0 && m.findMessageByPacketID(msg.replyID) != nil {
		h++
	}
	return h
}

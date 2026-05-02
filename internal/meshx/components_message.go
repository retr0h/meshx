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
	"strings"
)

// messageRow renders one messageItem at exactly box.Width cells per
// ansiCells, for box.Height visual rows. The legacy renderMessageRow
// string emitter handles styling; this wrapper enforces the layout
// contract (every line padded to box.Width, blank lines added to
// reach box.Height) so a buggy emitter cannot cascade into pane-
// level overflow.
type messageRow struct {
	m         model
	msg       messageItem
	selected  bool
	rowBg     string
	pinFirst  bool
	pinLast   bool
	faded     bool
	rowsInner int // box width the legacy renderer was sized against
}

// Render returns box.Height lines × box.Width cells. The legacy
// emitter's output has its width baked into renderMessageRow's
// `inner` param, so we ask it for innerW = box.Width cells (passed
// via rowsInner at construct time). Any per-line drift from the
// emitter is forced back onto contract via padCells.
func (r messageRow) Render(box Box) string {
	if box.Empty() {
		return ""
	}
	innerW := r.rowsInner
	if innerW == 0 {
		innerW = box.Width
	}
	raw := r.m.renderMessageRow(r.msg, r.selected, innerW, r.rowBg, r.pinFirst, r.pinLast)
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

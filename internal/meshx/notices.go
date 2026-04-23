// Copyright (c) 2026 John Dewey

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

package meshx

// notices.go is the single home for every "tell the user something"
// surface in the app. Every `-!-` row — storage alerts, /whois
// cards, /ping replies, identified notifications, the splash
// banner, any future error/success pulse — flows through one of
// three writers:
//
//   m.notice(text, noticeStyle{...})        // one-shot row
//   m.noticeBlock(header, body1, body2)     // grouped card with
//                                           // auto-indented body
//                                           // (for /whois, /config)
//   m.noticeCard(row0, row1, ...)           // low-level grouped
//                                           // block with per-row
//                                           // style + no auto-
//                                           // indent (for splash
//                                           // block-art, pre-shaped
//                                           // ASCII, custom colors)
//
// buildNotice is the internal primitive all three writers call; it
// lives here because the top-level chrome (`-!- ` prefix, timestamp,
// status="notice") is always the same — only grouping, per-row
// style, and auto-indent vary.
//
// Named wrappers carry intent, not implementation:
//
//   m.systemLine("storage: ...")    → m.notice(…, noticeStyle{})
//   m.storagePersist(err)           → one-per-session m.systemLine
//
// The renderer (renderNoticeRow) reads msg.style and applies it —
// no branching on ad-hoc fields. Adding a new kind of notice is
// one helper + one noticeStyle literal; no renderer changes.

// noticeStyle drives everything about how a `-!-` row looks. Zero
// value = default lavender-italic system line. Every field is a
// positive override; the renderer is a dumb style applier.
type noticeStyle struct {
	// fg — body foreground color. Empty → mhLavender. Set for
	// per-row color overrides like splash block-art's rotating
	// variant colors, or an mhPink error notice.
	fg string
	// bold — emphasis. Default off.
	bold bool
	// center — push the body text toward the pane's visual center.
	// The `-!- ` prefix stays flush-left regardless; only content
	// after it shifts. Default off.
	center bool
}

// buildNotice composes a messageItem the render pipeline treats as a
// `-!-` row. The caller is responsible for any batch behavior
// (grouping, ordering); this helper just sets the `-!- ` chrome
// and attaches the style. Used by both the m.notice writer and
// slice-returning helpers like splashAsNotices.
func buildNotice(text string, style noticeStyle) messageItem {
	s := style // copy so the pointer lives past the caller's scope
	return messageItem{
		time:   timeNowHHMM(),
		text:   "-!- " + text,
		status: "notice",
		style:  &s,
	}
}

// notice appends one `-!-` row to the messages pane. Single
// entrypoint every caller uses — a rogue `m.messages = append(...)`
// with status="notice" elsewhere in the tree is a smell.
func (m *model) notice(text string, style noticeStyle) {
	m.messages = append(m.messages, buildNotice(text, style))
	m.selectedMsg = len(m.messages) - 1
}

// noticeRow is the (text, style) pair noticeCard consumes — one row
// of a grouped `-!-` block. Callers that need per-row color (splash
// block-art's rotating gradient) or pre-shaped row text (ASCII art
// that can't tolerate the auto-indent noticeBlock prepends) build a
// slice of these. The simpler /whois-style cards stay on
// m.noticeBlock which takes plain strings.
type noticeRow struct {
	text  string
	style noticeStyle
}

// noticeCard is the low-level block emitter — every row in `rows`
// gets stamped with the same group id, rendered with `-!- ` chrome,
// and only the first row carries a timestamp so the `-!-` column
// stays aligned down the whole block. No auto-indent, no style
// flattening: callers get exactly the per-row style they pass.
// m.noticeBlock is a thin convenience wrapper over this for the
// "one header + a few indented body lines" case; splash calls it
// directly because the block-art needs per-row fg overrides.
func (m *model) noticeCard(rows ...noticeRow) {
	if len(rows) == 0 {
		return
	}
	t := timeNowHHMM()
	gid := nextGroupID()
	for i, r := range rows {
		n := buildNotice(r.text, r.style)
		n.group = gid
		if i == 0 {
			n.time = t
		} else {
			n.time = ""
		}
		m.messages = append(m.messages, n)
	}
	m.selectedMsg = len(m.messages) - 1
}

// noticeBlock emits a multi-line card — /whois, /config, /env, /ping
// replies. Every line shares a group id so the renderer binds them
// visually (same bg, timestamp on header only). Body lines render
// with the same noticeStyle as the header; pass fg/bold/center once
// to color the whole block. Thin wrapper over m.noticeCard that
// pre-indents body lines with "   " for the /whois card look.
func (m *model) noticeBlock(header string, body ...string) {
	rows := make([]noticeRow, 0, 1+len(body))
	rows = append(rows, noticeRow{text: header})
	for _, line := range body {
		rows = append(rows, noticeRow{text: "   " + line})
	}
	m.noticeCard(rows...)
}

// systemLine — vocabulary wrapper. Reads as "tell the user this
// one thing" at the call site; implementation is m.notice with
// the default (lavender italic) style.
func (m *model) systemLine(text string) {
	m.notice(text, noticeStyle{})
}

// systemBlock — vocabulary wrapper for grouped cards. Forwards to
// m.noticeBlock.
func (m *model) systemBlock(header string, lines ...string) {
	m.noticeBlock(header, lines...)
}

// storagePersist wraps a save-to-sqlite call and surfaces the first
// failure per session as a systemLine ("-!- storage: ..."). Every
// subsequent error from any save path is silently swallowed so a
// degraded db doesn't machine-gun the messages pane. Runtime keeps
// operating in-memory — losing persistence is preferable to
// crashing the UI.
func (m *model) storagePersist(err error) {
	if err == nil {
		return
	}
	if m.storageAlerted {
		return
	}
	m.storageAlerted = true
	m.systemLine("storage: persistence degraded — " + err.Error())
}

// groupCounter is a monotonically-increasing counter used to tag
// members of a noticeBlock with a shared ID so the renderer can
// bind them visually.
var groupCounter uint64

func nextGroupID() uint64 {
	groupCounter++
	return groupCounter
}

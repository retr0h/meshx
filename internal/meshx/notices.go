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

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// notices.go is the single home for every "tell the user something"
// surface in the app. Every `-!-` row — storage alerts, /whois
// cards, /ping replies, identified notifications, the splash
// banner, any future error/success pulse — flows through one of
// three writers:
//
//	m.notice(text, noticeStyle{...})        // one-shot row
//	m.noticeBlock(header, body1, body2)     // grouped card with
//	                                        // auto-indented body
//	                                        // (for /whois, /config)
//	m.noticeCard(row0, row1, ...)           // low-level grouped
//	                                        // block with per-row
//	                                        // style + no auto-
//	                                        // indent (for splash
//	                                        // block-art, pre-shaped
//	                                        // ASCII, custom colors)
//
// buildNotice is the internal primitive all three writers call; it
// lives here because the top-level chrome (`-!- ` prefix, timestamp,
// status="notice") is always the same — only grouping, per-row
// style, and auto-indent vary.
//
// Named wrappers carry intent, not implementation:
//
//	m.systemLine("storage: ...")    → m.notice(…, noticeStyle{})
//	m.storagePersist(err)           → one-per-session systemLinePermanent
//
// The renderer (renderNoticeRow) reads msg.style and applies it —
// no branching on ad-hoc fields. Adding a new kind of notice is
// one helper + one noticeStyle literal; no renderer changes.
//
// TTL — every notice written through m.notice / m.noticeCard auto-
// expires so /whois, /ping, /config output (and the splash banner
// itself) doesn't pile up forever in the log. Both writers stamp
// expireAt = now + noticeTTL on every row they append; the reap tick
// drops expired rows (whole groups atomically, paused while the user
// is in modeNav so mid-scroll reads don't vanish) and the renderer
// lerps fg toward rowBg during the last noticeFadeWindow so expiry
// is visible before it happens. Callers that need a permanent row
// (storage "persistence degraded" alerts, future hard-fault banners)
// go through m.noticePermanent / m.systemLinePermanent which skip
// the stamp entirely.
const (
	// noticeTTL — lifetime of a command-triggered notice before
	// reaping. Long enough that a /whois card stays readable while
	// the user composes a follow-up, short enough that the log
	// doesn't drown in stale system chatter over a session.
	noticeTTL = 60 * time.Second
	// noticeFadeWindow — trailing slice of noticeTTL during which
	// the renderer dims the row toward rowBg. Communicates "this is
	// about to go" without adding a per-row countdown indicator.
	noticeFadeWindow = 10 * time.Second
)

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
		Message: mdl.Message{
			Time:   timeNowHHMM(),
			Text:   "-!- " + text,
			Status: mdl.StatusNotice,
		},
		style: &s,
	}
}

// notice appends one `-!-` row to the messages pane. Single
// entrypoint every caller uses — a rogue `m.messages = append(...)`
// with status="notice" elsewhere in the tree is a smell. Stamps a
// noticeTTL expiry; use m.noticePermanent for rows the user must
// keep seeing (storage alerts, error states).
func (m *model) notice(text string, style noticeStyle) {
	row := buildNotice(text, style)
	exp := time.Now().Add(noticeTTL)
	row.expireAt = &exp
	m.messages = append(m.messages, row)
	m.selectedMsg = len(m.messages) - 1
}

// noticePermanent is the opt-out sibling of m.notice — no TTL, the
// row stays until the process exits. Reserved for messages the user
// must not miss: storage-persistence degraded, future hard-fault
// banners, anything where silent auto-expiry would hide a problem.
func (m *model) noticePermanent(text string, style noticeStyle) {
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
	// Every row in the group shares one expiry stamp so the reap
	// drops them atomically and the fade is in lockstep. Taking
	// time.Now() once per call also keeps the stamp stable across a
	// potentially slow append for very large blocks.
	e := time.Now().Add(noticeTTL)
	for i, r := range rows {
		n := buildNotice(r.text, r.style)
		n.group = gid
		if i == 0 {
			n.Time = t
		} else {
			n.Time = ""
		}
		n.expireAt = &e
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

// systemLinePermanent — permanent variant of systemLine for rows
// the user must not miss (storage errors, hard-fault banners).
// Skips the TTL stamp so the row stays until the process exits.
func (m *model) systemLinePermanent(text string) {
	m.noticePermanent(text, noticeStyle{})
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
	m.systemLinePermanent("storage: persistence degraded — " + err.Error())
}

// groupCounter is a monotonically-increasing counter used to tag
// members of a noticeBlock with a shared ID so the renderer can
// bind them visually.
var groupCounter uint64

func nextGroupID() uint64 {
	groupCounter++
	return groupCounter
}

// noticeFadeAlpha returns 0..1 indicating how faded a notice row
// should render. 0 = full brightness, 1 = fully faded into rowBg.
// Returns 0 for permanent rows (expireAt == nil), pinned rows, and
// rows more than noticeFadeWindow away from expiry. Exported at
// package scope because renderNoticeRow + reapExpiredNotices both
// consume it; keeping the math in one place means a future change
// to the fade curve touches only here.
func noticeFadeAlpha(msg messageItem, now time.Time) float64 {
	if msg.expireAt == nil || msg.pinned {
		return 0
	}
	remain := msg.expireAt.Sub(now)
	if remain <= 0 {
		return 1
	}
	if remain >= noticeFadeWindow {
		return 0
	}
	return 1 - float64(remain)/float64(noticeFadeWindow)
}

// lerpHex blends two `#rrggbb` colors by `t` in [0, 1]. t=0 returns
// `a`, t=1 returns `b`, in-between interpolates each channel
// linearly. Garbage-in garbage-out: malformed hex strings yield
// black. Used by the notice renderer to fade expiring rows toward
// the row background.
func lerpHex(a, b string, t float64) string {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	ar, ag, ab := hexToRGB(a)
	br, bg, bb := hexToRGB(b)
	r := int(float64(ar) + (float64(br)-float64(ar))*t)
	g := int(float64(ag) + (float64(bg)-float64(ag))*t)
	bl := int(float64(ab) + (float64(bb)-float64(ab))*t)
	return fmt.Sprintf("#%02x%02x%02x", r, g, bl)
}

// reapExpiredNotices drops every messageItem whose expireAt has
// passed, atomically removing the whole group when any member of a
// grouped block is expired (every row in a block shares one stamp
// anyway, but the group-aware sweep is belt-and-braces against
// drift). Pinned rows and rows without an expireAt stamp are
// preserved unconditionally. selectedMsg is clamped to the new
// tail so the nav cursor doesn't point at freed slots.
//
// Caller guards: Update skips reap while m.mode == modeNav so a
// mid-scroll read doesn't vanish under the cursor. Expiry fires
// once the user ESCs back to input.
func (m *model) reapExpiredNotices() {
	now := time.Now()
	expiredGroups := map[uint64]struct{}{}
	anyExpiredSingle := false
	for _, msg := range m.messages {
		if msg.expireAt == nil || msg.pinned {
			continue
		}
		if now.Before(*msg.expireAt) {
			continue
		}
		if msg.group != 0 {
			expiredGroups[msg.group] = struct{}{}
		} else {
			anyExpiredSingle = true
		}
	}
	if len(expiredGroups) == 0 && !anyExpiredSingle {
		return
	}
	out := make([]messageItem, 0, len(m.messages))
	for _, msg := range m.messages {
		if msg.group != 0 {
			if _, drop := expiredGroups[msg.group]; drop {
				continue
			}
		} else if msg.expireAt != nil && !msg.pinned && !now.Before(*msg.expireAt) {
			continue
		}
		out = append(out, msg)
	}
	m.messages = out
	if m.selectedMsg >= len(m.messages) {
		m.selectedMsg = len(m.messages) - 1
	}
	if m.selectedMsg < 0 {
		m.selectedMsg = 0
	}
}

// toggleNoticePin flips the pin state of the messageItem at `idx`
// and, if the row is part of a group, every sibling in that group.
// Pin captures time.Until(expireAt) so unpin can re-stamp the row
// with the same remaining budget. No-op for indices out of range
// or for rows without an expireAt stamp (permanent rows are already
// pinned-by-nature — toggling would be a lie).
func (m *model) toggleNoticePin(idx int) {
	if idx < 0 || idx >= len(m.messages) {
		return
	}
	target := m.messages[idx]
	if target.expireAt == nil {
		return
	}
	nowPin := !target.pinned
	now := time.Now()
	apply := func(mi *messageItem) {
		if nowPin {
			remain := mi.expireAt.Sub(now)
			if remain < 0 {
				remain = 0
			}
			mi.pinnedRemaining = remain
			mi.pinned = true
		} else {
			resume := now.Add(mi.pinnedRemaining)
			mi.expireAt = &resume
			mi.pinnedRemaining = 0
			mi.pinned = false
		}
	}
	if target.group == 0 {
		apply(&m.messages[idx])
		return
	}
	for i := range m.messages {
		if m.messages[i].group == target.group {
			apply(&m.messages[i])
		}
	}
}

// lastEphemeralNoticeIdx walks m.messages backwards and returns the
// index of the last row eligible for pinning — a notice that still
// carries an expireAt stamp. Skips permanent notices (splash,
// storage) and non-notice chat rows. Returns -1 if nothing is
// pinnable. `/pin` with no explicit selection uses this to pick
// "the thing the user most recently ran."
func (m *model) lastEphemeralNoticeIdx() int {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].expireAt != nil {
			return i
		}
	}
	return -1
}

func hexToRGB(h string) (r, g, b int) {
	h = strings.TrimPrefix(h, "#")
	if len(h) != 6 {
		return 0, 0, 0
	}
	rv, _ := strconv.ParseInt(h[0:2], 16, 0)
	gv, _ := strconv.ParseInt(h[2:4], 16, 0)
	bv, _ := strconv.ParseInt(h[4:6], 16, 0)
	return int(rv), int(gv), int(bv)
}

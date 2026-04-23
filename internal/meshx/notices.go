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

import "fmt"

// notices.go is the single home for every "tell the user something"
// surface in the app. Three output channels all flow through here so
// subsystems (storage, transport, commands, input) don't need to
// know the rendering shape — they just call a helper:
//
//   m.systemLine("text")              → one irssi-style `-!-` row
//   m.systemBlock("header", lines...) → multi-row grouped reply card
//   m.flashf("format %s", arg)        → transient status-bar blurb
//
// Plus two failure-surface helpers:
//
//   m.storagePersist(err)             → once-per-session db alert
//   fatalf("fatal %s", ...)           → terminate the process
//
// The rule: any code that wants to show something to the user
// imports *nothing* from lipgloss / tea for the purpose — it calls
// a method here and this file decides how it renders.

// systemLine appends a single-line system/meta entry to the message
// log. Prefixed with `-!-` irssi-style. Never transmits over LoRa —
// display-only. Used for short one-shot notices.
func (m *model) systemLine(text string) {
	m.messages = append(m.messages, messageItem{
		time:   timeNowHHMM(),
		text:   "-!- " + text,
		status: "system",
	})
	m.selectedMsg = len(m.messages) - 1
}

// systemBlock emits a multi-line "server reply" block. Each line
// becomes its own messageItem, but all carry the same `group` ID —
// the renderer uses this to (a) give every row in the block the
// same zebra stripe color, (b) hide the timestamp on continuation
// rows so only the header carries it, and (c) let j/k navigation
// keep cursor movement smooth across blocks.
func (m *model) systemBlock(header string, lines ...string) {
	gid := nextGroupID()
	t := timeNowHHMM()
	m.messages = append(m.messages, messageItem{
		time:   t,
		text:   "-!- " + header,
		status: "system",
		group:  gid,
	})
	for _, l := range lines {
		m.messages = append(m.messages, messageItem{
			time:   t,
			text:   "-!-    " + l,
			status: "system",
			group:  gid,
		})
	}
	m.selectedMsg = len(m.messages) - 1
}

// flashf sets the transient status-bar blurb to the formatted
// string. Clears on the next keystroke (see updateInput's Tab
// handling). Use systemLine instead for anything that should stay
// visible after the user presses a key.
func (m *model) flashf(format string, args ...any) {
	m.flash = fmt.Sprintf(format, args...)
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
// members of a systemBlock with a shared ID so the renderer can
// bind them visually.
var groupCounter uint64

func nextGroupID() uint64 {
	groupCounter++
	return groupCounter
}

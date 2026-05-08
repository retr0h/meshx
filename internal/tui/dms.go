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

package tui

import mdl "github.com/retr0h/meshx/internal/meshx/model"

// broadcastNodeNum is the firmware-canonical "everyone on this
// channel" address — MeshPacket.to=0xFFFFFFFF on the wire. Used to
// distinguish broadcast from DM in the inbound auto-open path.
const broadcastNodeNum uint32 = 0xFFFFFFFF

// dmThread is one virtual @peer tab in the channel strip. Created
// when the user runs /query <peer>, /msg <peer> ..., or when an
// inbound TEXT_MESSAGE_APP arrives addressed to MyNodeNum. Session-
// only for now; the set re-derives from persisted DM history at
// startup once hydration is wired.
type dmThread struct {
	NodeNum  uint32
	Callsign string // longname snapshot at thread-open; refreshed on focus
	Unread   int
}

// dmIndexOfNum returns the index of the dmThread for nodeNum, or
// -1 if none is open.
func (m *model) dmIndexOfNum(nodeNum uint32) int {
	for i, t := range m.dmThreads {
		if t.NodeNum == nodeNum {
			return i
		}
	}
	return -1
}

// openDMThread ensures a dmThread exists for nodeNum and returns
// its index. Refreshes the Callsign snapshot from the live NodeDB
// each call so a peer that was a placeholder when first opened
// promotes to its real name once NodeInfo arrives.
func (m *model) openDMThread(nodeNum uint32) int {
	if nodeNum == 0 {
		return -1
	}
	call := dmCallsignFor(m, nodeNum)
	if i := m.dmIndexOfNum(nodeNum); i >= 0 {
		m.dmThreads[i].Callsign = call
		return i
	}
	m.dmThreads = append(m.dmThreads, dmThread{
		NodeNum:  nodeNum,
		Callsign: call,
	})
	return len(m.dmThreads) - 1
}

// switchToDMThread focuses the DM tab for nodeNum (creating it if
// needed) and clears its unread count. Leaves CurrentChannel intact
// so a later switchChannelByIndex can restore the prior channel
// view without the caller threading state. Snaps m.selectedMsg to
// the tail of the now-visible thread so j/k starts on a real row.
func (m *model) switchToDMThread(nodeNum uint32) {
	idx := m.openDMThread(nodeNum)
	if idx < 0 {
		return
	}
	m.dmThreads[idx].Unread = 0
	m.currentDMNum = nodeNum
	m.snapSelectionToTail()
}

// msgIsInCurrentDM reports whether msg belongs in the currently
// focused DM thread — either we wrote it to that peer or that peer
// wrote it to us. System rows belong to the global log and are
// excluded from DM filtering.
func (m *model) msgIsInCurrentDM(msg mdl.MessageItem) bool {
	if m.currentDMNum == 0 {
		return false
	}
	if msg.Status == mdl.StatusSystem || msg.Status == mdl.StatusNotice {
		return false
	}
	peer := m.currentDMNum
	me := m.MyNodeNum
	switch {
	case msg.FromNum == peer && msg.ToNum == me:
		return true
	case msg.Mine && msg.ToNum == peer:
		return true
	}
	return false
}

// dmCallsignFor resolves nodeNum to its current display callsign,
// falling back to the Meshtastic-canonical "!<8-hex>" placeholder
// when the peer isn't in the NodeDB yet.
func dmCallsignFor(m *model, nodeNum uint32) string {
	if idx, ok := m.NodesByNum[nodeNum]; ok && idx < len(m.Nodes) {
		return m.Nodes[idx].Callsign
	}
	long, _ := mdl.DefaultCallsign(nodeNum)
	return long
}

// visibleMessageIndices returns the absolute m.Messages indices that
// the messages pane will currently render — the full slice when on a
// channel tab, only the active DM thread's messages when on a DM tab.
// Used by both the renderer (to translate index-into-filter back to
// absolute for selection-highlight comparison) and j/k nav (to walk
// through what the user can actually see).
//
// Returns nil when m.Messages is empty so callers can length-check
// without an extra allocation.
func (m *model) visibleMessageIndices() []int {
	if len(m.Messages) == 0 {
		return nil
	}
	if m.currentDMNum == 0 {
		idx := make([]int, len(m.Messages))
		for i := range m.Messages {
			idx[i] = i
		}
		return idx
	}
	idx := make([]int, 0, len(m.Messages))
	for i, msg := range m.Messages {
		if m.msgIsInCurrentDM(msg) {
			idx = append(idx, i)
		}
	}
	return idx
}

// snapSelectionToTail moves m.selectedMsg to the last currently-
// visible message — the natural "you're now on this tab, here's the
// latest" landing spot after a tab switch. No-op when nothing's
// visible.
func (m *model) snapSelectionToTail() {
	idx := m.visibleMessageIndices()
	if len(idx) == 0 {
		m.selectedMsg = 0
		return
	}
	m.selectedMsg = idx[len(idx)-1]
}

// hydrateDMThreadsFromHistory walks m.Messages and opens a DM tab
// for every peer we've ever exchanged a DM with. Called after
// startup hydration so the user sees their DM threads from the
// previous session without waiting for a new message to arrive.
//
// Idempotent: openDMThread dedupes; calling twice produces the same
// thread set.
func (m *model) hydrateDMThreadsFromHistory() {
	me := m.MyNodeNum
	if me == 0 {
		return
	}
	for _, msg := range m.Messages {
		if msg.Status == mdl.StatusSystem || msg.Status == mdl.StatusNotice {
			continue
		}
		switch {
		case msg.FromNum != 0 && msg.ToNum == me:
			m.openDMThread(msg.FromNum)
		case msg.Mine && msg.ToNum != 0 && msg.ToNum != broadcastNodeNum:
			m.openDMThread(msg.ToNum)
		}
	}
}

// switchToSlot focuses tab slot `n` (1-based) in the combined
// channel+DM strip. Slots 1..len(Channels) address channels in
// order; slots beyond that address DM threads. No-op when the slot
// is out of range — the caller's caller (Alt+digit handler) lets
// the bubbletea textinput consume the key in that case so the
// digit isn't silently swallowed.
//
// Returns true when the slot resolved + the switch happened.
func (m *model) switchToSlot(n int) bool {
	idx := n - 1
	if idx < 0 {
		return false
	}
	if idx < len(m.Channels) {
		m.switchChannelByIndex(idx)
		return true
	}
	dmIdx := idx - len(m.Channels)
	if dmIdx < len(m.dmThreads) {
		t := m.dmThreads[dmIdx]
		m.switchToDMThread(t.NodeNum)
		m.flash = "switched to @" + t.Callsign
		return true
	}
	return false
}

// closeCurrentDMThread drops the active DM tab and returns the user
// to the channel they were on (CurrentChannel). No-op when not on a
// DM tab. Returns the closed thread's callsign for the flash.
func (m *model) closeCurrentDMThread() string {
	if m.currentDMNum == 0 {
		return ""
	}
	idx := m.dmIndexOfNum(m.currentDMNum)
	if idx < 0 {
		m.currentDMNum = 0
		return ""
	}
	call := m.dmThreads[idx].Callsign
	m.dmThreads = append(m.dmThreads[:idx], m.dmThreads[idx+1:]...)
	m.currentDMNum = 0
	return call
}

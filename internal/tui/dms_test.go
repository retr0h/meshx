// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

package tui

import (
	"testing"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

func TestMsgIsDM(t *testing.T) {
	tests := []struct {
		name string
		msg  mdl.MessageItem
		want bool
	}{
		{
			name: "broadcast-tonum-not-a-dm",
			msg:  mdl.MessageItem{Message: mdl.Message{ToNum: broadcastNodeNum}},
			want: false,
		},
		{
			name: "zero-tonum-not-a-dm",
			msg:  mdl.MessageItem{Message: mdl.Message{ToNum: 0}},
			want: false,
		},
		{
			name: "system-status-not-a-dm",
			msg:  mdl.MessageItem{Message: mdl.Message{ToNum: 0x1234, Status: mdl.StatusSystem}},
			want: false,
		},
		{
			name: "notice-status-not-a-dm",
			msg:  mdl.MessageItem{Message: mdl.Message{ToNum: 0x1234, Status: mdl.StatusNotice}},
			want: false,
		},
		{
			name: "nonzero-nonbroadcast-tonum-is-dm",
			msg:  mdl.MessageItem{Message: mdl.Message{ToNum: 0x1234}},
			want: true,
		},
		{
			name: "ack-status-directed-is-dm",
			msg:  mdl.MessageItem{Message: mdl.Message{ToNum: 0x5678, Status: mdl.StatusAck}},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := msgIsDM(tt.msg); got != tt.want {
				t.Errorf("msgIsDM() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVisibleMessageIndices(t *testing.T) {
	// Helpers
	broadcast := func(text string) mdl.MessageItem {
		return mdl.MessageItem{Message: mdl.Message{Text: text, ToNum: broadcastNodeNum}}
	}
	dm := func(from, text string, fromNum, toNum uint32) mdl.MessageItem {
		return mdl.MessageItem{Message: mdl.Message{
			Text:    text,
			From:    from,
			FromNum: fromNum,
			ToNum:   toNum,
		}}
	}
	system := func(text string) mdl.MessageItem {
		return mdl.MessageItem{Message: mdl.Message{Text: text, Status: mdl.StatusSystem}}
	}

	tests := []struct {
		name        string
		setupModel  func() model
		wantIndices []int
	}{
		{
			name: "empty-messages-returns-nil",
			setupModel: func() model {
				return newTestModel()
			},
			wantIndices: nil,
		},
		{
			name: "channel-view-excludes-dms",
			setupModel: func() model {
				m := newTestModel()
				m.currentDMNum = 0
				m.Messages = []mdl.MessageItem{
					broadcast("hello channel"),     // idx 0 — visible
					dm("peer", "hi", 0x1, 0x2),     // idx 1 — DM, excluded
					system("system row"),           // idx 2 — visible (system)
					broadcast("another broadcast"), // idx 3 — visible
				}
				return m
			},
			wantIndices: []int{0, 2, 3},
		},
		{
			name: "dm-view-shows-only-thread-messages",
			setupModel: func() model {
				m := newTestModel()
				m.MyNodeNum = 0xAAAA
				m.currentDMNum = 0x1234
				m.Messages = []mdl.MessageItem{
					broadcast("public msg"),                           // idx 0 — excluded
					dm("peer", "hi there", 0x1234, 0xAAAA),            // idx 1 — from peer to me
					dm("me", "reply", 0, 0x1234),                      // idx 2 — mine to peer
					{Message: mdl.Message{Mine: true, ToNum: 0x1234}}, // idx 3 — mine to peer
					dm(
						"other",
						"noise",
						0x9999,
						0xAAAA,
					), // idx 4 — different peer, excluded
					system("sys"), // idx 5 — excluded (system)
				}
				// Mark row 2 as mine
				m.Messages[2].Mine = true
				return m
			},
			wantIndices: []int{1, 2, 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupModel()
			got := m.visibleMessageIndices()
			if len(got) != len(tt.wantIndices) {
				t.Errorf("visibleMessageIndices() = %v, want %v", got, tt.wantIndices)
				return
			}
			for i, idx := range got {
				if idx != tt.wantIndices[i] {
					t.Errorf("visibleMessageIndices()[%d] = %d, want %d", i, idx, tt.wantIndices[i])
				}
			}
		})
	}
}

func TestDmIndexOfNum(t *testing.T) {
	tests := []struct {
		name    string
		threads []dmThread
		nodeNum uint32
		wantIdx int
	}{
		{
			name:    "empty-threads-returns-minus-one",
			threads: nil,
			nodeNum: 0x1234,
			wantIdx: -1,
		},
		{
			name: "found-at-index-zero",
			threads: []dmThread{
				{NodeNum: 0x1234, Callsign: "KC7XYZ"},
			},
			nodeNum: 0x1234,
			wantIdx: 0,
		},
		{
			name: "found-at-nonzero-index",
			threads: []dmThread{
				{NodeNum: 0xAAAA, Callsign: "W6ABC"},
				{NodeNum: 0x1234, Callsign: "KC7XYZ"},
				{NodeNum: 0xBBBB, Callsign: "N0CALL"},
			},
			nodeNum: 0x1234,
			wantIdx: 1,
		},
		{
			name: "not-found-returns-minus-one",
			threads: []dmThread{
				{NodeNum: 0xAAAA, Callsign: "W6ABC"},
			},
			nodeNum: 0x9999,
			wantIdx: -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.dmThreads = tt.threads
			if got := m.dmIndexOfNum(tt.nodeNum); got != tt.wantIdx {
				t.Errorf("dmIndexOfNum(%#x) = %d, want %d", tt.nodeNum, got, tt.wantIdx)
			}
		})
	}
}

func TestHydrateDMThreadsFromHistory(t *testing.T) {
	tests := []struct {
		name        string
		setupModel  func() model
		wantThreads int
	}{
		{
			name: "no-mynodenumber-noop",
			setupModel: func() model {
				m := newTestModel()
				m.MyNodeNum = 0
				m.Messages = []mdl.MessageItem{
					{Message: mdl.Message{FromNum: 0x1234, ToNum: 0xAAAA}},
				}
				return m
			},
			wantThreads: 0,
		},
		{
			name: "inbound-dm-creates-thread",
			setupModel: func() model {
				m := newTestModel()
				m.MyNodeNum = 0xAAAA
				m.Messages = []mdl.MessageItem{
					{Message: mdl.Message{FromNum: 0x1234, ToNum: 0xAAAA}},
				}
				return m
			},
			wantThreads: 1,
		},
		{
			name: "outbound-dm-creates-thread",
			setupModel: func() model {
				m := newTestModel()
				m.MyNodeNum = 0xAAAA
				m.Messages = []mdl.MessageItem{
					{Message: mdl.Message{Mine: true, ToNum: 0x5678}},
				}
				return m
			},
			wantThreads: 1,
		},
		{
			name: "broadcasts-excluded",
			setupModel: func() model {
				m := newTestModel()
				m.MyNodeNum = 0xAAAA
				m.Messages = []mdl.MessageItem{
					{Message: mdl.Message{FromNum: 0x1234, ToNum: broadcastNodeNum}},
					{Message: mdl.Message{Mine: true, ToNum: broadcastNodeNum}},
				}
				return m
			},
			wantThreads: 0,
		},
		{
			name: "system-rows-excluded",
			setupModel: func() model {
				m := newTestModel()
				m.MyNodeNum = 0xAAAA
				m.Messages = []mdl.MessageItem{
					{Message: mdl.Message{Status: mdl.StatusSystem, ToNum: 0xAAAA}},
					{Message: mdl.Message{Status: mdl.StatusNotice, ToNum: 0xAAAA}},
				}
				return m
			},
			wantThreads: 0,
		},
		{
			name: "deduplicates-same-peer",
			setupModel: func() model {
				m := newTestModel()
				m.MyNodeNum = 0xAAAA
				m.Messages = []mdl.MessageItem{
					{Message: mdl.Message{FromNum: 0x1234, ToNum: 0xAAAA}},
					{Message: mdl.Message{FromNum: 0x1234, ToNum: 0xAAAA}},
					{Message: mdl.Message{FromNum: 0x1234, ToNum: 0xAAAA}},
				}
				return m
			},
			wantThreads: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupModel()
			m.hydrateDMThreadsFromHistory()
			if len(m.dmThreads) != tt.wantThreads {
				t.Errorf("hydrateDMThreadsFromHistory() threads=%d, want %d",
					len(m.dmThreads), tt.wantThreads)
			}
		})
	}
}

func TestCloseCurrentDMThread(t *testing.T) {
	tests := []struct {
		name         string
		setupModel   func() model
		wantCallsign string
		wantDMNum    uint32
		wantLen      int
	}{
		{
			name: "not-on-dm-tab-noop",
			setupModel: func() model {
				m := newTestModel()
				m.currentDMNum = 0
				return m
			},
			wantCallsign: "",
			wantDMNum:    0,
			wantLen:      0,
		},
		{
			name: "closes-active-thread",
			setupModel: func() model {
				m := newTestModel()
				m.currentDMNum = 0x1234
				m.dmThreads = []dmThread{
					{NodeNum: 0x1234, Callsign: "KC7XYZ"},
					{NodeNum: 0xAAAA, Callsign: "W6ABC"},
				}
				return m
			},
			wantCallsign: "KC7XYZ",
			wantDMNum:    0,
			wantLen:      1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupModel()
			gotCall := m.closeCurrentDMThread()
			if gotCall != tt.wantCallsign {
				t.Errorf("closeCurrentDMThread() callsign = %q, want %q", gotCall, tt.wantCallsign)
			}
			if m.currentDMNum != tt.wantDMNum {
				t.Errorf("after close, currentDMNum = %#x, want %#x", m.currentDMNum, tt.wantDMNum)
			}
			if len(m.dmThreads) != tt.wantLen {
				t.Errorf("after close, len(dmThreads) = %d, want %d", len(m.dmThreads), tt.wantLen)
			}
		})
	}
}

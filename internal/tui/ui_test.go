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
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/radio"
)

// newTestModel returns a minimal model with an initialized radio.State.
func newTestModel() model {
	return model{State: &radio.State{
		NodesByNum: make(map[uint32]int),
	}}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		name      string
		v, lo, hi int
		want      int
	}{
		{name: "within-range", v: 5, lo: 0, hi: 10, want: 5},
		{name: "below-min", v: -1, lo: 0, hi: 10, want: 0},
		{name: "above-max", v: 15, lo: 0, hi: 10, want: 10},
		{name: "at-min", v: 0, lo: 0, hi: 10, want: 0},
		{name: "at-max", v: 10, lo: 0, hi: 10, want: 10},
		{name: "lo-equals-hi", v: 5, lo: 3, hi: 3, want: 3},
		{name: "negative-range", v: -5, lo: -10, hi: -1, want: -5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clamp(tt.v, tt.lo, tt.hi); got != tt.want {
				t.Errorf("clamp(%d,%d,%d) = %d, want %d", tt.v, tt.lo, tt.hi, got, tt.want)
			}
		})
	}
}

func TestIsMsgSearchHit(t *testing.T) {
	tests := []struct {
		name        string
		searchQuery string
		msg         messageItem
		want        bool
	}{
		{
			name:        "empty-query-never-hits",
			searchQuery: "",
			msg:         messageItem{Message: mdl.Message{From: "KC7XYZ", Text: "hello"}},
			want:        false,
		},
		{
			name:        "match-in-from",
			searchQuery: "kc7xyz",
			msg:         messageItem{Message: mdl.Message{From: "KC7XYZ", Text: "hi there"}},
			want:        true,
		},
		{
			name:        "match-in-text",
			searchQuery: "hello",
			msg: messageItem{
				Message: mdl.Message{From: "node 0x1234", Text: "Hello world"},
			},
			want: true,
		},
		{
			name:        "case-insensitive-match",
			searchQuery: "mesh",
			msg:         messageItem{Message: mdl.Message{From: "MeshUser", Text: "testing"}},
			want:        true,
		},
		{
			name:        "no-match",
			searchQuery: "zzz",
			msg:         messageItem{Message: mdl.Message{From: "KC7XYZ", Text: "hello world"}},
			want:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.searchQuery = tt.searchQuery
			if got := m.isMsgSearchHit(tt.msg); got != tt.want {
				t.Errorf("isMsgSearchHit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsStringSearchHit(t *testing.T) {
	tests := []struct {
		name        string
		searchQuery string
		s           string
		want        bool
	}{
		{name: "empty-query", searchQuery: "", s: "anything", want: false},
		{name: "match", searchQuery: "kc7", s: "KC7XYZ", want: true},
		{name: "no-match", searchQuery: "zzz", s: "KC7XYZ", want: false},
		{name: "case-insensitive", searchQuery: "node", s: "NODE", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.searchQuery = tt.searchQuery
			if got := m.isStringSearchHit(tt.s); got != tt.want {
				t.Errorf("isStringSearchHit(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestPadOrTruncate(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		w     int
		wantW int
	}{
		{name: "short-string-pads", s: "hi", w: 10, wantW: 10},
		{name: "exact-fit", s: "hello", w: 5, wantW: 5},
		{name: "long-string-truncates", s: strings.Repeat("X", 50), w: 20, wantW: 20},
		{name: "empty-string-pads", s: "", w: 5, wantW: 5},
		{name: "emoji-2cell", s: "👋", w: 4, wantW: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := padOrTruncate(tt.s, tt.w)
			if w := ansi.StringWidth(got); w != tt.wantW {
				t.Errorf("padOrTruncate(%q, %d) width=%d, want %d (got=%q)",
					tt.s, tt.w, w, tt.wantW, got)
			}
		})
	}
}

func TestDisplayFrom(t *testing.T) {
	tests := []struct {
		name       string
		setupModel func() model
		msg        messageItem
		want       string
	}{
		{
			name: "mine-with-known-callsign",
			setupModel: func() model {
				m := newTestModel()
				m.MyNodeNum = 0xABCD
				m.NodesByNum = map[uint32]int{0xABCD: 0}
				m.Nodes = []nodeItem{{Callsign: "W6ABC"}}
				return m
			},
			msg:  messageItem{Message: mdl.Message{Mine: true, From: "—"}},
			want: "W6ABC",
		},
		{
			name: "mine-without-myinfo-falls-back-to-from",
			setupModel: func() model {
				return newTestModel()
			},
			msg:  messageItem{Message: mdl.Message{Mine: true, From: "oldcall"}},
			want: "oldcall",
		},
		{
			name: "peer-with-known-nodenum",
			setupModel: func() model {
				m := newTestModel()
				m.NodesByNum = map[uint32]int{0x1234: 0}
				m.Nodes = []nodeItem{{Callsign: "KD9ABC"}}
				return m
			},
			msg:  messageItem{Message: mdl.Message{From: "old", FromNum: 0x1234}},
			want: "KD9ABC",
		},
		{
			name: "peer-with-zero-fronum-uses-from",
			setupModel: func() model {
				return newTestModel()
			},
			msg:  messageItem{Message: mdl.Message{From: "system", FromNum: 0}},
			want: "system",
		},
		{
			name: "peer-unknown-synthesizes-default-callsign",
			setupModel: func() model {
				return newTestModel()
			},
			msg:  messageItem{Message: mdl.Message{From: "old", FromNum: 0xDEAD}},
			want: "node 0xdead",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupModel()
			if got := m.displayFrom(tt.msg); got != tt.want {
				t.Errorf("displayFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSenderUnresolved(t *testing.T) {
	tests := []struct {
		name       string
		setupModel func() model
		msg        messageItem
		want       bool
	}{
		{
			name: "mine-never-unresolved",
			setupModel: func() model {
				m := newTestModel()
				m.NodesByNum = map[uint32]int{0x1234: 0}
				m.Nodes = []nodeItem{{Unresolved: true}}
				return m
			},
			msg:  messageItem{Message: mdl.Message{Mine: true, FromNum: 0x1234}},
			want: false,
		},
		{
			name: "zero-fromnum-never-unresolved",
			setupModel: func() model {
				return newTestModel()
			},
			msg:  messageItem{Message: mdl.Message{FromNum: 0}},
			want: false,
		},
		{
			name: "peer-resolved",
			setupModel: func() model {
				m := newTestModel()
				m.NodesByNum = map[uint32]int{0x1234: 0}
				m.Nodes = []nodeItem{{Callsign: "KC7ABC", Unresolved: false}}
				return m
			},
			msg:  messageItem{Message: mdl.Message{FromNum: 0x1234}},
			want: false,
		},
		{
			name: "peer-unresolved",
			setupModel: func() model {
				m := newTestModel()
				m.NodesByNum = map[uint32]int{0x1234: 0}
				m.Nodes = []nodeItem{{Callsign: "node 0x1234", Unresolved: true}}
				return m
			},
			msg:  messageItem{Message: mdl.Message{FromNum: 0x1234}},
			want: true,
		},
		{
			name: "peer-not-in-nodedb",
			setupModel: func() model {
				return newTestModel()
			},
			msg:  messageItem{Message: mdl.Message{FromNum: 0x9999}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupModel()
			if got := m.senderUnresolved(tt.msg); got != tt.want {
				t.Errorf("senderUnresolved() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveJump(t *testing.T) {
	tests := []struct {
		name string
		to   int
		n    int
		want int
	}{
		{name: "negative-jumps-to-last", to: -1, n: 10, want: 9},
		{name: "zero-is-clamped-to-zero", to: 0, n: 10, want: 0},
		{name: "within-range", to: 5, n: 10, want: 5},
		{name: "above-max-clamped", to: 20, n: 10, want: 9},
		{name: "at-max", to: 9, n: 10, want: 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveJump(tt.to, tt.n); got != tt.want {
				t.Errorf("resolveJump(%d, %d) = %d, want %d", tt.to, tt.n, got, tt.want)
			}
		})
	}
}

func TestToggleFlash(t *testing.T) {
	tests := []struct {
		name    string
		on      bool
		whenOn  string
		whenOff string
		want    string
	}{
		{name: "on-returns-whenOn", on: true, whenOn: "YES", whenOff: "NO", want: "YES"},
		{name: "off-returns-whenOff", on: false, whenOn: "YES", whenOff: "NO", want: "NO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toggleFlash(tt.on, tt.whenOn, tt.whenOff); got != tt.want {
				t.Errorf(
					"toggleFlash(%v, %q, %q) = %q, want %q",
					tt.on,
					tt.whenOn,
					tt.whenOff,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestFindMessageByPacketID(t *testing.T) {
	tests := []struct {
		name     string
		messages []messageItem
		id       uint32
		wantNil  bool
		wantText string
	}{
		{
			name:    "zero-id-returns-nil",
			id:      0,
			wantNil: true,
		},
		{
			name: "found-returns-pointer",
			messages: []messageItem{
				{Message: mdl.Message{Text: "first", PacketID: 0x1111}},
				{Message: mdl.Message{Text: "second", PacketID: 0x2222}},
			},
			id:       0x1111,
			wantNil:  false,
			wantText: "first",
		},
		{
			name: "not-found-returns-nil",
			messages: []messageItem{
				{Message: mdl.Message{Text: "only", PacketID: 0x1111}},
			},
			id:      0x9999,
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Messages = tt.messages
			got := m.findMessageByPacketID(tt.id)
			if tt.wantNil && got != nil {
				t.Errorf("findMessageByPacketID(%#x) = %+v, want nil", tt.id, got)
			}
			if !tt.wantNil {
				if got == nil {
					t.Errorf("findMessageByPacketID(%#x) = nil, want non-nil", tt.id)
					return
				}
				if got.Text != tt.wantText {
					t.Errorf(
						"findMessageByPacketID(%#x).Text = %q, want %q",
						tt.id,
						got.Text,
						tt.wantText,
					)
				}
			}
		})
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{name: "shorter-than-limit-unchanged", s: "hello", n: 10, want: "hello"},
		{name: "exact-length-unchanged", s: "hello", n: 5, want: "hello"},
		{name: "longer-truncated-with-ellipsis", s: "hello world", n: 5, want: "hello…"},
		{name: "empty-string", s: "", n: 5, want: ""},
		{name: "unicode-truncated-at-rune", s: "héllo", n: 3, want: "hél…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateRunes(tt.s, tt.n); got != tt.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

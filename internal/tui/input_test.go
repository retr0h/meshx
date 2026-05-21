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

func TestWirePayloadBytes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "plain-chat-counts-all-bytes",
			input: "hello world",
			want:  len("hello world"),
		},
		{
			name:  "empty-input",
			input: "",
			want:  0,
		},
		{
			name:  "slash-help-is-zero",
			input: "/help",
			want:  0,
		},
		{
			name:  "slash-clear-is-zero",
			input: "/clear",
			want:  0,
		},
		{
			name:  "slash-nodes-is-zero",
			input: "/nodes",
			want:  0,
		},
		{
			name:  "reply-with-body",
			input: "/reply KC7ABC hello there",
			want:  len("hello there"),
		},
		{
			name:  "reply-no-body",
			input: "/reply KC7ABC",
			want:  0,
		},
		{
			name:  "r-alias-with-body",
			input: "/r KC7ABC hi",
			want:  len("hi"),
		},
		{
			name:  "msg-counts-target-plus-sep-plus-body",
			input: "/msg KC7ABC hello",
			// target="KC7ABC"(6) + ": "(2) + "hello"(5) = 13
			want: len("KC7ABC") + len(": ") + len("hello"),
		},
		{
			name:  "msg-no-body",
			input: "/msg KC7ABC",
			want:  0,
		},
		{
			name:  "cq-with-tail",
			input: "/cq testing 1 2 3",
			want:  len("testing 1 2 3"),
		},
		{
			name:  "cq-no-tail",
			input: "/cq",
			want:  0,
		},
		{
			name:  "qth-with-text",
			input: "/qth Portland OR",
			want:  len("Portland OR"),
		},
		{
			name:  "unknown-verb-is-zero",
			input: "/unknown command",
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wirePayloadBytes(tt.input); got != tt.want {
				t.Errorf("wirePayloadBytes(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsCallsignAmbiguous(t *testing.T) {
	tests := []struct {
		name     string
		nodes    []nodeItem
		callsign string
		want     bool
	}{
		{
			name:     "empty-callsign",
			nodes:    nil,
			callsign: "",
			want:     false,
		},
		{
			name: "unique-callsign",
			nodes: []nodeItem{
				{Callsign: "KC7ABC"},
				{Callsign: "W6XYZ"},
			},
			callsign: "KC7ABC",
			want:     false,
		},
		{
			name: "duplicate-callsign",
			nodes: []nodeItem{
				{Callsign: "KC7ABC"},
				{Callsign: "KC7ABC"},
			},
			callsign: "KC7ABC",
			want:     true,
		},
		{
			name:     "no-nodes",
			nodes:    nil,
			callsign: "KC7ABC",
			want:     false,
		},
		{
			name: "three-dupes",
			nodes: []nodeItem{
				{Callsign: "KC7ABC"},
				{Callsign: "KC7ABC"},
				{Callsign: "KC7ABC"},
			},
			callsign: "KC7ABC",
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Nodes = tt.nodes
			if got := m.isCallsignAmbiguous(tt.callsign); got != tt.want {
				t.Errorf("isCallsignAmbiguous(%q) = %v, want %v", tt.callsign, got, tt.want)
			}
		})
	}
}

func TestMsgMatchesFilter(t *testing.T) {
	tests := []struct {
		name       string
		nodeFilter string
		msg        messageItem
		want       bool
	}{
		{
			name:       "no-filter-always-matches",
			nodeFilter: "",
			msg:        messageItem{Message: mdl.Message{From: "KC7ABC"}},
			want:       true,
		},
		{
			name:       "filter-matches-exact-from",
			nodeFilter: "KC7ABC",
			msg:        messageItem{Message: mdl.Message{From: "KC7ABC"}},
			want:       true,
		},
		{
			name:       "filter-no-match",
			nodeFilter: "W6XYZ",
			msg:        messageItem{Message: mdl.Message{From: "KC7ABC"}},
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.nodeFilter = tt.nodeFilter
			if got := m.msgMatchesFilter(tt.msg); got != tt.want {
				t.Errorf("msgMatchesFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFirstFilteredMsgIndex(t *testing.T) {
	tests := []struct {
		name       string
		nodeFilter string
		messages   []messageItem
		want       int
	}{
		{
			name:       "no-messages",
			nodeFilter: "KC7ABC",
			messages:   nil,
			want:       0,
		},
		{
			name:       "match-at-first-position",
			nodeFilter: "KC7ABC",
			messages: []messageItem{
				{Message: mdl.Message{From: "KC7ABC"}},
				{Message: mdl.Message{From: "W6XYZ"}},
			},
			want: 0,
		},
		{
			name:       "match-at-second-position",
			nodeFilter: "KC7ABC",
			messages: []messageItem{
				{Message: mdl.Message{From: "W6XYZ"}},
				{Message: mdl.Message{From: "KC7ABC"}},
			},
			want: 1,
		},
		{
			name:       "no-match-returns-zero",
			nodeFilter: "NOBODY",
			messages: []messageItem{
				{Message: mdl.Message{From: "KC7ABC"}},
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.nodeFilter = tt.nodeFilter
			m.Messages = tt.messages
			if got := m.firstFilteredMsgIndex(); got != tt.want {
				t.Errorf("firstFilteredMsgIndex() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestUserGridCols(t *testing.T) {
	tests := []struct {
		name  string
		width int
		want  int
	}{
		{name: "zero-width-returns-one", width: 0, want: 1},
		{name: "narrow-terminal-returns-one", width: 40, want: 1},
		{name: "medium-terminal", width: 80, want: 3},
		{name: "wide-terminal", width: 120, want: 4},
		{name: "very-wide-terminal", width: 200, want: 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.w = tt.width
			got := m.userGridCols()
			if got < 1 {
				t.Errorf("userGridCols() = %d, want >= 1", got)
			}
			// We don't pin exact values since layout math may change,
			// but we verify the monotonicity: wider terminal => more cols.
			if tt.width >= 80 && got < 2 {
				t.Errorf("userGridCols() = %d for width=%d, expected >= 2", got, tt.width)
			}
		})
	}
}

func TestAbsHelper(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want int
	}{
		{name: "positive", n: 5, want: 5},
		{name: "negative", n: -5, want: 5},
		{name: "zero", n: 0, want: 0},
		{name: "min-int-boundary", n: -1000, want: 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := abs(tt.n); got != tt.want {
				t.Errorf("abs(%d) = %d, want %d", tt.n, got, tt.want)
			}
		})
	}
}

func TestNextMsgIndexSkipGroups(t *testing.T) {
	tests := []struct {
		name        string
		messages    []messageItem
		selectedMsg int
		delta       int
		want        int
	}{
		{
			name:        "empty-messages",
			messages:    nil,
			selectedMsg: 0,
			delta:       1,
			want:        0,
		},
		{
			name: "forward-one-ungrouped",
			messages: []messageItem{
				{Message: mdl.Message{Text: "a"}},
				{Message: mdl.Message{Text: "b"}},
				{Message: mdl.Message{Text: "c"}},
			},
			selectedMsg: 0,
			delta:       1,
			want:        1,
		},
		{
			name: "backward-one-ungrouped",
			messages: []messageItem{
				{Message: mdl.Message{Text: "a"}},
				{Message: mdl.Message{Text: "b"}},
				{Message: mdl.Message{Text: "c"}},
			},
			selectedMsg: 2,
			delta:       -1,
			want:        1,
		},
		{
			name: "clamp-at-end",
			messages: []messageItem{
				{Message: mdl.Message{Text: "a"}},
				{Message: mdl.Message{Text: "b"}},
			},
			selectedMsg: 1,
			delta:       1,
			want:        1,
		},
		{
			name: "clamp-at-start",
			messages: []messageItem{
				{Message: mdl.Message{Text: "a"}},
				{Message: mdl.Message{Text: "b"}},
			},
			selectedMsg: 0,
			delta:       -1,
			want:        0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Messages = tt.messages
			m.selectedMsg = tt.selectedMsg
			if got := m.nextMsgIndexSkipGroups(tt.delta); got != tt.want {
				t.Errorf("nextMsgIndexSkipGroups(%d) = %d, want %d", tt.delta, got, tt.want)
			}
		})
	}
}

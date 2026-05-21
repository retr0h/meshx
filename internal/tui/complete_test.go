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
)

func TestLooksLikeHexStem(t *testing.T) {
	tests := []struct {
		name string
		word string
		want bool
	}{
		{name: "0x-prefix-any-length", word: "0x", want: true},
		{name: "0x-with-digits", word: "0xdead", want: true},
		{name: "bare-hex-at-least-2", word: "de", want: true},
		{name: "bare-hex-with-abcdef", word: "abcdef", want: true},
		{name: "single-hex-char-not-hex-stem", word: "a", want: false},
		{name: "non-hex-chars", word: "hello", want: false},
		{name: "empty-string", word: "", want: false},
		{name: "mixed-non-hex", word: "xy", want: false},
		{name: "uppercase-treated-lowercase", word: "AB", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeHexStem(tt.word); got != tt.want {
				t.Errorf("looksLikeHexStem(%q) = %v, want %v", tt.word, got, tt.want)
			}
		})
	}
}

func TestHelpUniverse(t *testing.T) {
	tests := []struct {
		name         string
		word         string
		wantEmpty    bool
		wantContains []string
	}{
		{
			name:      "empty-stem-returns-nil",
			word:      "",
			wantEmpty: true,
		},
		{
			name:      "whitespace-only-returns-nil",
			word:      "   ",
			wantEmpty: true,
		},
		{
			name:         "ping-prefix-matches-ping",
			word:         "pin",
			wantContains: []string{"ping"},
		},
		{
			name:         "ch-prefix-matches-channel-channels",
			word:         "ch",
			wantContains: []string{"channel", "channels"},
		},
		{
			name:         "exact-match-returns-just-that",
			word:         "quit",
			wantContains: []string{"quit"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := helpUniverse(tt.word)
			if tt.wantEmpty {
				if len(got) != 0 {
					t.Errorf("helpUniverse(%q) = %v, want empty", tt.word, got)
				}
				return
			}
			displays := make([]string, len(got))
			for i, m := range got {
				displays[i] = m.display
			}
			for _, want := range tt.wantContains {
				found := false
				for _, d := range displays {
					if d == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf(
						"helpUniverse(%q) = %v, want %q to be present",
						tt.word,
						displays,
						want,
					)
				}
			}
		})
	}
}

func TestComputeCompletions_SlashCommands(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		cursor       int
		wantAny      bool
		firstDisplay string // if wantAny is true, check first match starts with
	}{
		{
			name:         "slash-ping-completes-ping",
			text:         "/pin",
			cursor:       4,
			wantAny:      true,
			firstDisplay: "/pin",
		},
		{
			name:    "no-slash-no-command-completions",
			text:    "hello",
			cursor:  5,
			wantAny: false, // no nodes in model
		},
		{
			name:         "slash-h-completes-ham-and-help-commands",
			text:         "/h",
			cursor:       2,
			wantAny:      true,
			firstDisplay: "/h",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			matches, _, _ := m.computeCompletions(tt.text, tt.cursor)
			if tt.wantAny && len(matches) == 0 {
				t.Errorf(
					"computeCompletions(%q, %d) returned no matches, want some",
					tt.text,
					tt.cursor,
				)
				return
			}
			if !tt.wantAny && len(matches) != 0 {
				t.Errorf(
					"computeCompletions(%q, %d) returned %d matches, want 0",
					tt.text,
					tt.cursor,
					len(matches),
				)
				return
			}
			if tt.wantAny && tt.firstDisplay != "" {
				ok := false
				for _, m := range matches {
					if strings.HasPrefix(m.display, tt.firstDisplay) {
						ok = true
						break
					}
				}
				if !ok {
					t.Errorf("computeCompletions(%q, %d) matches=%v, none start with %q",
						tt.text, tt.cursor, matches, tt.firstDisplay)
				}
			}
		})
	}
}

func TestNickUniverse(t *testing.T) {
	tests := []struct {
		name         string
		nodes        []nodeItem
		nodesByNum   map[uint32]int
		word         string
		wantEmpty    bool
		wantDisplays []string
	}{
		{
			name:      "empty-stem-returns-nil",
			word:      "",
			wantEmpty: true,
		},
		{
			name:      "whitespace-only-returns-nil",
			word:      "   ",
			wantEmpty: true,
		},
		{
			name: "prefix-match-on-callsign",
			nodes: []nodeItem{
				{Callsign: "KC7XYZ", NodeNum: 0x1111},
				{Callsign: "W6ABC", NodeNum: 0x2222},
			},
			nodesByNum:   map[uint32]int{0x1111: 0, 0x2222: 1},
			word:         "KC7",
			wantDisplays: []string{"KC7XYZ"},
		},
		{
			name: "no-match-returns-empty",
			nodes: []nodeItem{
				{Callsign: "KC7XYZ", NodeNum: 0x1111},
			},
			nodesByNum: map[uint32]int{0x1111: 0},
			word:       "W6",
			wantEmpty:  true,
		},
		{
			name: "shortname-prefix-match",
			nodes: []nodeItem{
				{Callsign: "KC7XYZ", ShortName: "KXYZ", NodeNum: 0x1111},
			},
			nodesByNum:   map[uint32]int{0x1111: 0},
			word:         "KX",
			wantDisplays: []string{"KC7XYZ"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Nodes = tt.nodes
			if tt.nodesByNum != nil {
				m.NodesByNum = tt.nodesByNum
			}
			got := m.nickUniverse(tt.word)
			if tt.wantEmpty {
				if len(got) != 0 {
					t.Errorf("nickUniverse(%q) = %v, want empty", tt.word, got)
				}
				return
			}
			for _, want := range tt.wantDisplays {
				found := false
				for _, mi := range got {
					if mi.display == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("nickUniverse(%q) displays=%v, want %q", tt.word, got, want)
				}
			}
		})
	}
}

func TestApplyCompletion(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		start, end int
		match      string
		wantOut    string
		wantCursor int
	}{
		{
			name:  "start-of-line-callsign-appends-colon-space",
			text:  "KC7",
			start: 0, end: 3,
			match:      "KC7XYZ",
			wantOut:    "KC7XYZ: ",
			wantCursor: 8,
		},
		{
			name:  "slash-command-appends-space",
			text:  "/pin",
			start: 0, end: 4,
			match:      "/ping",
			wantOut:    "/ping ",
			wantCursor: 6,
		},
		{
			name:  "no-double-space-when-followed-by-space",
			text:  "KC7 msg",
			start: 0, end: 3,
			match:      "KC7XYZ",
			wantOut:    "KC7XYZ msg",
			wantCursor: 6,
		},
		{
			name:  "mid-line-callsign-appends-space",
			text:  "/whois KC7",
			start: 7, end: 10,
			match:      "KC7XYZ",
			wantOut:    "/whois KC7XYZ ",
			wantCursor: 14,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOut, gotCursor := applyCompletion(tt.text, tt.start, tt.end, tt.match)
			if gotOut != tt.wantOut {
				t.Errorf("applyCompletion(%q, %d, %d, %q) out = %q, want %q",
					tt.text, tt.start, tt.end, tt.match, gotOut, tt.wantOut)
			}
			if gotCursor != tt.wantCursor {
				t.Errorf("applyCompletion(%q, %d, %d, %q) cursor = %d, want %d",
					tt.text, tt.start, tt.end, tt.match, gotCursor, tt.wantCursor)
			}
		})
	}
}

func TestWordBounds(t *testing.T) {
	tests := []struct {
		name      string
		s         string
		cur       int
		wantStart int
		wantEnd   int
	}{
		{
			name:      "word-at-start",
			s:         "hello world",
			cur:       3,
			wantStart: 0,
			wantEnd:   5,
		},
		{
			name:      "cursor-at-end-of-word",
			s:         "hello world",
			cur:       5,
			wantStart: 0,
			wantEnd:   5,
		},
		{
			name:      "cursor-in-second-word",
			s:         "hello world",
			cur:       8,
			wantStart: 6,
			wantEnd:   11,
		},
		{
			name:      "cursor-on-space",
			s:         "hello world",
			cur:       5, // space position — treated as between words
			wantStart: 0,
			wantEnd:   5,
		},
		{
			name:      "slash-command",
			s:         "/ping",
			cur:       5,
			wantStart: 0,
			wantEnd:   5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd := wordBounds(tt.s, tt.cur)
			if gotStart != tt.wantStart || gotEnd != tt.wantEnd {
				t.Errorf("wordBounds(%q, %d) = (%d, %d), want (%d, %d)",
					tt.s, tt.cur, gotStart, gotEnd, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestCommandArgStart(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		cursor    int
		wantStart int
		wantOk    bool
	}{
		{
			name:   "no-slash-not-command",
			text:   "hello world",
			cursor: 5,
			wantOk: false,
		},
		{
			name:   "slash-command-cursor-before-space",
			text:   "/whois KC7",
			cursor: 4, // still on verb
			wantOk: false,
		},
		{
			name:      "slash-whois-cursor-in-arg",
			text:      "/whois KC7",
			cursor:    9,
			wantStart: 7,
			wantOk:    true,
		},
		{
			name:      "slash-ping-cursor-in-arg",
			text:      "/ping target",
			cursor:    10,
			wantStart: 6,
			wantOk:    true,
		},
		{
			name:   "slash-unknown-verb-not-callsign-arg-cmd",
			text:   "/nodes target",
			cursor: 10,
			wantOk: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotOk := commandArgStart(tt.text, tt.cursor)
			if gotOk != tt.wantOk {
				t.Errorf(
					"commandArgStart(%q, %d) ok = %v, want %v",
					tt.text,
					tt.cursor,
					gotOk,
					tt.wantOk,
				)
			}
			if gotOk && gotStart != tt.wantStart {
				t.Errorf(
					"commandArgStart(%q, %d) start = %d, want %d",
					tt.text,
					tt.cursor,
					gotStart,
					tt.wantStart,
				)
			}
		})
	}
}

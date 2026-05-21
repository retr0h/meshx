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

func TestPaneInnerWidth(t *testing.T) {
	tests := []struct {
		name  string
		width int
		want  int
	}{
		{name: "subtracts-two", width: 80, want: 78},
		{name: "zero-width", width: 0, want: -2},
		{name: "minimal-valid", width: 4, want: 2},
		{name: "large", width: 200, want: 198},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := paneInnerWidth(tt.width); got != tt.want {
				t.Errorf("paneInnerWidth(%d) = %d, want %d", tt.width, got, tt.want)
			}
		})
	}
}

func TestTailStartList(t *testing.T) {
	tests := []struct {
		name       string
		msgs       []messageItem
		rowsBudget int
		want       int
	}{
		{
			name:       "empty-messages",
			msgs:       nil,
			rowsBudget: 10,
			want:       0,
		},
		{
			name: "all-fit-returns-zero",
			msgs: []messageItem{
				{Message: mdl.Message{Text: "a"}},
				{Message: mdl.Message{Text: "b"}},
			},
			rowsBudget: 10,
			want:       0,
		},
		{
			name: "one-row-budget-shows-last-only",
			msgs: []messageItem{
				{Message: mdl.Message{Text: "a"}},
				{Message: mdl.Message{Text: "b"}},
				{Message: mdl.Message{Text: "c"}},
			},
			rowsBudget: 1,
			want:       2, // last message is at index 2
		},
		{
			name: "multi-line-message-counted-correctly",
			msgs: []messageItem{
				{Message: mdl.Message{Text: "one\ntwo\nthree"}}, // 3 lines
				{Message: mdl.Message{Text: "four"}},
			},
			rowsBudget: 2,
			// 3-line msg + 1-line msg = 4 total; budget=2 means only last 2 fit
			// last msg costs 1 row, that fits; previous costs 3 rows, doesn't fit
			want: 1,
		},
		{
			name: "ack-line-adds-one-to-cost",
			msgs: []messageItem{
				{
					Message: mdl.Message{Text: "msg"},
					Ackers:  []mdl.Acker{{Callsign: "KC7"}},
				}, // cost=2
			},
			rowsBudget: 1,
			// cost=2 > budget=1, so start=1 (past the end)
			want: 1,
		},
		{
			name: "exact-fit-returns-zero",
			msgs: []messageItem{
				{Message: mdl.Message{Text: "a"}},
				{Message: mdl.Message{Text: "b"}},
			},
			rowsBudget: 2,
			want:       0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tailStartList(tt.msgs, tt.rowsBudget); got != tt.want {
				t.Errorf("tailStartList() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBoolToOnOff(t *testing.T) {
	tests := []struct {
		name string
		b    bool
		want string
	}{
		{name: "true-returns-on", b: true, want: "on"},
		{name: "false-returns-off", b: false, want: "off"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := boolToOnOff(tt.b); got != tt.want {
				t.Errorf("boolToOnOff(%v) = %q, want %q", tt.b, got, tt.want)
			}
		})
	}
}

func TestStateWeight(t *testing.T) {
	tests := []struct {
		name  string
		state nodeState
	}{
		{name: "online-lightest", state: stateOnline},
		{name: "offline", state: stateOffline},
		{name: "muted", state: stateMuted},
		{name: "failed-heaviest", state: stateFailed},
	}
	weights := make([]int, len(tests))
	for i, tt := range tests {
		weights[i] = stateWeight(tt.state)
	}
	// Verify ordering: online < offline < muted < failed.
	for i := 1; i < len(weights); i++ {
		if weights[i] <= weights[i-1] {
			t.Errorf(
				"stateWeight(%s)=%d should be > stateWeight(%s)=%d",
				tests[i].name, weights[i], tests[i-1].name, weights[i-1],
			)
		}
	}
}

func TestSortedNodes(t *testing.T) {
	tests := []struct {
		name      string
		nodes     []nodeItem
		sortMode  sortMode
		wantFirst string
	}{
		{
			name: "fav-node-always-first",
			nodes: []nodeItem{
				{Callsign: "Beta", HeardRank: 1},
				{Callsign: "Alpha", HeardRank: 2, Fav: true},
			},
			sortMode:  sortByLastHeard,
			wantFirst: "Alpha",
		},
		{
			name: "sort-by-name-alphabetical",
			nodes: []nodeItem{
				{Callsign: "Zulu"},
				{Callsign: "Alpha"},
				{Callsign: "Mike"},
			},
			sortMode:  sortByName,
			wantFirst: "Alpha",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Nodes = tt.nodes
			m.nodeSort = tt.sortMode
			sorted := m.sortedNodes()
			if len(sorted) == 0 {
				t.Fatal("sortedNodes() returned empty slice")
			}
			if sorted[0].Callsign != tt.wantFirst {
				t.Errorf(
					"sortedNodes()[0].Callsign = %q, want %q",
					sorted[0].Callsign, tt.wantFirst,
				)
			}
		})
	}
}

func TestSortedNodesDoesNotMutateOriginal(t *testing.T) {
	// sortedNodes returns a copy — the original slice must be untouched.
	m := newTestModel()
	m.Nodes = []nodeItem{
		{Callsign: "Beta"},
		{Callsign: "Alpha"},
	}
	m.nodeSort = sortByName
	_ = m.sortedNodes()
	if m.Nodes[0].Callsign != "Beta" {
		t.Error("sortedNodes() mutated the original m.Nodes slice")
	}
}

func TestPaneAccentColor(t *testing.T) {
	tests := []struct {
		name    string
		paneIdx int
		want    string
	}{
		{name: "channels-is-cyan", paneIdx: paneChannels, want: mhCyan},
		{name: "nodes-is-magenta", paneIdx: paneNodes, want: mhMagenta},
		{name: "messages-is-meshgreen", paneIdx: paneMessages, want: meshGreen},
		{name: "unknown-is-meshgreen", paneIdx: 99, want: meshGreen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := paneAccentColor(tt.paneIdx); got != tt.want {
				t.Errorf("paneAccentColor(%d) = %q, want %q", tt.paneIdx, got, tt.want)
			}
		})
	}
}

func TestSelectableConfigEntryIndices(t *testing.T) {
	m := newTestModel()
	indices := m.selectableConfigEntryIndices()
	if len(indices) == 0 {
		t.Error("selectableConfigEntryIndices() returned empty slice — expected interactive rows")
	}
	// All returned indices must point to non-read-only entries.
	entries := m.configEntries()
	for _, idx := range indices {
		if idx < 0 || idx >= len(entries) {
			t.Errorf("selectableConfigEntryIndices() returned out-of-range index %d", idx)
			continue
		}
		if entries[idx].kind == cfgEntryReadOnly {
			t.Errorf("selectableConfigEntryIndices() returned read-only entry at index %d", idx)
		}
	}
}

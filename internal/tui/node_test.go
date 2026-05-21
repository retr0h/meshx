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
	"fmt"
	"testing"
	"time"
)

func TestDefaultCallsign(t *testing.T) {
	tests := []struct {
		name          string
		nodeNum       uint32
		wantLongName  string
		wantShortName string
	}{
		{
			name:          "typical-node",
			nodeNum:       0xDEADBEEF,
			wantLongName:  "node 0xdeadbeef",
			wantShortName: "beef",
		},
		{
			name:          "small-node-num",
			nodeNum:       0x0001,
			wantLongName:  "node 0x1",
			wantShortName: "0001",
		},
		{
			name:          "zero-node-num",
			nodeNum:       0,
			wantLongName:  "node 0x0",
			wantShortName: "0000",
		},
		{
			name:          "max-uint32",
			nodeNum:       0xFFFFFFFF,
			wantLongName:  "node 0xffffffff",
			wantShortName: "ffff",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			longName, shortName := defaultCallsign(tt.nodeNum)
			if longName != tt.wantLongName {
				t.Errorf("defaultCallsign(%#x) longName = %q, want %q",
					tt.nodeNum, longName, tt.wantLongName)
			}
			if shortName != tt.wantShortName {
				t.Errorf("defaultCallsign(%#x) shortName = %q, want %q",
					tt.nodeNum, shortName, tt.wantShortName)
			}
		})
	}
}

func TestMyCallsign(t *testing.T) {
	tests := []struct {
		name       string
		setupModel func() model
		want       string
	}{
		{
			name: "zero-mynodenumber-returns-dash",
			setupModel: func() model {
				return newTestModel()
			},
			want: "—",
		},
		{
			name: "known-node-returns-callsign",
			setupModel: func() model {
				m := newTestModel()
				m.MyNodeNum = 0xABCD
				m.NodesByNum = map[uint32]int{0xABCD: 0}
				m.Nodes = []nodeItem{{Callsign: "W6ABC"}}
				return m
			},
			want: "W6ABC",
		},
		{
			name: "mynodenumber-not-in-nodedb",
			setupModel: func() model {
				m := newTestModel()
				m.MyNodeNum = 0x1234
				return m
			},
			want: fmt.Sprintf("node 0x%x", uint32(0x1234)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupModel()
			if got := m.myCallsign(); got != tt.want {
				t.Errorf("myCallsign() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsIgnored(t *testing.T) {
	tests := []struct {
		name    string
		ignored map[string]bool
		from    string
		want    bool
	}{
		{
			name:    "nil-ignored-never-matches",
			ignored: nil,
			from:    "KC7XYZ",
			want:    false,
		},
		{
			name:    "empty-from-never-matches",
			ignored: map[string]bool{"kc7xyz": true},
			from:    "",
			want:    false,
		},
		{
			name:    "direct-match-case-insensitive",
			ignored: map[string]bool{"kc7xyz": true},
			from:    "KC7XYZ",
			want:    true,
		},
		{
			name:    "substring-match",
			ignored: map[string]bool{"kc7": true},
			from:    "KC7XYZ",
			want:    true,
		},
		{
			name:    "no-match",
			ignored: map[string]bool{"w6abc": true},
			from:    "KC7XYZ",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Ignored = tt.ignored
			if got := m.isIgnored(tt.from); got != tt.want {
				t.Errorf("isIgnored(%q) = %v, want %v", tt.from, got, tt.want)
			}
		})
	}
}

func TestSortModeLabel(t *testing.T) {
	tests := []struct {
		name string
		mode sortMode
		want string
	}{
		{name: "by-last-heard", mode: sortByLastHeard, want: "heard"},
		{name: "by-name", mode: sortByName, want: "name"},
		{name: "by-state", mode: sortByState, want: "state"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mode.label(); got != tt.want {
				t.Errorf("sortMode(%d).label() = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

func TestNodeLastHeard(t *testing.T) {
	tests := []struct {
		name string
		node nodeItem
		want string
	}{
		{
			name: "zero-lastHeardat-uses-LastHeard-string",
			node: nodeItem{LastHeard: "3h"},
			want: "3h",
		},
		{
			name: "sub-minute-returns-lt1m",
			node: nodeItem{LastHeardAt: time.Now().Add(-30 * time.Second)},
			want: "<1m",
		},
		{
			name: "several-minutes-ago",
			node: nodeItem{LastHeardAt: time.Now().Add(-5 * time.Minute)},
			want: "5m",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nodeLastHeard(&tt.node)
			if got != tt.want {
				t.Errorf("nodeLastHeard() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseNodeHex(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		wantNum uint32
		wantOk  bool
	}{
		{name: "bang-notation", s: "!deadbeef", wantNum: 0xDEADBEEF, wantOk: true},
		{name: "0x-notation", s: "0xdeadbeef", wantNum: 0xDEADBEEF, wantOk: true},
		{name: "0x-uppercase", s: "0xDEADBEEF", wantNum: 0xDEADBEEF, wantOk: true},
		{name: "node-0x-prefix", s: "node 0xabcd", wantNum: 0xABCD, wantOk: true},
		{name: "plain-string-rejected", s: "KC7XYZ", wantNum: 0, wantOk: false},
		{name: "empty-string-rejected", s: "", wantNum: 0, wantOk: false},
		{name: "bang-empty-rejected", s: "!", wantNum: 0, wantOk: false},
		{name: "overflow-rejected", s: "!fffffffff", wantNum: 0, wantOk: false},
		{name: "invalid-hex-char", s: "!xyz", wantNum: 0, wantOk: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			num, ok := parseNodeHex(tt.s)
			if ok != tt.wantOk {
				t.Errorf("parseNodeHex(%q) ok = %v, want %v", tt.s, ok, tt.wantOk)
			}
			if ok && num != tt.wantNum {
				t.Errorf("parseNodeHex(%q) num = %#x, want %#x", tt.s, num, tt.wantNum)
			}
		})
	}
}

func TestLookupNode(t *testing.T) {
	tests := []struct {
		name         string
		setupModel   func() *model
		callsign     string
		wantNil      bool
		wantCallsign string
	}{
		{
			name: "empty-callsign-returns-nil",
			setupModel: func() *model {
				m := newTestModel()
				return &m
			},
			callsign: "",
			wantNil:  true,
		},
		{
			name: "exact-match",
			setupModel: func() *model {
				m := newTestModel()
				m.Nodes = []nodeItem{{Callsign: "KC7XYZ"}}
				m.NodesByNum = map[uint32]int{0x1111: 0}
				return &m
			},
			callsign:     "KC7XYZ",
			wantCallsign: "KC7XYZ",
		},
		{
			name: "case-insensitive-exact",
			setupModel: func() *model {
				m := newTestModel()
				m.Nodes = []nodeItem{{Callsign: "KC7XYZ"}}
				m.NodesByNum = map[uint32]int{0x1111: 0}
				return &m
			},
			callsign:     "kc7xyz",
			wantCallsign: "KC7XYZ",
		},
		{
			name: "prefix-match",
			setupModel: func() *model {
				m := newTestModel()
				m.Nodes = []nodeItem{{Callsign: "KC7XYZ Mesh Node"}}
				m.NodesByNum = map[uint32]int{0x1111: 0}
				return &m
			},
			callsign:     "KC7XYZ",
			wantCallsign: "KC7XYZ Mesh Node",
		},
		{
			name: "hex-notation-resolves-via-nodesByNum",
			setupModel: func() *model {
				m := newTestModel()
				m.Nodes = []nodeItem{{Callsign: "KC7XYZ", NodeNum: 0xABCD}}
				m.NodesByNum = map[uint32]int{0xABCD: 0}
				return &m
			},
			callsign:     "!0000abcd",
			wantCallsign: "KC7XYZ",
		},
		{
			name: "not-found-returns-nil",
			setupModel: func() *model {
				m := newTestModel()
				m.Nodes = []nodeItem{{Callsign: "KC7XYZ"}}
				return &m
			},
			callsign: "W6NOTHERE",
			wantNil:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupModel()
			got := m.lookupNode(tt.callsign)
			if tt.wantNil {
				if got != nil {
					t.Errorf("lookupNode(%q) = %+v, want nil", tt.callsign, got)
				}
				return
			}
			if got == nil {
				t.Errorf("lookupNode(%q) = nil, want non-nil", tt.callsign)
				return
			}
			if got.Callsign != tt.wantCallsign {
				t.Errorf(
					"lookupNode(%q).Callsign = %q, want %q",
					tt.callsign,
					got.Callsign,
					tt.wantCallsign,
				)
			}
		})
	}
}

func TestWhoisHops(t *testing.T) {
	tests := []struct {
		name         string
		node         nodeItem
		isSelf       bool
		wantContains string
	}{
		{
			name:         "self-returns-self-label",
			node:         nodeItem{},
			isSelf:       true,
			wantContains: "self",
		},
		{
			name:         "direct-peer-returns-direct",
			node:         nodeItem{LastHops: 0},
			isSelf:       false,
			wantContains: "direct",
		},
		{
			name:         "relayed-peer-returns-hop-count",
			node:         nodeItem{LastHops: 3},
			isSelf:       false,
			wantContains: "3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := whoisHops(&tt.node, tt.isSelf)
			if !contains(got, tt.wantContains) {
				t.Errorf("whoisHops(isSelf=%v, hops=%d) = %q, want substring %q",
					tt.isSelf, tt.node.LastHops, got, tt.wantContains)
			}
		})
	}
}

func TestNodeNumOf(t *testing.T) {
	tests := []struct {
		name       string
		setupModel func() *model
		callsign   string
		wantNum    uint32
	}{
		{
			name: "exact-callsign-match",
			setupModel: func() *model {
				m := newTestModel()
				m.Nodes = []nodeItem{{Callsign: "KC7XYZ", NodeNum: 0x1111}}
				m.NodesByNum = map[uint32]int{0x1111: 0}
				return &m
			},
			callsign: "KC7XYZ",
			wantNum:  0x1111,
		},
		{
			name: "hex-notation-parsed",
			setupModel: func() *model {
				m := newTestModel()
				m.Nodes = []nodeItem{{Callsign: "KC7XYZ", NodeNum: 0xABCD}}
				m.NodesByNum = map[uint32]int{0xABCD: 0}
				return &m
			},
			callsign: "!0000abcd",
			wantNum:  0xABCD,
		},
		{
			name: "no-match-returns-zero",
			setupModel: func() *model {
				m := newTestModel()
				return &m
			},
			callsign: "NOTHERE",
			wantNum:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupModel()
			got := m.nodeNumOf(tt.callsign)
			if got != tt.wantNum {
				t.Errorf("nodeNumOf(%q) = %#x, want %#x", tt.callsign, got, tt.wantNum)
			}
		})
	}
}

func TestSignalReport(t *testing.T) {
	tests := []struct {
		name     string
		node     nodeItem
		contains []string
		want     string
	}{
		{
			name: "no-telemetry",
			node: nodeItem{},
			want: "no telemetry yet",
		},
		{
			name:     "snr-only",
			node:     nodeItem{LastSNR: "-8.5"},
			contains: []string{"SNR -8.5 dB"},
		},
		{
			name:     "rssi-only",
			node:     nodeItem{LastRSSI: "-92"},
			contains: []string{"RSSI -92 dBm"},
		},
		{
			name:     "hops-and-snr",
			node:     nodeItem{LastHops: 2, LastSNR: "5.0"},
			contains: []string{"hop 2", "SNR 5.0 dB"},
		},
		{
			name:     "all-fields",
			node:     nodeItem{LastHops: 1, LastSNR: "3.5", LastRSSI: "-80"},
			contains: []string{"hop 1", "SNR 3.5 dB", "RSSI -80 dBm"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := signalReport(&tt.node)
			if tt.want != "" {
				if got != tt.want {
					t.Errorf("signalReport() = %q, want %q", got, tt.want)
				}
				return
			}
			for _, substr := range tt.contains {
				if !contains(got, substr) {
					t.Errorf("signalReport() = %q, want substring %q", got, substr)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && len(substr) > 0 && indexStr(s, substr) >= 0)
}

func indexStr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

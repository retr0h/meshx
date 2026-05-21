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

package model

import (
	"testing"
	"time"
)

// TestDefaultCallsign verifies that the synthesized long and short names
// match Meshtastic's firmware convention: "Meshtastic xxxx" / "xxxx" where
// xxxx is the zero-padded lowercase hex of the low 16 bits of node_num.
func TestDefaultCallsign(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		nodeNum   uint32
		wantLong  string
		wantShort string
	}{
		{
			name:      "zero",
			nodeNum:   0,
			wantLong:  "Meshtastic 0000",
			wantShort: "0000",
		},
		{
			name:      "low-nibble only",
			nodeNum:   0x0001,
			wantLong:  "Meshtastic 0001",
			wantShort: "0001",
		},
		{
			name:      "exactly 4 hex digits",
			nodeNum:   0xABCD,
			wantLong:  "Meshtastic abcd",
			wantShort: "abcd",
		},
		{
			name:      "upper bits masked off",
			nodeNum:   0xDEADBEEF,
			wantLong:  "Meshtastic beef",
			wantShort: "beef",
		},
		{
			name:      "max uint32",
			nodeNum:   0xFFFFFFFF,
			wantLong:  "Meshtastic ffff",
			wantShort: "ffff",
		},
		{
			name:      "typical node num",
			nodeNum:   0x1234CAFE,
			wantLong:  "Meshtastic cafe",
			wantShort: "cafe",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			long, short := DefaultCallsign(tc.nodeNum)
			if long != tc.wantLong {
				t.Errorf("long = %q, want %q", long, tc.wantLong)
			}
			if short != tc.wantShort {
				t.Errorf("short = %q, want %q", short, tc.wantShort)
			}
		})
	}
}

// TestNodeItemFromCached verifies the name-fallback chain and that every
// CachedNode field is projected faithfully to the resulting NodeItem.
func TestNodeItemFromCached(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		cached        CachedNode
		state         NodeState
		wantCallsign  string
		wantShortName string
		wantNodeNum   uint32
		wantState     NodeState
		wantFav       bool
		wantLastHeard string
		wantHwModel   string
	}{
		{
			name: "long-name present — preferred",
			cached: CachedNode{
				NodeNum:   0xAABBCCDD,
				LongName:  "Alice Station",
				ShortName: "ALIC",
				HwModel:   "TBEAM",
				Favorite:  true,
			},
			state:         StateOffline,
			wantCallsign:  "Alice Station",
			wantShortName: "ALIC",
			wantNodeNum:   0xAABBCCDD,
			wantState:     StateOffline,
			wantFav:       true,
			wantLastHeard: "cached",
			wantHwModel:   "TBEAM",
		},
		{
			name: "no long-name — falls back to short-name",
			cached: CachedNode{
				NodeNum:   0x00000042,
				LongName:  "",
				ShortName: "BOBJ",
				HwModel:   "HELTEC_V3",
			},
			state:         StateMuted,
			wantCallsign:  "BOBJ",
			wantShortName: "BOBJ",
			wantNodeNum:   0x00000042,
			wantState:     StateMuted,
			wantFav:       false,
			wantLastHeard: "cached",
			wantHwModel:   "HELTEC_V3",
		},
		{
			name: "no long or short — falls back to hex placeholder",
			cached: CachedNode{
				NodeNum:   0xDEAD,
				LongName:  "",
				ShortName: "",
				HwModel:   "",
			},
			state:         StateUnknown,
			wantCallsign:  "node 0xdead",
			wantShortName: "",
			wantNodeNum:   0xDEAD,
			wantState:     StateUnknown,
			wantFav:       false,
			wantLastHeard: "cached",
			wantHwModel:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := NodeItemFromCached(tc.cached, tc.state)
			if got.Callsign != tc.wantCallsign {
				t.Errorf("Callsign = %q, want %q", got.Callsign, tc.wantCallsign)
			}
			if got.ShortName != tc.wantShortName {
				t.Errorf("ShortName = %q, want %q", got.ShortName, tc.wantShortName)
			}
			if got.NodeNum != tc.wantNodeNum {
				t.Errorf("NodeNum = %d, want %d", got.NodeNum, tc.wantNodeNum)
			}
			if got.State != tc.wantState {
				t.Errorf("State = %q, want %q", got.State, tc.wantState)
			}
			if got.Fav != tc.wantFav {
				t.Errorf("Fav = %v, want %v", got.Fav, tc.wantFav)
			}
			if got.LastHeard != tc.wantLastHeard {
				t.Errorf("LastHeard = %q, want %q", got.LastHeard, tc.wantLastHeard)
			}
			if got.HwModel != tc.wantHwModel {
				t.Errorf("HwModel = %q, want %q", got.HwModel, tc.wantHwModel)
			}
		})
	}
}

// TestNodeItem_CurrentState verifies that CurrentState derives the peer's
// effective NodeState according to the muted-wins / LastHeardAt rules.
func TestNodeItem_CurrentState(t *testing.T) {
	t.Parallel()

	now := time.Now()

	cases := []struct {
		name string
		item *NodeItem
		want NodeState
	}{
		{
			name: "nil receiver returns StateUnknown",
			item: nil,
			want: StateUnknown,
		},
		{
			name: "StateMuted wins regardless of LastHeardAt",
			item: &NodeItem{State: StateMuted, LastHeardAt: now},
			want: StateMuted,
		},
		{
			name: "zero LastHeardAt returns stored State",
			item: &NodeItem{State: StateOffline},
			want: StateOffline,
		},
		{
			name: "zero LastHeardAt with StateUnknown returns StateUnknown",
			item: &NodeItem{State: StateUnknown},
			want: StateUnknown,
		},
		{
			name: "heard within 15 minutes returns StateOnline",
			item: &NodeItem{LastHeardAt: now.Add(-5 * time.Minute)},
			want: StateOnline,
		},
		{
			name: "heard exactly at boundary edge (just under) returns StateOnline",
			item: &NodeItem{LastHeardAt: now.Add(-14*time.Minute - 59*time.Second)},
			want: StateOnline,
		},
		{
			name: "heard more than 15 minutes ago returns StateOffline",
			item: &NodeItem{LastHeardAt: now.Add(-16 * time.Minute)},
			want: StateOffline,
		},
		{
			name: "heard long ago returns StateOffline",
			item: &NodeItem{LastHeardAt: now.Add(-24 * time.Hour)},
			want: StateOffline,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.item.CurrentState()
			if got != tc.want {
				t.Errorf("CurrentState() = %q, want %q", got, tc.want)
			}
		})
	}
}

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

	"github.com/retr0h/meshx/internal/radio"
)

func TestSortPlotsByDistance(t *testing.T) {
	tests := []struct {
		name      string
		plots     []peerPlot
		wantFirst float64
		wantLast  float64
	}{
		{
			name: "sorted-ascending",
			plots: []peerPlot{
				{node: &nodeItem{NodeNum: 3}, distKm: 30},
				{node: &nodeItem{NodeNum: 1}, distKm: 5},
				{node: &nodeItem{NodeNum: 2}, distKm: 15},
			},
			wantFirst: 5,
			wantLast:  30,
		},
		{
			name:      "empty-is-noop",
			plots:     nil,
			wantFirst: 0,
			wantLast:  0,
		},
		{
			name: "equal-distances-tiebreak-by-nodenum",
			plots: []peerPlot{
				{node: &nodeItem{NodeNum: 20}, distKm: 10},
				{node: &nodeItem{NodeNum: 5}, distKm: 10},
				{node: &nodeItem{NodeNum: 15}, distKm: 10},
			},
			// All equal distance; nodenum 5 < 15 < 20
			wantFirst: 10,
			wantLast:  10,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortPlotsByDistance(tt.plots)
			if len(tt.plots) == 0 {
				return
			}
			if tt.plots[0].distKm != tt.wantFirst {
				t.Errorf(
					"after sort: plots[0].distKm = %v, want %v",
					tt.plots[0].distKm,
					tt.wantFirst,
				)
			}
			last := tt.plots[len(tt.plots)-1]
			if last.distKm != tt.wantLast {
				t.Errorf("after sort: plots[last].distKm = %v, want %v", last.distKm, tt.wantLast)
			}
		})
	}
}

func TestSortPlotsByDistanceTiebreakOrder(t *testing.T) {
	// Equal-distance peers must be ordered ascending by NodeNum.
	plots := []peerPlot{
		{node: &nodeItem{NodeNum: 30}, distKm: 10},
		{node: &nodeItem{NodeNum: 10}, distKm: 10},
		{node: &nodeItem{NodeNum: 20}, distKm: 10},
	}
	sortPlotsByDistance(plots)
	for i := 1; i < len(plots); i++ {
		if plots[i].node.NodeNum < plots[i-1].node.NodeNum {
			t.Errorf(
				"tiebreak: NodeNum not ascending at index %d: %d < %d",
				i, plots[i].node.NodeNum, plots[i-1].node.NodeNum,
			)
		}
	}
}

func TestCollectPeerPlotsExcludesSelf(t *testing.T) {
	m := newTestModel()
	m.MyNodeNum = 0x1234
	m.MyLatitude = 45.0
	m.MyLongitude = -122.0
	m.NodesByNum[0x1234] = 0
	m.Nodes = []nodeItem{{NodeNum: 0x1234, Callsign: "ME"}}
	m.PeerPositions = map[uint32]radio.PeerPosition{
		0x1234: {Latitude: 46.0, Longitude: -122.0},
	}
	plots := m.collectPeerPlots()
	for _, p := range plots {
		if p.node.NodeNum == m.MyNodeNum {
			t.Errorf("collectPeerPlots() returned self (NodeNum=%x)", m.MyNodeNum)
		}
	}
}

func TestCollectPeerPlotsIncludesPeerWithPosition(t *testing.T) {
	m := newTestModel()
	m.MyNodeNum = 0x0001
	m.MyLatitude = 45.5
	m.MyLongitude = -122.5
	m.NodesByNum[0x0002] = 0
	m.Nodes = []nodeItem{{NodeNum: 0x0002, Callsign: "PEER"}}
	m.PeerPositions = map[uint32]radio.PeerPosition{
		0x0002: {Latitude: 46.5, Longitude: -122.5},
	}
	plots := m.collectPeerPlots()
	if len(plots) != 1 {
		t.Errorf("collectPeerPlots() count = %d, want 1", len(plots))
	}
	if len(plots) > 0 && plots[0].distKm <= 0 {
		t.Errorf("collectPeerPlots() distKm = %v, want > 0", plots[0].distKm)
	}
}

func TestCollectPeerPlotsSkipsMissingNodeDB(t *testing.T) {
	// Position exists for a peer not in NodesByNum — must be skipped.
	m := newTestModel()
	m.MyNodeNum = 0x0001
	m.MyLatitude = 45.0
	m.MyLongitude = -122.0
	m.PeerPositions = map[uint32]radio.PeerPosition{
		0x9999: {Latitude: 46.0, Longitude: -122.0},
	}
	// NodesByNum does not contain 0x9999
	plots := m.collectPeerPlots()
	if len(plots) != 0 {
		t.Errorf("collectPeerPlots() should skip peer not in NodesByNum, got %d plots", len(plots))
	}
}

func TestDrawRingPaintsGlyphs(t *testing.T) {
	rows := 10
	cols := 20
	canvas := make([][]rune, rows)
	colors := make([][]string, rows)
	for r := 0; r < rows; r++ {
		canvas[r] = make([]rune, cols)
		colors[r] = make([]string, cols)
		for c := range canvas[r] {
			canvas[r][c] = ' '
		}
	}
	cx, cy := cols/2, rows/2
	drawRing(canvas, colors, cx, cy, float64(cols)/4, float64(rows)/4, mhDrained)

	// At least some cells should have been painted with '·'.
	count := 0
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if canvas[r][c] == '·' {
				count++
			}
		}
	}
	if count == 0 {
		t.Error("drawRing() painted no '·' cells onto the canvas")
	}
}

func TestDrawRingDoesNotOverwriteExistingGlyphs(t *testing.T) {
	rows := 10
	cols := 20
	canvas := make([][]rune, rows)
	colors := make([][]string, rows)
	for r := 0; r < rows; r++ {
		canvas[r] = make([]rune, cols)
		colors[r] = make([]string, cols)
		for c := range canvas[r] {
			canvas[r][c] = ' '
		}
	}
	// Place an existing non-space glyph at the center.
	cx, cy := cols/2, rows/2
	canvas[cy][cx] = '@'

	// A zero-radius ring would only try to paint the center.
	drawRing(canvas, colors, cx, cy, 0, 0, mhDrained)
	// The '@' must still be there (ring only paints ' ' cells).
	if canvas[cy][cx] != '@' {
		t.Errorf("drawRing() overwrote existing glyph '@' at center")
	}
}

func TestNearbyRosterPrependsSelf(t *testing.T) {
	m := newTestModel()
	m.MyNodeNum = 0x0001
	m.MyLatitude = 45.5
	m.MyLongitude = -122.5
	m.NodesByNum[0x0001] = 0
	m.Nodes = []nodeItem{{NodeNum: 0x0001, Callsign: "ME"}}
	// One peer with a position.
	m.NodesByNum[0x0002] = 1
	m.Nodes = append(m.Nodes, nodeItem{NodeNum: 0x0002, Callsign: "PEER"})
	m.PeerPositions = map[uint32]radio.PeerPosition{
		0x0002: {Latitude: 46.5, Longitude: -122.5},
	}

	roster := m.nearbyRoster()
	if len(roster) < 2 {
		t.Fatalf("nearbyRoster() count = %d, want >= 2", len(roster))
	}
	// Self must be first (distance 0).
	if roster[0].distKm != 0 {
		t.Errorf("nearbyRoster()[0].distKm = %v, want 0 (self)", roster[0].distKm)
	}
	if roster[0].node.NodeNum != m.MyNodeNum {
		t.Errorf(
			"nearbyRoster()[0].NodeNum = %x, want %x (self)",
			roster[0].node.NodeNum, m.MyNodeNum,
		)
	}
}

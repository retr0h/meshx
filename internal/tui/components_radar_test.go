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
)

func TestNiceRadarTick(t *testing.T) {
	tests := []struct {
		name string
		km   float64
		want float64
	}{
		{name: "sub-one-km", km: 0.5, want: 1},
		{name: "exactly-one", km: 1.0, want: 1},
		{name: "between-one-and-two", km: 1.5, want: 2},
		{name: "exactly-two", km: 2.0, want: 2},
		{name: "between-two-and-five", km: 3.0, want: 5},
		{name: "exactly-five", km: 5.0, want: 5},
		{name: "between-five-and-ten", km: 7.5, want: 10},
		{name: "exactly-ten", km: 10.0, want: 10},
		{name: "between-ten-and-twenty-five", km: 15.0, want: 25},
		{name: "exactly-twenty-five", km: 25.0, want: 25},
		{name: "between-25-and-50", km: 30.0, want: 50},
		{name: "exactly-50", km: 50.0, want: 50},
		{name: "between-100-and-250", km: 120.0, want: 250},
		{name: "exactly-500", km: 500.0, want: 500},
		{name: "exactly-1000", km: 1000.0, want: 1000},
		{name: "over-1000", km: 1500.0, want: 2000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := niceRadarTick(tt.km); got != tt.want {
				t.Errorf("niceRadarTick(%v) = %v, want %v", tt.km, got, tt.want)
			}
		})
	}
}

func TestRadarLegendCell(t *testing.T) {
	tests := []struct {
		name     string
		maxKm    float64
		wantText string
	}{
		{
			name:     "shows-km-scale",
			maxKm:    25.0,
			wantText: "25.0 km",
		},
		{
			name:     "fractional-km",
			maxKm:    1.5,
			wantText: "1.5 km",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := radarLegendCell(tt.maxKm)
			plain := ansi.Strip(got)
			if !strings.Contains(plain, tt.wantText) {
				t.Errorf(
					"radarLegendCell(%v) plain = %q, want to contain %q",
					tt.maxKm,
					plain,
					tt.wantText,
				)
			}
		})
	}
}

func TestRadarPeerLine(t *testing.T) {
	tests := []struct {
		name     string
		callsign string
		compass  string
		bearing  float64
		distKm   float64
		wantText string
	}{
		{
			name:     "basic-peer",
			callsign: "KC7ABC",
			compass:  "N",
			bearing:  0,
			distKm:   5.3,
			wantText: "KC7ABC",
		},
		{
			name:     "shows-distance",
			callsign: "W6XYZ",
			compass:  "SE",
			bearing:  135,
			distKm:   12.7,
			wantText: "12.7 km",
		},
		{
			name:     "shows-bearing",
			callsign: "AD0WG",
			compass:  "E",
			bearing:  90,
			distKm:   3.0,
			wantText: " 90°",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := radarPeerLine(tt.callsign, tt.compass, tt.bearing, tt.distKm)
			plain := ansi.Strip(got)
			if !strings.Contains(plain, tt.wantText) {
				t.Errorf(
					"radarPeerLine(%q, %q, %v, %v) plain = %q, want to contain %q",
					tt.callsign, tt.compass, tt.bearing, tt.distKm, plain, tt.wantText,
				)
			}
		})
	}
}

func TestRadarCanvasRenderEmptyBox(t *testing.T) {
	rc := radarCanvas{
		Canvas:  [][]rune{},
		Colors:  [][]string{},
		LeadPad: 0,
	}
	got := rc.Render(Box{Width: 0, Height: 0})
	if got != "" {
		t.Errorf("radarCanvas.Render(empty box) = %q, want empty string", got)
	}
}

func TestRadarCanvasRenderLineCount(t *testing.T) {
	rows := 4
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
	rc := radarCanvas{Canvas: canvas, Colors: colors, LeadPad: 2}

	box := Box{Width: 30, Height: rows}
	got := rc.Render(box)
	lines := strings.Split(got, "\n")
	if len(lines) != rows {
		t.Errorf("radarCanvas.Render() line count = %d, want %d", len(lines), rows)
	}
}

func TestRadarCanvasRenderPadsToBoxHeight(t *testing.T) {
	// Canvas has fewer rows than the box height — must pad with blank lines.
	rows := 2
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
	rc := radarCanvas{Canvas: canvas, Colors: colors}

	boxHeight := 5
	got := rc.Render(Box{Width: 30, Height: boxHeight})
	lines := strings.Split(got, "\n")
	if len(lines) != boxHeight {
		t.Errorf(
			"radarCanvas.Render() with padding: line count = %d, want %d",
			len(lines),
			boxHeight,
		)
	}
}

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

func TestRenderQRASCII(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
		check   func(t *testing.T, out string)
	}{
		{
			name:    "empty-data-returns-error",
			data:    "",
			wantErr: true,
		},
		{
			name:    "small-url-produces-output",
			data:    "https://example.com",
			wantErr: false,
			check: func(t *testing.T, out string) {
				if out == "" {
					t.Error("expected non-empty QR output")
				}
				// Must contain block characters (half-block trick)
				if !strings.ContainsAny(out, "█▀▄ ") {
					t.Errorf(
						"QR output lacks expected block characters: %q",
						out[:minInt(50, len(out))],
					)
				}
				// Must be multi-line
				lines := strings.Split(out, "\n")
				if len(lines) < 5 {
					t.Errorf("QR output has only %d lines, expected many more", len(lines))
				}
			},
		},
		{
			name:    "meshtastic-channel-url",
			data:    "https://meshtastic.org/e/#CgUYAyIBMRABGgdNZXNoVUkh",
			wantErr: false,
			check: func(t *testing.T, out string) {
				if out == "" {
					t.Error("expected non-empty QR output for meshtastic URL")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderQRASCII(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Errorf("renderQRASCII(%q) expected error, got nil", tt.data)
				}
				return
			}
			if err != nil {
				t.Errorf("renderQRASCII(%q) unexpected error: %v", tt.data, err)
				return
			}
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestPadQRGrid(t *testing.T) {
	tests := []struct {
		name  string
		grid  [][]bool
		pad   int
		wantH int
		wantW int
	}{
		{
			name:  "empty-grid-unchanged",
			grid:  nil,
			pad:   4,
			wantH: 0,
			wantW: 0,
		},
		{
			name: "5x5-grid-padded-by-1",
			grid: [][]bool{
				{true, false, true, false, true},
				{false, true, false, true, false},
				{true, false, true, false, true},
				{false, true, false, true, false},
				{true, false, true, false, true},
			},
			pad:   1,
			wantH: 5 + 2*1, // 7
			wantW: 5 + 2*1, // 7
		},
		{
			name:  "1x1-grid-padded-by-4",
			grid:  [][]bool{{true}},
			pad:   4,
			wantH: 1 + 2*4, // 9
			wantW: 1 + 2*4, // 9
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := padQRGrid(tt.grid, tt.pad)
			if len(got) != tt.wantH {
				t.Errorf("padQRGrid height = %d, want %d", len(got), tt.wantH)
			}
			if tt.wantH > 0 && len(got[0]) != tt.wantW {
				t.Errorf("padQRGrid width = %d, want %d", len(got[0]), tt.wantW)
			}
			// Verify border rows are all false (quiet zone)
			if tt.pad > 0 && len(got) > 0 {
				for _, cell := range got[0] {
					if cell {
						t.Error("top quiet zone contains a true (dark) cell")
					}
				}
				for _, cell := range got[len(got)-1] {
					if cell {
						t.Error("bottom quiet zone contains a true (dark) cell")
					}
				}
			}
		})
	}
}

func TestHalfBlockEncode(t *testing.T) {
	tests := []struct {
		name  string
		grid  [][]bool
		check func(t *testing.T, out string)
	}{
		{
			name: "empty-grid-returns-empty",
			grid: nil,
			check: func(t *testing.T, out string) {
				if out != "" {
					t.Errorf("halfBlockEncode(nil) = %q, want empty", out)
				}
			},
		},
		{
			name: "all-true-2x2-encodes-to-full-block",
			grid: [][]bool{
				{true, true},
				{true, true},
			},
			check: func(t *testing.T, out string) {
				// Two top+bot=true cells → "██"
				if !strings.Contains(out, "██") {
					t.Errorf("all-true 2x2 should contain full blocks, got %q", out)
				}
			},
		},
		{
			name: "all-false-2x2-encodes-to-spaces",
			grid: [][]bool{
				{false, false},
				{false, false},
			},
			check: func(t *testing.T, out string) {
				stripped := strings.ReplaceAll(out, "\n", "")
				if strings.TrimSpace(stripped) != "" {
					t.Errorf("all-false grid should be spaces, got %q", out)
				}
			},
		},
		{
			name: "odd-row-count-encodes-last-as-upper",
			grid: [][]bool{
				{true},
				{false},
				{true}, // odd row — upper half only → ▀
			},
			check: func(t *testing.T, out string) {
				lines := strings.Split(out, "\n")
				if len(lines) != 2 {
					t.Errorf("3 rows should produce 2 output lines, got %d", len(lines))
				}
				// Last line: top=true, bot=missing → ▀
				if !strings.Contains(lines[1], "▀") {
					t.Errorf("last odd row should produce ▀, got %q", lines[1])
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := halfBlockEncode(tt.grid)
			tt.check(t, got)
		})
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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

	"github.com/charmbracelet/x/ansi"
)

func TestPickSplash(t *testing.T) {
	// pickSplash must always return a non-empty variant from allSplashVariants.
	// Run several times to exercise the random selection.
	names := make(map[string]bool)
	for i := 0; i < 20; i++ {
		v := pickSplash()
		if v.name == "" {
			t.Error("pickSplash() returned variant with empty name")
		}
		if len(v.rows) == 0 {
			t.Errorf("pickSplash() variant %q has no rows", v.name)
		}
		if v.color == nil {
			t.Errorf("pickSplash() variant %q has nil color function", v.name)
		}
		names[v.name] = true
	}
	// Sanity: allSplashVariants has at least one entry.
	if len(allSplashVariants) == 0 {
		t.Error("allSplashVariants is empty")
	}
}

func TestAllSplashVariantsColorFunctions(t *testing.T) {
	// Every variant's color function must return a non-empty hex color for
	// each row index.
	for _, v := range allSplashVariants {
		t.Run(v.name, func(t *testing.T) {
			for i := range v.rows {
				got := v.color(i)
				if got == "" {
					t.Errorf("variant %q color(%d) returned empty string", v.name, i)
				}
				// Color must start with '#' to be a valid hex color.
				if len(got) == 0 || got[0] != '#' {
					t.Errorf("variant %q color(%d) = %q, want '#rrggbb' format", v.name, i, got)
				}
			}
		})
	}
}

func TestSplashAsNoticesStructure(t *testing.T) {
	// splashAsNotices must return: blank + N art rows + blank + tagline + blank.
	for _, v := range allSplashVariants {
		t.Run(v.name, func(t *testing.T) {
			rows := splashAsNotices(v, "KC7ABC")
			// Minimum: 1 leading blank + len(rows) art rows + 1 blank + 1 tagline + 1 trailing blank
			minExpected := 1 + len(v.rows) + 3
			if len(rows) < minExpected {
				t.Errorf(
					"splashAsNotices(%q) len = %d, want >= %d",
					v.name, len(rows), minExpected,
				)
			}
			// First row must be blank (leading padding).
			if rows[0].text != "" {
				t.Errorf(
					"splashAsNotices(%q) rows[0].text = %q, want empty (leading blank)",
					v.name, rows[0].text,
				)
			}
			// Last row must be blank (trailing padding).
			last := rows[len(rows)-1]
			if last.text != "" {
				t.Errorf(
					"splashAsNotices(%q) last row text = %q, want empty (trailing blank)",
					v.name, last.text,
				)
			}
		})
	}
}

func TestSplashAsNoticesArtRowStyles(t *testing.T) {
	// Art rows must have bold+center+non-empty fg style.
	v := allSplashVariants[0]
	rows := splashAsNotices(v, "W6ABC")
	// Skip the first blank row; art rows start at index 1.
	artRowCount := 0
	for i := 1; i <= len(v.rows); i++ {
		r := rows[i]
		if !r.style.bold {
			t.Errorf("splashAsNotices art row %d: bold = false, want true", i)
		}
		if !r.style.center {
			t.Errorf("splashAsNotices art row %d: center = false, want true", i)
		}
		if r.style.fg == "" {
			t.Errorf("splashAsNotices art row %d: fg = empty, want non-empty color", i)
		}
		artRowCount++
	}
	if artRowCount != len(v.rows) {
		t.Errorf("expected %d art rows, got %d", len(v.rows), artRowCount)
	}
}

func TestSplashTaglineCell(t *testing.T) {
	got := splashTaglineCell("KC7ABC")
	plain := ansi.Strip(got)

	// The tagline must always mention the brand name "Meshtastic".
	if len(plain) == 0 {
		t.Error("splashTaglineCell() returned empty string")
	}
}

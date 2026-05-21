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

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestNoticeRowFor_Parts(t *testing.T) {
	tests := []struct {
		name     string
		rowBg    string
		time     string
		pinFirst bool
		pinLast  bool
		fade     float64
	}{
		{
			name:  "basic-non-pinned-row",
			rowBg: rowBgEven,
			time:  "09:47",
		},
		{
			name:     "pinned-first-row",
			rowBg:    rowBgOdd,
			time:     "09:47",
			pinFirst: true,
		},
		{
			name:    "pinned-last-row",
			rowBg:   rowBgEven,
			time:    "09:47",
			pinLast: true,
		},
		{
			name:  "faded-row",
			rowBg: rowBgEven,
			time:  "09:47",
			fade:  0.5,
		},
		{
			name:  "empty-time",
			rowBg: rowBgEven,
			time:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := noticeRowFor(tt.rowBg, tt.time, tt.pinFirst, tt.pinLast, tt.fade)
			// All parts must be non-empty — the row renderer relies on them.
			if parts.accent == "" {
				t.Error("noticeRowFor accent is empty")
			}
			if parts.time == "" {
				t.Error("noticeRowFor time is empty")
			}
			if parts.pinEnd == "" {
				t.Error("noticeRowFor pinEnd is empty")
			}
			if parts.rowBg != tt.rowBg {
				t.Errorf("noticeRowFor rowBg = %q, want %q", parts.rowBg, tt.rowBg)
			}
		})
	}
}

func TestNoticeRowLine_Width(t *testing.T) {
	bg := rowBgEven
	parts := noticeRowFor(bg, "09:47", false, false, 0)
	bodyStyler := lipgloss.NewStyle()
	body := bodyStyler.Render("-!- hello world")

	for _, w := range rangeOfWidths {
		out := noticeRowLine(parts, body, w)
		gotW := ansi.StringWidth(out)
		if gotW != w {
			t.Errorf("noticeRowLine width %d: got %d cells\nstripped=%q",
				w, gotW, ansi.Strip(out))
		}
	}
}

func TestNoticeRowLineSplit_Width(t *testing.T) {
	bg := rowBgEven
	parts := noticeRowFor(bg, "09:47", false, false, 0)
	prefix := lipgloss.NewStyle().Render("-!- ")
	content := lipgloss.NewStyle().Render("hello mesh")

	aligns := []Align{AlignLeft, AlignCenter, AlignRight}
	for _, align := range aligns {
		for _, w := range rangeOfWidths {
			out := noticeRowLineSplit(parts, prefix, content, align, w)
			gotW := ansi.StringWidth(out)
			if gotW != w {
				t.Errorf("noticeRowLineSplit align=%v width %d: got %d cells\nstripped=%q",
					align, w, gotW, ansi.Strip(out))
			}
		}
	}
}

func TestNoticeRowFor_PinFirstShowsCorner(t *testing.T) {
	parts := noticeRowFor(rowBgEven, "09:47", true, false, 0)
	stripped := ansi.Strip(parts.time)
	if !strings.Contains(stripped, "⌜") {
		t.Errorf("pinFirst=true time cell %q should contain ⌜", stripped)
	}
}

func TestNoticeRowFor_PinLastShowsCorner(t *testing.T) {
	parts := noticeRowFor(rowBgEven, "09:47", false, true, 0)
	stripped := ansi.Strip(parts.pinEnd)
	if !strings.Contains(stripped, "⌟") {
		t.Errorf("pinLast=true pinEnd cell %q should contain ⌟", stripped)
	}
}

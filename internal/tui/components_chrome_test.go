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

func TestChannelTabCell(t *testing.T) {
	tests := []struct {
		name          string
		tabName       string
		idx           int
		active        bool
		private       bool
		isDM          bool
		unread        int
		checkStripped func(t *testing.T, stripped string)
	}{
		{
			name:    "active-tab-is-bracketed",
			tabName: "default",
			idx:     0,
			active:  true,
			checkStripped: func(t *testing.T, s string) {
				if !strings.Contains(s, "[") || !strings.Contains(s, "]") {
					t.Errorf("active tab %q should contain brackets", s)
				}
				if !strings.Contains(s, "default") {
					t.Errorf("active tab %q should contain name", s)
				}
			},
		},
		{
			name:    "inactive-tab-has-no-brackets",
			tabName: "mesh",
			idx:     1,
			active:  false,
			checkStripped: func(t *testing.T, s string) {
				if strings.HasPrefix(strings.TrimSpace(s), "[") {
					t.Errorf("inactive tab %q should not start with bracket", s)
				}
				if !strings.Contains(s, "mesh") {
					t.Errorf("inactive tab %q should contain name", s)
				}
			},
		},
		{
			name:    "unread-count-appears-in-output",
			tabName: "mesh",
			idx:     0,
			active:  false,
			unread:  3,
			checkStripped: func(t *testing.T, s string) {
				if !strings.Contains(s, "3") {
					t.Errorf("tab with unread=3 %q should contain '3'", s)
				}
			},
		},
		{
			name:    "private-unread-uses-bang",
			tabName: "secret",
			idx:     0,
			active:  false,
			private: true,
			unread:  2,
			checkStripped: func(t *testing.T, s string) {
				if !strings.Contains(s, "!") {
					t.Errorf("private unread tab %q should contain '!'", s)
				}
			},
		},
		{
			name:    "zero-unread-no-badge",
			tabName: "mesh",
			idx:     0,
			active:  false,
			unread:  0,
			checkStripped: func(t *testing.T, s string) {
				// Should not contain parenthesized number
				if strings.Contains(s, "(") {
					t.Errorf("zero unread tab %q should not have badge", s)
				}
			},
		},
		{
			name:    "slot-index-1based",
			tabName: "mesh",
			idx:     2, // slot 3
			active:  false,
			checkStripped: func(t *testing.T, s string) {
				if !strings.Contains(s, "3:") {
					t.Errorf("slot-idx 2 tab %q should contain '3:'", s)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := channelTabCell(tt.tabName, tt.idx, tt.active, tt.private, tt.isDM, tt.unread)
			stripped := ansi.Strip(out)
			tt.checkStripped(t, stripped)
		})
	}
}

func TestStatusSegment(t *testing.T) {
	tests := []struct {
		name    string
		content string
		chrome  string
	}{
		{name: "plain-content", content: "online", chrome: mhDrained},
		{name: "empty-content", content: "", chrome: mhDrained},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := statusSegment(tt.content, tt.chrome)
			stripped := ansi.Strip(out)
			// Must contain the gradient chars
			if !strings.Contains(stripped, "░▒▓") {
				t.Errorf("statusSegment() = %q, want gradient chars ░▒▓", stripped)
			}
			// Must contain content
			if tt.content != "" && !strings.Contains(stripped, tt.content) {
				t.Errorf("statusSegment() = %q, want content %q", stripped, tt.content)
			}
		})
	}
}

func TestStatusBar_Render_Width(t *testing.T) {
	m := newTestModel()
	m.Connected = true
	sb := statusBar{m: m}

	for _, w := range rangeOfWidths {
		box := Box{Width: w, Height: 1}
		out := sb.Render(box)
		lines := strings.Split(out, "\n")
		if len(lines) != 1 {
			t.Errorf("statusBar.Render w=%d: got %d lines, want 1", w, len(lines))
			continue
		}
		gotW := ansi.StringWidth(out)
		if gotW != w {
			t.Errorf("statusBar.Render w=%d: got %d cells\nstripped=%q",
				w, gotW, ansi.Strip(out))
		}
	}
}

func TestStatusBar_Render_OnlineState(t *testing.T) {
	tests := []struct {
		name      string
		connected bool
		wantText  string
	}{
		{
			name:      "connected-shows-online",
			connected: true,
			wantText:  "online",
		},
		{
			name:      "disconnected-shows-connecting",
			connected: false,
			wantText:  "connecting",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Connected = tt.connected
			sb := statusBar{m: m}
			out := sb.Render(Box{Width: 120, Height: 1})
			stripped := ansi.Strip(out)
			if !strings.Contains(stripped, tt.wantText) {
				t.Errorf("statusBar.Render Connected=%v: %q does not contain %q",
					tt.connected, stripped, tt.wantText)
			}
		})
	}
}

func TestByteCounterCell(t *testing.T) {
	tests := []struct {
		name         string
		used         int
		capBytes     int
		wantContains []string
	}{
		{
			name:         "shows-used-slash-cap",
			used:         50,
			capBytes:     228,
			wantContains: []string{"50/228"},
		},
		{
			name:         "at-cap",
			used:         228,
			capBytes:     228,
			wantContains: []string{"228/228"},
		},
		{
			name:         "zero-used",
			used:         0,
			capBytes:     228,
			wantContains: []string{"0/228"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := byteCounterCell(tt.used, tt.capBytes)
			stripped := ansi.Strip(out)
			for _, want := range tt.wantContains {
				if !strings.Contains(stripped, want) {
					t.Errorf("byteCounterCell(%d,%d) = %q, want substring %q",
						tt.used, tt.capBytes, stripped, want)
				}
			}
		})
	}
}

func TestFlashBannerCell(t *testing.T) {
	tests := []struct {
		name      string
		flash     string
		wantEmpty bool
	}{
		{
			name:      "empty-flash-returns-empty",
			flash:     "",
			wantEmpty: true,
		},
		{
			name:  "nonempty-flash-renders-content",
			flash: "ack received",
		},
		{
			name:  "unknown-prefix-still-renders",
			flash: "unknown command",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := flashBannerCell(tt.flash)
			if tt.wantEmpty {
				if out != "" {
					t.Errorf("flashBannerCell(%q) = %q, want empty", tt.flash, out)
				}
				return
			}
			stripped := ansi.Strip(out)
			if !strings.Contains(stripped, tt.flash) {
				t.Errorf(
					"flashBannerCell(%q) stripped=%q, want to contain flash text",
					tt.flash,
					stripped,
				)
			}
		})
	}
}

func TestTopDivider_Render(t *testing.T) {
	td := topDivider{}
	for _, w := range rangeOfWidths {
		box := Box{Width: w, Height: 1}
		out := td.Render(box)
		stripped := ansi.Strip(out)
		// ═ is a multi-byte UTF-8 char that renders as 1 cell; measure with StringWidth
		gotW := ansi.StringWidth(stripped)
		if gotW != w {
			t.Errorf("topDivider.Render w=%d: got %d display cells (out=%q)",
				w, gotW, stripped)
		}
		// Should be all ═ signs (U+2550 BOX DRAWINGS DOUBLE HORIZONTAL)
		for _, r := range stripped {
			if r != '═' {
				t.Errorf("topDivider.Render w=%d: unexpected char %q in output", w, r)
				break
			}
		}
	}
}

func TestChannelTabsRow_Render_Width(t *testing.T) {
	m := newTestModel()
	ctr := channelTabsRow{m: m}

	for _, w := range rangeOfWidths {
		box := Box{Width: w, Height: 1}
		out := ctr.Render(box)
		gotW := ansi.StringWidth(out)
		if gotW != w {
			t.Errorf("channelTabsRow.Render w=%d: got %d cells\nstripped=%q",
				w, gotW, ansi.Strip(out))
		}
	}
}

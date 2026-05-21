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

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

func TestZebraBg(t *testing.T) {
	tests := []struct {
		name string
		i    int
		want string
	}{
		{name: "even-row", i: 0, want: rowBgEven},
		{name: "odd-row", i: 1, want: rowBgOdd},
		{name: "even-large", i: 100, want: rowBgEven},
		{name: "odd-large", i: 101, want: rowBgOdd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := zebraBg(tt.i); got != tt.want {
				t.Errorf("zebraBg(%d) = %q, want %q", tt.i, got, tt.want)
			}
		})
	}
}

func TestNickColor(t *testing.T) {
	tests := []struct {
		name     string
		callsign string
		wantFg   string // empty = no specific color check, just non-empty
	}{
		{
			name:     "empty-callsign-returns-drained",
			callsign: "",
			wantFg:   mhDrained,
		},
		{
			name:     "same-callsign-same-color-deterministic",
			callsign: "KC7XYZ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nickColor(tt.callsign)
			if got == "" {
				t.Errorf("nickColor(%q) returned empty string", tt.callsign)
			}
			if tt.wantFg != "" && got != tt.wantFg {
				t.Errorf("nickColor(%q) = %q, want %q", tt.callsign, got, tt.wantFg)
			}
			// Determinism: same callsign → same color
			if got2 := nickColor(tt.callsign); got2 != got {
				t.Errorf("nickColor(%q) not deterministic: %q vs %q", tt.callsign, got, got2)
			}
		})
	}
}

func TestNickColorInPalette(t *testing.T) {
	// Every non-empty callsign must resolve to a color in the palette
	for _, cs := range []string{"KC7XYZ", "W6ABC", "N0CALL", "retr0h", "node 0xdead"} {
		color := nickColor(cs)
		found := false
		for _, c := range nickColorPalette {
			if c == color {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("nickColor(%q) = %q not in nickColorPalette %v", cs, color, nickColorPalette)
		}
	}
}

func TestChatRowFor_BasicParts(t *testing.T) {
	tests := []struct {
		name    string
		msg     messageItem
		checkFn func(t *testing.T, parts chatRowParts)
	}{
		{
			name: "inbound-message-status-cell-is-space",
			msg: messageItem{
				Message: mdl.Message{From: "KC7XYZ", Time: "09:47", Status: mdl.StatusOK},
			},
			checkFn: func(t *testing.T, parts chatRowParts) {
				// Status should render a space for OK (no ack indicator)
				stripped := ansi.Strip(parts.status)
				if stripped != " " {
					t.Errorf("status for OK message = %q, want space", stripped)
				}
			},
		},
		{
			name: "ack-message-status-cell-is-checkmark",
			msg: messageItem{
				Message: mdl.Message{From: "KC7XYZ", Time: "09:47", Status: mdl.StatusAck},
			},
			checkFn: func(t *testing.T, parts chatRowParts) {
				stripped := ansi.Strip(parts.status)
				if stripped != "✓" {
					t.Errorf("status for Ack message = %q, want ✓", stripped)
				}
			},
		},
		{
			name: "fail-message-status-cell-is-x",
			msg: messageItem{
				Message: mdl.Message{From: "KC7XYZ", Time: "09:47", Status: mdl.StatusFail},
			},
			checkFn: func(t *testing.T, parts chatRowParts) {
				stripped := ansi.Strip(parts.status)
				if stripped != "✗" {
					t.Errorf("status for Fail message = %q, want ✗", stripped)
				}
			},
		},
		{
			name: "pending-message-status-cell-is-ellipsis",
			msg: messageItem{
				Message: mdl.Message{From: "KC7XYZ", Time: "09:47", Status: mdl.StatusPending},
			},
			checkFn: func(t *testing.T, parts chatRowParts) {
				stripped := ansi.Strip(parts.status)
				if stripped != "…" {
					t.Errorf("status for Pending message = %q, want …", stripped)
				}
			},
		},
		{
			name: "time-cell-contains-timestamp",
			msg:  messageItem{Message: mdl.Message{From: "KC7XYZ", Time: "09:47"}},
			checkFn: func(t *testing.T, parts chatRowParts) {
				stripped := ansi.Strip(parts.time)
				if !strings.Contains(stripped, "09:47") {
					t.Errorf("time cell %q does not contain timestamp '09:47'", stripped)
				}
			},
		},
		{
			name: "sender-cell-contains-callsign",
			msg:  messageItem{Message: mdl.Message{From: "KC7XYZ", FromNum: 0}},
			checkFn: func(t *testing.T, parts chatRowParts) {
				stripped := ansi.Strip(parts.sender)
				if !strings.Contains(stripped, "KC7XYZ") {
					t.Errorf("sender cell %q does not contain callsign 'KC7XYZ'", stripped)
				}
			},
		},
		{
			name: "hop-cell-shows-direct-for-inbound-zero-hops",
			msg:  messageItem{Message: mdl.Message{From: "peer", Hops: 0}},
			checkFn: func(t *testing.T, parts chatRowParts) {
				stripped := ansi.Strip(parts.hop)
				if !strings.Contains(stripped, "dx") {
					t.Errorf("hop cell for 0 hops = %q, want to contain 'dx'", stripped)
				}
			},
		},
		{
			name: "hop-cell-shows-count-for-multi-hop",
			msg:  messageItem{Message: mdl.Message{From: "peer", Hops: 3}},
			checkFn: func(t *testing.T, parts chatRowParts) {
				stripped := ansi.Strip(parts.hop)
				if !strings.Contains(stripped, "3") {
					t.Errorf("hop cell for 3 hops = %q, want to contain '3'", stripped)
				}
			},
		},
		{
			name: "mine-hop-cell-is-blank",
			msg:  messageItem{Message: mdl.Message{Mine: true, From: "me", Hops: 0}},
			checkFn: func(t *testing.T, parts chatRowParts) {
				stripped := strings.TrimSpace(ansi.Strip(parts.hop))
				if stripped != "" {
					t.Errorf("mine hop cell = %q, want blank", stripped)
				}
			},
		},
		{
			name: "snr-cell-contains-db-suffix-when-set",
			msg:  messageItem{Message: mdl.Message{From: "peer", SNR: "-8.5"}},
			checkFn: func(t *testing.T, parts chatRowParts) {
				stripped := ansi.Strip(parts.snr)
				if !strings.Contains(stripped, "dB") {
					t.Errorf("snr cell %q does not contain 'dB'", stripped)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			parts := chatRowFor(m, tt.msg, rowBgEven)
			tt.checkFn(t, parts)
		})
	}
}

func TestChatRowMainLine_Width(t *testing.T) {
	m := newTestModel()
	msg := messageItem{Message: mdl.Message{From: "KC7XYZ", Time: "09:47", Text: "hello world"}}
	parts := chatRowFor(m, msg, rowBgEven)
	styler := lipgloss.NewStyle()

	for _, w := range []int{80, 120, 40} {
		out := chatRowMainLine(parts, "hello world", styler, w)
		gotW := ansi.StringWidth(out)
		if gotW != w {
			t.Errorf("chatRowMainLine width %d: got %d cells\nstripped=%q",
				w, gotW, ansi.Strip(out))
		}
	}
}

func TestChatContinuationLine_Width(t *testing.T) {
	m := newTestModel()
	msg := messageItem{Message: mdl.Message{From: "KC7XYZ", Time: "09:47"}}
	parts := chatRowFor(m, msg, rowBgOdd)
	styler := lipgloss.NewStyle()

	for _, w := range []int{80, 120, 40} {
		out := chatContinuationLine(parts, "continuation text", styler, w)
		gotW := ansi.StringWidth(out)
		if gotW != w {
			t.Errorf("chatContinuationLine width %d: got %d cells\nstripped=%q",
				w, gotW, ansi.Strip(out))
		}
	}
}

func TestFormatAckers(t *testing.T) {
	tests := []struct {
		name   string
		ackers []mdl.Acker
		want   string
	}{
		{
			name:   "empty-ackers-returns-empty",
			ackers: nil,
			want:   "",
		},
		{
			name:   "empty-slice-returns-empty",
			ackers: []mdl.Acker{},
			want:   "",
		},
		{
			name: "single-acker-no-hops",
			ackers: []mdl.Acker{
				{Callsign: "KC7XYZ", Hops: 0},
			},
			want: "↳ 1 ack — KC7XYZ",
		},
		{
			name: "single-acker-with-hops",
			ackers: []mdl.Acker{
				{Callsign: "W6ABC", Hops: 2},
			},
			want: "↳ 1 ack — W6ABC (2h)",
		},
		{
			name: "multiple-ackers",
			ackers: []mdl.Acker{
				{Callsign: "KC7XYZ", Hops: 0},
				{Callsign: "W6ABC", Hops: 1},
			},
			want: "↳ 2 acks — KC7XYZ, W6ABC (1h)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatAckers(tt.ackers); got != tt.want {
				t.Errorf("formatAckers() = %q, want %q", got, tt.want)
			}
		})
	}
}

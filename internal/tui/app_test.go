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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/retr0h/meshx/internal/radio"
)

func TestReconnectFlashText(t *testing.T) {
	tests := []struct {
		name      string
		reconnect *radio.ReconnectState
		want      string
	}{
		{
			name:      "nil-reconnect-returns-empty",
			reconnect: nil,
			want:      "",
		},
		{
			name: "initial-connection-returns-connecting",
			reconnect: &radio.ReconnectState{
				Initial: true,
				ReadyAt: time.Now().Add(5 * time.Second),
			},
			want: "connecting…",
		},
		{
			name: "retry-attempt-without-error",
			reconnect: &radio.ReconnectState{
				Attempt: 3,
				ReadyAt: time.Now().Add(10 * time.Second),
			},
			want: "reconnecting · attempt 3",
		},
		{
			name: "retry-attempt-with-error",
			reconnect: &radio.ReconnectState{
				Attempt: 2,
				Err:     errors.New("connection refused"),
				ReadyAt: time.Now().Add(5 * time.Second),
			},
			want: "connection refused",
		},
		{
			name: "dialing-now-when-ready-at-in-past",
			reconnect: &radio.ReconnectState{
				Attempt: 1,
				ReadyAt: time.Now().Add(-1 * time.Second),
			},
			want: "dialing now",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Reconnect = tt.reconnect
			got := m.reconnectFlashText()
			if !strings.Contains(got, tt.want) {
				t.Errorf("reconnectFlashText() = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestTruncateForFlash(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{
			name: "short-string-unchanged",
			s:    "hello",
			n:    10,
			want: "hello",
		},
		{
			name: "exact-length-unchanged",
			s:    "hello",
			n:    5,
			want: "hello",
		},
		{
			name: "long-string-truncated-with-ellipsis",
			s:    "this is a very long error message",
			n:    10,
			want: "this is a…",
		},
		{
			name: "empty-string",
			s:    "",
			n:    5,
			want: "",
		},
		{
			name: "unicode-truncated-at-rune-boundary",
			s:    "héllo world",
			n:    5,
			want: "héll…",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateForFlash(tt.s, tt.n); got != tt.want {
				t.Errorf("truncateForFlash(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

func TestClearReconnectBanner(t *testing.T) {
	tests := []struct {
		name          string
		reconnect     *radio.ReconnectState
		flash         string
		wantReconnect bool
		wantFlash     string
	}{
		{
			name:          "nil-reconnect-is-idempotent",
			reconnect:     nil,
			flash:         "some flash",
			wantReconnect: false,
			wantFlash:     "some flash", // flash unchanged when Reconnect is already nil
		},
		{
			name: "clears-reconnect-and-flash",
			reconnect: &radio.ReconnectState{
				Attempt: 5,
				ReadyAt: time.Now().Add(10 * time.Second),
			},
			flash:         "reconnecting · attempt 5 · next try in 10s",
			wantReconnect: false,
			wantFlash:     "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Reconnect = tt.reconnect
			m.flash = tt.flash
			m.clearReconnectBanner()
			if tt.wantReconnect && m.Reconnect == nil {
				t.Error("clearReconnectBanner() cleared Reconnect but expected it to remain")
			}
			if !tt.wantReconnect && m.Reconnect != nil {
				t.Error("clearReconnectBanner() did not clear Reconnect")
			}
			if m.flash != tt.wantFlash {
				t.Errorf("clearReconnectBanner() flash = %q, want %q", m.flash, tt.wantFlash)
			}
		})
	}
}

func TestShortFirmware(t *testing.T) {
	tests := []struct {
		name string
		fw   string
		want string
	}{
		{name: "empty-returns-dash", fw: "", want: "—"},
		{name: "plain-semver-unchanged", fw: "2.7.15", want: "2.7.15"},
		{name: "four-part-trimmed-to-three", fw: "2.7.15.567b8ea", want: "2.7.15"},
		{name: "two-part-unchanged", fw: "2.7", want: "2.7"},
		{name: "one-part-unchanged", fw: "2", want: "2"},
		{name: "five-part-trimmed-to-three", fw: "2.7.15.567.extra", want: "2.7.15"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortFirmware(tt.fw); got != tt.want {
				t.Errorf("shortFirmware(%q) = %q, want %q", tt.fw, got, tt.want)
			}
		})
	}
}

func TestMod(t *testing.T) {
	tests := []struct {
		name string
		x, y float64
		want float64
	}{
		{name: "positive-remainder", x: 7.5, y: 3.0, want: 1.5},
		{name: "exact-multiple", x: 6.0, y: 3.0, want: 0.0},
		{name: "negative-x-positive-result", x: -1.5, y: 3.0, want: 1.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mod(tt.x, tt.y)
			// Float comparison with tolerance.
			const eps = 1e-9
			if got < tt.want-eps || got > tt.want+eps {
				t.Errorf("mod(%v, %v) = %v, want %v", tt.x, tt.y, got, tt.want)
			}
			// Result must always be >= 0.
			if got < 0 {
				t.Errorf("mod(%v, %v) = %v, want non-negative result", tt.x, tt.y, got)
			}
		})
	}
}

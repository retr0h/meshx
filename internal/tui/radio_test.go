// Copyright (c) 2026 John Dewey
//
// Boundary tests for sanitizeMessageText — the inbound text scrub
// that hardens the renderer against bad UTF-8, control bytes, edge
// whitespace, and Meshtastic's "alert bell" (BEL / 0x07) signal.
//
// BEL is special: the Meshtastic firmware's external_notification
// module fires the radio's buzzer when an incoming message contains
// 0x07, and the official apps render those rows with a 🔔 badge.
// Lumping BEL in with "corrupted bytes we had to drop" loses both
// signals (silent strip + spurious (?) marker), so the sanitizer
// reports BEL through a separate `alert` return that the caller
// stores on the message and the renderer surfaces explicitly.

package tui

import (
	"strings"
	"testing"
)

func TestSanitizeMessageText(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		want      string
		corrupted bool
		alert     bool
	}{
		{
			name: "plain ascii passes through clean",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "edge whitespace trimmed without corruption",
			in:   "  hi\n",
			want: "hi",
		},
		{
			name: "CRLF normalized to LF without corruption",
			in:   "line1\r\nline2",
			want: "line1\nline2",
		},
		{
			name:  "lone BEL flags alert without corruption",
			in:    "\x07",
			want:  "",
			alert: true,
		},
		{
			name:  "BEL embedded in body strips and flags alert",
			in:    "Alert!\x07",
			want:  "Alert!",
			alert: true,
		},
		{
			name:  "BEL at message start strips and flags alert",
			in:    "\x07wake up",
			want:  "wake up",
			alert: true,
		},
		{
			name:  "multiple BELs collapse and flag alert once",
			in:    "\x07ping\x07pong\x07",
			want:  "pingpong",
			alert: true,
		},
		{
			name:      "NUL byte flags corruption not alert",
			in:        "hello\x00world",
			want:      "helloworld",
			corrupted: true,
		},
		{
			name:      "ESC byte flags corruption not alert",
			in:        "trick\x1b[31m",
			want:      "trick[31m",
			corrupted: true,
		},
		{
			name:      "BEL plus NUL flags both alert and corruption",
			in:        "\x07boom\x00",
			want:      "boom",
			alert:     true,
			corrupted: true,
		},
		{
			name:      "invalid UTF-8 substitutes ? and flags corruption",
			in:        "bad\xffbyte",
			want:      "bad?byte",
			corrupted: true,
		},
		{
			name: "emoji ZWJ sequence preserved",
			in:   "🙋🏼‍♂️",
			want: "🙋🏼‍♂️",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, corrupted, alert := sanitizeMessageText(tc.in)
			if got != tc.want {
				t.Errorf("text: got %q, want %q", got, tc.want)
			}
			if corrupted != tc.corrupted {
				t.Errorf("corrupted: got %v, want %v", corrupted, tc.corrupted)
			}
			if alert != tc.alert {
				t.Errorf("alert: got %v, want %v", alert, tc.alert)
			}
			// BEL must never survive into the rendered text — terminals
			// would re-interpret it and double-ring on every redraw.
			if strings.ContainsRune(got, '\x07') {
				t.Errorf("BEL leaked into output: %q", got)
			}
		})
	}
}

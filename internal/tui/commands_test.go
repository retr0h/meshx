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
	"encoding/hex"
	"strings"
	"testing"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

func TestCurrentChannelIndex(t *testing.T) {
	tests := []struct {
		name           string
		currentChannel string
		channels       []mdl.ChannelItem
		want           uint32
	}{
		{
			name:           "no-channels-defaults-to-zero",
			currentChannel: "LongFast",
			channels:       nil,
			want:           0,
		},
		{
			name:           "first-channel-returns-zero",
			currentChannel: "LongFast",
			channels: []mdl.ChannelItem{
				{Name: "LongFast"},
				{Name: "Admin"},
			},
			want: 0,
		},
		{
			name:           "second-channel-returns-one",
			currentChannel: "Admin",
			channels: []mdl.ChannelItem{
				{Name: "LongFast"},
				{Name: "Admin"},
			},
			want: 1,
		},
		{
			name:           "third-channel-returns-two",
			currentChannel: "Private",
			channels: []mdl.ChannelItem{
				{Name: "LongFast"},
				{Name: "Admin"},
				{Name: "Private"},
			},
			want: 2,
		},
		{
			name:           "no-match-defaults-to-zero",
			currentChannel: "NonExistent",
			channels: []mdl.ChannelItem{
				{Name: "LongFast"},
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.CurrentChannel = tt.currentChannel
			m.Channels = tt.channels
			if got := m.currentChannelIndex(); got != tt.want {
				t.Errorf("currentChannelIndex() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReplyTargetFor(t *testing.T) {
	tests := []struct {
		name     string
		call     string
		messages []messageItem
		want     uint32
	}{
		{
			name:     "empty-call-returns-zero",
			call:     "",
			messages: nil,
			want:     0,
		},
		{
			name: "finds-most-recent-from-callsign",
			call: "KC7ABC",
			messages: []messageItem{
				{Message: mdl.Message{From: "KC7ABC", PacketID: 0x1111}},
				{Message: mdl.Message{From: "KC7ABC", PacketID: 0x2222}},
			},
			want: 0x2222,
		},
		{
			name: "case-insensitive-match",
			call: "kc7abc",
			messages: []messageItem{
				{Message: mdl.Message{From: "KC7ABC", PacketID: 0x1111}},
			},
			want: 0x1111,
		},
		{
			name: "skips-mine-messages",
			call: "KC7ABC",
			messages: []messageItem{
				{Message: mdl.Message{From: "KC7ABC", PacketID: 0x1111, Mine: true}},
			},
			want: 0,
		},
		{
			name: "skips-system-messages",
			call: "KC7ABC",
			messages: []messageItem{
				{Message: mdl.Message{From: "KC7ABC", PacketID: 0x1111, Status: mdl.StatusSystem}},
			},
			want: 0,
		},
		{
			name: "skips-zero-packet-id",
			call: "KC7ABC",
			messages: []messageItem{
				{Message: mdl.Message{From: "KC7ABC", PacketID: 0}},
			},
			want: 0,
		},
		{
			name: "no-match-returns-zero",
			call: "W6XYZ",
			messages: []messageItem{
				{Message: mdl.Message{From: "KC7ABC", PacketID: 0x1111}},
			},
			want: 0,
		},
		{
			name: "partial-name-match",
			call: "KC7",
			messages: []messageItem{
				{Message: mdl.Message{From: "KC7ABC", PacketID: 0x3333}},
			},
			want: 0x3333,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.Messages = tt.messages
			if got := m.replyTargetFor(tt.call); got != tt.want {
				t.Errorf("replyTargetFor(%q) = 0x%x, want 0x%x", tt.call, got, tt.want)
			}
		})
	}
}

func TestPskFingerprint(t *testing.T) {
	tests := []struct {
		name    string
		psk     []byte
		wantLen int
	}{
		{
			name:    "32-byte-psk-produces-8-hex-chars",
			psk:     make([]byte, 32),
			wantLen: 8,
		},
		{
			name:    "empty-psk-produces-8-hex-chars",
			psk:     []byte{},
			wantLen: 8,
		},
		{
			name:    "short-psk-produces-8-hex-chars",
			psk:     []byte{0xDE, 0xAD, 0xBE, 0xEF},
			wantLen: 8,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pskFingerprint(tt.psk)
			if len(got) != tt.wantLen {
				t.Errorf("pskFingerprint() len = %d, want %d (got %q)", len(got), tt.wantLen, got)
			}
			// Verify it is valid hex.
			if _, err := hex.DecodeString(got); err != nil {
				t.Errorf("pskFingerprint() = %q, not valid hex: %v", got, err)
			}
		})
	}
}

func TestPskFingerprintDeterministic(t *testing.T) {
	// Same PSK must always produce the same fingerprint.
	psk := []byte{0x01, 0x02, 0x03, 0x04}
	a := pskFingerprint(psk)
	b := pskFingerprint(psk)
	if a != b {
		t.Errorf("pskFingerprint not deterministic: %q != %q", a, b)
	}
}

func TestPskFingerprintDistinct(t *testing.T) {
	// Different PSKs must produce different fingerprints.
	a := pskFingerprint([]byte{0x01})
	b := pskFingerprint([]byte{0x02})
	if a == b {
		t.Errorf("pskFingerprint produced identical output for distinct inputs: %q", a)
	}
}

func TestPlural(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want string
	}{
		{name: "zero-is-plural", n: 0, want: "s"},
		{name: "one-is-singular", n: 1, want: ""},
		{name: "two-is-plural", n: 2, want: "s"},
		{name: "negative-is-plural", n: -1, want: "s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := plural(tt.n); got != tt.want {
				t.Errorf("plural(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

func TestAckWord(t *testing.T) {
	tests := []struct {
		name   string
		status mdl.MessageStatus
		want   string
	}{
		{name: "ack-returns-ok", status: mdl.StatusAck, want: "ok"},
		{name: "fail-returns-timeout", status: mdl.StatusFail, want: "timeout"},
		{name: "pending-returns-pending", status: mdl.StatusPending, want: "pending"},
		{name: "unknown-returns-pending", status: mdl.MessageStatus("???"), want: "pending"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ackWord(tt.status); got != tt.want {
				t.Errorf("ackWord(%v) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestBuildVersionLines(t *testing.T) {
	m := newTestModel()
	lines := buildVersionLines(&m)

	if len(lines) == 0 {
		t.Fatal("buildVersionLines() returned empty slice")
	}
	// The first line must mention "meshx:".
	if !strings.HasPrefix(lines[0], "meshx:") {
		t.Errorf("buildVersionLines()[0] = %q, want prefix 'meshx:'", lines[0])
	}
	// Without a connected radio, firmware line should say "waiting".
	found := false
	for _, l := range lines {
		if strings.Contains(l, "Firmware:") {
			found = true
			if !strings.Contains(l, "waiting") {
				t.Errorf("expected firmware line to contain 'waiting' when no radio, got %q", l)
			}
		}
	}
	if !found {
		t.Error("buildVersionLines() missing 'Firmware:' line")
	}
}

func TestBuildVersionLinesWithFirmware(t *testing.T) {
	m := newTestModel()
	m.RadioFirmware = "2.7.15"
	lines := buildVersionLines(&m)

	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "Firmware:") {
			found = true
			if !strings.Contains(l, "2.7.15") {
				t.Errorf("expected firmware line to contain '2.7.15', got %q", l)
			}
		}
	}
	if !found {
		t.Error("buildVersionLines() missing 'Firmware:' line")
	}
}

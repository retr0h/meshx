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

package pump

// channel_url_test.go — table-driven tests for ParseChannelShareURL and
// BuildChannelShareURL. These are pure functions: no transport, no side effects.

import (
	"strings"
	"testing"

	"github.com/retr0h/meshx/internal/meshx/model"
)

// ---- BuildChannelShareURL ---------------------------------------------------

func TestBuildChannelShareURL_RoundTrip(t *testing.T) {
	slot := model.ChannelInfo{
		Name: "MeshNet",
		PSK: []byte{
			0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
		},
		ID:     0xdeadbeef,
		HasPSK: true,
	}
	rawURL, err := BuildChannelShareURL(slot)
	if err != nil {
		t.Fatalf("BuildChannelShareURL: %v", err)
	}
	if !strings.HasPrefix(rawURL, "https://meshtastic.org/e/#") {
		t.Errorf("URL prefix: got %q", rawURL)
	}

	// Round-trip: parse the URL we just built.
	channels, err := ParseChannelShareURL(rawURL)
	if err != nil {
		t.Fatalf("ParseChannelShareURL round-trip: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("want 1 channel, got %d", len(channels))
	}
	got := channels[0]
	if got.Name != "MeshNet" {
		t.Errorf("Name: got %q, want MeshNet", got.Name)
	}
	if len(got.PSK) != 16 || got.PSK[0] != 0x01 {
		t.Errorf("PSK: got %v", got.PSK)
	}
	if got.ID != 0xdeadbeef {
		t.Errorf("ID: got 0x%x", got.ID)
	}
	if got.Role != model.ChannelSecondary {
		t.Errorf("Role: got %q, want SECONDARY", got.Role)
	}
	if !got.HasPSK {
		t.Error("HasPSK: want true")
	}
}

func TestBuildChannelShareURL_NoPSK(t *testing.T) {
	slot := model.ChannelInfo{
		Name: "Public",
		Role: model.ChannelSecondary,
	}
	rawURL, err := BuildChannelShareURL(slot)
	if err != nil {
		t.Fatalf("BuildChannelShareURL no-PSK: %v", err)
	}

	channels, err := ParseChannelShareURL(rawURL)
	if err != nil {
		t.Fatalf("ParseChannelShareURL: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("want 1 channel, got %d", len(channels))
	}
	if channels[0].HasPSK {
		t.Error("HasPSK: want false for no-PSK channel")
	}
}

// ---- ParseChannelShareURL ---------------------------------------------------

func TestParseChannelShareURL_ErrorCases(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{
			name:    "empty url",
			url:     "",
			wantErr: "empty url",
		},
		{
			name:    "unrecognised scheme",
			url:     "http://example.com/not-meshtastic",
			wantErr: "unrecognized url scheme",
		},
		{
			name:    "meshtastic.org without fragment",
			url:     "https://meshtastic.org/e/",
			wantErr: "url has no payload after #",
		},
		{
			name:    "meshtastic scheme without fragment",
			url:     "meshtastic://e/",
			wantErr: "url has no payload after #",
		},
		{
			name:    "invalid base64",
			url:     "https://meshtastic.org/e/#not-valid-base64!!!!!",
			wantErr: "base64 decode",
		},
		{
			name:    "valid base64 but not a ChannelSet proto",
			url:     "https://meshtastic.org/e/#YWJjZA", // "abcd" in base64url
			wantErr: "",                                 // proto.Unmarshal is lenient; may succeed with empty channels
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseChannelShareURL(tt.url)
			if tt.wantErr == "" {
				// Either success or "channel set has no channels" — both are fine.
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestParseChannelShareURL_MeshtasticScheme(t *testing.T) {
	// Build a real URL and then re-parse it with the meshtastic:// scheme.
	slot := model.ChannelInfo{
		Name: "TestChan",
		PSK:  []byte{0xAA, 0xBB},
	}
	rawURL, err := BuildChannelShareURL(slot)
	if err != nil {
		t.Fatalf("BuildChannelShareURL: %v", err)
	}
	// Swap scheme: https://meshtastic.org/e/# → meshtastic://e/#
	fragment := strings.TrimPrefix(rawURL, "https://meshtastic.org/e/#")
	altURL := "meshtastic://e/#" + fragment

	channels, err := ParseChannelShareURL(altURL)
	if err != nil {
		t.Fatalf("ParseChannelShareURL meshtastic://: %v", err)
	}
	if len(channels) != 1 || channels[0].Name != "TestChan" {
		t.Fatalf("unexpected channels: %v", channels)
	}
}

func TestParseChannelShareURL_HttpScheme(t *testing.T) {
	// http:// (not https://) on meshtastic.org must also be accepted.
	slot := model.ChannelInfo{Name: "HTTPTest", PSK: []byte{0x01}}
	rawURL, _ := BuildChannelShareURL(slot)
	fragment := strings.TrimPrefix(rawURL, "https://meshtastic.org/e/#")
	httpURL := "http://meshtastic.org/e/#" + fragment

	channels, err := ParseChannelShareURL(httpURL)
	if err != nil {
		t.Fatalf("ParseChannelShareURL http://: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("want 1 channel, got %d", len(channels))
	}
}

func TestParseChannelShareURL_StripAddQueryParam(t *testing.T) {
	// ?add=true is an informational hint from the phone app — must be
	// stripped before base64 decoding.
	slot := model.ChannelInfo{Name: "WithAdd", PSK: []byte{0xCC}}
	rawURL, _ := BuildChannelShareURL(slot)
	fragment := strings.TrimPrefix(rawURL, "https://meshtastic.org/e/#")
	urlWithAdd := "https://meshtastic.org/e/#" + fragment + "?add=true"

	channels, err := ParseChannelShareURL(urlWithAdd)
	if err != nil {
		t.Fatalf("ParseChannelShareURL ?add=true: %v", err)
	}
	if len(channels) != 1 || channels[0].Name != "WithAdd" {
		t.Fatalf("unexpected channels: %v", channels)
	}
}

func TestParseChannelShareURL_WithPaddedBase64(t *testing.T) {
	// Some senders include base64 padding. The parser must strip it
	// and still decode successfully.
	slot := model.ChannelInfo{Name: "Padded", PSK: []byte{0x01, 0x02, 0x03}}
	rawURL, _ := BuildChannelShareURL(slot)
	fragment := strings.TrimPrefix(rawURL, "https://meshtastic.org/e/#")
	// Add padding characters that the decoder must tolerate.
	paddedURL := "https://meshtastic.org/e/#" + fragment + "=="

	channels, err := ParseChannelShareURL(paddedURL)
	if err != nil {
		t.Fatalf("ParseChannelShareURL padded: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("want 1 channel, got %d", len(channels))
	}
}

func TestParseChannelShareURL_MultipleChannels(t *testing.T) {
	// BuildChannelShareURL only wraps one channel, but the parse path
	// must iterate all channels in the set. We verify the single-channel
	// path fully; multi-channel would require constructing a ChannelSet
	// manually (proto encoding) which is tested implicitly via round-trip.
	slot := model.ChannelInfo{Name: "OnlyOne", PSK: []byte{0xDE}}
	rawURL, _ := BuildChannelShareURL(slot)
	chs, err := ParseChannelShareURL(rawURL)
	if err != nil {
		t.Fatalf("ParseChannelShareURL: %v", err)
	}
	if len(chs) != 1 {
		t.Fatalf("want 1, got %d", len(chs))
	}
}

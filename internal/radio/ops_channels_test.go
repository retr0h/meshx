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

package radio

import (
	"errors"
	"strings"
	"testing"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/pump"
)

// channelFakePump satisfies Pump for ops_channels tests. When rejected
// is true, Send returns (0, false) to simulate buffer-full / no radio.
type channelFakePump struct {
	rejected bool
}

func (p *channelFakePump) Send(mdl.Command) (uint32, bool) {
	if p.rejected {
		return 0, false
	}
	return 1, true
}

func (p *channelFakePump) Stop() {}

// newChannelSession returns a Session with two pre-loaded channels
// (slot 0 PRIMARY, slot 1 SECONDARY) and an accepting pump.
func newChannelSession(pump Pump) *Session {
	s := New(nil, pump, nil)
	s.State.Channels = []mdl.ChannelItem{
		{Index: 0, Name: "#default", Role: "PRIMARY"},
		{Index: 1, Name: "#ham", Role: "SECONDARY"},
	}
	s.State.CurrentChannel = "#default"
	return s
}

// TestSession_MintChannel covers validation, slot allocation, optimistic
// state update, and error paths.
func TestSession_MintChannel(t *testing.T) {
	t.Parallel()

	t.Run("empty-name-rejected", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		_, err := s.MintChannel(MintChannelRequest{Name: "  "})
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Code != 400 {
			t.Fatalf("want 400 OpError, got %v", err)
		}
	})

	t.Run("name-too-long-rejected", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		_, err := s.MintChannel(MintChannelRequest{Name: "averylongname12"}) // > 11 bytes
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Code != 400 {
			t.Fatalf("want 400 OpError, got %v", err)
		}
	})

	t.Run("duplicate-name-rejected", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		_, err := s.MintChannel(MintChannelRequest{Name: "ham"})
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Code != 409 {
			t.Fatalf("want 409 conflict, got %v", err)
		}
	})

	t.Run("no-pump-unavailable", func(t *testing.T) {
		s := newChannelSession(nil)
		_, err := s.MintChannel(MintChannelRequest{Name: "newchan"})
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Code != 503 {
			t.Fatalf("want 503 unavailable, got %v", err)
		}
	})

	t.Run("pump-rejected-unavailable", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{rejected: true})
		_, err := s.MintChannel(MintChannelRequest{Name: "newchan"})
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Code != 503 {
			t.Fatalf("want 503 unavailable, got %v", err)
		}
	})

	t.Run("successful-mint-allocates-slot", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		res, err := s.MintChannel(MintChannelRequest{Name: "newchan"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Index < 1 || res.Index > 7 {
			t.Fatalf("Index = %d, want 1..7", res.Index)
		}
		if res.Name != "newchan" {
			t.Fatalf("Name = %q, want newchan", res.Name)
		}
		if len(res.PSK) != 32 {
			t.Fatalf("PSK len = %d, want 32", len(res.PSK))
		}
		if !strings.HasPrefix(res.ShareURL, "https://meshtastic.org/e/#") {
			t.Fatalf("ShareURL = %q, missing meshtastic prefix", res.ShareURL)
		}
		// Optimistic state update must reserve the slot.
		if res.Index >= len(s.State.Channels) {
			t.Fatal("State.Channels not grown to include minted slot")
		}
		if s.State.Channels[res.Index].Name != "newchan" {
			t.Fatalf(
				"State.Channels[%d].Name = %q, want newchan",
				res.Index,
				s.State.Channels[res.Index].Name,
			)
		}
	})

	t.Run("hash-prefix-trimmed-from-name", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		res, err := s.MintChannel(MintChannelRequest{Name: "#metro"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Name != "metro" {
			t.Fatalf("Name = %q, want metro (# trimmed)", res.Name)
		}
	})

	t.Run("full-slot-table-rejected", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		// Fill all 7 secondary slots.
		for i := 1; i < 8; i++ {
			s.State.Channels = append(s.State.Channels, mdl.ChannelItem{
				Index: i, Name: "ch" + string(rune('a'+i-1)), Role: "SECONDARY",
			})
		}
		_, err := s.MintChannel(MintChannelRequest{Name: "overflow"})
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Code != 409 {
			t.Fatalf("want 409 when all slots full, got %v", err)
		}
	})
}

// TestSession_DeleteChannel covers validation (slot 0 refused), no-pump
// unavailability, successful delete, and out-of-range errors.
func TestSession_DeleteChannel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		idx      int
		pump     Pump
		wantCode int // 0 = expect success
		wantName string
	}{
		{
			name:     "slot-0-refused",
			idx:      0,
			pump:     &channelFakePump{},
			wantCode: 400,
		},
		{
			name:     "negative-index-refused",
			idx:      -1,
			pump:     &channelFakePump{},
			wantCode: 400,
		},
		{
			name:     "index-8-refused",
			idx:      8,
			pump:     &channelFakePump{},
			wantCode: 400,
		},
		{
			name:     "no-pump-unavailable",
			idx:      1,
			pump:     nil,
			wantCode: 503,
		},
		{
			name:     "pump-rejected-unavailable",
			idx:      1,
			pump:     &channelFakePump{rejected: true},
			wantCode: 503,
		},
		{
			name:     "successful-delete",
			idx:      1,
			pump:     &channelFakePump{},
			wantCode: 0,
			wantName: "#ham",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newChannelSession(tc.pump)
			res, err := s.DeleteChannel(DeleteChannelRequest{Index: tc.idx})
			if tc.wantCode != 0 {
				var opErr *OpError
				if !errors.As(err, &opErr) || opErr.Code != tc.wantCode {
					t.Fatalf("want %d OpError, got %v", tc.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Name != tc.wantName {
				t.Fatalf("Name = %q, want %q", res.Name, tc.wantName)
			}
			// Slot must now be DISABLED in local state.
			if s.State.Channels[tc.idx].Role != string(mdl.ChannelDisabled) {
				t.Fatalf(
					"Channels[%d].Role = %q, want DISABLED",
					tc.idx,
					s.State.Channels[tc.idx].Role,
				)
			}
		})
	}
}

// TestSession_ShareChannel covers the 404 paths and successful URL
// construction for an unkeyed and a PSK-bearing channel.
func TestSession_ShareChannel(t *testing.T) {
	t.Parallel()

	t.Run("negative-index-not-found", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		_, err := s.ShareChannel(ShareChannelRequest{Index: -1})
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Code != 404 {
			t.Fatalf("want 404, got %v", err)
		}
	})

	t.Run("beyond-table-not-found", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		_, err := s.ShareChannel(ShareChannelRequest{Index: 99})
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Code != 404 {
			t.Fatalf("want 404, got %v", err)
		}
	})

	t.Run("disabled-slot-not-found", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		s.State.Channels = append(s.State.Channels, mdl.ChannelItem{
			Index: 2, Role: string(mdl.ChannelDisabled),
		})
		_, err := s.ShareChannel(ShareChannelRequest{Index: 2})
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Code != 404 {
			t.Fatalf("want 404 for disabled slot, got %v", err)
		}
	})

	t.Run("primary-channel-share-url", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		res, err := s.ShareChannel(ShareChannelRequest{Index: 0})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(res.ShareURL, "https://meshtastic.org/e/#") {
			t.Fatalf("ShareURL = %q, missing meshtastic prefix", res.ShareURL)
		}
		if res.Index != 0 {
			t.Fatalf("Index = %d, want 0", res.Index)
		}
	})
}

// buildTestShareURL uses pump.BuildChannelShareURL to construct a
// meshtastic:// URL for the given channel info — gives ImportChannel
// tests a valid URL without hardcoding base64 payload bytes.
func buildTestShareURL(t *testing.T, info mdl.ChannelInfo) string {
	t.Helper()
	u, err := pump.BuildChannelShareURL(info)
	if err != nil {
		t.Fatalf("buildTestShareURL: %v", err)
	}
	return u
}

// TestSession_ImportChannel covers empty-URL validation, bad-URL
// parse rejection, empty-name skips, duplicate skips, pump-full
// skips, and a successful single-channel import.
func TestSession_ImportChannel(t *testing.T) {
	t.Parallel()

	t.Run("empty-url-rejected", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		_, err := s.ImportChannel(ImportChannelRequest{URL: "  "})
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Code != 400 {
			t.Fatalf("want 400, got %v", err)
		}
	})

	t.Run("malformed-url-rejected", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		_, err := s.ImportChannel(ImportChannelRequest{URL: "not-a-valid-share-url"})
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Code != 400 {
			t.Fatalf("want 400, got %v", err)
		}
	})

	t.Run("successful-import-reserves-slot", func(t *testing.T) {
		// Build a valid share URL for a fresh channel not in the session.
		url := buildTestShareURL(t, mdl.ChannelInfo{
			Index:  0,
			Name:   "metro",
			Role:   mdl.ChannelSecondary,
			ID:     12345,
			HasPSK: false,
		})

		s := newChannelSession(&channelFakePump{})
		res, err := s.ImportChannel(ImportChannelRequest{URL: url})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.Imported) != 1 {
			t.Fatalf("Imported len = %d, want 1", len(res.Imported))
		}
		if res.Imported[0].Name != "metro" {
			t.Fatalf("Imported[0].Name = %q, want metro", res.Imported[0].Name)
		}
		// The slot must now appear in State.Channels.
		idx := res.Imported[0].Index
		if idx >= len(s.State.Channels) {
			t.Fatalf("State.Channels not grown for imported slot %d", idx)
		}
		if s.State.Channels[idx].Name != "metro" {
			t.Fatalf("State.Channels[%d].Name = %q, want metro", idx, s.State.Channels[idx].Name)
		}
	})

	t.Run("duplicate-channel-skipped", func(t *testing.T) {
		// "ham" already exists in the session from newChannelSession.
		url := buildTestShareURL(t, mdl.ChannelInfo{
			Index: 0,
			Name:  "ham",
			Role:  mdl.ChannelSecondary,
			ID:    99,
		})

		s := newChannelSession(&channelFakePump{})
		res, err := s.ImportChannel(ImportChannelRequest{URL: url})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.Imported) != 0 {
			t.Fatalf("Imported = %v, want empty (duplicate)", res.Imported)
		}
		if len(res.Skipped) != 1 {
			t.Fatalf("Skipped len = %d, want 1", len(res.Skipped))
		}
	})

	t.Run("pump-rejected-slot-skipped", func(t *testing.T) {
		url := buildTestShareURL(t, mdl.ChannelInfo{
			Index: 0,
			Name:  "unique",
			Role:  mdl.ChannelSecondary,
			ID:    77,
		})

		s := newChannelSession(&channelFakePump{rejected: true})
		res, err := s.ImportChannel(ImportChannelRequest{URL: url})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.Imported) != 0 {
			t.Fatalf("Imported = %v, want empty when pump rejected", res.Imported)
		}
		if len(res.Skipped) != 1 {
			t.Fatalf("Skipped len = %d, want 1", len(res.Skipped))
		}
	})
}

// TestSession_LookupChannelByName verifies name resolution including
// the bare-name, "#name", and "*name*" decorated forms.
func TestSession_LookupChannelByName(t *testing.T) {
	t.Parallel()

	s := newTestSession()
	s.State.Channels = []mdl.ChannelItem{
		{Index: 0, Name: "#default", Role: "PRIMARY"},
		{Index: 1, Name: "#ham", Role: "SECONDARY"},
		{Index: 2, Name: "*secret*", Role: "SECONDARY"},
		{Index: 3, Role: "DISABLED"},
	}

	cases := []struct {
		typed   string
		wantIdx int
	}{
		{"default", 0},
		{"#default", 0},
		{"ham", 1},
		{"#ham", 1},
		{"secret", 2},
		{"*secret*", 2},
		{"missing", -1},
		// DISABLED slot must not match.
		{"", -1},
	}

	for _, tc := range cases {
		got := s.LookupChannelByName(tc.typed)
		if got != tc.wantIdx {
			t.Errorf("LookupChannelByName(%q) = %d, want %d", tc.typed, got, tc.wantIdx)
		}
	}

	t.Run("nil-state-returns-minus-one", func(t *testing.T) {
		s2 := &Session{} // no State
		if got := s2.LookupChannelByName("anything"); got != -1 {
			t.Fatalf("want -1, got %d", got)
		}
	})
}

// TestSession_Sync exercises the single dispatch-or-unavailable paths.
func TestSession_Sync(t *testing.T) {
	t.Parallel()

	t.Run("no-pump-unavailable", func(t *testing.T) {
		s := newTestSession()
		_, err := s.Sync()
		var opErr *OpError
		if !errors.As(err, &opErr) || opErr.Code != 503 {
			t.Fatalf("want 503, got %v", err)
		}
	})

	t.Run("dispatches-and-returns-ok", func(t *testing.T) {
		s := newChannelSession(&channelFakePump{})
		res, err := s.Sync()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.OK {
			t.Fatal("SyncResult.OK = false, want true")
		}
	})
}

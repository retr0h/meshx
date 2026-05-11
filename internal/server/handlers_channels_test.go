// Copyright (c) 2026 John Dewey

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/pump"
	"github.com/retr0h/meshx/internal/radio"
)

// channelHarness wires a Server with one Session that has a fakePump
// AND seeded channels — so list/share endpoints have something to
// project, while mint/import/delete endpoints have a pump to dispatch
// against. State.Channels[0] is the unnamed PRIMARY; secondary slots
// 1..2 carry distinct names so tests can assert on placement.
func channelHarness(t *testing.T) (*httptest.Server, *fakePump) {
	t.Helper()
	s := New(Config{Radios: NewRegistry()})
	pump := newFakePump()
	sess := radio.New(nil, pump, nil)
	sess.State.RadioID = "0xabcdef01"
	sess.State.MyNodeNum = 0xdeadbeef
	sess.State.Channels = []mdl.ChannelItem{
		{
			Index:  0,
			Name:   "",
			Role:   string(mdl.ChannelPrimary),
			HasPSK: true,
			PSK:    []byte("default-psk-bytes-32-byte-fixture"),
		},
		{
			Index:  1,
			Name:   "ham",
			Role:   string(mdl.ChannelSecondary),
			HasPSK: true,
			PSK:    []byte("ham-psk-bytes-32-byte-fixture-aa"),
		},
		{
			Index:  2,
			Name:   "field",
			Role:   string(mdl.ChannelSecondary),
			HasPSK: true,
			PSK:    []byte("field-psk-bytes-32-byte-fixture-"),
		},
	}
	s.radios.Add(sess.State.RadioID, sess)
	srv := httptest.NewServer(s.http.Handler)
	t.Cleanup(srv.Close)
	return srv, pump
}

// TestEndpointListChannels — GET /radios/{id}/channels. Projects the
// ChannelItem wire shape with PSK redacted (json:"-") and the HasPSK
// flag computed at projection time.
func TestEndpointListChannels(t *testing.T) {
	t.Parallel()

	srv, _ := channelHarness(t)

	cases := []struct {
		name       string
		radioID    string
		wantStatus int
		wantNames  []string
	}{
		{
			name:       "returns-channel-table-with-roles",
			radioID:    "0xabcdef01",
			wantStatus: http.StatusOK,
			wantNames:  []string{"", "ham", "field"},
		},
		{
			name:       "unknown-radio-returns-404",
			radioID:    "nope-no-such-radio",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodGet,
				srv.URL+"/radios/"+tc.radioID+"/channels", nil,
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var body struct {
				Channels []mdl.ChannelItem `json:"channels"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(body.Channels) != len(tc.wantNames) {
				t.Fatalf("len(channels) = %d, want %d", len(body.Channels), len(tc.wantNames))
			}
			for i, want := range tc.wantNames {
				if body.Channels[i].Name != want {
					t.Fatalf(
						"channels[%d].name = %q, want %q",
						i, body.Channels[i].Name, want,
					)
				}
				if !body.Channels[i].HasPSK {
					t.Fatalf("channels[%d].has_psk = false, want true", i)
				}
				// PSK must NEVER be on the wire — handler redacts via json:"-".
				if len(body.Channels[i].PSK) != 0 {
					t.Fatalf(
						"channels[%d].PSK leaked %d bytes onto the wire",
						i, len(body.Channels[i].PSK),
					)
				}
			}
		})
	}
}

// TestEndpointMintChannel — POST /radios/{id}/channels. Generates a
// fresh AES256 PSK on the server side, dispatches SetChannel into the
// first free slot, returns the meshtastic:// share URL. Raw PSK bytes
// stay server-side; clients only see the URL.
func TestEndpointMintChannel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		radioID     string
		body        string
		wantStatus  int
		wantSlot    int
		wantName    string
		wantNCmds   int
		wantPrefix  string // share_url prefix when applicable
		wantInError string // substring in error body when applicable
	}{
		{
			name:       "fresh-name-mints-into-first-free-secondary-slot",
			radioID:    "0xabcdef01",
			body:       `{"name":"adventure"}`,
			wantStatus: http.StatusAccepted,
			wantSlot:   3, // 1 ham, 2 field, so 3 is first free
			wantName:   "adventure",
			wantNCmds:  1,
			wantPrefix: "https://meshtastic.org/e/",
		},
		{
			name:       "leading-#-stripped-before-conflict-check",
			radioID:    "0xabcdef01",
			body:       `{"name":"#camp"}`,
			wantStatus: http.StatusAccepted,
			wantSlot:   3,
			wantName:   "camp",
			wantNCmds:  1,
		},
		{
			name:        "duplicate-name-rejected-with-409",
			radioID:     "0xabcdef01",
			body:        `{"name":"ham"}`,
			wantStatus:  http.StatusConflict,
			wantInError: "already exists",
		},
		{
			name:        "name-too-long-rejected-with-400",
			radioID:     "0xabcdef01",
			body:        `{"name":"this-name-is-way-too-long-for-meshtastic"}`,
			wantStatus:  http.StatusBadRequest,
			wantInError: "max 11",
		},
		{
			name:       "empty-name-rejected-by-huma-with-422",
			radioID:    "0xabcdef01",
			body:       `{"name":""}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "unknown-radio-returns-404-no-dispatch",
			radioID:    "nope-no-such-radio",
			body:       `{"name":"adventure"}`,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, p := channelHarness(t)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPost,
				srv.URL+"/radios/"+tc.radioID+"/channels",
				bytes.NewReader([]byte(tc.body)),
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("content-type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}

			cmds := p.snapshot()
			if got := len(cmds); got != tc.wantNCmds {
				t.Fatalf("dispatched %d commands, want %d", got, tc.wantNCmds)
			}

			if tc.wantStatus != http.StatusAccepted {
				if tc.wantInError != "" {
					var errBody struct {
						Detail string `json:"detail"`
					}
					_ = json.NewDecoder(resp.Body).Decode(&errBody)
					if !strings.Contains(errBody.Detail, tc.wantInError) {
						t.Fatalf(
							"error.detail = %q, want substring %q",
							errBody.Detail,
							tc.wantInError,
						)
					}
				}
				return
			}

			var body MintChannelResult
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Index != tc.wantSlot {
				t.Fatalf("index = %d, want %d", body.Index, tc.wantSlot)
			}
			if body.Name != tc.wantName {
				t.Fatalf("name = %q, want %q", body.Name, tc.wantName)
			}
			if tc.wantPrefix != "" && !strings.HasPrefix(body.ShareURL, tc.wantPrefix) {
				t.Fatalf("share_url = %q, want prefix %q", body.ShareURL, tc.wantPrefix)
			}

			// Verify the dispatched command carries the slot + name we
			// expect, and that the PSK is exactly 32 bytes (AES256).
			set, ok := cmds[0].(mdl.SetChannel)
			if !ok {
				t.Fatalf("dispatched %T, want mdl.SetChannel", cmds[0])
			}
			if set.Slot.Index != tc.wantSlot {
				t.Fatalf("dispatched slot = %d, want %d", set.Slot.Index, tc.wantSlot)
			}
			if set.Slot.Name != tc.wantName {
				t.Fatalf("dispatched name = %q, want %q", set.Slot.Name, tc.wantName)
			}
			if len(set.Slot.PSK) != 32 {
				t.Fatalf("dispatched PSK len = %d, want 32 (AES256)", len(set.Slot.PSK))
			}
		})
	}
}

// TestEndpointImportChannel — POST /radios/{id}/channels/import.
// Parses a meshtastic:// share URL and dispatches SetChannel for each
// channel that fits a free slot. Multi-channel URLs are handled
// per-slot — collisions go in skipped[] rather than failing the call.
func TestEndpointImportChannels(t *testing.T) {
	t.Parallel()

	// Build two share URLs for fixture inputs:
	// - "novel" → fresh name, should import into slot 3
	// - "ham"   → already on the radio, should skip
	novelURL, err := pump.BuildChannelShareURL(mdl.ChannelInfo{
		Index:  1,
		Name:   "novel",
		Role:   mdl.ChannelSecondary,
		ID:     0x12345678,
		HasPSK: true,
		PSK:    []byte("novel-psk-32-byte-fixture-aaaaaa"),
	})
	if err != nil {
		t.Fatalf("build novel URL: %v", err)
	}
	dupURL, err := pump.BuildChannelShareURL(mdl.ChannelInfo{
		Index:  1,
		Name:   "ham", // already on the radio per channelHarness
		Role:   mdl.ChannelSecondary,
		ID:     0x12345678,
		HasPSK: true,
		PSK:    []byte("ham-psk-32-byte-fixture-aaaaaaaa"),
	})
	if err != nil {
		t.Fatalf("build dup URL: %v", err)
	}

	cases := []struct {
		name         string
		radioID      string
		body         string
		wantStatus   int
		wantImported []string // names in imported[]
		wantSkipped  []string // names in skipped[]
		wantNCmds    int
	}{
		{
			name:         "novel-channel-imports-into-first-free-slot",
			radioID:      "0xabcdef01",
			body:         `{"url":"` + novelURL + `"}`,
			wantStatus:   http.StatusAccepted,
			wantImported: []string{"novel"},
			wantSkipped:  []string{},
			wantNCmds:    1,
		},
		{
			name:         "duplicate-name-recorded-in-skipped-not-failed",
			radioID:      "0xabcdef01",
			body:         `{"url":"` + dupURL + `"}`,
			wantStatus:   http.StatusAccepted,
			wantImported: []string{},
			wantSkipped:  []string{"ham"},
			wantNCmds:    0,
		},
		{
			name:       "malformed-url-rejected-with-400",
			radioID:    "0xabcdef01",
			body:       `{"url":"not-a-meshtastic-url"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty-url-rejected-by-huma-with-422",
			radioID:    "0xabcdef01",
			body:       `{"url":""}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "unknown-radio-returns-404",
			radioID:    "nope-no-such-radio",
			body:       `{"url":"` + novelURL + `"}`,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, p := channelHarness(t)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPost,
				srv.URL+"/radios/"+tc.radioID+"/channels/import",
				bytes.NewReader([]byte(tc.body)),
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("content-type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}

			cmds := p.snapshot()
			if got := len(cmds); got != tc.wantNCmds {
				t.Fatalf("dispatched %d commands, want %d", got, tc.wantNCmds)
			}

			if tc.wantStatus != http.StatusAccepted {
				return
			}

			var body ImportChannelResult
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			gotImported := make([]string, 0, len(body.Imported))
			for _, ic := range body.Imported {
				gotImported = append(gotImported, ic.Name)
			}
			if strings.Join(gotImported, ",") != strings.Join(tc.wantImported, ",") {
				t.Fatalf("imported = %v, want %v", gotImported, tc.wantImported)
			}
			gotSkipped := make([]string, 0, len(body.Skipped))
			for _, sc := range body.Skipped {
				gotSkipped = append(gotSkipped, sc.Name)
			}
			if strings.Join(gotSkipped, ",") != strings.Join(tc.wantSkipped, ",") {
				t.Fatalf("skipped = %v, want %v", gotSkipped, tc.wantSkipped)
			}
		})
	}
}

// TestEndpointDeleteChannel — DELETE /radios/{id}/channels/{index}.
// Dispatches DeleteChannel for slots 1..7; refuses slot 0 (PRIMARY).
func TestEndpointDeleteChannel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		radioID    string
		index      string
		wantStatus int
		wantNCmds  int
		wantCmd    mdl.Command
	}{
		{
			name:       "deletes-secondary-slot-via-DeleteChannel",
			radioID:    "0xabcdef01",
			index:      "2",
			wantStatus: http.StatusAccepted,
			wantNCmds:  1,
			wantCmd:    mdl.DeleteChannel{Index: 2},
		},
		{
			name:       "refuses-slot-0-PRIMARY-with-422",
			radioID:    "0xabcdef01",
			index:      "0",
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "refuses-slot-out-of-range-with-422",
			radioID:    "0xabcdef01",
			index:      "8",
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "unknown-radio-returns-404",
			radioID:    "nope-no-such-radio",
			index:      "1",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, p := channelHarness(t)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodDelete,
				srv.URL+"/radios/"+tc.radioID+"/channels/"+tc.index, nil,
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			cmds := p.snapshot()
			if got := len(cmds); got != tc.wantNCmds {
				t.Fatalf("dispatched %d commands, want %d", got, tc.wantNCmds)
			}
			if tc.wantNCmds == 1 && cmds[0] != tc.wantCmd {
				t.Fatalf("dispatched %#v, want %#v", cmds[0], tc.wantCmd)
			}
		})
	}
}

// TestEndpointShareChannel — GET /radios/{id}/channels/{index}/share.
// Builds a meshtastic:// universal-link share URL from the slot's
// stored PSK + name. Returns 404 for slot indexes beyond the channel
// table or for DISABLED slots.
func TestEndpointShareChannel(t *testing.T) {
	t.Parallel()

	srv, _ := channelHarness(t)

	cases := []struct {
		name       string
		radioID    string
		index      string
		wantStatus int
		wantName   string
		wantPrefix string
	}{
		{
			name:       "secondary-slot-1-returns-share-url-for-ham",
			radioID:    "0xabcdef01",
			index:      "1",
			wantStatus: http.StatusOK,
			wantName:   "ham",
			wantPrefix: "https://meshtastic.org/e/",
		},
		{
			name:       "primary-slot-0-allowed-and-returns-share-url",
			radioID:    "0xabcdef01",
			index:      "0",
			wantStatus: http.StatusOK,
			wantName:   "",
			wantPrefix: "https://meshtastic.org/e/",
		},
		{
			name:       "slot-beyond-channels-table-returns-404",
			radioID:    "0xabcdef01",
			index:      "5",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "out-of-range-index-rejected-by-huma-with-422",
			radioID:    "0xabcdef01",
			index:      "8",
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "unknown-radio-returns-404",
			radioID:    "nope-no-such-radio",
			index:      "1",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodGet,
				srv.URL+"/radios/"+tc.radioID+"/channels/"+tc.index+"/share", nil,
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var body ChannelShareResult
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Name != tc.wantName {
				t.Fatalf("name = %q, want %q", body.Name, tc.wantName)
			}
			if !strings.HasPrefix(body.ShareURL, tc.wantPrefix) {
				t.Fatalf("share_url = %q, want prefix %q", body.ShareURL, tc.wantPrefix)
			}
		})
	}
}

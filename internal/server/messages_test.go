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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/session"
)

// newMessagesHarness pre-seeds State.Messages with a fixed
// DM-vs-broadcast mix so tests can assert on the ?dm= filter without
// driving ApplyText.
//
// Layout (chronological):
//
//	row 0: broadcast inbound from peer A
//	row 1: DM to me (inbound)
//	row 2: my outbound broadcast
//	row 3: my outbound DM to peer B
//	row 4: DM between two other peers (relayed traffic we observe)
func newMessagesHarness(t *testing.T) *httptest.Server {
	t.Helper()
	s := New(Config{Radios: NewRegistry()})
	sess := session.New(nil, nil, nil)
	sess.State.RadioID = "0xabcdef01"
	sess.State.MyNodeNum = 0xdeadbeef
	const peerA = uint32(0xc0ffee)
	const peerB = uint32(0xbabec0fe)
	const peerC = uint32(0xa11ce)
	sess.State.Messages = []mdl.MessageItem{
		{Message: mdl.Message{FromNum: peerA, ToNum: mdl.BroadcastNum, Text: "broadcast from A"}},
		{
			Message: mdl.Message{
				FromNum: peerA,
				ToNum:   sess.State.MyNodeNum,
				Text:    "DM from A to me",
			},
		},
		{Message: mdl.Message{
			FromNum: sess.State.MyNodeNum, Mine: true, ToNum: mdl.BroadcastNum,
			Text: "my broadcast",
		}},
		{Message: mdl.Message{
			FromNum: sess.State.MyNodeNum, Mine: true, ToNum: peerB, Text: "my DM to B",
		}},
		{Message: mdl.Message{FromNum: peerC, ToNum: peerB, Text: "DM peer C → peer B"}},
	}
	s.radios.Add(sess.State.RadioID, sess)
	srv := httptest.NewServer(s.http.Handler)
	t.Cleanup(srv.Close)
	return srv
}

// TestEndpointListMessages — GET /radios/{id}/messages with the ?dm=
// and ?limit= query params. One row per scenario; rows that expect
// 200 OK assert the returned message texts in order, rows that
// expect a non-2xx skip the body check.
func TestEndpointListMessages(t *testing.T) {
	t.Parallel()

	srv := newMessagesHarness(t)

	cases := []struct {
		name       string
		query      string
		wantStatus int
		wantText   []string // ignored when wantStatus != 200
	}{
		{
			name:       "empty-filter-returns-everything-in-order",
			query:      "",
			wantStatus: http.StatusOK,
			wantText: []string{
				"broadcast from A",
				"DM from A to me",
				"my broadcast",
				"my DM to B",
				"DM peer C → peer B",
			},
		},
		{
			name:       "dm-1-returns-every-peer-addressed-row",
			query:      "?dm=1",
			wantStatus: http.StatusOK,
			wantText:   []string{"DM from A to me", "my DM to B", "DM peer C → peer B"},
		},
		{
			name:       "dm-mine-returns-only-my-thread",
			query:      "?dm=mine",
			wantStatus: http.StatusOK,
			wantText:   []string{"DM from A to me", "my DM to B"},
		},
		{
			// limit applies AFTER the dm filter — otherwise a noisy
			// channel could push DMs out of the tail even though the
			// caller asked specifically for DMs.
			name:       "limit-applies-after-dm-filter",
			query:      "?dm=1&limit=2",
			wantStatus: http.StatusOK,
			wantText:   []string{"my DM to B", "DM peer C → peer B"},
		},
		{
			// Huma's enum:",1,mine" rejects anything else with 422
			// before the handler runs.
			name:       "rejects-unknown-dm-value-with-422",
			query:      "?dm=bogus",
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodGet,
				srv.URL+"/radios/0xabcdef01/messages"+tc.query, nil,
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
			var out struct {
				Messages []mdl.MessageItem `json:"messages"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(out.Messages) != len(tc.wantText) {
				t.Fatalf("len(messages) = %d, want %d", len(out.Messages), len(tc.wantText))
			}
			for i, want := range tc.wantText {
				if out.Messages[i].Text != want {
					t.Fatalf("messages[%d].Text = %q, want %q", i, out.Messages[i].Text, want)
				}
			}
		})
	}
}

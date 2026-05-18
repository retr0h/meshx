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

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/radio"
)

// TestEndpointListNodes — GET /radios/{id}/nodes. Returns the
// canonical NodeItem wire shape with State derived at request time
// via NodeItem.CurrentState — a peer heard within 15 minutes shows
// "online", a peer with no LastHeardAt keeps the stored State, an
// unknown radio_id is gated by resolveRadio with 404.
func TestEndpointListNodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		radioID    string
		seedNodes  []mdl.NodeItem
		wantStatus int
		wantLen    int
		wantStates []mdl.NodeState // nil = skip per-row state check
	}{
		{
			name:       "empty-nodes-list-returns-empty-array",
			radioID:    "the-radio",
			seedNodes:  nil,
			wantStatus: http.StatusOK,
			wantLen:    0,
		},
		{
			name:    "recent-LastHeardAt-derives-online-state",
			radioID: "the-radio",
			seedNodes: []mdl.NodeItem{
				{
					NodeNum:     0xc0ffee,
					Callsign:    "RECENT",
					ShortName:   "RCT",
					LastHeardAt: time.Now().Add(-2 * time.Minute),
				},
				{
					NodeNum:     0xbabec0fe,
					Callsign:    "STALE",
					ShortName:   "STL",
					LastHeardAt: time.Now().Add(-2 * time.Hour),
				},
				{
					NodeNum:   0xa11ce,
					Callsign:  "MUTED",
					ShortName: "MTD",
					State:     mdl.StateMuted, // sticky — never derives anything else
				},
			},
			wantStatus: http.StatusOK,
			wantLen:    3,
			wantStates: []mdl.NodeState{mdl.StateOnline, mdl.StateOffline, mdl.StateMuted},
		},
		{
			name:       "unknown-radio-returns-404",
			radioID:    "nope-no-such-radio",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Config{Radios: NewRegistry()})
			sess := radio.New(nil, nil, nil)
			sess.State.RadioID = "the-radio"
			sess.State.MyNodeNum = 0xdeadbeef
			sess.State.Nodes = tc.seedNodes
			s.radios.Add(sess.State.RadioID, sess)
			srv := httptest.NewServer(s.http.Handler)
			t.Cleanup(srv.Close)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodGet,
				srv.URL+"/radios/"+tc.radioID+"/nodes", nil,
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
				Nodes []mdl.NodeItem `json:"nodes"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(body.Nodes) != tc.wantLen {
				t.Fatalf("len(nodes) = %d, want %d", len(body.Nodes), tc.wantLen)
			}
			for i, want := range tc.wantStates {
				if body.Nodes[i].State != want {
					t.Fatalf("nodes[%d].state = %q, want %q", i, body.Nodes[i].State, want)
				}
			}
		})
	}
}

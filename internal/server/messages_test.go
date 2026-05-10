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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/session"
)

// newMessagesHarness mirrors newSSEHarness but pre-seeds State.Messages
// with a fixed DM-vs-broadcast mix so tests can assert on the ?dm=
// filter without driving ApplyText.
//
// Layout (in chronological order):
//
//	row 0: broadcast inbound from peer A
//	row 1: DM to me (inbound)
//	row 2: my outbound broadcast
//	row 3: my outbound DM to peer B
//	row 4: DM between two other peers (e.g. relayed traffic we observe)
func newMessagesHarness(t *testing.T) (*httptest.Server, *session.Session) {
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
	return srv, sess
}

func getMessagesBody(t *testing.T, url string) []mdl.MessageItem {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Messages []mdl.MessageItem `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out.Messages
}

// TestListMessagesDMFilter is the single behavior-as-table covering
// the new ?dm= query param. Each row states the filter value and
// the message texts we expect back, in order. Adding a new filter
// mode is one row.
func TestListMessagesDMFilter(t *testing.T) {
	t.Parallel()

	srv, _ := newMessagesHarness(t)

	cases := []struct {
		name     string
		query    string
		wantText []string
	}{
		{
			name:  "empty-filter-returns-all",
			query: "",
			wantText: []string{
				"broadcast from A",
				"DM from A to me",
				"my broadcast",
				"my DM to B",
				"DM peer C → peer B",
			},
		},
		{
			name:     "dm-1-returns-every-peer-addressed-row",
			query:    "?dm=1",
			wantText: []string{"DM from A to me", "my DM to B", "DM peer C → peer B"},
		},
		{
			name:     "dm-mine-returns-only-my-thread",
			query:    "?dm=mine",
			wantText: []string{"DM from A to me", "my DM to B"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := getMessagesBody(t, srv.URL+"/radios/0xabcdef01/messages"+tc.query)
			if len(got) != len(tc.wantText) {
				t.Fatalf("len(got) = %d, want %d", len(got), len(tc.wantText))
			}
			for i, want := range tc.wantText {
				if got[i].Text != want {
					t.Fatalf("messages[%d].Text = %q, want %q", i, got[i].Text, want)
				}
			}
		})
	}
}

// TestListMessagesDMFilterWithLimit verifies the limit window
// applies AFTER the DM filter, not before — otherwise a noisy
// channel could push a DM out of the limited tail even though the
// caller specifically asked for DMs.
func TestListMessagesDMFilterWithLimit(t *testing.T) {
	t.Parallel()
	srv, _ := newMessagesHarness(t)
	got := getMessagesBody(t, srv.URL+"/radios/0xabcdef01/messages?dm=1&limit=2")
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	// Tail of the DM-filtered slice = last two DMs in chronological order.
	want := []string{"my DM to B", "DM peer C → peer B"}
	for i, w := range want {
		if got[i].Text != w {
			t.Fatalf("messages[%d].Text = %q, want %q", i, got[i].Text, w)
		}
	}
}

// TestListMessagesUnknownDMValue — Huma's enum:",1,mine" should
// reject anything else with a 422 before the handler runs, so a
// typo'd value can't silently fall through to the permissive
// default.
func TestListMessagesUnknownDMValue(t *testing.T) {
	t.Parallel()
	srv, _ := newMessagesHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(
		ctx, http.MethodGet,
		srv.URL+"/radios/0xabcdef01/messages?dm=bogus", nil,
	)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

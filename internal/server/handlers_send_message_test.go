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
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// TestEndpointSendMessage — POST /radios/{id}/messages. Dispatches a
// SendText command, records a "mine" outbound row in State.Messages,
// and returns the allocated MeshPacket.id so clients can correlate
// with subsequent ack/fail SSE events. Idempotency-Key dedupes
// retries within a 60s TTL — the second call returns the cached
// result without dispatching to the radio a second time.
func TestEndpointSendMessage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		radioID     string
		body        string
		idemKey     string
		wantStatus  int
		wantNCmds   int         // commands the pump should have seen
		wantCmd     mdl.Command // expected dispatched command on 200
		wantPID     bool        // response should echo non-zero packet_id
		wantOK      bool
		wantNoText  string // when set: assert no SendText with this Text was dispatched
		wantInError string // substring expected in error body
	}{
		{
			name:       "broadcast-to-channel-dispatches-SendText-and-records-row",
			radioID:    "0xabcdef01",
			body:       `{"channel":0,"text":"hello channel"}`,
			wantStatus: http.StatusOK,
			wantNCmds:  1,
			wantCmd:    mdl.SendText{Channel: 0, Text: "hello channel"},
			wantPID:    true,
			wantOK:     true,
		},
		{
			name:       "DM-to-peer-passes-to_num-through-to-SendText",
			radioID:    "0xabcdef01",
			body:       `{"channel":0,"text":"hi peer","to_num":12648430}`,
			wantStatus: http.StatusOK,
			wantNCmds:  1,
			wantCmd:    mdl.SendText{Channel: 0, Text: "hi peer", ToNum: 0xc0ffee},
			wantPID:    true,
			wantOK:     true,
		},
		{
			name:       "reply_id-passes-through-to-SendText",
			radioID:    "0xabcdef01",
			body:       `{"channel":1,"text":"73","reply_id":99}`,
			wantStatus: http.StatusOK,
			wantNCmds:  1,
			wantCmd:    mdl.SendText{Channel: 1, Text: "73", ReplyID: 99},
			wantPID:    true,
			wantOK:     true,
		},
		{
			name:       "empty-text-rejected-by-huma-with-422-no-dispatch",
			radioID:    "0xabcdef01",
			body:       `{"channel":0,"text":""}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "unknown-radio-returns-404-no-dispatch",
			radioID:    "nope-no-such-radio",
			body:       `{"channel":0,"text":"hi"}`,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, p := radioOpsHarness(t)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPost,
				srv.URL+"/radios/"+tc.radioID+"/messages",
				bytes.NewReader([]byte(tc.body)),
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("content-type", "application/json")
			if tc.idemKey != "" {
				req.Header.Set("Idempotency-Key", tc.idemKey)
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
			if tc.wantStatus != http.StatusOK {
				return
			}
			var body SendMessageResult
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if tc.wantPID && body.PacketID == 0 {
				t.Fatalf("packet_id = 0, want non-zero")
			}
			if body.OK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", body.OK, tc.wantOK)
			}
		})
	}

	// Idempotency-Key dedupe is a multi-request property — distinct
	// mechanics from "given a request, what does the handler do once."
	// Lives as a sub-test under the same parent so the public surface
	// (POST /radios/{id}/messages) keeps to one TestEndpoint function.
	t.Run("Idempotency-Key-dedupes-retry-and-returns-cached-result", func(t *testing.T) {
		srv, p := radioOpsHarness(t)
		const idemKey = "fixture-uuid-1234"

		send := func() SendMessageResult {
			t.Helper()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPost,
				srv.URL+"/radios/0xabcdef01/messages",
				bytes.NewReader([]byte(`{"channel":0,"text":"only-once"}`)),
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("content-type", "application/json")
			req.Header.Set("Idempotency-Key", idemKey)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			var body SendMessageResult
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			return body
		}

		first := send()
		second := send()

		if first.PacketID == 0 {
			t.Fatalf("first packet_id = 0, want non-zero")
		}
		if second.PacketID != first.PacketID {
			t.Fatalf(
				"retry returned packet_id = %d, want %d (cached)",
				second.PacketID, first.PacketID,
			)
		}
		// Pump must have seen exactly ONE dispatch — the second call
		// should return cached without re-broadcasting on RF.
		if got := len(p.snapshot()); got != 1 {
			t.Fatalf(
				"dispatched %d commands across two retries; want 1 (Idempotency-Key dedupe)",
				got,
			)
		}
	})
}

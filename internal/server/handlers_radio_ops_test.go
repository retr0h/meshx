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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/radio"
)

// fakePump captures every command Driver.Send dispatches so radio-op
// handlers can be tested without a real radio. Satisfies the seam
// radio.Pump that Session.Send forwards to.
type fakePump struct {
	mu       sync.Mutex
	commands []mdl.Command
	nextID   uint32
}

func newFakePump() *fakePump {
	return &fakePump{nextID: 1000}
}

func (p *fakePump) Send(cmd mdl.Command) (uint32, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.commands = append(p.commands, cmd)
	pid := p.nextID
	p.nextID++
	return pid, true
}

func (p *fakePump) Stop() {}

func (p *fakePump) snapshot() []mdl.Command {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]mdl.Command, len(p.commands))
	copy(out, p.commands)
	return out
}

// radioOpsHarness wires a Server with one Session that has a fakePump
// attached, so handler dispatches reach the fake instead of trying to
// talk to a real radio.
func radioOpsHarness(t *testing.T) (*httptest.Server, *fakePump) {
	t.Helper()
	s := New(Config{Radios: NewRegistry()})
	pump := newFakePump()
	sess := radio.New(nil, pump, nil)
	sess.State.RadioID = "0xabcdef01"
	sess.State.MyNodeNum = 0xdeadbeef
	s.radios.Add(sess.State.RadioID, sess)
	srv := httptest.NewServer(s.http.Handler)
	t.Cleanup(srv.Close)
	return srv, pump
}

// radioOpCase is the row shape shared by the three TestEndpoint
// functions below. Each route's table covers its happy path, its
// validation failures, and the unknown-radio gate in one place.
type radioOpCase struct {
	name       string
	radioID    string // overrides default 0xabcdef01 (used for unknown-radio rows)
	body       string // raw JSON body
	wantStatus int
	wantCmd    mdl.Command // expected mdl.Command on Send (if wantStatus == 202)
	wantPID    bool        // response should echo non-zero packet_id
}

func runRadioOpCase(t *testing.T, path string, tc radioOpCase) {
	t.Helper()
	srv, pump := radioOpsHarness(t)

	radioID := tc.radioID
	if radioID == "" {
		radioID = "0xabcdef01"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		srv.URL+"/radios/"+radioID+path,
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

	cmds := pump.snapshot()
	if tc.wantStatus != http.StatusAccepted {
		if got := len(cmds); got != 0 {
			t.Fatalf("dispatched %d commands on non-202; want pump untouched", got)
		}
		return
	}

	if got := len(cmds); got != 1 {
		t.Fatalf("dispatched %d commands, want 1", got)
	}
	if cmds[0] != tc.wantCmd {
		t.Fatalf("dispatched %#v, want %#v", cmds[0], tc.wantCmd)
	}

	if tc.wantPID {
		var body struct {
			PacketID uint32 `json:"packet_id"`
			OK       bool   `json:"ok"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.PacketID == 0 {
			t.Fatalf("packet_id = 0, want non-zero")
		}
		if !body.OK {
			t.Fatalf("ok = false, want true")
		}
	}
}

// TestEndpointPing — POST /radios/{id}/ping. Happy path dispatches
// SendPing to the pump and echoes a non-zero packet_id; validation
// rejects to_num < 1 with 422; unknown radio_id returns 404.
func TestEndpointPingPeer(t *testing.T) {
	t.Parallel()
	cases := []radioOpCase{
		{
			name:       "dispatches-SendPing-with-target-and-echoes-packet-id",
			body:       `{"to_num":12648430}`, // 0xc0ffee
			wantStatus: http.StatusAccepted,
			wantCmd:    mdl.SendPing{TargetNum: 0xc0ffee},
			wantPID:    true,
		},
		{
			name:       "rejects-zero-to_num-with-422",
			body:       `{"to_num":0}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "rejects-missing-to_num-with-422",
			body:       `{}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "returns-404-for-unknown-radio",
			radioID:    "nope-no-such-radio",
			body:       `{"to_num":1234}`,
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runRadioOpCase(t, "/ping", tc) })
	}
}

// TestEndpointTraceroute — POST /radios/{id}/traceroute. Same shape
// as ping (target NodeNum required, packet_id correlator returned).
func TestEndpointTraceroute(t *testing.T) {
	t.Parallel()
	cases := []radioOpCase{
		{
			name:       "dispatches-SendTraceroute-with-target-and-echoes-packet-id",
			body:       `{"to_num":12648430}`,
			wantStatus: http.StatusAccepted,
			wantCmd:    mdl.SendTraceroute{TargetNum: 0xc0ffee},
			wantPID:    true,
		},
		{
			name:       "rejects-zero-to_num-with-422",
			body:       `{"to_num":0}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "rejects-missing-to_num-with-422",
			body:       `{}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "returns-404-for-unknown-radio",
			radioID:    "nope-no-such-radio",
			body:       `{"to_num":1234}`,
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runRadioOpCase(t, "/traceroute", tc) })
	}
}

// TestEndpointSync — POST /radios/{id}/sync. Fire-and-forget at the
// wire level, so no required body fields and no packet_id correlator
// in the response (the radio re-dumps its NodeDB / channels /
// configs / Metadata, each arriving as its own SSE event).
func TestEndpointSync(t *testing.T) {
	t.Parallel()
	cases := []radioOpCase{
		{
			name:       "dispatches-RequestSync-with-empty-body",
			body:       `{}`,
			wantStatus: http.StatusAccepted,
			wantCmd:    mdl.RequestSync{},
		},
		{
			name:       "returns-404-for-unknown-radio",
			radioID:    "nope-no-such-radio",
			body:       `{}`,
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runRadioOpCase(t, "/sync", tc) })
	}
}

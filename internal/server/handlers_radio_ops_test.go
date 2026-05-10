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
	"sync"
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/session"
)

// fakePump captures every command Driver.Send dispatches so radio-op
// handlers can be tested without a real radio. ctor returns a
// session.Pump that satisfies the seam Session.Send forwards to.
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

// radioOpsHarness wires a Server with one Session that has a
// fakePump attached, so handler dispatches reach the fake instead
// of trying to talk to a real radio.
func radioOpsHarness(t *testing.T) (*httptest.Server, *fakePump) {
	t.Helper()
	s := New(Config{Radios: NewRegistry()})
	pump := newFakePump()
	sess := session.New(nil, pump, nil)
	sess.State.RadioID = "0xabcdef01"
	sess.State.MyNodeNum = 0xdeadbeef
	s.radios.Add(sess.State.RadioID, sess)
	srv := httptest.NewServer(s.http.Handler)
	t.Cleanup(srv.Close)
	return srv, pump
}

// TestRadioOpsDispatchTypedCommand is the single behavior-as-table
// covering all three new endpoints. Each row states the path, body,
// and the model.Command we expect Driver.Send to receive. Adds a
// fourth/fifth route in one row.
func TestRadioOpsDispatchTypedCommand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		path     string
		body     any
		wantCmd  mdl.Command
		wantPID  bool // response should echo non-zero packet_id
		wantBody string
	}{
		{
			name:    "ping-dispatches-SendPing-with-target",
			path:    "/ping",
			body:    map[string]uint32{"to_num": 0xc0ffee},
			wantCmd: mdl.SendPing{TargetNum: 0xc0ffee},
			wantPID: true,
		},
		{
			name:    "traceroute-dispatches-SendTraceroute-with-target",
			path:    "/traceroute",
			body:    map[string]uint32{"to_num": 0xc0ffee},
			wantCmd: mdl.SendTraceroute{TargetNum: 0xc0ffee},
			wantPID: true,
		},
		{
			name:    "sync-dispatches-RequestSync-with-empty-body",
			path:    "/sync",
			body:    map[string]any{},
			wantCmd: mdl.RequestSync{},
			wantPID: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, pump := radioOpsHarness(t)
			payload, err := json.Marshal(tc.body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPost,
				srv.URL+"/radios/0xabcdef01"+tc.path,
				bytes.NewReader(payload),
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
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", resp.StatusCode)
			}

			cmds := pump.snapshot()
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
		})
	}
}

// TestRadioOpsRejectMissingRequiredFields asserts Huma's validation
// blocks malformed bodies before they hit Driver.Send. ping and
// traceroute require to_num >= 1; sync has no required fields.
func TestRadioOpsRejectMissingRequiredFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		body string
	}{
		{
			name: "ping-rejects-zero-to_num",
			path: "/ping",
			body: `{"to_num":0}`,
		},
		{
			name: "ping-rejects-missing-to_num",
			path: "/ping",
			body: `{}`,
		},
		{
			name: "traceroute-rejects-zero-to_num",
			path: "/traceroute",
			body: `{"to_num":0}`,
		},
		{
			name: "traceroute-rejects-missing-to_num",
			path: "/traceroute",
			body: `{}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, pump := radioOpsHarness(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(
				ctx, http.MethodPost,
				srv.URL+"/radios/0xabcdef01"+tc.path,
				bytes.NewReader([]byte(tc.body)),
			)
			req.Header.Set("content-type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", resp.StatusCode)
			}
			if got := len(pump.snapshot()); got != 0 {
				t.Fatalf("dispatched %d commands; expected pump untouched on 422", got)
			}
		})
	}
}

// TestRadioOpsReturn404ForUnknownRadio mirrors the existing
// resolveRadio gate — hit the routes for a radio_id that isn't
// registered, verify 404, verify nothing dispatched.
func TestRadioOpsReturn404ForUnknownRadio(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		body string
	}{
		{name: "ping", path: "/ping", body: `{"to_num":1234}`},
		{name: "traceroute", path: "/traceroute", body: `{"to_num":1234}`},
		{name: "sync", path: "/sync", body: `{}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, pump := radioOpsHarness(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(
				ctx, http.MethodPost,
				srv.URL+"/radios/nope-no-such-radio"+tc.path,
				bytes.NewReader([]byte(tc.body)),
			)
			req.Header.Set("content-type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", resp.StatusCode)
			}
			if got := len(pump.snapshot()); got != 0 {
				t.Fatalf("dispatched %d commands on unknown radio; want 0", got)
			}
		})
	}
}

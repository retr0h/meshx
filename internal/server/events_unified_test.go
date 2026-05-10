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
	"encoding/json"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/session"
)

// fleetHarness wires a Server with two registered radios so tests
// can exercise the unified-stream fan-in. Each radio is a real
// *session.Session with no pump; events get injected via
// session.Publish.
type fleetHarness struct {
	srv      *httptest.Server
	radios   []*session.Session
	radioIDs []string
}

func newFleetHarness(t *testing.T) *fleetHarness {
	t.Helper()
	s := New(Config{Radios: NewRegistry()})
	radioA := session.New(nil, nil, nil)
	radioA.State.RadioID = "0xradio_a"
	radioA.State.MyNodeNum = 0xaaaa
	radioB := session.New(nil, nil, nil)
	radioB.State.RadioID = "0xradio_b"
	radioB.State.MyNodeNum = 0xbbbb
	s.radios.Add(radioA.State.RadioID, radioA)
	s.radios.Add(radioB.State.RadioID, radioB)
	srv := httptest.NewServer(s.http.Handler)
	t.Cleanup(srv.Close)
	return &fleetHarness{
		srv:      srv,
		radios:   []*session.Session{radioA, radioB},
		radioIDs: []string{radioA.State.RadioID, radioB.State.RadioID},
	}
}

// publishOnEach injects a single text event into both radios with
// distinct payloads so tests can identify which event came from
// which radio without ambiguity.
func (h *fleetHarness) publishOnEach(label string) {
	h.radios[0].PublishText(mdl.Text{Body: mdl.Message{Text: label + "@A"}})
	h.radios[1].PublishText(mdl.Text{Body: mdl.Message{Text: label + "@B"}})
}

// TestUnifiedEventsStreamMergesAcrossRadios drives an end-to-end
// SSE subscription to GET /events, publishes one event per radio,
// and asserts both events arrive on the same connection with
// distinct radio_id fields.
func TestUnifiedEventsStreamMergesAcrossRadios(t *testing.T) {
	h := newFleetHarness(t)

	done := make(chan []sseEvent, 1)
	go func() {
		done <- readNSSEEvents(t, h.srv.URL+"/events", nil, 2, 3*time.Second)
	}()
	time.Sleep(50 * time.Millisecond)

	h.publishOnEach("hello")

	events := <-done
	if got := len(events); got != 2 {
		t.Fatalf("len(events) = %d, want 2", got)
	}

	gotRadios := make([]string, 0, 2)
	for i, ev := range events {
		if ev.event != "meshx_event" {
			t.Fatalf("event[%d].event = %q, want \"meshx_event\"", i, ev.event)
		}
		var env MeshxEvent
		if err := json.Unmarshal([]byte(ev.data), &env); err != nil {
			t.Fatalf("event[%d] decode: %v", i, err)
		}
		if env.RadioID == "" {
			t.Fatalf("event[%d] missing radio_id (envelope = %+v)", i, env)
		}
		if env.Kind != "text" {
			t.Fatalf("event[%d].kind = %q, want \"text\"", i, env.Kind)
		}
		gotRadios = append(gotRadios, env.RadioID)
	}
	sort.Strings(gotRadios)
	want := append([]string{}, h.radioIDs...)
	sort.Strings(want)
	for i, w := range want {
		if gotRadios[i] != w {
			t.Fatalf("radio_id set = %v, want %v", gotRadios, want)
		}
	}
}

// TestUnifiedEventsCarriesEventIDAndData asserts the envelope
// surfaces the per-radio event_id and that nested data passes
// through intact (so consumers can deserialize the per-kind payload
// the same way they would on the per-radio endpoint).
func TestUnifiedEventsCarriesEventIDAndData(t *testing.T) {
	h := newFleetHarness(t)

	done := make(chan []sseEvent, 1)
	go func() {
		done <- readNSSEEvents(t, h.srv.URL+"/events", nil, 1, 3*time.Second)
	}()
	time.Sleep(50 * time.Millisecond)

	h.radios[0].PublishText(mdl.Text{
		Channel: 7,
		Body:    mdl.Message{Text: "specific message", PacketID: 12345},
	})

	events := <-done
	if got := len(events); got != 1 {
		t.Fatalf("len(events) = %d, want 1", got)
	}
	var env struct {
		EventID uint64 `json:"event_id"`
		Kind    string `json:"kind"`
		RadioID string `json:"radio_id"`
		Data    struct {
			Channel int `json:"Channel"`
			Body    struct {
				Text     string `json:"text"`
				PacketID uint32 `json:"packet_id"`
			} `json:"Body"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(events[0].data), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.EventID == 0 {
		t.Fatalf("event_id = 0, want non-zero")
	}
	if env.RadioID != "0xradio_a" {
		t.Fatalf("radio_id = %q, want 0xradio_a", env.RadioID)
	}
	if env.Data.Channel != 7 {
		t.Fatalf("data.Channel = %d, want 7", env.Data.Channel)
	}
	if env.Data.Body.Text != "specific message" {
		t.Fatalf("data.Body.text = %q, want \"specific message\"", env.Data.Body.Text)
	}
	if env.Data.Body.PacketID != 12345 {
		t.Fatalf("data.Body.packet_id = %d, want 12345", env.Data.Body.PacketID)
	}
}

// TestUnifiedEventsEmptyRegistryClosesCleanly checks that GET
// /events against a daemon with no radios opens 200, blocks, and
// closes when the request context cancels — no panic, no log
// firehose.
func TestUnifiedEventsEmptyRegistryClosesCleanly(t *testing.T) {
	t.Parallel()
	s := New(Config{Radios: NewRegistry()})
	srv := httptest.NewServer(s.http.Handler)
	t.Cleanup(srv.Close)

	// Short timeout — the empty-registry case has no events to
	// deliver, so we just want to confirm the handler doesn't crash
	// and the connection terminates cleanly when ctx fires.
	events := readNSSEEvents(t, srv.URL+"/events", nil, 1, 200*time.Millisecond)
	if got := len(events); got != 0 {
		t.Fatalf("len(events) = %d, want 0", got)
	}
}

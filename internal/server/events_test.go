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
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/radio"
)

// reflectTypeName is a tiny indirection so the typeMap-collision
// guard reads cleanly without spreading reflect imports through
// the table-driven callers.
func reflectTypeName(v any) string {
	t := reflect.TypeOf(v)
	if t == nil {
		return "<nil>"
	}
	return t.PkgPath() + "." + t.Name()
}

// httptest harness — spins up an httptest.Server backed by the same
// *Server production uses, registers a *radio.Session under a
// known radio_id, hands back URL + session so tests can publish
// events and read the SSE stream.
type sseHarness struct {
	t       *testing.T
	srv     *httptest.Server
	session *radio.Session
	radioID string
}

func newSSEHarness(t *testing.T) *sseHarness {
	t.Helper()
	s := New(Config{Radios: NewRegistry()})
	sess := radio.New(nil, nil, nil)
	sess.State.RadioID = "0xabcdef01"
	s.radios.Add(sess.State.RadioID, sess)
	srv := httptest.NewServer(s.http.Handler)
	t.Cleanup(srv.Close)
	return &sseHarness{t: t, srv: srv, session: sess, radioID: sess.State.RadioID}
}

func (h *sseHarness) eventsURL(query string) string {
	u := h.srv.URL + "/radios/" + h.radioID + "/events"
	if query != "" {
		u += "?" + query
	}
	return u
}

// sseEvent is the parsed shape of one server-sent message.
type sseEvent struct {
	id    string
	event string
	data  string
}

// readNSSEEvents opens GET url with optional headers, parses the
// stream, and returns once n complete events have been read OR the
// timeout fires. Cancels the request on return so the server-side
// goroutine exits cleanly.
func readNSSEEvents(
	t *testing.T,
	url string,
	headers map[string]string,
	n int,
	timeout time.Duration,
) []sseEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	out := make([]sseEvent, 0, n)
	var cur sseEvent
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if cur.data != "" || cur.id != "" || cur.event != "" {
				out = append(out, cur)
				cur = sseEvent{}
				if len(out) >= n {
					return out
				}
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "id: "):
			cur.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			cur.event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF && err != context.DeadlineExceeded &&
		err != context.Canceled {
		t.Fatalf("scanner: %v", err)
	}
	return out
}

// publishOne queues a single Text event with a recognizable payload
// so tests can assert on the JSON content end-to-end.
func (h *sseHarness) publishOne(label string) {
	h.session.PublishText(mdl.Text{
		Channel: 0,
		Body:    mdl.Message{Text: label},
	})
}

// TestEndpointEventsStream — GET /radios/{id}/events. Every row
// follows the same shape: optionally pre-publish into the ring,
// open the SSE stream with a query / headers, optionally publish
// live events while the subscription is active, then assert on
// the parsed SSE id / event-name lines. Mechanics are uniform so
// scenarios live as table rows; the unknown-radio failure path
// genuinely diverges (no events at all, just a body assertion) and
// runs as a t.Run sub-test under the same parent.
func TestEndpointEventsStream(t *testing.T) {
	const myNum = uint32(0xdeadbeef)
	const peer = uint32(0xc0ffee)

	pubBroadcastN := func(n int) func(*sseHarness) {
		return func(h *sseHarness) {
			for i := 0; i < n; i++ {
				h.publishOne("history")
			}
		}
	}
	applyDM := func(h *sseHarness) {
		h.session.State.MyNodeNum = myNum
		h.session.ApplyText(mdl.Text{
			Channel: 0,
			ToNum:   myNum,
			Body: mdl.Message{
				FromNum: peer, Text: "hi from peer",
				PacketID: 99, SentAt: time.Now(),
			},
		}, "hi from peer", false, false)
	}
	applyBroadcast := func(h *sseHarness) {
		h.session.State.MyNodeNum = myNum
		h.session.ApplyText(mdl.Text{
			Channel: 0,
			ToNum:   mdl.BroadcastNum,
			Body: mdl.Message{
				FromNum: peer, Text: "channel chat",
				PacketID: 100, SentAt: time.Now(),
			},
		}, "channel chat", false, false)
	}

	cases := []struct {
		name           string
		prePublish     func(*sseHarness) // before subscribe — fills the ring
		query          string
		headers        map[string]string
		livePublish    func(*sseHarness) // after subscribe lands in fan-out
		wantN          int
		wantIDs        []string // nil = skip id check
		wantEventNames []string // nil = skip event-name check
	}{
		{
			name:           "live-only-no-cursor-receives-post-subscribe-events",
			livePublish:    pubBroadcastN(3),
			wantN:          3,
			wantIDs:        []string{"1", "2", "3"},
			wantEventNames: []string{"text", "text", "text"},
		},
		{
			name:       "since-query-replays-buffer-from-cursor",
			prePublish: pubBroadcastN(5),
			query:      "since=2",
			wantN:      3,
			wantIDs:    []string{"3", "4", "5"},
		},
		{
			name:       "Last-Event-ID-header-replays-buffer-from-cursor",
			prePublish: pubBroadcastN(5),
			headers:    map[string]string{"Last-Event-ID": "3"},
			wantN:      2,
			wantIDs:    []string{"4", "5"},
		},
		{
			// Explicit deliberate seek beats the auto-tracked
			// reconnect cursor when both are supplied.
			name:       "since-query-overrides-Last-Event-ID-header",
			prePublish: pubBroadcastN(5),
			query:      "since=4",
			headers:    map[string]string{"Last-Event-ID": "1"},
			wantN:      1,
			wantIDs:    []string{"5"},
		},
		{
			// "Client thinks it's caught up but daemon restarted and
			// reset the counter" — replay nothing, but live still
			// flows.
			name:        "cursor-beyond-head-replays-empty-but-live-still-flows",
			prePublish:  pubBroadcastN(3),
			query:       "since=999999",
			livePublish: pubBroadcastN(1),
			wantN:       1,
			wantIDs:     []string{"4"},
		},
		{
			name:       "since-zero-replays-whole-buffer",
			prePublish: pubBroadcastN(4),
			query:      "since=0",
			wantN:      4,
			wantIDs:    []string{"1", "2", "3", "4"},
		},
		{
			// ApplyText must route to the dm_received variant when
			// ToNum == MyNodeNum — earlier broadcast-only smoke test
			// missed this because PublishText bypassed the
			// branching in ApplyText.
			name:           "ApplyText-DM-routes-to-dm_received-event-name",
			livePublish:    applyDM,
			wantN:          1,
			wantEventNames: []string{"dm_received"},
		},
		{
			name:           "ApplyText-broadcast-routes-to-text-event-name",
			livePublish:    applyBroadcast,
			wantN:          1,
			wantEventNames: []string{"text"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newSSEHarness(t)
			if tc.prePublish != nil {
				tc.prePublish(h)
			}

			done := make(chan []sseEvent, 1)
			go func() {
				done <- readNSSEEvents(
					t, h.eventsURL(tc.query), tc.headers, tc.wantN, 3*time.Second,
				)
			}()
			if tc.livePublish != nil {
				// Give the subscribe a moment to land in the
				// registry's fan-out before publishing live.
				time.Sleep(50 * time.Millisecond)
				tc.livePublish(h)
			}

			events := <-done
			if got := len(events); got != tc.wantN {
				t.Fatalf("len(events) = %d, want %d", got, tc.wantN)
			}
			for i, ev := range events {
				if tc.wantIDs != nil && ev.id != tc.wantIDs[i] {
					t.Fatalf("event[%d].id = %q, want %q", i, ev.id, tc.wantIDs[i])
				}
				if tc.wantEventNames != nil && ev.event != tc.wantEventNames[i] {
					t.Fatalf(
						"event[%d].event = %q, want %q",
						i, ev.event, tc.wantEventNames[i],
					)
				}
			}
		})
	}

	// Unknown-radio failure path: no events arrive at all, so the
	// row-shaped assertion above doesn't apply. Stream opens 200
	// (response headers committed before the handler runs) but
	// emits zero events before timing out — documents current
	// behavior so a future refactor that 404s at request setup
	// surfaces as a test failure rather than silent breakage.
	t.Run("unknown-radio-emits-no-events", func(t *testing.T) {
		h := newSSEHarness(t)
		url := h.srv.URL + "/radios/nope-no-such-radio/events"
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if got := strings.TrimSpace(string(body)); got != "" {
			t.Fatalf("unknown radio leaked event data: %q", got)
		}
	})
}

// TestEventsTypeMapHasDistinctTypes guards against the regression
// that produced this file's existence: Huma's sse typeMap is keyed
// by reflect.Type, so two map entries pointing at the same Go type
// race on map iteration order and the loser's event name silently
// becomes unreachable. Enumerates eventsTypeMap and fails if any
// two entries share a Go type — catching the next time someone
// adds an event variant that reuses an existing payload shape
// without a distinct named type.
//
// Kept separate from TestEndpointEventsStream because it's a
// package-level invariant guard, not a /events behavior scenario.
func TestEventsTypeMapHasDistinctTypes(t *testing.T) {
	t.Parallel()
	seen := make(map[string]string, len(eventsTypeMap))
	for name, val := range eventsTypeMap {
		typeName := reflectTypeName(val)
		if prev, ok := seen[typeName]; ok {
			t.Fatalf(
				"events typeMap collision: %q and %q both map to Go type %q — "+
					"define a distinct named type (e.g. `type DM Text`) so the "+
					"sse typeMap can route both event names",
				prev, name, typeName,
			)
		}
		seen[typeName] = name
	}
}

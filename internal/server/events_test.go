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
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/session"
)

// httptest harness — spins up an httptest.Server backed by the same
// *Server production uses, registers a *session.Session under a
// known radio_id, hands back URL + session so tests can publish
// events and read the SSE stream.
type sseHarness struct {
	t       *testing.T
	srv     *httptest.Server
	session *session.Session
	radioID string
}

func newSSEHarness(t *testing.T) *sseHarness {
	t.Helper()
	s := New(Config{Radios: NewRegistry()})
	sess := session.New(nil, nil, nil)
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

// formatInt64 is a hot-path helper that avoids importing fmt for one
// integer-to-string conversion in test code. strconv would also
// work; this stays dependency-light.
func formatInt64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// TestEventsStreamLiveOnlyNoCursor connects with no cursor; events
// published after the subscribe should land on the stream and carry
// matching SSE id: + JSON event_id fields.
func TestEventsStreamLiveOnlyNoCursor(t *testing.T) {
	h := newSSEHarness(t)

	done := make(chan []sseEvent, 1)
	go func() {
		// Subscribe and read 3 live events.
		done <- readNSSEEvents(t, h.eventsURL(""), nil, 3, 3*time.Second)
	}()
	// Give the subscribe a moment to land in the registry's fan-out.
	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 3; i++ {
		h.publishOne("hi")
	}

	events := <-done
	if got := len(events); got != 3 {
		t.Fatalf("len(events) = %d, want 3", got)
	}
	for i, ev := range events {
		wantID := formatInt64(int64(i + 1))
		if ev.id != wantID {
			t.Fatalf("event[%d].id = %q, want %q (SSE id: line)", i, ev.id, wantID)
		}
		if ev.event != "text" {
			t.Fatalf("event[%d].event = %q, want \"text\"", i, ev.event)
		}
	}
}

// TestEventsStreamReplaysFromQueryCursor — ?since=N returns events
// N+1..head from the ring buffer, then continues live. The core
// resumption case.
func TestEventsStreamReplaysFromQueryCursor(t *testing.T) {
	h := newSSEHarness(t)

	for i := 0; i < 5; i++ {
		h.publishOne("history")
	}

	events := readNSSEEvents(t, h.eventsURL("since=2"), nil, 3, 3*time.Second)
	if got := len(events); got != 3 {
		t.Fatalf("len(events) = %d, want 3 (events 3, 4, 5)", got)
	}
	wantIDs := []string{"3", "4", "5"}
	for i, ev := range events {
		if ev.id != wantIDs[i] {
			t.Fatalf("event[%d].id = %q, want %q", i, ev.id, wantIDs[i])
		}
	}
}

// TestEventsStreamReplaysFromLastEventIDHeader — same replay
// behavior, but driven by the EventSource standard Last-Event-ID
// header instead of an explicit query param.
func TestEventsStreamReplaysFromLastEventIDHeader(t *testing.T) {
	h := newSSEHarness(t)

	for i := 0; i < 5; i++ {
		h.publishOne("history")
	}

	events := readNSSEEvents(
		t, h.eventsURL(""), map[string]string{"Last-Event-ID": "3"}, 2, 3*time.Second,
	)
	if got := len(events); got != 2 {
		t.Fatalf("len(events) = %d, want 2 (events 4, 5)", got)
	}
	if events[0].id != "4" || events[1].id != "5" {
		t.Fatalf(
			"got ids %q,%q; want 4,5",
			events[0].id, events[1].id,
		)
	}
}

// TestEventsStreamSinceTakesPriorityOverHeader — when both ?since=
// and Last-Event-ID are supplied, the explicit query param wins (a
// deliberate seek beats the auto-tracked reconnect cursor).
func TestEventsStreamSinceTakesPriorityOverHeader(t *testing.T) {
	h := newSSEHarness(t)

	for i := 0; i < 5; i++ {
		h.publishOne("history")
	}

	events := readNSSEEvents(
		t, h.eventsURL("since=4"), map[string]string{"Last-Event-ID": "1"}, 1, 3*time.Second,
	)
	if got := len(events); got != 1 {
		t.Fatalf("len(events) = %d, want 1 (only event 5; query ?since=4 wins over header=1)", got)
	}
	if events[0].id != "5" {
		t.Fatalf("event id = %q, want 5", events[0].id)
	}
}

// TestEventsStreamCursorBeyondHeadLiveOnly — cursor past the highest
// id replays nothing; the live stream still delivers subsequent
// events. Covers the "client thinks it's caught up but daemon
// restarted and lost the counter" path.
func TestEventsStreamCursorBeyondHeadLiveOnly(t *testing.T) {
	h := newSSEHarness(t)

	for i := 0; i < 3; i++ {
		h.publishOne("history")
	}

	done := make(chan []sseEvent, 1)
	go func() {
		done <- readNSSEEvents(t, h.eventsURL("since=999999"), nil, 1, 3*time.Second)
	}()
	time.Sleep(50 * time.Millisecond)
	h.publishOne("after-cursor")

	events := <-done
	if got := len(events); got != 1 {
		t.Fatalf("len(events) = %d, want 1 (only the post-cursor live event)", got)
	}
	if events[0].id != "4" {
		t.Fatalf("event id = %q, want 4", events[0].id)
	}
}

// TestEventsStreamSinceZeroReplaysWholeBuffer — ?since=0 is the
// "I've never seen anything" cursor; replays everything still in the
// ring buffer.
func TestEventsStreamSinceZeroReplaysWholeBuffer(t *testing.T) {
	h := newSSEHarness(t)

	for i := 0; i < 4; i++ {
		h.publishOne("history")
	}

	events := readNSSEEvents(t, h.eventsURL("since=0"), nil, 4, 3*time.Second)
	if got := len(events); got != 4 {
		t.Fatalf("len(events) = %d, want 4 (whole buffer)", got)
	}
	wantIDs := []string{"1", "2", "3", "4"}
	for i, ev := range events {
		if ev.id != wantIDs[i] {
			t.Fatalf("event[%d].id = %q, want %q", i, ev.id, wantIDs[i])
		}
	}
}

// TestEventsStreamReturns404ForUnknownRadio — the SSE handler can't
// emit huma.Error404NotFound the usual way (the response is already
// committed once sse.Register hands us the writer), so the bail-out
// path logs + returns. Verifies the HTTP layer's resolve happens
// BEFORE we hit that path — a request to an unregistered radio_id
// should fail at request setup, not stream-then-bail.
//
// Today this hits the SSE-handler-internal warn-and-return path
// because Huma's sse.Register doesn't run the same error-translation
// pipeline as huma.Register. Documents the current behavior so a
// future refactor that fixes this surfaces as a test failure rather
// than silent breakage.
func TestEventsStreamReturns404ForUnknownRadio(t *testing.T) {
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
	// Stream opens 200 (response headers committed before handler
	// runs) but emits zero events before timing out. If we ever flip
	// this to a 404 at request setup, change the assertion to
	// `resp.StatusCode == 404`.
	body, _ := io.ReadAll(resp.Body)
	if got := strings.TrimSpace(string(body)); got != "" {
		t.Fatalf("unknown radio leaked event data: %q", got)
	}
}

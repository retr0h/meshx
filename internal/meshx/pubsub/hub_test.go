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

package pubsub

import (
	"sync"
	"testing"
	"time"
)

func TestHubFanOut(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch1, _ := h.Subscribe(4)
	ch2, _ := h.Subscribe(4)

	delivered := h.Publish(Event{RadioID: "r1", Body: "hello"})
	if delivered != 2 {
		t.Fatalf("expected delivery to 2 subscribers, got %d", delivered)
	}

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case e := <-ch:
			if e.RadioID != "r1" || e.Body != "hello" {
				t.Errorf("subscriber %d got wrong event: %+v", i, e)
			}
		case <-time.After(50 * time.Millisecond):
			t.Errorf("subscriber %d timed out", i)
		}
	}
}

func TestHubDropsOnFullBuffer(t *testing.T) {
	h := NewHub()
	defer h.Close()

	// buffer=1, never drained — second publish should drop, not block.
	_, _ = h.Subscribe(1)

	h.Publish(Event{RadioID: "r1", Body: "first"})
	delivered := h.Publish(Event{RadioID: "r1", Body: "second"})
	if delivered != 0 {
		t.Errorf("expected drop (0 delivered), got %d", delivered)
	}
}

func TestHubUnsubscribeStopsDelivery(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch, unsub := h.Subscribe(4)
	unsub()

	delivered := h.Publish(Event{RadioID: "r1", Body: "after-unsub"})
	if delivered != 0 {
		t.Errorf("expected 0 delivered after unsub, got %d", delivered)
	}
	// Channel should be closed.
	if _, ok := <-ch; ok {
		t.Errorf("channel should be closed after unsubscribe")
	}
}

func TestHubUnsubscribeIdempotent(_ *testing.T) {
	h := NewHub()
	defer h.Close()
	_, unsub := h.Subscribe(1)
	unsub()
	unsub() // must not panic
}

func TestHubCloseClosesSubscribers(t *testing.T) {
	h := NewHub()
	ch, _ := h.Subscribe(4)
	h.Close()

	// Range over closed channel terminates cleanly.
	for range ch {
		t.Errorf("got event from closed hub")
	}
}

func TestHubConcurrentPublishSubscribe(_ *testing.T) {
	// Smoke test: race detector should be clean under -race. The test
	// driver runs this with -race in CI; nothing here asserts beyond
	// "doesn't deadlock or trigger the detector."
	h := NewHub()
	defer h.Close()

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, unsub := h.Subscribe(8)
			defer unsub()
			drainDeadline := time.After(100 * time.Millisecond)
			for {
				select {
				case <-ch:
				case <-drainDeadline:
					return
				}
			}
		}()
	}
	for i := range 50 {
		h.Publish(Event{RadioID: "r1", Body: i})
	}
	wg.Wait()
}

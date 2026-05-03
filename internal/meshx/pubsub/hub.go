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

// Package pubsub is the in-process fan-out hub for radio events. The
// pump publishes every decoded FromRadio packet here; subscribers
// (the TUI, the future HTTP daemon, any logging/telemetry consumer)
// each get their own buffered channel and consume independently.
//
// The hub is the architectural seam that lets meshx grow from
// "single-consumer TUI" into "daemon with N consumers" without
// touching transport or pump code. Every Event carries the RadioID
// it came from so multi-radio operation Just Works once the daemon
// holds more than one Client at once.
package pubsub

import (
	"sync"
)

// Event is one decoded radio packet plus the RadioID it came from.
// Body is whatever typed `radio*Msg` value the pump produced (kept
// `any` here so this package doesn't import the parent meshx
// package — the parent imports pubsub, not the other way around).
//
// A subscriber receiving an Event with an unfamiliar Body type
// should ignore it: the contract is "you'll see every event from
// every radio, filter to the ones you care about." Type-switching
// at the consumer is intentionally the consumer's problem.
type Event struct {
	RadioID string
	Body    any
}

// Hub is the fan-out broker. Zero value is unusable; construct via
// NewHub. The hub is goroutine-safe — Publish is called from the
// pump goroutine, Subscribe / Unsubscribe from the model + future
// HTTP handlers, all concurrently.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[int]chan Event
	nextID      int
	closed      bool
}

// NewHub constructs an empty hub.
func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[int]chan Event),
	}
}

// Subscribe registers a new consumer and returns its receive channel
// plus an unsubscribe function. buffer is the per-subscriber channel
// capacity — events arriving when the buffer is full are DROPPED
// (not blocked) so a slow / paused consumer can never stall the
// pump or starve other subscribers. 64 is a reasonable default for
// bursty radio traffic; the TUI uses something on that order.
//
// Always pair with `defer unsubscribe()` at the call site so a
// crashing consumer doesn't leak its slot in the subscribers map.
// Calling unsubscribe more than once is safe (idempotent).
func (h *Hub) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer < 1 {
		buffer = 1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		// Closed hub returns a closed channel + a no-op unsubscribe
		// so the consumer's range loop terminates cleanly without
		// special-casing post-close.
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	id := h.nextID
	h.nextID++
	ch := make(chan Event, buffer)
	h.subscribers[id] = ch
	unsubscribe := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if existing, ok := h.subscribers[id]; ok {
			close(existing)
			delete(h.subscribers, id)
		}
	}
	return ch, unsubscribe
}

// Publish broadcasts e to every current subscriber. Subscribers
// whose buffer is full silently miss this event — that's the
// "slow consumer can't stall the pump" property. Returns the
// number of subscribers the event was successfully delivered to
// (useful for tests + the future /metrics endpoint).
//
// Publish is safe to call after Close; it just becomes a no-op.
func (h *Hub) Publish(e Event) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed {
		return 0
	}
	delivered := 0
	for _, ch := range h.subscribers {
		select {
		case ch <- e:
			delivered++
		default:
			// Subscriber buffer full — drop. We don't log here
			// because Publish runs on the hot path; consumers that
			// care can expose their own drop counter via a
			// /metrics-style endpoint later.
		}
	}
	return delivered
}

// Close shuts the hub down. All subscriber channels are closed so
// every range-loop consumer terminates cleanly. Subsequent Publish
// calls become no-ops; subsequent Subscribe calls return a closed
// channel. Idempotent.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for id, ch := range h.subscribers {
		close(ch)
		delete(h.subscribers, id)
	}
}

// SubscriberCount reports the live consumer count. Used by tests
// and the future /metrics endpoint; the model itself doesn't care.
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

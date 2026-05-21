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

// Package bus provides a simple in-process publish/subscribe event bus.
// It is intentionally small: one Bus type, three methods (Subscribe,
// Unsubscribe, Publish), no generics, no reflection.
//
// Slow consumers that can't keep up with the publish rate have their
// events dropped rather than blocking the publisher — meshx's primary
// concern is real-time radio telemetry, so lossiness under load is
// preferable to back-pressure stalls.
package bus

import "sync"

// Event is the opaque payload type that flows through the bus. Callers
// use type assertions on the receiving end to dispatch to the concrete
// event type.
type Event any

// Bus is a fan-out publish/subscribe hub. The zero value is NOT usable;
// construct via New.
type Bus struct {
	mu   sync.RWMutex
	subs []chan Event
}

// New returns a ready-to-use *Bus.
func New() *Bus {
	return &Bus{}
}

// Subscribe returns a new channel that will receive events published to
// the bus. bufSize controls the channel buffer depth; a value of 0 makes
// the channel unbuffered (Publish blocks until the receiver drains it —
// generally not recommended). Use a modest positive value (e.g. 64) for
// consumers that may fall behind briefly.
func (b *Bus) Subscribe(bufSize int) <-chan Event {
	ch := make(chan Event, bufSize)
	b.mu.Lock()
	b.subs = append(b.subs, ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes ch from the bus and closes it. Calling Unsubscribe
// with a channel that was not created by this bus is a no-op. After
// Unsubscribe returns, the channel will be closed and any pending values
// can still be drained by the receiver.
func (b *Bus) Unsubscribe(ch <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.subs {
		if s == ch {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			close(s)
			return
		}
	}
}

// Publish sends ev to every current subscriber. If a subscriber's channel
// buffer is full the event is dropped for that subscriber (non-blocking
// send). Publish acquires only a read-lock so concurrent Publish calls do
// not serialize against each other; Subscribe and Unsubscribe do a brief
// write-lock to mutate the slice.
func (b *Bus) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// slow consumer — drop event rather than stalling the publisher.
		}
	}
}

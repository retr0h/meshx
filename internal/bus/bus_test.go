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

package bus_test

import (
	"sync"
	"testing"
	"time"

	"github.com/retr0h/meshx/internal/bus"
)

func TestNew(t *testing.T) {
	b := bus.New()
	if b == nil {
		t.Fatal("New() returned nil")
	}
}

func TestSubscribeReceivesPublished(t *testing.T) {
	b := bus.New()
	ch := b.Subscribe(1)

	b.Publish("hello")

	select {
	case got := <-ch:
		if got != "hello" {
			t.Fatalf("got %v, want %q", got, "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestMultipleSubscribersAllReceive(t *testing.T) {
	b := bus.New()
	const n = 5
	chs := make([]<-chan bus.Event, n)
	for i := range chs {
		chs[i] = b.Subscribe(1)
	}

	b.Publish(42)

	for i, ch := range chs {
		select {
		case got := <-ch:
			if got != 42 {
				t.Fatalf("sub %d: got %v, want 42", i, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("sub %d: timed out", i)
		}
	}
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	b := bus.New()
	ch := b.Subscribe(1)
	b.Unsubscribe(ch)

	// Channel must be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed, got value")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out — channel not closed")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	b := bus.New()
	ch := b.Subscribe(4)

	b.Publish("before")
	b.Unsubscribe(ch)
	b.Publish("after")

	// Drain the one pre-unsubscribe event.
	select {
	case got := <-ch:
		if got != "before" {
			t.Fatalf("got %v, want %q", got, "before")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pre-unsubscribe event")
	}

	// The channel is closed; the "after" event must not arrive.
	select {
	case v, ok := <-ch:
		if ok {
			t.Fatalf("unexpected value after unsubscribe: %v", v)
		}
		// ok==false means closed — acceptable.
	default:
		// Nothing pending — also fine.
	}
}

func TestUnsubscribeNonMemberIsNoop(_ *testing.T) {
	b := bus.New()
	other := make(chan bus.Event)
	// Should not panic.
	b.Unsubscribe(other)
}

func TestSlowConsumerDropsEvent(t *testing.T) {
	b := bus.New()
	// Buffer of 0 → every send would block; we test with buffer 1 and
	// send 2 events — only the first fits, second is dropped.
	ch := b.Subscribe(1)

	b.Publish("first")
	b.Publish("dropped")

	got := <-ch
	if got != "first" {
		t.Fatalf("got %v, want %q", got, "first")
	}
	// Nothing else should be pending.
	select {
	case v := <-ch:
		t.Fatalf("unexpected second event: %v", v)
	default:
	}
}

func TestPublishMultipleEvents(t *testing.T) {
	b := bus.New()
	ch := b.Subscribe(10)

	want := []int{1, 2, 3, 4, 5}
	for _, v := range want {
		b.Publish(v)
	}

	for i, w := range want {
		select {
		case got := <-ch:
			if got != w {
				t.Fatalf("event %d: got %v, want %d", i, got, w)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out at event %d", i)
		}
	}
}

func TestConcurrentPublish(t *testing.T) {
	b := bus.New()
	const publishers = 10
	const msgsEach = 100
	ch := b.Subscribe(publishers * msgsEach)

	var wg sync.WaitGroup
	wg.Add(publishers)
	for i := 0; i < publishers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < msgsEach; j++ {
				b.Publish(id*msgsEach + j)
			}
		}(i)
	}
	wg.Wait()

	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != publishers*msgsEach {
		t.Fatalf("got %d events, want %d", count, publishers*msgsEach)
	}
}

func TestConcurrentSubscribeUnsubscribe(_ *testing.T) {
	b := bus.New()
	const goroutines = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			ch := b.Subscribe(4)
			b.Publish("x")
			b.Unsubscribe(ch)
		}()
	}
	wg.Wait()
}

func TestPublishNoSubscribersIsNoop(_ *testing.T) {
	b := bus.New()
	// Should not panic.
	b.Publish("nobody home")
}

func TestEventTypesArbitrary(t *testing.T) {
	type myEvent struct{ N int }
	b := bus.New()
	ch := b.Subscribe(1)

	b.Publish(myEvent{N: 99})

	select {
	case got := <-ch:
		ev, ok := got.(myEvent)
		if !ok {
			t.Fatalf("type assertion failed: %T", got)
		}
		if ev.N != 99 {
			t.Fatalf("got N=%d, want 99", ev.N)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

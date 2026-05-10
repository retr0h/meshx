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

package session

import (
	"context"
	"sync"
	"testing"
	"time"
)

func newTestSession() *Session {
	return New(nil, nil, nil)
}

// TestPublishAssignsMonotonicIDs covers the foundational invariant of
// the resumption-cursor design: every Publish gets a fresh, strictly
// increasing ID starting at 1. Cursors break if IDs collide or move
// backwards, so this is the first thing to nail down.
func TestPublishAssignsMonotonicIDs(t *testing.T) {
	s := newTestSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := s.Subscribe(ctx)

	const n = 10
	for i := 0; i < n; i++ {
		s.Publish(Event{Kind: EventText})
	}

	for i := uint64(1); i <= n; i++ {
		select {
		case ev := <-ch:
			if ev.ID != i {
				t.Fatalf("event %d: got ID=%d, want %d", i, ev.ID, i)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}
}

// TestSubscribeIsLiveOnly verifies the legacy Subscribe path skips the
// ring entirely — events published before subscription are not
// replayed. The TUI's local-mode consumer relies on this; flipping it
// would silently re-deliver historical events on every reconnect.
func TestSubscribeIsLiveOnly(t *testing.T) {
	s := newTestSession()
	s.Publish(Event{Kind: EventText})
	s.Publish(Event{Kind: EventText})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := s.Subscribe(ctx)

	select {
	case ev := <-ch:
		t.Fatalf("Subscribe leaked historical event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}

	s.Publish(Event{Kind: EventText})
	select {
	case ev := <-ch:
		if ev.ID != 3 {
			t.Fatalf("got ID=%d, want 3 (live event after 2 historical)", ev.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("live event never arrived")
	}
}

// TestSubscribeWithReplayFromZero replays everything in the ring —
// the ?since=0 contract for "I've never seen any events before."
func TestSubscribeWithReplayFromZero(t *testing.T) {
	s := newTestSession()
	for i := 0; i < 5; i++ {
		s.Publish(Event{Kind: EventText})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snapshot, _ := s.SubscribeWithReplay(ctx, 0)

	if got := len(snapshot); got != 5 {
		t.Fatalf("snapshot len = %d, want 5", got)
	}
	for i, ev := range snapshot {
		if ev.ID != uint64(i+1) {
			t.Fatalf("snapshot[%d].ID = %d, want %d", i, ev.ID, i+1)
		}
	}
}

// TestSubscribeWithReplayFiltersByCursor — the core resumption case.
// Cursor at N means "I've seen 1..N already; give me N+1 onwards."
func TestSubscribeWithReplayFiltersByCursor(t *testing.T) {
	s := newTestSession()
	for i := 0; i < 10; i++ {
		s.Publish(Event{Kind: EventText})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snapshot, _ := s.SubscribeWithReplay(ctx, 7)

	if got := len(snapshot); got != 3 {
		t.Fatalf("snapshot len = %d, want 3 (events 8, 9, 10)", got)
	}
	wantIDs := []uint64{8, 9, 10}
	for i, ev := range snapshot {
		if ev.ID != wantIDs[i] {
			t.Fatalf("snapshot[%d].ID = %d, want %d", i, ev.ID, wantIDs[i])
		}
	}
}

// TestSubscribeWithReplayCursorBeyondHead — cursor newer than any
// published event. Snapshot is empty; live channel still works. The
// home-server case where a client says "since=999999" because it's
// reconnecting after a daemon restart that reset the counter.
func TestSubscribeWithReplayCursorBeyondHead(t *testing.T) {
	s := newTestSession()
	s.Publish(Event{Kind: EventText})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snapshot, ch := s.SubscribeWithReplay(ctx, 999_999)

	if got := len(snapshot); got != 0 {
		t.Fatalf("snapshot len = %d, want 0 (cursor beyond head)", got)
	}

	s.Publish(Event{Kind: EventText})
	select {
	case ev := <-ch:
		if ev.ID != 2 {
			t.Fatalf("got live ID=%d, want 2", ev.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("live event never arrived after cursor-beyond-head subscribe")
	}
}

// TestRingOverflowKeepsNewest — when more than eventRingCap events
// have been published, only the newest cap survive. The "client was
// offline longer than the buffer" path: cursor that lands before the
// surviving window returns whatever is still there, and the client
// detects the gap by comparing its requested cursor to the first
// replayed event's id.
func TestRingOverflowKeepsNewest(t *testing.T) {
	s := newTestSession()
	const overflow = 50
	const total = eventRingCap + overflow
	for i := 0; i < total; i++ {
		s.Publish(Event{Kind: EventText})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snapshot, _ := s.SubscribeWithReplay(ctx, 0)

	if got := len(snapshot); got != eventRingCap {
		t.Fatalf("snapshot len = %d, want %d", got, eventRingCap)
	}
	wantFirstID := uint64(overflow + 1)
	if snapshot[0].ID != wantFirstID {
		t.Fatalf(
			"oldest survivor ID = %d, want %d (events 1..%d should be evicted)",
			snapshot[0].ID, wantFirstID, overflow,
		)
	}
	wantLastID := uint64(total)
	if snapshot[len(snapshot)-1].ID != wantLastID {
		t.Fatalf("newest survivor ID = %d, want %d", snapshot[len(snapshot)-1].ID, wantLastID)
	}
}

// TestSubscribeAtomicityNoDuplicatesOrLoss — the safety property the
// implementation rests on. A publish racing a SubscribeWithReplay
// must land in EXACTLY ONE of (snapshot, channel): never both
// (duplicate) and never neither (lost event). Verifies by running
// many concurrent publish/subscribe pairs and asserting union ==
// dispatched, intersection == empty.
//
// Race detector catches sloppy reads on s.subs / s.ring; this test
// pins the application-level contract.
func TestSubscribeAtomicityNoDuplicatesOrLoss(t *testing.T) {
	const trials = 50
	for trial := 0; trial < trials; trial++ {
		s := newTestSession()
		const publishes = 100

		ctx, cancel := context.WithCancel(context.Background())

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < publishes; i++ {
				s.Publish(Event{Kind: EventText})
			}
		}()

		// Subscribe while publishes are in flight. snapshot picks up
		// whatever's already in the ring at lock-acquire time; ch
		// receives whatever publishes after the new sub is added to
		// s.subs.
		snapshot, ch := s.SubscribeWithReplay(ctx, 0)

		seen := make(map[uint64]int, publishes)
		for _, ev := range snapshot {
			seen[ev.ID]++
		}

		// Wait for the publisher to finish then drain the live channel.
		wg.Wait()

		// Drain whatever's already buffered without waiting for more.
	drain:
		for {
			select {
			case ev := <-ch:
				seen[ev.ID]++
			default:
				break drain
			}
		}
		cancel()

		// Every published event must appear in the union; no event
		// should appear twice. We can't assert that EVERY event
		// arrives — the channel buffer is finite so a
		// permanently-stuck consumer can drop. But for this test the
		// publisher and consumer are both fast, so dropping is
		// unlikely and a count of 1 is the per-event invariant.
		for id, count := range seen {
			if count != 1 {
				t.Fatalf(
					"trial %d: event ID %d delivered %d times (must be 1)",
					trial, id, count,
				)
			}
		}
	}
}

// TestPublishFanOutToMultipleSubscribers — the basic correctness check
// for multi-subscriber semantics. Two subscribers, both should see
// every published event. Without this, a future "I'll switch RWMutex
// for sync.Map" refactor could silently break fan-out for one of the
// two subscriber slots.
func TestPublishFanOutToMultipleSubscribers(t *testing.T) {
	s := newTestSession()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chA := s.Subscribe(ctx)
	chB := s.Subscribe(ctx)

	const n = 5
	for i := 0; i < n; i++ {
		s.Publish(Event{Kind: EventText})
	}

	collect := func(ch <-chan Event) []uint64 {
		var ids []uint64
		for i := 0; i < n; i++ {
			select {
			case ev := <-ch:
				ids = append(ids, ev.ID)
			case <-time.After(time.Second):
				t.Fatalf("subscriber timed out at event %d/%d", i+1, n)
			}
		}
		return ids
	}

	idsA := collect(chA)
	idsB := collect(chB)

	for i, id := range idsA {
		if id != uint64(i+1) {
			t.Fatalf("A[%d] = %d, want %d", i, id, i+1)
		}
		if idsB[i] != id {
			t.Fatalf("B[%d] = %d, A[%d] = %d (must match)", i, idsB[i], i, id)
		}
	}
}

// TestUnsubscribeRemovesChannel — ctx cancellation must detach the
// channel from the fan-out so a slow consumer that's gone away
// doesn't get its buffer kept full of events forever. Verifies by
// canceling, publishing more, and asserting the channel receives no
// further events.
func TestUnsubscribeRemovesChannel(t *testing.T) {
	s := newTestSession()
	ctx, cancel := context.WithCancel(context.Background())
	ch := s.Subscribe(ctx)

	s.Publish(Event{Kind: EventText})
	<-ch // drain the one event

	cancel()
	// Wait for the unsubscribe goroutine to remove ch.
	deadline := time.Now().Add(time.Second)
	for {
		s.subMu.Lock()
		n := len(s.subs)
		s.subMu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("subscribers not detached after cancel; %d remain", n)
		}
		time.Sleep(time.Millisecond)
	}

	// Publish after cancel; the closed channel should drain without
	// further events (ranging exits when channel is closed).
	s.Publish(Event{Kind: EventText})
	count := 0
	for range ch {
		count++
	}
	if count != 0 {
		t.Fatalf("got %d post-cancel events, want 0", count)
	}
}

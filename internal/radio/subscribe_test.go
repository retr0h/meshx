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

package radio

import (
	"context"
	"sync"
	"testing"
	"time"
)

func newTestSession() *Session {
	return New(nil, nil, nil)
}

// TestSessionSubscribe exercises Session.Subscribe — the legacy
// live-only fan-out path the local TUI uses. Each scenario has
// genuinely divergent mechanics (channel-drain timing, ctx cancel
// races, fan-out multiplexing) so they live as t.Run sub-tests under
// one parent rather than rows in a single table.
func TestSessionSubscribe(t *testing.T) {
	t.Run("live-only-skips-pre-subscribe", func(t *testing.T) {
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
	})

	t.Run("publish-assigns-monotonic-ids-from-1", func(t *testing.T) {
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
	})

	t.Run("fans-out-to-multiple-subscribers", func(t *testing.T) {
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
	})

	t.Run("ctx-cancel-detaches-channel", func(t *testing.T) {
		s := newTestSession()
		ctx, cancel := context.WithCancel(context.Background())
		ch := s.Subscribe(ctx)

		s.Publish(Event{Kind: EventText})
		<-ch

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
		// further events.
		s.Publish(Event{Kind: EventText})
		count := 0
		for range ch {
			count++
		}
		if count != 0 {
			t.Fatalf("got %d post-cancel events, want 0", count)
		}
	})
}

// TestSessionSubscribeWithReplay covers the resumable path the SSE
// stream uses. Mechanics are uniform across rows — pre-publish N
// events, subscribe with a cursor, assert snapshot contents — so each
// scenario is a row, not a sub-test.
func TestSessionSubscribeWithReplay(t *testing.T) {
	cases := []struct {
		name        string
		prePublish  int    // events to publish before subscribing
		cursor      uint64 // cursor passed to SubscribeWithReplay
		wantLen     int    // expected len(snapshot)
		wantFirstID uint64 // expected snapshot[0].ID (skipped if wantLen == 0)
		wantLastID  uint64 // expected snapshot[len-1].ID (skipped if wantLen == 0)
	}{
		{
			name:        "cursor-zero-replays-everything-in-ring",
			prePublish:  5,
			cursor:      0,
			wantLen:     5,
			wantFirstID: 1,
			wantLastID:  5,
		},
		{
			name:        "cursor-mid-stream-returns-tail-only",
			prePublish:  10,
			cursor:      7,
			wantLen:     3,
			wantFirstID: 8,
			wantLastID:  10,
		},
		{
			name:       "cursor-beyond-head-returns-empty-snapshot",
			prePublish: 1,
			cursor:     999_999,
			wantLen:    0,
		},
		{
			name:        "ring-overflow-keeps-newest-cap-events",
			prePublish:  eventRingCap + 50,
			cursor:      0,
			wantLen:     eventRingCap,
			wantFirstID: 51, // events 1..50 evicted
			wantLastID:  uint64(eventRingCap + 50),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestSession()
			for i := 0; i < tc.prePublish; i++ {
				s.Publish(Event{Kind: EventText})
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			snapshot, _ := s.SubscribeWithReplay(ctx, tc.cursor)

			if got := len(snapshot); got != tc.wantLen {
				t.Fatalf("snapshot len = %d, want %d", got, tc.wantLen)
			}
			if tc.wantLen == 0 {
				return
			}
			if snapshot[0].ID != tc.wantFirstID {
				t.Fatalf("snapshot[0].ID = %d, want %d", snapshot[0].ID, tc.wantFirstID)
			}
			if snapshot[len(snapshot)-1].ID != tc.wantLastID {
				t.Fatalf(
					"snapshot[last].ID = %d, want %d",
					snapshot[len(snapshot)-1].ID, tc.wantLastID,
				)
			}
			for i, ev := range snapshot {
				want := tc.wantFirstID + uint64(i)
				if ev.ID != want {
					t.Fatalf("snapshot[%d].ID = %d, want %d", i, ev.ID, want)
				}
			}
		})
	}

	// Cursor-beyond-head must still let live events through after the
	// empty-snapshot return — the home-server "since=999999 after a
	// daemon restart that reset the counter" path. Distinct mechanics
	// (asserts on the live channel, not the snapshot) so it's a sub-
	// test under the same parent.
	t.Run("cursor-beyond-head-still-delivers-live-events", func(t *testing.T) {
		s := newTestSession()
		s.Publish(Event{Kind: EventText})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_, ch := s.SubscribeWithReplay(ctx, 999_999)

		s.Publish(Event{Kind: EventText})
		select {
		case ev := <-ch:
			if ev.ID != 2 {
				t.Fatalf("got live ID=%d, want 2", ev.ID)
			}
		case <-time.After(time.Second):
			t.Fatal("live event never arrived after cursor-beyond-head subscribe")
		}
	})
}

// TestSessionPublishSubscribeAtomicity is the safety property the
// resumable-stream design rests on: a publish racing a
// SubscribeWithReplay must land in EXACTLY ONE of (snapshot, channel)
// — never both (duplicate) and never neither (lost). Race detector
// catches sloppy reads on s.subs / s.ring; this test pins the
// application-level contract by running many concurrent
// publish/subscribe pairs and asserting per-event count == 1 across
// the union.
//
// Kept separate from TestSessionSubscribeWithReplay because it's a
// race-property fuzz, not a scenario-shaped table row.
func TestSessionPublishSubscribeAtomicity(t *testing.T) {
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

		wg.Wait()

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

		// Every observed event must appear exactly once across the
		// union. We can't assert every event is observed — the
		// channel buffer is finite so a permanently-stuck consumer
		// can drop — but for fast publishers + consumers like this
		// test, count != 1 is a real bug.
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

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
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// drainKinds reads the channel non-blockingly and returns the
// observed Event.Kind values in order. Lets a table-driven test
// assert "the publish stream produced exactly these event kinds in
// this order" without bookkeeping per row.
func drainKinds(ch <-chan Event, n int, timeout time.Duration) []Event {
	out := make([]Event, 0, n)
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-deadline:
			return out
		}
	}
	return out
}

// seedOutboundRow appends a "mine" message row with the given
// PacketID so ApplyRouting has a target to flip. Mirrors the shape
// RecordOutbound produces in the live path.
func seedOutboundRow(s *Session, packetID uint32) {
	s.State.Messages = append(s.State.Messages, mdl.MessageItem{
		Message: mdl.Message{
			PacketID: packetID,
			Mine:     true,
			Status:   mdl.StatusPending,
		},
	})
}

// TestApplyRoutingPublishesMessageStatus is the single behavior-as-
// table that pins down which Routing replies surface a
// message_status event and which don't. Each case states the input
// (Routing reply + the row's pre-state) and the expected publish
// stream — kinds + final row Status. Adding a new case is one row;
// the assertion logic stays the same.
func TestApplyRoutingPublishesMessageStatus(t *testing.T) {
	t.Parallel()

	type ackerSeed struct {
		nodeNum uint32
		hops    int
	}

	cases := []struct {
		name        string
		packetID    uint32 // outbound row's PacketID; 0 = no row seeded
		seedAckers  []ackerSeed
		routing     mdl.Routing
		wantKinds   []string          // Event.Kind sequence (ordered)
		wantStatus  mdl.MessageStatus // final row.Status
		wantPubStat mdl.MessageStatus // status carried in MessageStatusUpdate
		wantAckers  int               // count of Ackers in the published update
	}{
		{
			name:     "ok-flips-to-ack-and-publishes-update",
			packetID: 100,
			routing: mdl.Routing{
				RequestID: 100,
				OK:        true,
				FromNum:   2066382700,
				Hops:      1,
				At:        time.Unix(1700000000, 0),
			},
			wantKinds:   []string{EventMessageStatus, EventRouting},
			wantStatus:  mdl.StatusAck,
			wantPubStat: mdl.StatusAck,
			wantAckers:  1,
		},
		{
			name:     "fail-flips-to-fail-and-publishes-update",
			packetID: 101,
			routing: mdl.Routing{
				RequestID: 101,
				OK:        false,
				ErrorName: "TIMEOUT",
				FromNum:   2066382700,
				At:        time.Unix(1700000001, 0),
			},
			wantKinds:   []string{EventMessageStatus, EventRouting},
			wantStatus:  mdl.StatusFail,
			wantPubStat: mdl.StatusFail,
			wantAckers:  0,
		},
		{
			name: "no-matching-row-publishes-routing-only",
			// packetID 0 → don't seed a row; Routing reply has no
			// match and falls through without flipping anything.
			routing: mdl.Routing{
				RequestID: 999,
				OK:        true,
				FromNum:   2066382700,
				At:        time.Unix(1700000002, 0),
			},
			wantKinds: []string{EventRouting},
		},
		{
			name:       "second-acker-still-publishes-status-update",
			packetID:   102,
			seedAckers: []ackerSeed{{nodeNum: 100, hops: 1}},
			routing: mdl.Routing{
				RequestID: 102,
				OK:        true,
				FromNum:   200, // different peer — appended to Ackers
				Hops:      2,
				At:        time.Unix(1700000003, 0),
			},
			wantKinds:   []string{EventMessageStatus, EventRouting},
			wantStatus:  mdl.StatusAck,
			wantPubStat: mdl.StatusAck,
			wantAckers:  2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestSession()
			ch := s.Subscribe(t.Context())

			if tc.packetID != 0 {
				seedOutboundRow(s, tc.packetID)
				row := &s.State.Messages[len(s.State.Messages)-1]
				for _, a := range tc.seedAckers {
					row.Ackers = append(row.Ackers, mdl.Acker{
						NodeNum: a.nodeNum,
						Hops:    a.hops,
					})
				}
			}

			s.ApplyRouting(tc.routing)

			events := drainKinds(ch, len(tc.wantKinds), 200*time.Millisecond)
			if got := len(events); got != len(tc.wantKinds) {
				t.Fatalf("len(events) = %d, want %d", got, len(tc.wantKinds))
			}
			for i, want := range tc.wantKinds {
				if events[i].Kind != want {
					t.Fatalf("events[%d].Kind = %q, want %q", i, events[i].Kind, want)
				}
			}

			if tc.packetID != 0 {
				row := s.State.Messages[len(s.State.Messages)-1]
				if row.Status != tc.wantStatus {
					t.Fatalf("row.Status = %q, want %q", row.Status, tc.wantStatus)
				}
			}

			// Validate the published update payload when one was
			// expected. Only the OK + fail paths fire this; the
			// no-match case skips this block.
			if tc.wantPubStat == "" {
				return
			}
			// message_status is published inline; PublishRouting is
			// deferred — so the order on the wire is
			// message_status, then routing.
			updateEv := events[0]
			update, ok := updateEv.Data.(mdl.MessageStatusUpdate)
			if !ok {
				t.Fatalf("Data is %T, want mdl.MessageStatusUpdate", updateEv.Data)
			}
			if update.PacketID != tc.packetID {
				t.Fatalf("update.PacketID = %d, want %d", update.PacketID, tc.packetID)
			}
			if update.Status != tc.wantPubStat {
				t.Fatalf("update.Status = %q, want %q", update.Status, tc.wantPubStat)
			}
			if got := len(update.Ackers); got != tc.wantAckers {
				t.Fatalf("update.Ackers len = %d, want %d", got, tc.wantAckers)
			}
		})
	}
}

// TestApplyRoutingAckersSnapshotIndependentOfRow ensures the Ackers
// slice carried on the published event is a copy, not a live alias
// of the row's Ackers — a later ack on the same row mustn't mutate
// the previously-published event payload.
func TestApplyRoutingAckersSnapshotIndependentOfRow(t *testing.T) {
	t.Parallel()
	s := newTestSession()
	ch := s.Subscribe(t.Context())

	const pid = 200
	seedOutboundRow(s, pid)

	s.ApplyRouting(mdl.Routing{
		RequestID: pid,
		OK:        true,
		FromNum:   100,
		Hops:      1,
		At:        time.Unix(1700000000, 0),
	})

	// Drain the routing + status events from the first apply.
	_ = drainKinds(ch, 2, 200*time.Millisecond)

	// Read the published update via the subscriber channel: we
	// already drained it above, so re-trigger and capture the second
	// publish; the Ackers slice on event #1 should NOT contain the
	// peer that arrived in event #2.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch2 := s.Subscribe(ctx)
	s.ApplyRouting(mdl.Routing{
		RequestID: pid,
		OK:        true,
		FromNum:   200, // second peer
		Hops:      2,
		At:        time.Unix(1700000001, 0),
	})
	events := drainKinds(ch2, 2, 200*time.Millisecond)
	if len(events) < 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	upd, ok := events[0].Data.(mdl.MessageStatusUpdate)
	if !ok {
		t.Fatalf("Data is %T, want mdl.MessageStatusUpdate", events[0].Data)
	}
	if got := len(upd.Ackers); got != 2 {
		t.Fatalf("second-update.Ackers len = %d, want 2", got)
	}
}

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

package radio

import (
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

func newTestSession() *Session {
	return New(nil, nil, nil)
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
	s.State.MessagesByPacketID[packetID] = len(s.State.Messages) - 1
}

// TestSession_ApplyRouting covers the public ApplyRouting surface.
// Scenarios (which routing replies flip a message row, which fall
// through, what status the row lands on) are table rows — uniform
// mechanics. The Ackers roll-up property has genuinely different
// mechanics (two ApplyRouting calls, dedup check) so it runs as a
// t.Run sub-test under the same parent.
func TestSession_ApplyRouting(t *testing.T) {
	t.Parallel()

	type ackerSeed struct {
		nodeNum uint32
		hops    int
	}

	cases := []struct {
		name       string
		packetID   uint32 // outbound row's PacketID; 0 = no row seeded
		seedAckers []ackerSeed
		routing    mdl.Routing
		wantMatch  bool
		wantStatus mdl.MessageStatus // final row.Status; checked only when packetID != 0
		wantAckers int               // count of Ackers on the row after apply
	}{
		{
			name:     "ok-flips-row-to-ack",
			packetID: 100,
			routing: mdl.Routing{
				RequestID: 100,
				OK:        true,
				FromNum:   2066382700,
				Hops:      1,
				At:        time.Unix(1700000000, 0),
			},
			wantMatch:  true,
			wantStatus: mdl.StatusAck,
			wantAckers: 1,
		},
		{
			name:     "fail-flips-row-to-fail",
			packetID: 101,
			routing: mdl.Routing{
				RequestID: 101,
				OK:        false,
				ErrorName: "TIMEOUT",
				FromNum:   2066382700,
				At:        time.Unix(1700000001, 0),
			},
			wantMatch:  true,
			wantStatus: mdl.StatusFail,
			wantAckers: 0,
		},
		{
			// packetID 0 → don't seed a row; Routing reply has no match.
			name: "no-matching-row-returns-unmatched",
			routing: mdl.Routing{
				RequestID: 999,
				OK:        true,
				FromNum:   2066382700,
				At:        time.Unix(1700000002, 0),
			},
			wantMatch: false,
		},
		{
			name:       "second-acker-appended",
			packetID:   102,
			seedAckers: []ackerSeed{{nodeNum: 100, hops: 1}},
			routing: mdl.Routing{
				RequestID: 102,
				OK:        true,
				FromNum:   200, // different peer — appended to Ackers
				Hops:      2,
				At:        time.Unix(1700000003, 0),
			},
			wantMatch:  true,
			wantStatus: mdl.StatusAck,
			wantAckers: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestSession()

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

			res := s.ApplyRouting(tc.routing)

			if res.Matched != tc.wantMatch {
				t.Fatalf("Matched = %v, want %v", res.Matched, tc.wantMatch)
			}

			if tc.packetID == 0 {
				return
			}

			row := s.State.Messages[len(s.State.Messages)-1]
			if row.Status != tc.wantStatus {
				t.Fatalf("row.Status = %q, want %q", row.Status, tc.wantStatus)
			}
			if got := len(row.Ackers); got != tc.wantAckers {
				t.Fatalf("row.Ackers len = %d, want %d", got, tc.wantAckers)
			}
		})
	}

	// RequestID == 0 must return an unmatched result immediately.
	t.Run("zero-request-id-returns-unmatched", func(t *testing.T) {
		s := newTestSession()
		res := s.ApplyRouting(mdl.Routing{RequestID: 0, OK: true})
		if res.Matched {
			t.Fatal("Matched = true for RequestID=0, want false")
		}
	})

	// When the Routing reply comes from MyNodeNum, it must NOT be added
	// to Ackers (local ack-of-send is excluded from the mesh peer list).
	t.Run("local-radio-ack-excluded-from-ackers", func(t *testing.T) {
		s := newTestSession()
		const myNum = uint32(0xdeadbeef)
		s.State.MyNodeNum = myNum
		const pid = uint32(300)
		seedOutboundRow(s, pid)

		s.ApplyRouting(mdl.Routing{
			RequestID: pid,
			OK:        true,
			FromNum:   myNum, // local ack — must be excluded
			Hops:      0,
			At:        time.Unix(1700000010, 0),
		})

		row := s.State.Messages[len(s.State.Messages)-1]
		if len(row.Ackers) != 0 {
			t.Fatalf("Ackers len = %d, want 0 (local ack excluded)", len(row.Ackers))
		}
		if row.Status != mdl.StatusAck {
			t.Fatalf("row.Status = %q, want ack", row.Status)
		}
	})

	// callsignForAck path: acker is a known node with a callsign.
	t.Run("known-node-callsign-used-in-acker", func(t *testing.T) {
		s := newTestSession()
		const pid = uint32(400)
		const peerNum = uint32(0xCAFE)
		seedOutboundRow(s, pid)

		// Seed a known node so callsignForAck resolves the callsign.
		s.State.Nodes = append(s.State.Nodes, mdl.NodeItem{
			NodeNum:  peerNum,
			Callsign: "Mesh-cafe",
		})
		s.State.NodesByNum[peerNum] = len(s.State.Nodes) - 1

		s.ApplyRouting(mdl.Routing{
			RequestID: pid,
			OK:        true,
			FromNum:   peerNum,
			Hops:      1,
			At:        time.Unix(1700000020, 0),
		})

		row := s.State.Messages[len(s.State.Messages)-1]
		if len(row.Ackers) != 1 {
			t.Fatalf("Ackers len = %d, want 1", len(row.Ackers))
		}
		if row.Ackers[0].Callsign != "Mesh-cafe" {
			t.Fatalf("Ackers[0].Callsign = %q, want Mesh-cafe", row.Ackers[0].Callsign)
		}
	})

	// Property: a second ack from a different peer at shorter hops
	// replaces the existing Ackers entry for that peer (shorter wins).
	t.Run("same-peer-shorter-hops-updates-in-place", func(t *testing.T) {
		s := newTestSession()
		const pid = 200
		seedOutboundRow(s, pid)

		// First ack from peer 100 at 3 hops.
		s.ApplyRouting(mdl.Routing{
			RequestID: pid, OK: true, FromNum: 100, Hops: 3,
			At: time.Unix(1700000000, 0),
		})
		// Second ack from the same peer at 1 hop — should update, not append.
		s.ApplyRouting(mdl.Routing{
			RequestID: pid, OK: true, FromNum: 100, Hops: 1,
			At: time.Unix(1700000001, 0),
		})

		row := s.State.Messages[len(s.State.Messages)-1]
		if got := len(row.Ackers); got != 1 {
			t.Fatalf("Ackers len = %d, want 1 (same peer deduped)", got)
		}
		if row.Ackers[0].Hops != 1 {
			t.Fatalf("Ackers[0].Hops = %d, want 1 (shorter hops wins)", row.Ackers[0].Hops)
		}
	})
}

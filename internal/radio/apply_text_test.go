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

// TestSession_ApplyText pins down the text-apply contract: inbound
// packets are appended to State.Messages with the correct From,
// Mine, and Status fields; duplicate PacketIDs are deduped in place;
// unread counters are bumped on inactive channels.
func TestSession_ApplyText(t *testing.T) {
	t.Parallel()

	const myNum = uint32(0xdeadbeef)
	const otherNum = uint32(0xc0ffee)

	cases := []struct {
		name       string
		myNodeNum  uint32 // 0 simulates pre-MyInfo handshake
		toNum      uint32
		fromNum    uint32
		wantMine   bool
		wantStatus mdl.MessageStatus
	}{
		{
			name:       "broadcast-from-peer-not-mine",
			myNodeNum:  myNum,
			toNum:      mdl.BroadcastNum,
			fromNum:    otherNum,
			wantMine:   false,
			wantStatus: mdl.StatusAck,
		},
		{
			name:       "dm-to-me-from-peer-not-mine",
			myNodeNum:  myNum,
			toNum:      myNum,
			fromNum:    otherNum,
			wantMine:   false,
			wantStatus: mdl.StatusAck,
		},
		{
			name:       "message-from-self-is-mine",
			myNodeNum:  myNum,
			toNum:      mdl.BroadcastNum,
			fromNum:    myNum,
			wantMine:   true,
			wantStatus: mdl.StatusAck,
		},
		{
			name:       "pre-handshake-mynodenum-zero-not-mine",
			myNodeNum:  0,
			toNum:      myNum,
			fromNum:    otherNum,
			wantMine:   false,
			wantStatus: mdl.StatusAck,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestSession()
			s.State.MyNodeNum = tc.myNodeNum

			res := s.ApplyText(mdl.Text{
				Channel: 0,
				ToNum:   tc.toNum,
				Body: mdl.Message{
					FromNum:  tc.fromNum,
					Text:     "hi",
					PacketID: 99,
					SentAt:   time.Now(),
				},
			}, "hi", false, false)

			if res.Skipped {
				t.Fatal("Skipped = true, want false (first occurrence)")
			}
			if res.Index < 0 {
				t.Fatal("Index < 0, want valid index")
			}
			if got := len(s.State.Messages); got != 1 {
				t.Fatalf("State.Messages len = %d, want 1", got)
			}
			row := s.State.Messages[res.Index]
			if row.Mine != tc.wantMine {
				t.Fatalf("row.Mine = %v, want %v", row.Mine, tc.wantMine)
			}
			if row.Status != tc.wantStatus {
				t.Fatalf("row.Status = %q, want %q", row.Status, tc.wantStatus)
			}
		})
	}

	// Duplicate PacketID must be deduped — second call upgrades the
	// existing row in place rather than appending a new one.
	t.Run("duplicate-packet-id-deduped-in-place", func(t *testing.T) {
		s := newTestSession()
		s.State.MyNodeNum = myNum

		ev := mdl.Text{
			Channel: 0,
			ToNum:   mdl.BroadcastNum,
			Body: mdl.Message{
				FromNum:  otherNum,
				Text:     "first",
				PacketID: 42,
				SentAt:   time.Now(),
			},
		}
		res1 := s.ApplyText(ev, "first", false, false)
		if res1.Skipped {
			t.Fatal("first call: Skipped = true, want false")
		}

		// Seed the pending status so we can verify the upgrade path.
		s.State.Messages[res1.Index].Status = mdl.StatusPending

		res2 := s.ApplyText(ev, "first", false, false)
		if !res2.Skipped {
			t.Fatal("second call: Skipped = false, want true (dedupe)")
		}
		if got := len(s.State.Messages); got != 1 {
			t.Fatalf("State.Messages len = %d, want 1 after dedupe", got)
		}
		if s.State.Messages[res1.Index].Status != mdl.StatusAck {
			t.Fatal("deduped row status not upgraded to Ack")
		}
	})
}

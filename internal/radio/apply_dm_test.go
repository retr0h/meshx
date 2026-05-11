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
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// TestSessionApplyText pins down the contract that drives every SSE /
// agent consumer of the text path: a DM addressed to MyNodeNum fires
// dm_received; everything else (broadcasts, DMs to other peers we
// overhear, pre-handshake packets where MyNodeNum is still 0) fires
// text. Mutual exclusion is the value here — consumers subscribe to
// one kind and trust they aren't also picking up the other.
func TestSessionApplyText(t *testing.T) {
	t.Parallel()

	const myNum = uint32(0xdeadbeef)
	const otherNum = uint32(0xc0ffee)

	cases := []struct {
		name      string
		myNodeNum uint32 // 0 simulates pre-MyInfo handshake
		toNum     uint32
		fromNum   uint32
		wantKind  string
	}{
		{
			name:      "broadcast-fires-text",
			myNodeNum: myNum,
			toNum:     mdl.BroadcastNum,
			fromNum:   otherNum,
			wantKind:  EventText,
		},
		{
			name:      "dm-to-me-fires-dm-received",
			myNodeNum: myNum,
			toNum:     myNum,
			fromNum:   otherNum,
			wantKind:  EventDMReceived,
		},
		{
			name:      "dm-between-other-peers-fires-text",
			myNodeNum: myNum,
			toNum:     otherNum,
			fromNum:   0xbabe,
			wantKind:  EventText,
		},
		{
			name:      "pre-handshake-mynodenum-zero-fires-text",
			myNodeNum: 0,
			toNum:     myNum, // would be a DM to me — but we don't know MyNodeNum yet
			fromNum:   otherNum,
			wantKind:  EventText,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestSession()
			s.State.MyNodeNum = tc.myNodeNum
			ch := s.Subscribe(t.Context())

			s.ApplyText(mdl.Text{
				Channel: 0,
				ToNum:   tc.toNum,
				Body: mdl.Message{
					FromNum:  tc.fromNum,
					Text:     "hi",
					PacketID: 99,
					SentAt:   time.Now(),
				},
			}, "hi", false)

			events := drainKinds(ch, 1, 200*time.Millisecond)
			if got := len(events); got != 1 {
				t.Fatalf("len(events) = %d, want 1", got)
			}
			if events[0].Kind != tc.wantKind {
				t.Fatalf("event kind = %q, want %q", events[0].Kind, tc.wantKind)
			}
		})
	}
}

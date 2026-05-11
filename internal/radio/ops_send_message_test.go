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
	"sync"
	"testing"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// sendMessageFakePump captures every command Send dispatches so the
// SendMessage tests can assert on what made it to the wire without a
// real radio. Satisfies the package's Pump consumer interface.
type sendMessageFakePump struct {
	mu       sync.Mutex
	commands []mdl.Command
	nextID   uint32
	rejected bool // when true, Send returns (0, false) — simulates buffer-full
}

func (p *sendMessageFakePump) Send(cmd mdl.Command) (uint32, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rejected {
		return 0, false
	}
	p.commands = append(p.commands, cmd)
	p.nextID++
	return p.nextID, true
}

func (p *sendMessageFakePump) Stop() {}

// TestSession_SendMessage — the consolidated send primitive must
// dispatch one mdl.SendText, append exactly one outbound row, and
// return PacketID + OK + Index in lockstep. Rows cover broadcast / DM
// / ham-bang / pump-rejected (state still gets the pending row).
func TestSession_SendMessage(t *testing.T) {
	cases := []struct {
		name       string
		pumpReject bool
		req        SendMessageRequest
		wantOK     bool
		wantPID    bool   // result.PacketID != 0
		wantToNum  uint32 // expected on the dispatched mdl.SendText
		wantBang   string // expected on the appended row (Bang isn't on the wire)
		wantMine   bool   // appended row's Mine flag
	}{
		{
			name:      "broadcast-plain-chat",
			req:       SendMessageRequest{Channel: 0, Text: "hi mesh"},
			wantOK:    true,
			wantPID:   true,
			wantToNum: 0,
			wantMine:  true,
		},
		{
			name:      "directed-message-carries-to_num",
			req:       SendMessageRequest{Channel: 0, Text: "hi peer", ToNum: 0xc0ffee},
			wantOK:    true,
			wantPID:   true,
			wantToNum: 0xc0ffee,
			wantMine:  true,
		},
		{
			name:     "ham-bang-stamps-row-bang-but-wire-text-unchanged",
			req:      SendMessageRequest{Channel: 1, Text: "QSL TNX", Bang: "/qsl"},
			wantOK:   true,
			wantPID:  true,
			wantBang: "/qsl",
			wantMine: true,
		},
		{
			name:       "pump-rejects-row-still-appended-as-pending",
			pumpReject: true,
			req:        SendMessageRequest{Channel: 0, Text: "queued offline"},
			wantOK:     false,
			wantPID:    false,
			wantMine:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pump := &sendMessageFakePump{nextID: 1000, rejected: tc.pumpReject}
			sess := New(nil, pump, nil)
			sess.State.RadioID = "0xradiotest"
			sess.State.MyNodeNum = 0xdeadbeef
			// Two channels so Channel=1 in the ham-bang row resolves.
			sess.State.Channels = []mdl.ChannelItem{
				{Index: 0, Name: "default", Role: string(mdl.ChannelPrimary)},
				{Index: 1, Name: "ham", Role: string(mdl.ChannelSecondary)},
			}

			before := len(sess.State.Messages)
			res := sess.SendMessage(tc.req)
			after := len(sess.State.Messages)

			if res.OK != tc.wantOK {
				t.Fatalf("OK = %v, want %v", res.OK, tc.wantOK)
			}
			if (res.PacketID != 0) != tc.wantPID {
				t.Fatalf("PacketID = %d, want non-zero=%v", res.PacketID, tc.wantPID)
			}
			if after != before+1 {
				t.Fatalf("State.Messages len delta = %d, want 1", after-before)
			}
			if res.Index != after-1 {
				t.Fatalf("Index = %d, want %d (last appended)", res.Index, after-1)
			}

			row := sess.State.Messages[res.Index]
			if row.Mine != tc.wantMine {
				t.Fatalf("row.Mine = %v, want %v", row.Mine, tc.wantMine)
			}
			if row.Text != tc.req.Text {
				t.Fatalf("row.Text = %q, want %q", row.Text, tc.req.Text)
			}
			if row.Bang != tc.wantBang {
				t.Fatalf("row.Bang = %q, want %q", row.Bang, tc.wantBang)
			}
			if row.PacketID != res.PacketID {
				t.Fatalf(
					"row.PacketID = %d, want %d (must match dispatched PID)",
					row.PacketID, res.PacketID,
				)
			}
			if row.Status != mdl.StatusPending {
				t.Fatalf("row.Status = %v, want StatusPending", row.Status)
			}

			if tc.pumpReject {
				if got := len(pump.commands); got != 0 {
					t.Fatalf("pump.commands = %d, want 0 when rejected", got)
				}
				return
			}

			if got := len(pump.commands); got != 1 {
				t.Fatalf("pump.commands = %d, want exactly 1 dispatched", got)
			}
			sent, ok := pump.commands[0].(mdl.SendText)
			if !ok {
				t.Fatalf("pump.commands[0] type = %T, want mdl.SendText", pump.commands[0])
			}
			if sent.Channel != tc.req.Channel {
				t.Fatalf("SendText.Channel = %d, want %d", sent.Channel, tc.req.Channel)
			}
			if sent.Text != tc.req.Text {
				t.Fatalf("SendText.Text = %q, want %q", sent.Text, tc.req.Text)
			}
			if sent.ToNum != tc.wantToNum {
				t.Fatalf("SendText.ToNum = %#x, want %#x", sent.ToNum, tc.wantToNum)
			}
		})
	}
}

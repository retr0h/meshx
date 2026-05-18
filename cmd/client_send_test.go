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

package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestRunClientSend — `meshx client send`'s testable core dispatches
// a SendMessage call through the SDK and prints a packet-id line.
// The pump-rejected branch (Send returns ok=false) is exercised by
// a radio with no pump attached.
func TestRunClientSend(t *testing.T) {
	cases := []struct {
		name        string
		opts        sendOpts
		wantSubstrs []string
	}{
		{
			name:        "broadcast-no-pump-prints-queued-offline",
			opts:        sendOpts{Text: "hi mesh", Channel: 0},
			wantSubstrs: []string{"queued offline"},
		},
		{
			name: "directed-message-with-to-num-and-channel",
			opts: sendOpts{
				Text:    "hi peer",
				Channel: 1,
				ToNum:   0xc0ffee,
			},
			wantSubstrs: []string{"queued offline"},
		},
		{
			name: "reply-thread-with-reply-id",
			opts: sendOpts{
				Text:    "yes — same here",
				Channel: 0,
				ReplyID: 0xdeadbeef,
			},
			wantSubstrs: []string{"queued offline"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := fakeRadio("0xabcdef01", "/dev/cu.usb")
			c := clientHarness(t, sess)
			var buf bytes.Buffer
			if err := runClientSend(context.Background(), c, &buf, sess.State.RadioID, tc.opts); err != nil {
				t.Fatalf("runClientSend: %v", err)
			}
			got := buf.String()
			for _, want := range tc.wantSubstrs {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\n--- got ---\n%s", want, got)
				}
			}
			// The outbound row must have been recorded in State.Messages
			// even when the pump rejected — this is the "pending entry"
			// invariant SendMessage maintains for offline mode.
			if n := len(sess.State.Messages); n != 1 {
				t.Errorf("State.Messages len = %d, want 1", n)
			}
			if row := sess.State.Messages[0]; row.Text != tc.opts.Text {
				t.Errorf("row.Text = %q, want %q", row.Text, tc.opts.Text)
			}
		})
	}
}

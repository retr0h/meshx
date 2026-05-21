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

package model

import "testing"

// TestCommand_Interface verifies that every concrete command type satisfies
// the Command interface at compile time. If any type drops the isCommand()
// method this file fails to compile, giving an immediate signal in CI.
//
// The test body is intentionally trivial — the value is in the type
// assertions, not in runtime assertions.
func TestCommand_Interface(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cmd  Command
	}{
		{name: "SendText", cmd: SendText{}},
		{name: "SendPing", cmd: SendPing{}},
		{name: "SendTraceroute", cmd: SendTraceroute{}},
		{name: "SetOwner", cmd: SetOwner{}},
		{name: "SetBuzzer", cmd: SetBuzzer{}},
		{name: "SetChannel", cmd: SetChannel{}},
		{name: "DeleteChannel", cmd: DeleteChannel{}},
		{name: "RequestSync", cmd: RequestSync{}},
		{name: "RequestBuzzerConfig", cmd: RequestBuzzerConfig{}},
		{name: "Reboot", cmd: Reboot{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.cmd == nil {
				t.Errorf("%s is nil — does not satisfy Command", tc.name)
			}
		})
	}
}

// TestSendText_Fields verifies that SendText carries its fields through
// construction without zero-value surprises.
func TestSendText_Fields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cmd     SendText
		wantCh  int
		wantTxt string
		wantTo  uint32
		wantRep uint32
	}{
		{
			name:    "broadcast message",
			cmd:     SendText{Channel: 0, Text: "hello mesh", ToNum: 0, ReplyID: 0},
			wantCh:  0,
			wantTxt: "hello mesh",
			wantTo:  0,
			wantRep: 0,
		},
		{
			name:    "direct message with reply thread",
			cmd:     SendText{Channel: 2, Text: "hi", ToNum: 0xdeadbeef, ReplyID: 42},
			wantCh:  2,
			wantTxt: "hi",
			wantTo:  0xdeadbeef,
			wantRep: 42,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.cmd.Channel != tc.wantCh {
				t.Errorf("Channel = %d, want %d", tc.cmd.Channel, tc.wantCh)
			}
			if tc.cmd.Text != tc.wantTxt {
				t.Errorf("Text = %q, want %q", tc.cmd.Text, tc.wantTxt)
			}
			if tc.cmd.ToNum != tc.wantTo {
				t.Errorf("ToNum = %d, want %d", tc.cmd.ToNum, tc.wantTo)
			}
			if tc.cmd.ReplyID != tc.wantRep {
				t.Errorf("ReplyID = %d, want %d", tc.cmd.ReplyID, tc.wantRep)
			}
		})
	}
}

// TestDeleteChannel_Fields verifies DeleteChannel carries its index field.
func TestDeleteChannel_Fields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		cmd       DeleteChannel
		wantIndex int
	}{
		{name: "slot 1", cmd: DeleteChannel{Index: 1}, wantIndex: 1},
		{name: "slot 7", cmd: DeleteChannel{Index: 7}, wantIndex: 7},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.cmd.Index != tc.wantIndex {
				t.Errorf("Index = %d, want %d", tc.cmd.Index, tc.wantIndex)
			}
		})
	}
}

// TestReboot_Fields verifies that the Seconds field is preserved.
func TestReboot_Fields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		cmd         Reboot
		wantSeconds int32
	}{
		{name: "reboot now (0)", cmd: Reboot{Seconds: 0}, wantSeconds: 0},
		{name: "reboot in 5s", cmd: Reboot{Seconds: 5}, wantSeconds: 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.cmd.Seconds != tc.wantSeconds {
				t.Errorf("Seconds = %d, want %d", tc.cmd.Seconds, tc.wantSeconds)
			}
		})
	}
}

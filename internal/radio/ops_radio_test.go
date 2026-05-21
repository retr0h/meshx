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
	"errors"
	"testing"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// radioFakePump satisfies Pump for ops_radio tests.
type radioFakePump struct {
	rejected bool
	nextID   uint32
}

func (p *radioFakePump) Send(mdl.Command) (uint32, bool) {
	if p.rejected {
		return 0, false
	}
	p.nextID++
	return p.nextID, true
}

func (p *radioFakePump) Stop() {}

// TestSession_Ping validates TargetNum=0 rejection, self-ping
// rejection, pump-unavailable, and successful dispatch returning
// a non-zero PacketID.
func TestSession_Ping(t *testing.T) {
	t.Parallel()

	const myNum = uint32(0xdeadbeef)

	cases := []struct {
		name     string
		myNum    uint32
		target   uint32
		pump     Pump
		wantCode int // 0 = success
		wantPID  bool
	}{
		{
			name:     "zero-target-rejected",
			target:   0,
			pump:     &radioFakePump{},
			wantCode: 400,
		},
		{
			name:     "self-ping-rejected",
			myNum:    myNum,
			target:   myNum,
			pump:     &radioFakePump{},
			wantCode: 400,
		},
		{
			name:     "no-pump-unavailable",
			target:   0x1234,
			pump:     nil,
			wantCode: 503,
		},
		{
			name:     "pump-rejected-unavailable",
			target:   0x1234,
			pump:     &radioFakePump{rejected: true},
			wantCode: 503,
		},
		{
			name:    "successful-ping",
			myNum:   myNum,
			target:  0xc0ffee,
			pump:    &radioFakePump{},
			wantPID: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(nil, tc.pump, nil)
			if tc.myNum != 0 {
				s.State.MyNodeNum = tc.myNum
			}

			res, err := s.Ping(PingRequest{TargetNum: tc.target})
			if tc.wantCode != 0 {
				var opErr *OpError
				if !errors.As(err, &opErr) || opErr.Code != tc.wantCode {
					t.Fatalf("want %d OpError, got %v", tc.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantPID && res.PacketID == 0 {
				t.Fatal("PacketID = 0, want non-zero on success")
			}
		})
	}
}

// TestSession_Traceroute mirrors Ping — same validation logic, same
// pump-unavailable path.
func TestSession_Traceroute(t *testing.T) {
	t.Parallel()

	const myNum = uint32(0xdeadbeef)

	cases := []struct {
		name     string
		myNum    uint32
		target   uint32
		pump     Pump
		wantCode int
		wantPID  bool
	}{
		{
			name:     "zero-target-rejected",
			target:   0,
			pump:     &radioFakePump{},
			wantCode: 400,
		},
		{
			name:     "self-traceroute-rejected",
			myNum:    myNum,
			target:   myNum,
			pump:     &radioFakePump{},
			wantCode: 400,
		},
		{
			name:     "no-pump-unavailable",
			target:   0x5678,
			pump:     nil,
			wantCode: 503,
		},
		{
			name:     "pump-rejected-unavailable",
			target:   0x5678,
			pump:     &radioFakePump{rejected: true},
			wantCode: 503,
		},
		{
			name:    "successful-traceroute",
			myNum:   myNum,
			target:  0xabcd,
			pump:    &radioFakePump{},
			wantPID: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(nil, tc.pump, nil)
			if tc.myNum != 0 {
				s.State.MyNodeNum = tc.myNum
			}

			res, err := s.Traceroute(TracerouteRequest{TargetNum: tc.target})
			if tc.wantCode != 0 {
				var opErr *OpError
				if !errors.As(err, &opErr) || opErr.Code != tc.wantCode {
					t.Fatalf("want %d OpError, got %v", tc.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantPID && res.PacketID == 0 {
				t.Fatal("PacketID = 0, want non-zero on success")
			}
		})
	}
}

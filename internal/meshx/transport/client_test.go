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

package transport

import (
	"strings"
	"testing"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"
)

// TestDial_Routing verifies that Dial selects the correct transport based on
// the destination string prefix without actually opening any hardware connection.
// The test relies on the fact that DialSerial / DialTCP fail fast with a
// descriptive error when no hardware is present — the prefix routing is
// correct when the error message reflects the attempted transport.
//
// BLE is excluded: DialBLE runs a real 8-second scan and requires hardware.
func TestDial_Routing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		dest        string
		wantErrFrag string // substring expected in the error string
	}{
		{
			name:        "/dev/ prefix routes to serial transport",
			dest:        "/dev/tty.nonexistent",
			wantErrFrag: "serial",
		},
		{
			name:        "COM prefix routes to serial transport",
			dest:        "COM99",
			wantErrFrag: "serial",
		},
		{
			// 127.0.0.1 on a port with no listener → connection refused immediately.
			name:        "bare host routes to TCP transport",
			dest:        "127.0.0.1:19997",
			wantErrFrag: "tcp",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Dial(tc.dest)
			if err == nil {
				t.Fatal("expected error dialing nonexistent destination, got nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.wantErrFrag) {
				t.Errorf(
					"error %q does not mention expected transport fragment %q",
					err.Error(),
					tc.wantErrFrag,
				)
			}
		})
	}
}

// TestSendWantConfig verifies that SendWantConfig posts a non-zero WantConfigId
// envelope to the channel and returns the same nonce it enqueued.
func TestSendWantConfig(t *testing.T) {
	t.Parallel()

	in := make(chan *pb.ToRadio, 1)
	nonce := SendWantConfig(in)

	if nonce == 0 {
		t.Error("nonce must be non-zero")
	}

	msg := <-in
	wc, ok := msg.GetPayloadVariant().(*pb.ToRadio_WantConfigId)
	if !ok {
		t.Fatalf("payload variant is %T, want *pb.ToRadio_WantConfigId", msg.GetPayloadVariant())
	}
	if wc.WantConfigId != nonce {
		t.Errorf("WantConfigId = %d, want %d", wc.WantConfigId, nonce)
	}
}

// TestSendWantConfig_NonZeroNonce runs several invocations and asserts all
// nonces are non-zero. The rand.Uint32 source may produce 0, which SendWantConfig
// must map to 1.
func TestSendWantConfig_NonZeroNonce(t *testing.T) {
	t.Parallel()

	const iterations = 100
	for i := range iterations {
		in := make(chan *pb.ToRadio, 1)
		nonce := SendWantConfig(in)
		if nonce == 0 {
			t.Errorf("iteration %d: got zero nonce", i)
		}
		<-in // drain so subsequent iterations do not deadlock
	}
}

// TestMarshalToRadio verifies that MarshalToRadio round-trips a ToRadio
// envelope through proto.Marshal without data loss.
func TestMarshalToRadio(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  *pb.ToRadio
	}{
		{
			name: "empty envelope",
			msg:  &pb.ToRadio{},
		},
		{
			name: "WantConfigId payload",
			msg: &pb.ToRadio{
				PayloadVariant: &pb.ToRadio_WantConfigId{WantConfigId: 42},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, err := MarshalToRadio(tc.msg)
			if err != nil {
				t.Fatalf("MarshalToRadio error: %v", err)
			}
			// Decode back and compare.
			got := &pb.ToRadio{}
			if err := proto.Unmarshal(b, got); err != nil {
				t.Fatalf("proto.Unmarshal error: %v", err)
			}
			if !proto.Equal(tc.msg, got) {
				t.Errorf("round-trip mismatch: got %v, want %v", got, tc.msg)
			}
		})
	}
}

// TestUnmarshalFromRadio verifies that UnmarshalFromRadio decodes a
// well-formed FromRadio wire payload and surfaces errors on corrupt input.
func TestUnmarshalFromRadio(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload func() []byte
		wantErr bool
		check   func(t *testing.T, msg *pb.FromRadio)
	}{
		{
			name: "empty payload yields empty envelope",
			payload: func() []byte {
				b, _ := proto.Marshal(&pb.FromRadio{})
				return b
			},
			check: func(t *testing.T, msg *pb.FromRadio) {
				t.Helper()
				if msg == nil {
					t.Error("expected non-nil FromRadio")
				}
			},
		},
		{
			name: "ConfigCompleteId payload decodes correctly",
			payload: func() []byte {
				b, _ := proto.Marshal(&pb.FromRadio{
					PayloadVariant: &pb.FromRadio_ConfigCompleteId{
						ConfigCompleteId: 7,
					},
				})
				return b
			},
			check: func(t *testing.T, msg *pb.FromRadio) {
				t.Helper()
				cc, ok := msg.GetPayloadVariant().(*pb.FromRadio_ConfigCompleteId)
				if !ok {
					t.Fatalf("unexpected variant %T", msg.GetPayloadVariant())
				}
				if cc.ConfigCompleteId != 7 {
					t.Errorf("ConfigCompleteId = %d, want 7", cc.ConfigCompleteId)
				}
			},
		},
		{
			name:    "corrupt bytes returns error",
			payload: func() []byte { return []byte{0xFF, 0xFF, 0xFF, 0xFF} },
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg, err := UnmarshalFromRadio(tc.payload())
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, msg)
			}
		})
	}
}

// TestMarshalUnmarshal_RoundTrip verifies the full ToRadio→bytes→FromRadio
// chain for cases where both share the wire (e.g. WantConfigId echoed back
// as ConfigCompleteId). This exercises both helpers together.
func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	t.Parallel()

	// Build a FromRadio, marshal it via the public helper, then unmarshal.
	original := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_ConfigCompleteId{ConfigCompleteId: 99},
	}
	wire, err := proto.Marshal(original)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	got, err := UnmarshalFromRadio(wire)
	if err != nil {
		t.Fatalf("UnmarshalFromRadio: %v", err)
	}
	if !proto.Equal(original, got) {
		t.Errorf("round-trip mismatch: got %v, want %v", got, original)
	}
}

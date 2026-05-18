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

package pump

import (
	"context"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
)

// Transport is the wire-level bridge the pump consumes — a
// bidirectional stream of Meshtastic protobuf envelopes. Declared
// here at the consumer seam (osapi-io pattern), implemented
// structurally by transport.Serial / transport.TCP / transport.BLE
// returned from transport.Dial. Twin of meshx.Store / meshx.Pump
// (declared where consumed, narrow surface): each consumer interface
// lists only the methods it actually calls so a future producer-side
// addition doesn't bloat the consumer's view.
type Transport interface {
	// Run pumps FromRadio frames to `out` and reads ToRadio envelopes
	// from `in`. Blocks until ctx is cancelled or the connection
	// fails. Returns the first error encountered.
	Run(ctx context.Context, out chan<- *pb.FromRadio, in <-chan *pb.ToRadio) error
	// Close shuts down the underlying connection. Always called from
	// runSession's defer; safe to call on a half-open client.
	Close() error
}

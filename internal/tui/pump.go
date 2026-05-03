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

package tui

import mdl "github.com/retr0h/meshx/internal/meshx/model"

// Pump is the transport ↔ tea bridge surface the meshx TUI consumes —
// the concrete implementation lives in internal/meshx/pump as
// *pump.Pump, cast to this interface at construction in the
// openPumpMsg handler. Defined here (where it's consumed) per the
// osapi-io pattern: each consumer declares only the methods it
// actually calls, so a future daemon package can declare its own
// (likely larger) interface without bloating the TUI's view of the
// bridge.
//
// Methods correspond 1:1 with *pump.Pump's exported methods used by
// the TUI. The compile-time bind `var p Pump = pump.New(...)` at
// the construction site catches drift the moment a method is added
// or renamed.
//
// Send takes typed mdl.Command values — meshx never touches
// gomeshproto on the outbound side, mirroring how the inbound side
// receives mdl.* events. Returns the generated MeshPacket.id for
// commands that allocate one (SendText / SendPing / SendTraceroute);
// returns 0 for fire-and-forget commands. ok is false when the
// outbound buffer is full or the proto build failed.
type Pump interface {
	Send(mdl.Command) (packetID uint32, ok bool)
	// Stop tears down the pump goroutine + closes the live client.
	Stop()
}

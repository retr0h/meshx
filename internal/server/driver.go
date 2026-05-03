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

package server

import (
	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/session"
)

// driver.go — the narrow Driver interface this package consumes,
// declared on the consumer seam per the osapi-io pattern. The HTTP
// handlers depend on *this* shape, not on the concrete
// *internal/driver.Driver — so a test (or a future in-memory shim,
// or a remote-driver-over-grpc variant) can satisfy the seam without
// pulling in the radio transport layer.
//
// Go's structural typing means callers don't `implements Driver` —
// they hand New() a *driver.Driver and the compiler verifies the
// methods line up. New methods get added here first (declare what
// we need) then on the concrete *driver.Driver.

// Driver is the read + dispatch surface the server requires. Grows
// as the data-wiring follow-up moves channel / node / message
// collections off the TUI and onto driver state — Channels(),
// Nodes(), Messages() get added here, then implemented on the
// concrete driver.
type Driver interface {
	// Session returns the canonical per-radio session state.
	// Nil-safe — handlers must check before dereferencing (an
	// uninitialized daemon, or a /healthz hit before the radio
	// attaches, gives nil here).
	Session() *session.Session

	// Send dispatches an outbound mdl.Command via the underlying
	// pump. Returns the allocated MeshPacket.id (zero for
	// fire-and-forget) and ok=false when the pump is nil (no radio
	// attached) or its outbound buffer is full.
	Send(cmd mdl.Command) (uint32, bool)
}

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

// Package driver is the headless radio session layer. It wraps the
// concrete pump (transport ↔ proto bridge) and storage (SQLite
// persistence) along with a *session.Session value, exposing them
// through narrow consumer interfaces (Pump, Store) declared in this
// package per the osapi-io pattern.
//
// Today the TUI is the only consumer — its model holds a *Driver and
// dispatches inbound mdl.X events back to apply* handlers in
// internal/tui/radio.go. MR-3.5c will move those handlers onto
// methods of *Driver so a future meshx serve daemon (MR-4) can drive
// the same Session from HTTP+SSE handlers without dragging Bubble
// Tea in. After that lands, internal/tui shrinks to "render Session
// + emit commands" and the (α) endgame falls into place: standalone
// meshx is a single binary that bundles the Driver + an in-process
// server, and the TUI is just one of its clients.
package driver

import (
	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/session"
)

// Driver is the per-radio session wrapper — owns Pump (outbound +
// reconnect), Store (persistence), and a *session.Session
// (in-memory canonical state). Constructors live in cmd/ where the
// concrete *pump.Pump and *storage.Sqlite are built; consumers
// (today's TUI, tomorrow's meshx serve) bind the result to a narrow
// interface they declare at their own seam.
type Driver struct {
	// Sess is the canonical in-memory state — channels, nodes,
	// messages, telemetry, in-flight ping/tr bookkeeping. Shared by
	// pointer with whatever wires Driver up so the TUI can read state
	// directly while Driver mutates it.
	Sess *session.Session

	// Pump is the outbound + reconnect bridge — Driver.Send forwards
	// here. Nil in demo mode (no transport).
	Pump Pump

	// Store is the persistence handle — Driver writes received +
	// outbound messages here. Nil in demo mode (in-memory only).
	Store Store
}

// New returns a Driver wired with the given Pump, Store, and
// pre-built Session. Callers (cmd/'s tui.RunRadio path today, future
// cmd/serve) construct the concrete *pump.Pump / *storage.Sqlite,
// build a Session, and hand them all in. Nil Pump or Store is
// allowed (demo mode runs entirely in-memory).
func New(s *session.Session, p Pump, st Store) *Driver {
	if s == nil {
		s = session.New()
	}
	return &Driver{Sess: s, Pump: p, Store: st}
}

// Send dispatches an outbound mdl.Command via the Pump. Returns the
// allocated MeshPacket.id (zero for fire-and-forget commands) and
// ok=false when the pump is nil (demo mode) or the outbound buffer
// is full.
func (d *Driver) Send(cmd mdl.Command) (uint32, bool) {
	if d == nil || d.Pump == nil {
		return 0, false
	}
	return d.Pump.Send(cmd)
}

// Stop tears down the live transport and pump goroutines. Idempotent
// — safe to call when Pump is nil. Storage.Close is not invoked here;
// the lifecycle of Store is the caller's concern (RunRadio's defer
// owns it today).
func (d *Driver) Stop() {
	if d == nil || d.Pump == nil {
		return
	}
	d.Pump.Stop()
}

// Session returns the canonical session state. Method (rather than
// just touching d.Sess) lets consumers depend on a narrow interface
// at their own seam — see internal/server/driver.go for the server's
// Driver interface, which uses Session() so a test or future variant
// can satisfy the seam without the concrete struct.
func (d *Driver) Session() *session.Session {
	if d == nil {
		return nil
	}
	return d.Sess
}

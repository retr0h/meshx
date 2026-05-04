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
// persistence) along with a *driver.State value, exposing them
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
)

// Driver is the per-radio session wrapper — owns Pump (outbound +
// reconnect), Store (persistence), and the canonical *State. The
// TUI today and the future meshx serve daemon both read State while
// Driver mutates it through the apply* path.
type Driver struct {
	// State is the canonical in-memory state — channels, nodes,
	// messages, telemetry, in-flight ping/tr bookkeeping. Shared by
	// pointer with consumers; Driver is the sole writer.
	State *State

	// Pump is the outbound + reconnect bridge — Driver.Send forwards
	// here. Nil when no transport is attached.
	Pump Pump

	// Store is the persistence handle. Nil = in-memory only.
	Store Store

	// subState owns the Subscribe/Publish fan-out registry. Embedded
	// so callers can read d.Subscribe / d.Publish at the receiver
	// without indirection.
	subState
}

// New returns a Driver wired with the given Pump, Store, and State.
// A nil State gets a fresh empty one. A nil Pump or Store is allowed
// (the daemon serves an empty session until a radio attaches).
func New(s *State, p Pump, st Store) *Driver {
	if s == nil {
		s = NewState()
	}
	return &Driver{State: s, Pump: p, Store: st}
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

// Session returns the canonical state. Method (rather than direct
// field access) lets consumers depend on a narrow interface at their
// own seam — see internal/server/driver.go for the server's Driver
// interface.
func (d *Driver) Session() *State {
	if d == nil {
		return nil
	}
	return d.State
}

// AttachPump sets the pump handle. Called by the TUI once the tea
// program is running and the transport has been dialed.
func (d *Driver) AttachPump(p Pump) {
	if d == nil {
		return
	}
	d.Pump = p
}

// AttachStore sets the storage handle. Called by newModel after
// storage.New succeeds.
func (d *Driver) AttachStore(s Store) {
	if d == nil {
		return
	}
	d.Store = s
}

// PumpHandle returns the current Pump, which may be nil (demo mode or
// pre-connection). Consumers that need direct Pump access during the
// transition can call this; future follow-ups will replace these call
// sites with higher-level methods on Driver.
func (d *Driver) PumpHandle() Pump {
	if d == nil {
		return nil
	}
	return d.Pump
}

// StoreHandle returns the current Store, which may be nil (in-memory
// mode). Consumers that need direct Store access during the transition
// call this; future follow-ups will replace these with driver methods.
func (d *Driver) StoreHandle() Store {
	if d == nil {
		return nil
	}
	return d.Store
}

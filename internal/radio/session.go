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

// Package radio is the headless per-radio session layer. It wraps
// the concrete pump (transport ↔ proto bridge) and storage (SQLite
// persistence) along with a *State value, exposing them through
// narrow consumer interfaces (Pump, Store) declared in this package
// per the osapi-io pattern.
//
// Both the TUI and the meshx serve daemon hold a *Session and route
// inbound mdl.X events through Apply* methods that mutate the
// canonical State + publish to subscribers, so render layers and
// HTTP+SSE handlers see the same single source of truth. The type
// is named Session (the live, stateful interaction with one radio),
// mirroring ssh.Session / database/sql semantics.
package radio

import (
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// Session is the per-radio session wrapper — owns Pump (outbound +
// reconnect), Store (persistence), and the canonical *State. The
// TUI today and the future meshx serve daemon both read State while
// Session mutates it through the apply* path.
type Session struct {
	// State is the canonical in-memory state — channels, nodes,
	// messages, telemetry, in-flight ping/tr bookkeeping. Shared by
	// pointer with consumers; Session is the sole writer.
	State *State

	// pump is the outbound + reconnect bridge — Session.Send forwards
	// here. Nil when no transport is attached. Unexported on purpose:
	// every external caller goes through Send / AttachPump /
	// PumpHandle so the field stays a Session-private invariant.
	pump Pump

	// store is the persistence handle. Nil = in-memory only.
	// Unexported for the same reason as pump — callers route through
	// PutSetting / SaveNodePrefs / HydrateFromStore / etc.
	store Store

	// OnStoreError fires once per failed Store call inside Apply*
	// and RecordOutbound. Nil = errors are dropped (test fixtures,
	// demo mode). The TUI sets this to surface "-!- storage:
	// degraded" once-per-session; the daemon points it at slog.
	// Session doesn't decide policy — caller does.
	OnStoreError func(error)

	// subState owns the Subscribe/Publish fan-out registry. Embedded
	// so callers can read s.Subscribe / s.Publish at the receiver
	// without indirection.
	subState
}

// storeError centralizes the "did a store call fail; should we
// surface it" check. Apply* methods call this immediately after
// every Store mutation. Nil error / nil callback = no-op.
func (s *Session) storeError(err error) {
	if err == nil || s.OnStoreError == nil {
		return
	}
	s.OnStoreError(err)
}

// PutSetting persists a key/value setting through the Store and
// surfaces the failure (if any) via OnStoreError. radioID="" is the
// global-scope namespace (per-app prefs like "ding_muted"); a
// radio_id keys per-radio prefs. No-op when Store is nil.
func (s *Session) PutSetting(radioID, key, value string) {
	if s.store == nil {
		return
	}
	s.storeError(s.store.PutSetting(radioID, key, value))
}

// SaveNodePrefs persists a peer's favorite / muted toggle through
// the Store and surfaces the failure via OnStoreError. No-op when
// Store is nil.
func (s *Session) SaveNodePrefs(radioID string, nodeNum uint32, favorite, muted bool) {
	if s.store == nil {
		return
	}
	s.storeError(s.store.SaveNodePrefs(radioID, nodeNum, favorite, muted))
}

// AlertStorageError is the canonical OnStoreError implementation
// — appends a permanent "-!- storage: ..." system row to
// State.Messages on the FIRST failure of a session, drops every
// subsequent error so a degraded sqlite handle doesn't machine-gun
// the messages pane. State.StorageAlerted is the gate; flips true
// on first surface.
//
// Callers wire this in via:
//
//	drv.OnStoreError = drv.AlertStorageError
//
// Daemon callers may prefer a slog-only sink instead — they wire
// their own callback that does not touch State.Messages.
func (s *Session) AlertStorageError(err error) {
	if err == nil || s.State.StorageAlerted {
		return
	}
	s.State.StorageAlerted = true
	s.State.Messages = append(s.State.Messages, mdl.MessageItem{
		Message: mdl.Message{
			Time:   time.Now().Format("15:04"),
			Text:   "-!- storage: persistence degraded — " + err.Error(),
			Status: mdl.StatusSystem,
		},
	})
}

// New returns a Session wired with the given Pump, Store, and State.
// A nil State gets a fresh empty one. A nil Pump or Store is allowed
// (the daemon serves an empty session until a radio attaches).
func New(s *State, p Pump, st Store) *Session {
	if s == nil {
		s = NewState()
	}
	return &Session{State: s, pump: p, store: st}
}

// Send dispatches an outbound mdl.Command via the Pump. Returns the
// allocated MeshPacket.id (zero for fire-and-forget commands) and
// ok=false when the pump is nil (demo mode) or the outbound buffer
// is full.
func (s *Session) Send(cmd mdl.Command) (uint32, bool) {
	if s.pump == nil {
		return 0, false
	}
	return s.pump.Send(cmd)
}

// Stop tears down the live transport and pump goroutines. Idempotent
// — safe to call when Pump is nil. Storage.Close is not invoked here;
// the lifecycle of Store is the caller's concern (RunRadio's defer
// owns it today).
func (s *Session) Stop() {
	if s.pump == nil {
		return
	}
	s.pump.Stop()
}

// Snapshot returns the canonical State. Method (rather than direct
// field access) lets consumers depend on a narrow interface at their
// own seam — see internal/server/session.go for the server's Driver
// interface, internal/tui/session.go for the TUI's radioSession.
//
// Named Snapshot rather than Session() because *Session is embedded
// in *sdk.Remote — `r.Session` would resolve to the embedded field
// and shadow a Session() method, which Go silently allows but breaks
// callers expecting the method.
func (s *Session) Snapshot() *State {
	return s.State
}

// AttachPump sets the pump handle. Called by the TUI once the tea
// program is running and the transport has been dialed.
func (s *Session) AttachPump(p Pump) {
	s.pump = p
}

// AttachStore sets the storage handle. Called by newModel after
// storage.New succeeds.
func (s *Session) AttachStore(st Store) {
	s.store = st
}

// PumpHandle returns the current Pump, which may be nil (demo mode or
// pre-connection). Consumers that need direct Pump access during the
// transition can call this; future follow-ups will replace these call
// sites with higher-level methods on Session.
func (s *Session) PumpHandle() Pump {
	return s.pump
}

// StoreHandle returns the current Store, which may be nil (in-memory
// mode). Consumers that need direct Store access during the transition
// call this; future follow-ups will replace these with driver methods.
func (s *Session) StoreHandle() Store {
	return s.store
}

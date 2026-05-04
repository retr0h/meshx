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

import "sync"

// registry.go — the per-radio Driver registry the server multiplexes
// across. Every API path lives under /radios/{radio_id}/… so a single
// `meshx serve` process can host multiple radios simultaneously
// (HT + mobile + base, or a household with several Meshtastic
// devices) and clients address each by its canonical RadioID.
//
// The registry is the obvious shared mutable state in the server —
// HTTP handlers can hit Get concurrently, while attach / detach
// flows (today: cmd/serve startup; future: a /radios POST endpoint
// for hot-attach) mutate it. RWMutex is plenty: Get / List
// (read-mostly) take the read lock, Add / Remove (rare) take the
// write lock.

// Registry is the daemon's radio-id → Driver map. Methods are
// safe for concurrent use across HTTP handlers + lifecycle paths.
type Registry struct {
	mu      sync.RWMutex
	drivers map[string]Driver
}

// NewRegistry returns an empty registry. Drivers attach via Add
// after their pump + handshake reveal a stable RadioID (until
// then RadioID is "pending:<transport>:<addr>" and the driver
// can register under that and re-key once handshake completes —
// the future hot-attach flow handles this).
func NewRegistry() *Registry {
	return &Registry{drivers: map[string]Driver{}}
}

// Add registers a Driver under radioID. Overwrites any existing
// registration for the same id (re-attach after a disconnect /
// reconnect cycle is the common case — the storage layer already
// partitions per-radio history by id).
func (r *Registry) Add(radioID string, d Driver) {
	if r == nil || d == nil || radioID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drivers[radioID] = d
}

// Remove drops the registration. No-op when missing — idempotent
// on disconnect handlers that don't track whether they registered.
func (r *Registry) Remove(radioID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.drivers, radioID)
}

// Get returns the Driver for radioID and ok=true when registered;
// ok=false otherwise. Handlers translate ok=false into HTTP 404.
func (r *Registry) Get(radioID string) (Driver, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.drivers[radioID]
	return d, ok
}

// Rekey atomically replaces an old radio_id with a new one, pointing
// at the same Driver. Used by the daemon when ApplyMyInfo claims the
// canonical "0xNNNNNNNN" identity and the original key was a pending
// placeholder. Idempotent — if oldID isn't registered, Add(newID, d)
// is the entire effect; if oldID == newID, no-op.
func (r *Registry) Rekey(oldID, newID string, d Driver) {
	if r == nil || d == nil || newID == "" || oldID == newID {
		if r != nil && d != nil && newID != "" {
			r.Add(newID, d)
		}
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.drivers, oldID)
	r.drivers[newID] = d
}

// IDs returns all registered radio ids, sorted is NOT guaranteed —
// handlers that need stable order should sort the returned slice.
// Returned slice is a copy; safe to iterate without holding the
// registry lock.
func (r *Registry) IDs() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.drivers))
	for id := range r.drivers {
		out = append(out, id)
	}
	return out
}

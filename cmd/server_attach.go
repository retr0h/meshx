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

package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/retr0h/meshx/internal/meshx/pump"
	"github.com/retr0h/meshx/internal/meshx/storage"
	"github.com/retr0h/meshx/internal/radio"
	"github.com/retr0h/meshx/internal/server"
)

// daemonAttacher is the concrete RadioAttacher the daemon uses.
// Satisfies server.RadioAttacher structurally. Holds the shared
// store + registry + logger so AttachRadio can wire a new pump +
// sink the same way the --radio startup path does.
type daemonAttacher struct {
	store    *storage.Sqlite
	registry *server.Registry
	log      *slog.Logger

	mu       sync.Mutex
	sessions map[string]*radio.Session
}

func newDaemonAttacher(
	store *storage.Sqlite,
	registry *server.Registry,
	log *slog.Logger,
) *daemonAttacher {
	return &daemonAttacher{
		store:    store,
		registry: registry,
		log:      log,
		sessions: make(map[string]*radio.Session),
	}
}

// AttachRadio dials dest, creates a Session + Pump, registers in
// the Registry, and returns the pending radio_id. The pump runs in
// its own goroutine; once MyInfo arrives the sink rekeys the
// registry from "pending:<dest>" to "0x<nodenum>".
func (a *daemonAttacher) AttachRadio(_ context.Context, dest string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var drvStore radio.Store
	if a.store != nil {
		drvStore = a.store
	}
	drv := radio.New(nil, nil, drvStore)
	drv.State.ConnectDest = dest
	drv.State.RadioID = "pending:" + dest
	drv.OnStoreError = func(err error) {
		a.log.Warn("storage", slog.Any("error", err))
	}

	if a.store != nil {
		drv.HydrateFromStore(radio.HydrationOptions{
			Dest:                     dest,
			ResolveRadioByConnection: a.store.ResolveRadioByConnection,
			ParseRadioDest:           storage.ParseRadioDest,
		})
	}

	radioID := drv.State.RadioID
	a.registry.Add(radioID, drv)
	a.sessions[radioID] = drv

	a.log.Info(
		"attaching radio",
		slog.String("radio_id", radioID),
		slog.String("dest", dest),
	)

	sink := &daemonSink{drv: drv, registry: a.registry, log: a.log}
	var p radio.Pump = pump.New(dest, sink)
	drv.AttachPump(p)

	return radioID, nil
}

// DetachRadio stops the pump, removes the radio from the registry.
func (a *daemonAttacher) DetachRadio(_ context.Context, radioID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	drv, ok := a.sessions[radioID]
	if !ok {
		// Try the registry in case the ID was rekeyed (pending:… → 0x…)
		if _, found := a.registry.Get(radioID); !found {
			return fmt.Errorf("radio %s not found", radioID)
		}
		// Radio exists in registry but wasn't attached by us (e.g. --radio flag).
		// Still remove it.
		a.registry.Remove(radioID)
		return nil
	}

	drv.Stop()
	a.registry.Remove(radioID)
	delete(a.sessions, radioID)

	a.log.Info("detached radio", slog.String("radio_id", radioID))
	return nil
}

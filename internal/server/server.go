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

// Package server is the HTTP+SSE surface that sits between the
// driver (transport / pump / storage / canonical session state) and
// any client. The (α) endgame: TUI and any future SDK consumer talk
// to the daemon over this contract; standalone meshx runs server +
// TUI in the same process talking over localhost.
//
//	radio ─ transport ─ pump ─┐
//	                          ├─ driver (state + apply* + send) ─┐
//	                          │                                  ├─ server (HTTP+SSE)
//	                          │                                  │      │
//	                          │                                  │      ↓
//	                          │                                  │   client (TUI / SDK)
//	                          └──────── storage (SQLite) ────────┘
//
// Huma handles route registration → OpenAPI 3.1 generation. The spec
// is published at /openapi.json and the human-friendly explorer at
// /docs (Stoplight Elements bundled by Huma).
//
// This package ships the skeleton: a working listener, the readonly
// route surface (healthz, session snapshot, openapi), and the seam
// where future routes (channels / nodes / messages / events stream)
// will register once the canonical collections move from the TUI
// onto driver state. Auth + write routes are deferred to MR-6.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// Server bundles the HTTP listener with the Driver it wraps. One
// Server per Driver — the Driver's *session.Session is the single
// canonical state both reads and SSE pushes derive from. Concrete
// listener / mux / api types are unexported so callers go through
// New + Run, never poke the http.Server directly. The drv field
// holds the narrow Driver interface declared in driver.go (osapi-io
// consumer seam), not the concrete *internal/driver.Driver.
type Server struct {
	drv  Driver
	http *http.Server
	api  huma.API
}

// New wires a Server around the given Driver. The Driver must be
// constructed by the caller (cmd/serve, future cmd/standalone) —
// this constructor only registers Huma routes against an http.Mux.
// Run starts the listener.
func New(drv Driver) *Server {
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("meshx", "0.1.0")
	cfg.Info.Description = "meshx mesh-radio HTTP API — channels, nodes, messages, and live events from a Meshtastic-compatible LoRa radio."
	api := humago.New(mux, cfg)

	s := &Server{
		drv: drv,
		api: api,
		http: &http.Server{
			Handler: mux,
			// ReadHeaderTimeout protects against slowloris-style attacks;
			// 10s is plenty for any sane client and short enough that a
			// dead connection doesn't camp on a goroutine.
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
	s.registerRoutes()
	return s
}

// Run starts the HTTP listener on addr and blocks until ctx is
// canceled or the listener errors. ctx cancellation triggers a
// graceful shutdown with a 5s drain window for in-flight requests.
// Returns nil when shutdown completes cleanly, the listener error
// otherwise.
func (s *Server) Run(ctx context.Context, addr string) error {
	s.http.Addr = addr

	listenErr := make(chan error, 1)
	go func() {
		err := s.http.ListenAndServe()
		// http.ErrServerClosed is the expected error from Shutdown —
		// not actually a failure, just the listener notifying that
		// it stopped accepting. Anything else is a real bind / IO
		// error worth surfacing.
		if !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
		}
		close(listenErr)
	}()

	select {
	case err := <-listenErr:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}
		return nil
	}
}

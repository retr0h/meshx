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
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/retr0h/meshx/internal/transports"
)

// Config bundles the server's dependencies. Transports is optional
// — when nil, /transports/* routes return 503 at request time so
// clients get a real signal instead of a silently-broken route.
type Config struct {
	Radios *Registry

	// Transports is the hardware-management surface (BLE/USB scan,
	// pair, list, …). Constructed by the caller with whatever
	// adapters are wired (typically *storage.Sqlite + the BLE/USB
	// adapters in cmd/server_deps.go). nil disables /transports/*.
	Transports *transports.Manager

	// Logger is the slog handle every middleware (request log, panic
	// recovery, future audit) emits through. Nil falls back to a
	// stderr text handler so tests don't have to wire one up.
	Logger *slog.Logger

	// AuthToken gates every request with a constant-time bearer-
	// token check when non-empty. Empty disables the auth middleware
	// entirely (the default for loopback-only binds, which the cmd/
	// layer ensures). /healthz is exempt regardless so orchestration
	// liveness probes don't need credentials.
	AuthToken string
}

// OpenAPISpec returns the daemon's OpenAPI 3.0 spec as YAML bytes —
// the exact same output the daemon serves at /openapi-3.0.yaml,
// extracted directly from Huma without spinning up a listener. Used
// by the spec-regen generator (internal/sdk/gen/dumpspec) to refresh
// the vendored api.yaml without the daemon-dance, and by the drift
// test to compare the in-process spec against the on-disk copy.
func (s *Server) OpenAPISpec() ([]byte, error) {
	if s == nil || s.api == nil {
		return nil, errors.New("server: api uninitialized")
	}
	return s.api.OpenAPI().DowngradeYAML()
}

// Server is the HTTP+SSE daemon that multiplexes one or more radios
// to clients. Constructed via New(Config); the http.Server inside is
// driven by Run.
type Server struct {
	radios      *Registry
	transports  *transports.Manager
	http        *http.Server
	api         huma.API
	logger      *slog.Logger
	idempotency *idempotencyCache
	authToken   string
}

// New wires a Server around the given Config. The Registry is
// required; optional Store / Scanner / Pairer enable the BLE
// transport-management endpoints (and the radio + message endpoints
// that need persistence). Drivers can attach to the Registry later
// via Drivers().Add(...) — the /radios endpoints reflect live
// registry state with no restart required.
func New(cfg Config) *Server {
	if cfg.Radios == nil {
		cfg.Radios = NewRegistry()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	mux := http.NewServeMux()
	hc := huma.DefaultConfig("meshx", "0.1.0")
	// Huma emits 3.1 by default and a downgraded 3.0 spec at
	// /openapi-3.0.{json,yaml}. oapi-codegen still needs 3.0
	// (see oapi-codegen #373) so the SDK regen pulls from the 3.0
	// path. Don't pin OpenAPI.OpenAPI = "3.0.x" here — that flips
	// the version field but leaves 3.1-only schema constructs
	// (examples-as-array, type: [..,null]) intact and produces an
	// internally inconsistent spec.
	hc.Info.Description = "meshx mesh-radio HTTP API — channels, nodes, messages, transports, and live events from one or more Meshtastic-compatible LoRa radios. Radio-scoped resources live under /radios/{radio_id}/…; transport-management endpoints under /transports/…"
	api := humago.New(mux, hc)

	s := &Server{
		radios:      cfg.Radios,
		transports:  cfg.Transports,
		api:         api,
		logger:      cfg.Logger.With(slog.String("subsystem", "http")),
		idempotency: newIdempotencyCache(),
		authToken:   cfg.AuthToken,
		http: &http.Server{
			Handler: mux,
			// ReadHeaderTimeout protects against slowloris-style attacks;
			// 10s is plenty for any sane client and short enough that a
			// dead connection doesn't camp on a goroutine.
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
	s.registerMiddleware()
	s.registerRoutes()
	return s
}

// Drivers exposes the underlying Registry so callers can attach /
// detach radios after construction. The cmd/serve startup path uses
// this to register one Driver per --radio flag; a future hot-attach
// HTTP endpoint will use the same surface.
func (s *Server) Drivers() *Registry {
	if s == nil {
		return nil
	}
	return s.radios
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

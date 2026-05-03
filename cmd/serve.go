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

package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/driver"
	"github.com/retr0h/meshx/internal/server"
)

// daemonRunner is the narrow consumer-seam interface this cobra
// command depends on, declared per the osapi-io pattern. Both
// *server.Server and any future variant (an in-memory fake for
// tests, a unix-socket-only flavor, …) satisfy this surface — Go's
// structural typing means we don't `implements` anywhere; the
// compiler verifies. Today the only implementer is *server.Server,
// but the seam is here so future swap-ins don't need to touch
// `runServe`.
type daemonRunner interface {
	Run(ctx context.Context, addr string) error
}

// serveCmd boots the daemon mode — runs the HTTP+SSE server without
// the TUI. The architecture treats `meshx serve` as the canonical
// "headless" deployment: a long-running process that owns the radio
// transport, exposes the API, and lets remote clients (the meshx
// TUI elsewhere, future generated SDK consumers) connect over HTTP.
//
//	radio ─ transport ─ pump ─┐
//	                          ├─ driver (state + apply* + send) ─┐
//	                          │                                  ├─ server (HTTP+SSE) ──→ clients
//	                          │                                  │
//	                          └──────── storage (SQLite) ────────┘
//
// Standalone meshx (the bare `meshx` command) runs server + TUI in
// the same process talking over localhost — same daemon code path,
// just colocated. This MR ships the headless skeleton; the
// "TUI as HTTP client" follow-up makes the standalone case literal.
//
// Flags:
//
//	--bind   listener address (default ":8080")
//	--radio  optional transport target (USB path, host:port, ble:<uuid>).
//	         Without it, the daemon serves an empty session — useful for
//	         poking the API surface and inspecting the OpenAPI spec at
//	         /openapi.json.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the meshx HTTP+SSE daemon (headless)",
	Long: `Start the meshx daemon — exposes channels, nodes, messages, and
live events over HTTP+SSE. Generated OpenAPI spec at /openapi.json.

  meshx serve                          # bind :8080, no radio attached
  meshx serve --bind :3000             # custom listener address
  meshx serve --radio /dev/cu.usb…     # attach a radio over USB
  meshx serve --radio host:4403        # attach over TCP (meshtasticd)
  meshx serve --radio ble:<uuid>       # attach over Bluetooth LE

The daemon is the canonical 'middleman' between the radio and any
client. Clients (the meshx TUI today, future generated SDK
consumers) talk to this server, never to the pump or storage layer
directly.`,
	RunE: runServe,
}

var (
	serveBind  string
	serveRadio string
)

func init() {
	serveCmd.Flags().StringVar(
		&serveBind,
		"bind",
		":8080",
		"HTTP listener address (host:port; empty host = all interfaces)",
	)
	serveCmd.Flags().StringVar(
		&serveRadio,
		"radio",
		"",
		"transport target for the radio: /dev/cu.usb… | host:port | ble:<uuid>. Empty = serve with no radio attached.",
	)
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, _ []string) error {
	// Plumb a context that cancels on SIGINT / SIGTERM. Server.Run
	// watches it and triggers a graceful shutdown when canceled —
	// the `os.Interrupt` is the canonical "user pressed Ctrl-C"
	// signal across platforms; `syscall.SIGTERM` covers
	// systemd / docker stop / k8s grace-period termination.
	ctx, cancel := signal.NotifyContext(
		cmd.Context(),
		// os.Interrupt is constant in os/signal but importing os just
		// for it is noise — syscall.SIGINT is the same value.
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	// Build a Driver with no Pump / Store yet. The data-wiring MR
	// will introduce Driver.Open(ctx, dest) so that --radio attaches
	// a live transport here; until then, the server runs against an
	// empty session — the API surface + OpenAPI spec are real,
	// listings just return [] until Driver state populates.
	drv := driver.New(nil, nil, nil)

	if serveRadio != "" {
		fmt.Fprintf(
			cmd.ErrOrStderr(),
			"meshx serve: --radio %q ignored (transport attach not yet wired in this MR)\n",
			serveRadio,
		)
	}

	var srv daemonRunner = server.New(drv)

	fmt.Fprintf(
		cmd.OutOrStdout(),
		"meshx serve: listening on %s (OpenAPI: http://localhost%s/openapi.json)\n",
		serveBind, serveBind,
	)

	if err := srv.Run(ctx, serveBind); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	if err := ctx.Err(); err != nil && err != context.Canceled {
		return err
	}
	return nil
}

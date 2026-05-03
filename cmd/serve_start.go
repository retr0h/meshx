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
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/driver"
	"github.com/retr0h/meshx/internal/server"
)

var (
	serveBind  string
	serveRadio string
)

// serveStartCmd boots the daemon — runs the HTTP+SSE server without
// the TUI. The architecture treats `meshx serve start` as the
// canonical "headless" deployment: a long-running process that owns
// the radio transport, exposes the API, and lets remote clients
// connect over HTTP.
//
//	radio ─ transport ─ pump ─┐
//	                          ├─ driver (state + apply* + send) ─┐
//	                          │                                  ├─ server (HTTP+SSE) ──→ clients
//	                          └──────── storage (SQLite) ────────┘
var serveStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the meshx daemon",
	Long: `Start the meshx daemon — exposes channels, nodes, messages, and
live events over HTTP+SSE. Generated OpenAPI spec at /openapi.json.

  meshx serve start                          # bind :8080, no radio attached
  meshx serve start --bind :3000             # custom listener address
  meshx serve start --radio /dev/cu.usb…     # attach a radio over USB
  meshx serve start --radio host:4403        # attach over TCP (meshtasticd)
  meshx serve start --radio ble:<uuid>       # attach over Bluetooth LE`,
	RunE: runServeStart,
}

func init() {
	serveStartCmd.Flags().StringVar(
		&serveBind,
		"bind",
		":8080",
		"HTTP listener address (host:port; empty host = all interfaces)",
	)
	serveStartCmd.Flags().StringVar(
		&serveRadio,
		"radio",
		"",
		"transport target for the radio: /dev/cu.usb… | host:port | ble:<uuid>. Empty = serve with no radio attached.",
	)
	serveCmd.AddCommand(serveStartCmd)
}

func runServeStart(cmd *cobra.Command, _ []string) error {
	slog.Info("starting meshx daemon", "bind", serveBind, "radio", serveRadio)

	ctx, cancel := signal.NotifyContext(
		cmd.Context(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	radios := server.NewRegistry()

	if serveRadio != "" {
		drv := driver.New(nil, nil, nil)
		pendingID := "pending:" + serveRadio
		drv.State.ConnectDest = serveRadio
		drv.State.RadioID = pendingID
		radios.Add(pendingID, drv)
		_, _ = fmt.Fprintf(
			cmd.ErrOrStderr(),
			"meshx serve: registered radio %q under id %q (transport attach pending)\n",
			serveRadio, pendingID,
		)
	}

	var srv daemonRunner = server.New(radios)

	_, _ = fmt.Fprintf(
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

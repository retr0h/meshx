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
	"github.com/spf13/viper"

	"github.com/retr0h/meshx/internal/driver"
	"github.com/retr0h/meshx/internal/meshx/pump"
	"github.com/retr0h/meshx/internal/meshx/storage"
	"github.com/retr0h/meshx/internal/server"
)

// serverStartCmd boots the daemon — runs the HTTP+SSE server without
// the TUI. The architecture treats `meshx server start` as the
// canonical "headless" deployment: a long-running process that owns
// the radio transport, exposes the API, and lets remote clients
// connect over HTTP.
//
//	radio ─ transport ─ pump ─┐
//	                          ├─ driver (state + apply* + send) ─┐
//	                          │                                  ├─ server (HTTP+SSE) ──→ clients
//	                          └──────── storage (SQLite) ────────┘
var serverStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the meshx daemon",
	Long: `Start the meshx daemon — exposes channels, nodes, messages, and
live events over HTTP+SSE. Generated OpenAPI spec at /openapi.json.

  meshx server start                          # bind 127.0.0.1:4404
  meshx server start --bind :4404             # listen on all interfaces
  meshx server start --radio /dev/cu.usb…     # attach a radio over USB
  meshx server start --radio host:4403        # attach over TCP (meshtasticd)
  meshx server start --radio ble:<uuid>       # attach over Bluetooth LE

Every flag here is also overridable via env (MESHX_SERVER_BIND,
MESHX_SERVER_RADIO).`,
	RunE: runServerStart,
}

func init() {
	serverStartCmd.Flags().String(
		"bind",
		"127.0.0.1:4404",
		"HTTP listener address (host:port; empty host = all interfaces)",
	)
	serverStartCmd.Flags().String(
		"radio",
		"",
		"transport target for the radio: /dev/cu.usb… | host:port | ble:<uuid>. Empty = serve with no radio attached.",
	)
	_ = viper.BindPFlag("server.bind", serverStartCmd.Flags().Lookup("bind"))
	_ = viper.BindPFlag("server.radio", serverStartCmd.Flags().Lookup("radio"))

	serverCmd.AddCommand(serverStartCmd)
}

func runServerStart(cmd *cobra.Command, _ []string) error {
	bind := viper.GetString("server.bind")
	radio := viper.GetString("server.radio")

	log := logger.With(slog.String("subsystem", "server"))
	log.Info("config",
		slog.String("bind", bind),
		slog.String("radio", radio),
		slog.Bool("debug", viper.GetBool("debug")),
	)

	ctx, cancel := signal.NotifyContext(
		cmd.Context(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	radios := server.NewRegistry()

	// Open the concrete *storage.Sqlite once; it satisfies both
	// server.Store (HTTP read paths) and driver.Store (apply* +
	// identity claim). serverDeps lifts it through the narrower
	// server.Store interface for the daemon's Config; we hand the
	// concrete value to driver.New so ApplyMyInfo can claim
	// identity and ApplyText can persist messages.
	concreteStore := openStore(cmd, log)
	store, scanner, pairer, usbScan := serverDepsWithStore(concreteStore)

	if radio != "" {
		// Resolve the canonical radio_id from the SQLite cache when
		// we've handshaken with this radio before — that way the
		// /radios listing surfaces "0xNNNNNNNN" immediately instead
		// of "pending:..." until MyInfo arrives. Falls back to the
		// pending placeholder otherwise (the ApplyMyInfo path
		// rewrites it atomically once handshake completes).
		transport, addr := storage.ParseRadioDest(radio)
		radioID := "pending:" + radio
		if concreteStore != nil {
			if rid, err := concreteStore.ResolveRadioByConnection(transport, addr); err == nil &&
				rid != "" {
				radioID = rid
			}
		}

		// driver.Store is satisfied by *storage.Sqlite. nil is OK —
		// driver.New + every Apply* method nil-checks before calling
		// store methods, so a no-storage daemon still drives State
		// (just without persistence + identity claim).
		var drvStore driver.Store
		if concreteStore != nil {
			drvStore = concreteStore
		}
		drv := driver.New(nil, nil, drvStore)
		drv.State.ConnectDest = radio
		drv.State.RadioID = radioID
		radios.Add(radioID, drv)
		log.Info("radio registered",
			slog.String("radio_id", radioID),
			slog.String("dest", radio),
		)

		// Spawn the pump — same backoff + reconnect engine the local
		// TUI uses, but the sink dispatches every translated event to
		// driver.Apply* methods that mutate State, persist via Store,
		// and publish over SSE. The Registry rekey on identity claim
		// (pending:... → 0xNNNNNNNN) is handled inside the sink so
		// /radios reflects the canonical id the moment MyInfo lands.
		log.Info("dialing radio", slog.String("dest", radio))
		sink := &daemonSink{drv: drv, registry: radios, log: log}
		var p driver.Pump = pump.New(radio, sink)
		drv.AttachPump(p)
		defer drv.Stop()
	}

	var srv daemonRunner = server.New(server.Config{
		Radios:     radios,
		Store:      store,
		Scanner:    scanner,
		Pairer:     pairer,
		USBScanner: usbScan,
		Logger:     logger,
	})

	log.Info("listening",
		slog.String("bind", bind),
		slog.String("openapi", "http://"+bind+"/openapi.json"),
		slog.String("docs", "http://"+bind+"/docs"),
	)

	if err := srv.Run(ctx, bind); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	if err := ctx.Err(); err != nil && err != context.Canceled {
		return err
	}
	return nil
}

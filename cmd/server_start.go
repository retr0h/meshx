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
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/retr0h/meshx/internal/meshx/pump"
	"github.com/retr0h/meshx/internal/meshx/storage"
	"github.com/retr0h/meshx/internal/radio"
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
	serverStartCmd.Flags().String(
		"auth-token-file",
		"",
		"path to a bearer-token file. Read on startup; generated (32 random bytes hex, 0o600) on first run when missing. Required when --bind is non-loopback unless --auth-disabled is set. Clients send `Authorization: Bearer <token>`. /healthz is exempt.",
	)
	serverStartCmd.Flags().Bool(
		"auth-disabled",
		false,
		"explicitly run without bearer-token auth even on a non-loopback bind. Only safe when an upstream proxy / VPN / firewall is doing access control — anyone reachable on the network can otherwise broadcast on the radio.",
	)
	_ = viper.BindPFlag("server.bind", serverStartCmd.Flags().Lookup("bind"))
	_ = viper.BindPFlag("server.radio", serverStartCmd.Flags().Lookup("radio"))
	_ = viper.BindPFlag("server.auth_token_file", serverStartCmd.Flags().Lookup("auth-token-file"))
	_ = viper.BindPFlag("server.auth_disabled", serverStartCmd.Flags().Lookup("auth-disabled"))

	serverCmd.AddCommand(serverStartCmd)
}

func runServerStart(cmd *cobra.Command, _ []string) error {
	bind := viper.GetString("server.bind")
	dest := viper.GetString("server.radio")
	authTokenFile := viper.GetString("server.auth_token_file")
	authDisabled := viper.GetBool("server.auth_disabled")

	log := logger.With(slog.String("subsystem", "server"))

	authToken, err := resolveAuthToken(bind, authTokenFile, authDisabled, log)
	if err != nil {
		return err
	}

	log.Info(
		"config",
		slog.String("bind", bind),
		slog.String("radio", dest),
		slog.Bool("debug", viper.GetBool("debug")),
		slog.Bool("auth", authToken != ""),
	)

	ctx, cancel := signal.NotifyContext(
		cmd.Context(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	radios := server.NewRegistry()

	// Open the concrete *storage.Sqlite once; it satisfies both
	// server.Store (HTTP read paths) and radio.Store (apply* +
	// identity claim). serverDeps lifts it through the narrower
	// server.Store interface for the daemon's Config; we hand the
	// concrete value to radio.New so ApplyMyInfo can claim
	// identity and ApplyText can persist messages.
	concreteStore := openStore(cmd, log)
	store, scanner, pairer, usbScan := serverDepsWithStore(concreteStore)

	if dest != "" {
		// radio.Store is satisfied by *storage.Sqlite. nil is OK —
		// radio.New + every Apply* method nil-checks before calling
		// store methods, so a no-storage daemon still drives State
		// (just without persistence + identity claim).
		var drvStore radio.Store
		if concreteStore != nil {
			drvStore = concreteStore
		}
		drv := radio.New(nil, nil, drvStore)
		drv.State.ConnectDest = dest
		drv.State.RadioID = "pending:" + dest
		// Daemon surfaces persistence failures via slog rather than a
		// State.Messages row — remote clients can spot the slog line
		// in the daemon's log; injecting a system row would conflict
		// with the live SSE event stream.
		drv.OnStoreError = func(err error) {
			log.Warn("storage", slog.Any("error", err))
		}

		// Replay persisted history (identity + NodeDB + messages +
		// ghost-peer + last-heard backfill + stale-pending sweep)
		// through the same Driver.HydrateFromStore the local TUI
		// uses. Sanitize is nil — daemon stores raw bytes; remote
		// clients see whatever the radio actually sent.
		var hyd radio.HydrationResult
		if concreteStore != nil {
			hyd = drv.HydrateFromStore(radio.HydrationOptions{
				Dest:                     dest,
				ResolveRadioByConnection: concreteStore.ResolveRadioByConnection,
				ParseRadioDest:           storage.ParseRadioDest,
			})
		}
		radioID := drv.State.RadioID

		radios.Add(radioID, drv)
		log.Info(
			"radio registered",
			slog.String("radio_id", radioID),
			slog.String("dest", dest),
			slog.Int("history_messages", hyd.MessagesLoaded),
			slog.Int("history_nodes", hyd.NodesLoaded),
			slog.Int("ghost_peers", hyd.GhostsCreated),
			slog.Int("backfilled", hyd.LastHeardBackfilled),
			slog.Int("stale_pending_expired", hyd.StalePendingExpired),
		)
		for _, n := range hyd.BootNotes {
			log.Info("storage", slog.String("note", n))
		}

		// Spawn the pump — same backoff + reconnect engine the local
		// TUI uses, but the sink dispatches every translated event to
		// radio.Apply* methods that mutate State, persist via Store,
		// and publish over SSE. The Registry rekey on identity claim
		// (pending:... → 0xNNNNNNNN) is handled inside the sink so
		// /radios reflects the canonical id the moment MyInfo lands.
		log.Info("dialing radio", slog.String("dest", dest))
		sink := &daemonSink{drv: drv, registry: radios, log: log}
		var p radio.Pump = pump.New(dest, sink)
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
		AuthToken:  authToken,
	})

	log.Info(
		"listening",
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

// resolveAuthToken implements the bind-aware auth policy:
//
//   - --auth-disabled set:        no auth, regardless of bind.
//   - token-file set:             load (or generate-on-first-run); auth on.
//   - token-file unset, loopback: no auth (default for `127.0.0.1:4404`).
//   - token-file unset, non-loop: ERROR — refuse to expose the radio
//     unauthenticated. Operator must pass --auth-token-file or
//     --auth-disabled explicitly.
//
// First-run token generation logs the plaintext token once at INFO so
// the operator can copy it. Subsequent runs read silently.
func resolveAuthToken(bind, tokenFile string, disabled bool, log *slog.Logger) (string, error) {
	if disabled {
		if !server.IsLoopbackBind(bind) {
			log.Warn(
				"auth disabled on non-loopback bind",
				slog.String("bind", bind),
				slog.String(
					"hint",
					"anyone reachable on the network can broadcast on the radio; ensure an upstream proxy / VPN / firewall enforces access control",
				),
			)
		}
		return "", nil
	}
	if tokenFile == "" {
		if server.IsLoopbackBind(bind) {
			return "", nil
		}
		return "", fmt.Errorf(
			"--bind=%s is non-loopback but no --auth-token-file is set; "+
				"pass --auth-token-file <path> (a token will be generated on first run) "+
				"or --auth-disabled if access control lives upstream",
			bind,
		)
	}
	existed := true
	if _, err := os.Stat(tokenFile); errors.Is(err, os.ErrNotExist) {
		existed = false
	}
	token, err := server.LoadAuthToken(tokenFile)
	if err != nil {
		return "", fmt.Errorf("auth-token-file: %w", err)
	}
	if !existed {
		log.Info(
			"auth token generated",
			slog.String("path", tokenFile),
			slog.String("token", token),
			slog.String(
				"hint",
				"save this; clients send `Authorization: Bearer <token>`. Subsequent server restarts read from the file silently.",
			),
		)
	}
	return token, nil
}

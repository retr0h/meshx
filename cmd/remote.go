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
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/retr0h/meshx/internal/sdk/gen"
	"github.com/retr0h/meshx/internal/tui"
)

// remoteCmd opens the TUI as a client of a running meshx daemon.
// Inverts the local model: the daemon owns the radio + storage; the
// TUI consumes its HTTP+SSE API. Same key bindings, same modes, same
// rendering — the radioSession seam abstracts the difference.
var remoteCmd = &cobra.Command{
	Use:   "remote [radio]",
	Short: "Open the TUI against a remote meshx daemon over HTTP+SSE",
	Long: `Connect to a meshx daemon running on another host (or the same
host with --bind 127.0.0.1) and run the TUI as its client. The
daemon owns the radio transport, persistence, and reconnect; the
TUI receives state via /radios/{id}/* and live events via SSE.

The [radio] argument accepts any of:

  - canonical radio_id    (0xd64b01be)
  - BLE UUID              (48d917af-8a1f-e43e-4735-af3e1c8e35bc)
  - BLE dest string       (ble:48d917af-...)
  - USB device path       (/dev/cu.usbmodem2101)
  - TCP target            (host:4403)

Substring matches against the daemon's connect_dest also work
(e.g. "48d917af" matches the full UUID). Omit the argument
entirely when the daemon has exactly one radio attached and the
TUI auto-targets it.

  meshx remote                                        # auto — single radio
  meshx remote 48d917af-8a1f-e43e-4735-af3e1c8e35bc   # by UUID
  meshx remote 48d917af                               # UUID prefix
  meshx remote 0xd64b01be -s http://localhost:4404    # by canonical id
  MESHX_REMOTE_SERVER=http://host:4404 meshx remote   # via env`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRemote,
}

func init() {
	remoteCmd.Flags().StringP(
		"server",
		"s",
		"http://127.0.0.1:4404",
		"meshx daemon URL (scheme://host:port)",
	)
	_ = viper.BindPFlag("remote.server", remoteCmd.Flags().Lookup("server"))

	rootCmd.AddCommand(remoteCmd)
}

func runRemote(_ *cobra.Command, args []string) error {
	server := viper.GetString("remote.server")
	if server == "" {
		return errors.New("--server is required (or set MESHX_REMOTE_SERVER)")
	}
	needle := ""
	if len(args) == 1 {
		needle = args[0]
	}

	logger.Debug(
		"running",
		slog.String("subsystem", "remote.connect"),
		slog.String("server", server),
		slog.String("needle", needle),
	)

	radioID, err := resolveRemoteRadio(server, needle)
	if err != nil {
		return err
	}
	logger.Info(
		"resolved",
		slog.String("subsystem", "remote.connect"),
		slog.String("radio_id", radioID),
	)
	return tui.RunRadioRemote(server, radioID)
}

// resolveRemoteRadio asks the daemon for its registered radios and
// returns the canonical radio_id matching the user's input. Empty
// needle = auto-target if exactly one radio is attached. With a
// needle, matches in priority order: exact radio_id, exact
// connect_dest, then case-insensitive substring of either.
func resolveRemoteRadio(serverURL, needle string) (string, error) {
	c, err := gen.NewClientWithResponses(serverURL)
	if err != nil {
		return "", fmt.Errorf("remote: build client: %w", err)
	}
	resp, err := c.ListRadiosWithResponse(context.Background())
	if err != nil {
		return "", fmt.Errorf("remote: list radios on %s: %w", serverURL, err)
	}
	if resp.JSON200 == nil || resp.JSON200.Radios == nil {
		return "", fmt.Errorf("remote: %s returned no radios", serverURL)
	}
	radios := *resp.JSON200.Radios

	if needle == "" {
		switch len(radios) {
		case 0:
			return "", fmt.Errorf(
				"remote: no radios attached on %s — start the daemon with --radio first",
				serverURL,
			)
		case 1:
			return radios[0].RadioId, nil
		default:
			return "", fmt.Errorf(
				"remote: %d radios on %s — specify which (try %q)",
				len(radios),
				serverURL,
				radios[0].RadioId,
			)
		}
	}

	// Exact radio_id match.
	for _, r := range radios {
		if r.RadioId == needle {
			return r.RadioId, nil
		}
	}
	// Exact connect_dest match (covers "ble:<uuid>", "/dev/cu.usb…",
	// "host:port" — whatever the user typed at "server start --radio").
	for _, r := range radios {
		if r.ConnectDest == needle {
			return r.RadioId, nil
		}
	}
	// Case-insensitive substring of radio_id or connect_dest. Catches
	// "48d917af" against "ble:48d917af-8a1f-…", "0xd64b" against
	// "0xd64b01be", "usbmodem2101" against "/dev/cu.usbmodem2101".
	low := strings.ToLower(needle)
	for _, r := range radios {
		if strings.Contains(strings.ToLower(r.RadioId), low) ||
			strings.Contains(strings.ToLower(r.ConnectDest), low) {
			return r.RadioId, nil
		}
	}
	return "", fmt.Errorf("remote: no radio matching %q on %s", needle, serverURL)
}

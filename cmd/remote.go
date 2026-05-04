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
	"errors"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/retr0h/meshx/internal/tui"
)

// remoteCmd opens the TUI as a client of a running meshx daemon.
// Inverts the local model: the daemon owns the radio + storage; the
// TUI consumes its HTTP+SSE API. Same key bindings, same modes, same
// rendering — the radioDriver seam abstracts the difference.
var remoteCmd = &cobra.Command{
	Use:   "remote <radio_id>",
	Short: "Open the TUI against a remote meshx daemon over HTTP+SSE",
	Long: `Connect to a meshx daemon running on another host (or the same
host with --bind 127.0.0.1) and run the TUI as its client. The
daemon owns the radio transport, persistence, and reconnect; the
TUI receives state via /radios/{id}/* and live events via SSE.

  meshx remote 0xd64b01be --server http://laptop:4404
  meshx remote 0xd64b01be -s http://localhost:4404
  MESHX_REMOTE_SERVER=http://host:4404 meshx remote 0xd64b01be

Run "meshx server start" elsewhere first; "GET /radios" on the
daemon lists available radio_ids.`,
	Args: cobra.ExactArgs(1),
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
	radioID := args[0]
	server := viper.GetString("remote.server")
	if server == "" {
		return errors.New("--server is required (or set MESHX_REMOTE_SERVER)")
	}

	logger.Debug("running",
		slog.String("subsystem", "remote.connect"),
		slog.String("server", server),
		slog.String("radio_id", radioID),
	)

	return tui.RunRadioRemote(server, radioID)
}

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
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// clientCmd is the parent for every HTTP-client operation against a
// running meshx daemon. Where `meshx ble` and `meshx usb` touch the
// host's hardware directly, `meshx client` always goes through the
// daemon's HTTP+SSE API — so the daemon retains exclusive ownership
// of the BLE/USB adapter and clients (TUIs, scripts, future MCP
// servers) only see the wire surface.
//
// Persistent flags (--server, --auth-token-file) are declared here
// and inherited by every subcommand; this is the one place to
// configure where the daemon lives and how to authenticate.
//
//	meshx client status                          # health + radio list
//	meshx client scan ble                        # scan via the daemon's adapter
//	meshx client pair <uuid>                     # pair via the daemon
//	meshx client connect [<radio>]               # open TUI in remote mode
var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "Talk to a running meshx daemon over HTTP+SSE",
	Long: `Commands for using a meshx daemon from a separate process.
Every subcommand goes through the daemon's HTTP API; the daemon
keeps exclusive hardware access so no two processes fight over the
BLE/USB adapter.

The --server URL and --auth-token-file flags are persistent on the
parent — each subcommand inherits them. Both also read from env
(MESHX_CLIENT_SERVER, MESHX_CLIENT_AUTH_TOKEN_FILE).

  meshx client status                          # GET /healthz + /radios
  meshx client scan ble                        # POST /transports/ble/scan
  meshx client scan usb                        # POST /transports/usb/scan
  meshx client pair <uuid>                     # POST /transports/ble/devices
  meshx client connect [<radio>]               # remote TUI (replaces meshx remote)`,
}

func init() {
	clientCmd.PersistentFlags().StringP(
		"server",
		"s",
		"http://127.0.0.1:4404",
		"meshx daemon URL (scheme://host:port)",
	)
	clientCmd.PersistentFlags().String(
		"auth-token-file",
		"",
		"path to the daemon's bearer-token file; the same file --auth-token-file on the server writes. Sent as `Authorization: Bearer <token>` on every request",
	)
	_ = viper.BindPFlag("client.server", clientCmd.PersistentFlags().Lookup("server"))
	_ = viper.BindPFlag(
		"client.auth_token_file",
		clientCmd.PersistentFlags().Lookup("auth-token-file"),
	)

	clientCmd.AddCommand(clientStatusCmd)
	clientCmd.AddCommand(clientScanCmd)
	clientCmd.AddCommand(clientPairCmd)
	clientCmd.AddCommand(clientConnectCmd)
	clientCmd.AddCommand(clientListCmd)
	clientCmd.AddCommand(clientForgetCmd)
	clientCmd.AddCommand(clientFavCmd)
	clientCmd.AddCommand(clientUnfavCmd)
	clientCmd.AddCommand(clientSendCmd)
	clientCmd.AddCommand(clientTailCmd)
	clientCmd.AddCommand(clientAttachCmd)
	clientCmd.AddCommand(clientDetachCmd)
	rootCmd.AddCommand(clientCmd)
}

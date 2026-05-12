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

// mcpCmd is the parent for `meshx mcp` — the Model Context Protocol
// server surface. Spawned per agent session (Claude Code / Cursor /
// any MCP-aware host), it's the third client of the meshx daemon
// alongside the TUI and the `meshx client` CLI.
//
// Persistent flags (--server / --auth-token-file) match the
// `client` parent's, so the same daemon URL + token works for both
// surfaces. MCP-specific persistent flags would land here too.
//
// Subcommands:
//
//	start    open an MCP server over stdio (the typical
//	         agent-spawn lifecycle)
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run a Model Context Protocol server against a meshx daemon",
	Long: `Exposes every operation of a running meshx daemon over MCP.
The MCP server is spawned by an agent (Claude Code, Cursor, …) per
session — when the agent disconnects the process exits, the daemon
keeps running with the radio attached.

The --server URL and --auth-token-file flags match meshx client; the
same daemon serves both surfaces. Both also read env
(MESHX_MCP_SERVER, MESHX_MCP_AUTH_TOKEN_FILE).

Configure your agent (example for Claude Code) to spawn:

  meshx mcp start --server http://127.0.0.1:4404

…and it gets tools for send_message, list_radios, list_channels,
ping_peer, scan_ble, pair_ble, and the rest of the daemon's surface.`,
}

func init() {
	mcpCmd.PersistentFlags().StringP(
		"server",
		"s",
		"http://127.0.0.1:4404",
		"meshx daemon URL (scheme://host:port)",
	)
	mcpCmd.PersistentFlags().String(
		"auth-token-file",
		"",
		"path to the daemon's bearer-token file (same file --auth-token-file on the server writes)",
	)
	_ = viper.BindPFlag("mcp.server", mcpCmd.PersistentFlags().Lookup("server"))
	_ = viper.BindPFlag(
		"mcp.auth_token_file",
		mcpCmd.PersistentFlags().Lookup("auth-token-file"),
	)

	mcpCmd.AddCommand(mcpStartCmd)
	rootCmd.AddCommand(mcpCmd)
}

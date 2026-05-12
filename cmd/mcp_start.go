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
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	mcppkg "github.com/retr0h/meshx/internal/mcp"
)

var mcpStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Run the meshx MCP server over stdio",
	Long: `Speaks Model Context Protocol on stdin/stdout — the transport
agents (Claude Code, Cursor, …) expect when they spawn a server as
a subprocess. Blocks until the agent disconnects (the typical MCP
lifecycle); the daemon keeps running with the radio attached.

Logs go to stderr only — stdout is the JSON-RPC wire and writing
anything else there would corrupt the protocol.

  meshx mcp start                                       # localhost daemon, no auth
  meshx mcp start --server http://host:4404             # remote daemon
  meshx mcp start --auth-token-file ~/.meshx/token      # bearer-auth gated`,
	RunE: func(_ *cobra.Command, _ []string) error {
		serverURL := viper.GetString("mcp.server")
		if serverURL == "" {
			return fmt.Errorf("mcp start: --server URL required (or set MESHX_MCP_SERVER)")
		}
		tokenFile := viper.GetString("mcp.auth_token_file")
		var authToken string
		if tokenFile != "" {
			raw, err := os.ReadFile(tokenFile)
			if err != nil {
				return fmt.Errorf("mcp start: read auth-token-file %s: %w", tokenFile, err)
			}
			authToken = strings.TrimSpace(string(raw))
			if authToken == "" {
				return fmt.Errorf("mcp start: auth-token-file %s is empty", tokenFile)
			}
		}

		// Every diagnostic line from the MCP server has to go to
		// stderr — stdout is the JSON-RPC wire. Inherit the root
		// logger (which targets stderr by default) and tag with the
		// subsystem.
		log := logger.With(slog.String("subsystem", "mcp.start"))
		log.Debug("running", slog.String("server", serverURL))

		srv, err := mcppkg.New(mcppkg.Config{
			ServerURL: serverURL,
			AuthToken: authToken,
			Logger:    logger,
		})
		if err != nil {
			return fmt.Errorf("mcp start: %w", err)
		}
		return srv.Run(context.Background())
	},
}

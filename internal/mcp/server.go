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

// Package mcp is the Model Context Protocol server for meshx — the
// third client of the HTTP+SSE daemon (alongside the TUI and the
// `meshx client` CLI). Exposes every operation a `meshx server start`
// daemon supports as an MCP tool an LLM agent can call.
//
// Architecturally: this package owns no radio state, no transport, no
// store. It builds an SDK client against the daemon's HTTP API and
// wires each *radio.Session method into an mcp.Tool. Stdio transport
// is the default — an agent (Claude Code, Cursor, …) spawns
// `meshx mcp start` as a subprocess and pipes JSON-RPC over stdin /
// stdout; when the agent disconnects the process exits, the daemon
// keeps running with the radio attached.
//
// The dedup arc that landed in #83 / #84 / #88 is what makes this
// cheap: every tool is a 5–10 line adapter that translates an
// mcp.CallToolRequest into a daemon HTTP call and back.
package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

// version is stamped into mcp.Implementation so MCP clients see who
// they're talking to. Bumped manually for now; could move onto
// internal/version once the daemon's build identity grows a Server-
// Version field.
const version = "0.1.0"

// Config bundles the runtime inputs for a meshx MCP server. ServerURL
// is required (where to find the daemon); AuthToken is optional (the
// daemon may run unauthenticated on loopback). Logger is for the MCP
// server's own diagnostic chatter — never written to stdout, which
// belongs to the JSON-RPC wire.
type Config struct {
	ServerURL string
	AuthToken string
	Logger    *slog.Logger
}

// Server is the meshx MCP server. Holds a Driver (the narrow
// daemon-side consumer surface, see session.go) used by every tool
// handler, plus the underlying mcpsdk.Server. Constructed via New;
// the wire is driven by Run.
//
// client is typed as the Driver interface, not concrete
// *gen.ClientWithResponses — per the osapi-io pattern, this file
// holds the only concrete-type reference (in New, at the assignment
// site where the compiler verifies the structural fit).
type Server struct {
	mcp    *mcpsdk.Server
	client Driver
	logger *slog.Logger
}

// New wires an MCP server pointed at cfg.ServerURL with optional
// bearer auth. Every tool registration happens here so the returned
// Server is fully formed and ready to Run; later mutation isn't
// supported (matches the local daemon's *server.Server shape).
func New(cfg Config) (*Server, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("mcp: ServerURL required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	opts := []gen.ClientOption{}
	if cfg.AuthToken != "" {
		token := cfg.AuthToken
		opts = append(opts, gen.WithRequestEditorFn(
			func(_ context.Context, req *http.Request) error {
				req.Header.Set("Authorization", "Bearer "+token)
				return nil
			},
		))
	}
	c, err := gen.NewClientWithResponses(cfg.ServerURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("mcp: build SDK client: %w", err)
	}

	mcpSrv := mcpsdk.NewServer(
		&mcpsdk.Implementation{
			Name:    "meshx",
			Version: version,
		},
		&mcpsdk.ServerOptions{
			Instructions: instructions,
		},
	)

	s := &Server{
		mcp:    mcpSrv,
		client: c,
		logger: cfg.Logger.With(slog.String("subsystem", "mcp")),
	}
	s.registerTools()
	return s, nil
}

// Run wires the MCP server to stdin/stdout and blocks until the
// transport closes (the spawning agent disconnects) or ctx cancels.
// Returns nil on clean shutdown.
func (s *Server) Run(ctx context.Context) error {
	s.logger.Info(
		"running",
		slog.String("transport", "stdio"),
	)
	return s.mcp.Run(ctx, &mcpsdk.StdioTransport{})
}

// instructions is the MCP server's self-description, surfaced to the
// agent on initialize. Keep it short and concrete; agents read this
// to understand what the server can do without paging through every
// tool's description.
const instructions = `meshx — Meshtastic LoRa mesh-radio bridge.

This server proxies every operation of a running meshx daemon
(meshx server start) over MCP. Use the tools to:

  - send chat / DM messages on the mesh
  - list / mint / import / delete / share channels
  - ping or traceroute peers
  - read live radio + node + message state
  - manage paired BLE devices

Every radio-scoped tool takes a radio_id parameter. Call list_radios
first to enumerate every attached radio; tool inputs accept the
0x-prefixed canonical id that endpoint returns.

The daemon owns the hardware and persists state — this server is
ephemeral. When you disconnect the daemon and its radio attachment
keep running for the next session.`

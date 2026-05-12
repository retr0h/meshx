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

package mcp

// In-process round-trip tests for the MCP server. Each row drives
// the server through the SDK's InMemoryTransport pair — no stdio
// fork, no real daemon — and asserts on the tool-call result.
//
// The mcpServerHarness builder wires:
//
//	*server.Server (real daemon, httptest in front)
//	  ↓ HTTP
//	*mcp.Server (this package, real tool registration)
//	  ↓ MCP-over-pipe
//	*mcp.Client (sdk test client)
//
// so every assertion exercises the full request → daemon-handler →
// daemon-response → MCP-textcontent path.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/retr0h/meshx/internal/radio"
	"github.com/retr0h/meshx/internal/server"
)

// mcpServerHarness spins up: a real *server.Server with the supplied
// radios attached, an httptest.Server in front, an *mcp.Server (this
// package) pointed at the httptest URL, and an MCP client paired to
// the server via InMemoryTransport. Returns the connected client
// session for tool calls.
func mcpServerHarness(
	t *testing.T,
	radios ...*radio.Session,
) *mcpsdk.ClientSession {
	t.Helper()
	daemon := server.New(server.Config{Radios: server.NewRegistry()})
	for _, sess := range radios {
		daemon.Drivers().Add(sess.State.RadioID, sess)
	}
	httpSrv := httptest.NewServer(daemon.Handler())
	t.Cleanup(httpSrv.Close)

	mcpSrv, err := New(Config{ServerURL: httpSrv.URL})
	if err != nil {
		t.Fatalf("build mcp server: %v", err)
	}
	clientT, serverT := mcpsdk.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Run the MCP server in a goroutine. Cleanup ordering:
	// cancel (registered first, fires last) terminates Run via ctx;
	// cs.Close (registered last, fires first) tears down the client
	// session. Both paths unblock the goroutine.
	go func() { _ = mcpSrv.mcp.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "test-client"},
		nil,
	)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect mcp client: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// fakeRadio is a *radio.Session pre-seeded with the canonical-shape
// fields the /radios endpoints project. Test rows mutate one and
// hand it to mcpServerHarness.
func fakeRadio(id, dest string) *radio.Session {
	sess := radio.New(nil, nil, nil)
	sess.State.RadioID = id
	sess.State.ConnectDest = dest
	sess.State.MyNodeNum = 0xdeadbeef
	sess.State.Connected = true
	return sess
}

// TestServer_ListTools — every expected tool is registered + listed.
// Treat the tool catalog as a frozen surface so accidental removal
// trips the test; the row is one shape with the full expected set.
func TestServer_ListTools(t *testing.T) {
	cs := mcpServerHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	got := map[string]bool{}
	for _, tool := range resp.Tools {
		got[tool.Name] = true
	}

	want := []string{
		// meta
		"health",
		// radios
		"list_radios", "get_radio", "list_channels", "list_nodes", "list_messages",
		// messages
		"send_message",
		// channels
		"mint_channel", "import_channels", "delete_channel", "share_channel",
		// config
		"update_config", "reboot_radio",
		// radio ops
		"ping_peer", "traceroute_peer", "sync_radio",
		// transports
		"scan_ble", "scan_usb", "auto_detect_usb", "pair_ble",
		"list_ble_devices", "forget_ble_device",
		"set_ble_favorite", "clear_ble_favorite",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("expected tool %q not registered", name)
		}
	}
	if len(resp.Tools) != len(want) {
		t.Errorf("len(tools) = %d, want %d (extras: %v)",
			len(resp.Tools), len(want), resp.Tools)
	}
}

// TestServer_CallTool — every distinct tool-call shape (no-args
// read, radio-scoped read, write with body) goes through the full
// pipeline and surfaces the daemon's JSON response. The attached
// fakeRadio gives the harness one row in /radios so the listing
// path returns a populated array (also confirms the uint32-wide
// MyNodeNum survives the JSON round-trip through int64 — see #90).
func TestServer_CallTool(t *testing.T) {
	cases := []struct {
		name        string
		tool        string
		args        map[string]any
		wantSubstrs []string // substrings expected in the first TextContent block
	}{
		{
			name: "list_radios-returns-attached-radio",
			tool: "list_radios",
			args: nil,
			wantSubstrs: []string{
				`"radio_id": "0xabcdef01"`,
				`"my_node_num": 3735928559`, // 0xdeadbeef as JSON number
				`"connected": true`,
			},
		},
		{
			name: "list_channels-empty-channel-table-returns-empty-array",
			tool: "list_channels",
			args: map[string]any{"radio_id": "0xabcdef01"},
			wantSubstrs: []string{
				`"channels": []`,
			},
		},
		{
			name:        "get_radio-unknown-id-errors-cleanly",
			tool:        "get_radio",
			args:        map[string]any{"radio_id": "0xnonexistent"},
			wantSubstrs: []string{}, // expected to error; we assert on err separately
		},
	}
	cs := mcpServerHarness(t, fakeRadio("0xabcdef01", "/dev/cu.usb"))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			argBytes, _ := json.Marshal(tc.args)
			resp, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
				Name:      tc.tool,
				Arguments: json.RawMessage(argBytes),
			})

			// "get_radio unknown id" is expected to surface as an
			// error from the tool — let it fall through if we
			// didn't supply wantSubstrs.
			if len(tc.wantSubstrs) == 0 {
				if err == nil && (resp == nil || resp.IsError) {
					return // tool reported isError — fine
				}
				if err == nil {
					t.Fatalf("expected error or isError, got %+v", resp)
				}
				return
			}
			if err != nil {
				t.Fatalf("call %s: %v", tc.tool, err)
			}
			if resp.IsError {
				t.Fatalf("tool %s returned isError: %v", tc.tool, resp.Content)
			}
			if len(resp.Content) == 0 {
				t.Fatalf("tool %s returned no content", tc.tool)
			}
			text, ok := resp.Content[0].(*mcpsdk.TextContent)
			if !ok {
				t.Fatalf(
					"tool %s: first content type = %T, want *TextContent",
					tc.tool,
					resp.Content[0],
				)
			}
			for _, want := range tc.wantSubstrs {
				if !strings.Contains(text.Text, want) {
					t.Errorf(
						"tool %s response missing %q\n--- got ---\n%s",
						tc.tool,
						want,
						text.Text,
					)
				}
			}
		})
	}
}

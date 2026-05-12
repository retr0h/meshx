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

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

// Radio-op dispatches — ping_peer, traceroute_peer, sync_radio.
// All three return packet ids the agent can correlate with later
// SSE ping / traceroute events (not yet wired to MCP notifications;
// v2 work).

func (s *Server) registerRadioOpTools() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "ping_peer",
		Description: "Fire a REPLY_APP probe at a peer. The firmware echoes the packet back which surfaces as a ping SSE event on the daemon — call list_messages or use the SSE stream to observe the response.",
	}, s.toolPingPeer)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "traceroute_peer",
		Description: "Fire a TRACEROUTE_APP route-discovery at a peer. The firmware walks the mesh and echoes a route back, surfacing as a traceroute SSE event with the full hop list.",
	}, s.toolTraceroutePeer)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "sync_radio",
		Description: "Re-request the radio's full NodeDB, channel table, configs, and Metadata. Each piece arrives as its own inbound event over the next few seconds. Useful after a long disconnect or when the local state looks stale.",
	}, s.toolSyncRadio)
}

type pingPeerArgs struct {
	RadioID string `json:"radio_id" jsonschema:"canonical radio identifier from list_radios"`
	ToNum   int64  `json:"to_num"   jsonschema:"peer NodeNum to ping (from list_nodes); the firmware echoes a REPLY_APP packet back as a ping SSE event correlated by the returned packet_id"`
}

func (s *Server) toolPingPeer(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args pingPeerArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.PingPeerWithResponse(
		ctx,
		args.RadioID,
		gen.PingPeerJSONRequestBody{ToNum: args.ToNum},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("ping_peer: %w", err)
	}
	if resp.JSON202 == nil {
		return nil, nil, fmt.Errorf("ping_peer: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON202)), nil, nil
}

type traceroutePeerArgs struct {
	RadioID string `json:"radio_id" jsonschema:"canonical radio identifier from list_radios"`
	ToNum   int64  `json:"to_num"   jsonschema:"peer NodeNum to trace a route to"`
}

func (s *Server) toolTraceroutePeer(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args traceroutePeerArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.TraceroutePeerWithResponse(
		ctx,
		args.RadioID,
		gen.TraceroutePeerJSONRequestBody{ToNum: args.ToNum},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("traceroute_peer: %w", err)
	}
	if resp.JSON202 == nil {
		return nil, nil, fmt.Errorf("traceroute_peer: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON202)), nil, nil
}

func (s *Server) toolSyncRadio(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args radioIDArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.SyncRadioWithResponse(ctx, args.RadioID)
	if err != nil {
		return nil, nil, fmt.Errorf("sync_radio: %w", err)
	}
	if resp.JSON202 == nil {
		return nil, nil, fmt.Errorf("sync_radio: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON202)), nil, nil
}

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

// Radio enumeration + node / channel / message reads. These tools
// are how an agent discovers what's available before invoking write
// operations — the canonical entry point is `list_radios`.

func (s *Server) registerRadioTools() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "list_radios",
		Description: "List every radio attached to the meshx daemon. Returns canonical radio_id (passed to every other tool), my_node_num, connect_dest, and connected status. Call this first to discover what's available.",
	}, s.toolListRadios)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "get_radio",
		Description: "Fetch a single radio's detailed telemetry — battery, channel/air utilization, region, modem preset, position. Pass the radio_id from list_radios.",
	}, s.toolGetRadio)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "list_channels",
		Description: "List every channel slot on a radio (0..7). Returns name, role (PRIMARY / SECONDARY / DISABLED), and whether the slot is encrypted. PSK bytes are not returned by name — use share_channel to get the PSK as part of a meshtastic:// URL.",
	}, s.toolListChannels)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "list_nodes",
		Description: "List every peer in the radio's NodeDB. Each entry includes the peer's NodeNum (a uint32 used by ping/traceroute/send_message DMs), callsign, last-heard timestamp, signal stats, and live state (online / stale / offline).",
	}, s.toolListNodes)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "list_messages",
		Description: "Return the radio's message log (channel broadcasts and DMs). Optional dm filter: '' = everything, '1' = peer-addressed DMs, 'mine' = DMs to/from my_node_num. Optional limit caps the tail length.",
	}, s.toolListMessages)
}

type radioIDArgs struct {
	RadioID string `json:"radio_id" jsonschema:"canonical radio identifier from list_radios — 0x<hex node_num>"`
}

func (s *Server) toolListRadios(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	_ struct{},
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.ListRadiosWithResponse(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list_radios: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, nil, fmt.Errorf("list_radios: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON200)), nil, nil
}

func (s *Server) toolGetRadio(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args radioIDArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.GetRadioWithResponse(ctx, args.RadioID)
	if err != nil {
		return nil, nil, fmt.Errorf("get_radio: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, nil, fmt.Errorf("get_radio: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON200)), nil, nil
}

func (s *Server) toolListChannels(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args radioIDArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.ListChannelsWithResponse(ctx, args.RadioID)
	if err != nil {
		return nil, nil, fmt.Errorf("list_channels: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, nil, fmt.Errorf("list_channels: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON200)), nil, nil
}

func (s *Server) toolListNodes(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args radioIDArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.ListNodesWithResponse(ctx, args.RadioID)
	if err != nil {
		return nil, nil, fmt.Errorf("list_nodes: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, nil, fmt.Errorf("list_nodes: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON200)), nil, nil
}

type listMessagesArgs struct {
	RadioID string `json:"radio_id"        jsonschema:"canonical radio identifier from list_radios"`
	DM      string `json:"dm,omitempty"    jsonschema:"DM filter: '' = all rows, '1' = peer-addressed DMs (either direction), 'mine' = DMs to/from my_node_num"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max rows to return; 0 = no limit"`
}

func (s *Server) toolListMessages(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args listMessagesArgs,
) (*mcpsdk.CallToolResult, any, error) {
	params := &gen.ListMessagesParams{}
	if args.DM != "" {
		dm := gen.ListMessagesParamsDm(args.DM)
		params.Dm = &dm
	}
	if args.Limit > 0 {
		v := int64(args.Limit)
		params.Limit = &v
	}
	resp, err := s.client.ListMessagesWithResponse(ctx, args.RadioID, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list_messages: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, nil, fmt.Errorf("list_messages: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON200)), nil, nil
}

// textResult wraps a string as an MCP CallToolResult with a single
// TextContent block — the canonical response shape for the
// JSON-returning tools in this package.
func textResult(text string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: text},
		},
	}
}

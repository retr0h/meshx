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

// Channel ops — mint / import / delete / share. Mirrors the same
// four endpoints handlers_channels.go owns, which are thin adapters
// over *radio.Session.{MintChannel, ImportChannel, DeleteChannel,
// ShareChannel}. Validation, PSK generation, slot allocation all
// happen daemon-side; the MCP server is a typed dispatcher.

func (s *Server) registerChannelTools() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "mint_channel",
		Description: "Create a new SECONDARY channel slot with a freshly-generated PSK. The daemon allocates the first free slot (1..7), generates 32 random bytes for the PSK, dispatches the AdminMessage, and returns a meshtastic:// share URL containing the name + PSK + id.",
	}, s.toolMintChannel)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "import_channels",
		Description: "Import every channel encoded in a meshtastic:// share URL into the next available slots. The URL may carry one or many channels; returns per-channel imported/skipped lists.",
	}, s.toolImportChannels)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "delete_channel",
		Description: "Disable a channel slot (1..7). Slot 0 is PRIMARY and cannot be deleted. After deletion the slot's name disappears from list_channels and the radio stops decoding its PSK.",
	}, s.toolDeleteChannel)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "share_channel",
		Description: "Return the meshtastic:// share URL for an existing channel slot (0..7). Encodes the channel's name + PSK + id; safe to hand to other Meshtastic clients (display as a QR with /channel share in the TUI for in-person handoff).",
	}, s.toolShareChannel)
}

type mintChannelArgs struct {
	RadioID string `json:"radio_id" jsonschema:"canonical radio identifier from list_radios"`
	Name    string `json:"name"     jsonschema:"channel name (1..11 bytes UTF-8); Meshtastic packs this into the share URL"`
}

func (s *Server) toolMintChannel(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args mintChannelArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.MintChannelWithResponse(
		ctx,
		args.RadioID,
		gen.MintChannelJSONRequestBody{Name: args.Name},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("mint_channel: %w", err)
	}
	if resp.JSON202 == nil {
		return nil, nil, fmt.Errorf("mint_channel: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON202)), nil, nil
}

type importChannelsArgs struct {
	RadioID string `json:"radio_id" jsonschema:"canonical radio identifier from list_radios"`
	URL     string `json:"url"      jsonschema:"meshtastic:// or https://meshtastic.org/e/ share URL"`
}

func (s *Server) toolImportChannels(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args importChannelsArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.ImportChannelsWithResponse(
		ctx,
		args.RadioID,
		gen.ImportChannelsJSONRequestBody{Url: args.URL},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("import_channels: %w", err)
	}
	if resp.JSON202 == nil {
		return nil, nil, fmt.Errorf("import_channels: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON202)), nil, nil
}

type deleteChannelArgs struct {
	RadioID string `json:"radio_id" jsonschema:"canonical radio identifier from list_radios"`
	Index   int    `json:"index"    jsonschema:"slot index 1..7 (slot 0 is PRIMARY and cannot be deleted)"`
}

func (s *Server) toolDeleteChannel(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args deleteChannelArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.DeleteChannelWithResponse(ctx, args.RadioID, int64(args.Index))
	if err != nil {
		return nil, nil, fmt.Errorf("delete_channel: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, nil, fmt.Errorf("delete_channel: daemon returned %s", resp.Status())
	}
	return textResult(
		fmt.Sprintf("deleted channel slot %d on %s", args.Index, args.RadioID),
	), nil, nil
}

type shareChannelArgs struct {
	RadioID string `json:"radio_id" jsonschema:"canonical radio identifier from list_radios"`
	Index   int    `json:"index"    jsonschema:"slot index 0..7"`
}

func (s *Server) toolShareChannel(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args shareChannelArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.ShareChannelWithResponse(ctx, args.RadioID, int64(args.Index))
	if err != nil {
		return nil, nil, fmt.Errorf("share_channel: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, nil, fmt.Errorf("share_channel: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON200)), nil, nil
}

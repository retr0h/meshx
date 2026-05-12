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

// Transport-management tools — scan / pair / list / forget / fav /
// unfav for BLE, scan for USB. The daemon owns the host's BLE/USB
// adapter exclusively while it's running; these tools are the only
// way an agent can drive hardware discovery during a session.

func (s *Server) registerTransportTools() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "scan_ble",
		Description: "Trigger a BLE scan via the daemon's adapter. Returns every nearby Meshtastic radio's UUID + local_name + RSSI. Pass a returned UUID to pair_ble.",
	}, s.toolScanBLE)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "scan_usb",
		Description: "Walk every candidate USB-serial port and identify which respond to a non-destructive Meshtastic handshake. Returns per-port (port, is_meshtastic, name, hw_model).",
	}, s.toolScanUSB)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "pair_ble",
		Description: "Pair the daemon with a discovered BLE radio. The host OS handles the bonding (6-digit PIN dialog on macOS, BlueZ agent on Linux); the daemon persists the result. After pairing, attach the radio via `meshx server start --radio ble:<uuid>` (out of MCP scope today; restart the daemon).",
	}, s.toolPairBLE)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "list_ble_devices",
		Description: "Return every paired BLE radio in the daemon's store: UUID, longname, shortname, hardware model, favorite flag.",
	}, s.toolListBLEDevices)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "forget_ble_device",
		Description: "Remove a paired BLE device from the daemon's store. The needle can be the UUID or a longname/shortname substring.",
	}, s.toolForgetBLEDevice)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "set_ble_favorite",
		Description: "Mark a paired BLE device as the bare-launch favorite — the one bare `meshx` (no transport arg) falls through to.",
	}, s.toolSetBLEFavorite)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "clear_ble_favorite",
		Description: "Clear the bare-launch favorite. After this, bare `meshx` won't auto-connect until a new favorite is set.",
	}, s.toolClearBLEFavorite)
}

type scanArgs struct {
	TimeoutMs int `json:"timeout_ms,omitempty" jsonschema:"scan duration in milliseconds; 0 = daemon default (10000 for BLE, 1500 per port for USB)"`
}

func (s *Server) toolScanBLE(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args scanArgs,
) (*mcpsdk.CallToolResult, any, error) {
	body := gen.ScanBleJSONRequestBody{}
	if args.TimeoutMs > 0 {
		v := int64(args.TimeoutMs)
		body.TimeoutMs = &v
	}
	resp, err := s.client.ScanBleWithResponse(ctx, body)
	if err != nil {
		return nil, nil, fmt.Errorf("scan_ble: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, nil, fmt.Errorf("scan_ble: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON200)), nil, nil
}

func (s *Server) toolScanUSB(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args scanArgs,
) (*mcpsdk.CallToolResult, any, error) {
	body := gen.ScanUsbJSONRequestBody{}
	if args.TimeoutMs > 0 {
		v := int64(args.TimeoutMs)
		body.TimeoutMs = &v
	}
	resp, err := s.client.ScanUsbWithResponse(ctx, body)
	if err != nil {
		return nil, nil, fmt.Errorf("scan_usb: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, nil, fmt.Errorf("scan_usb: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON200)), nil, nil
}

type uuidArgs struct {
	UUID string `json:"uuid" jsonschema:"BLE peripheral UUID (from scan_ble) or a saved-device name/substring (the daemon resolves either)"`
}

func (s *Server) toolPairBLE(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args uuidArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.PairBleWithResponse(ctx, gen.PairBleJSONRequestBody{Uuid: args.UUID})
	if err != nil {
		return nil, nil, fmt.Errorf("pair_ble: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, nil, fmt.Errorf("pair_ble: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON200)), nil, nil
}

func (s *Server) toolListBLEDevices(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	_ struct{},
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.ListBleDevicesWithResponse(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list_ble_devices: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, nil, fmt.Errorf("list_ble_devices: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON200)), nil, nil
}

func (s *Server) toolForgetBLEDevice(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args uuidArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.ForgetBleDeviceWithResponse(ctx, args.UUID)
	if err != nil {
		return nil, nil, fmt.Errorf("forget_ble_device: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, nil, fmt.Errorf("forget_ble_device: daemon returned %s", resp.Status())
	}
	return textResult(fmt.Sprintf("forgot %s", args.UUID)), nil, nil
}

func (s *Server) toolSetBLEFavorite(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args uuidArgs,
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.SetBleFavoriteWithResponse(ctx, args.UUID)
	if err != nil {
		return nil, nil, fmt.Errorf("set_ble_favorite: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, nil, fmt.Errorf("set_ble_favorite: daemon returned %s", resp.Status())
	}
	return textResult(fmt.Sprintf("favorite set: %s", args.UUID)), nil, nil
}

func (s *Server) toolClearBLEFavorite(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	_ struct{},
) (*mcpsdk.CallToolResult, any, error) {
	resp, err := s.client.ClearBleFavoriteWithResponse(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("clear_ble_favorite: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, nil, fmt.Errorf("clear_ble_favorite: daemon returned %s", resp.Status())
	}
	return textResult("favorite cleared"), nil, nil
}

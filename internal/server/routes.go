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

package server

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"
)

// registerRoutes wires every endpoint Huma should serve. Route
// registration is the seam where method + path + request/response
// types are declared once and the OpenAPI spec falls out for free —
// no hand-written swagger.json, no doc drift.
//
// All radio-scoped resources live under /radios/{radio_id}/… so a
// single daemon can host multiple radios. {radio_id} is the
// driver.State.RadioID — "0x" + hex of MyNodeNum once handshake
// completes (or "pending:<transport>:<addr>" for a freshly-attached
// radio still waiting for its first MyInfo). GET /radios lists
// everything currently attached.
//
// OperationID strings ("list-radios", "list-channels", …) become
// the operationId in the emitted OpenAPI spec, which generated
// clients use as method names. They're stable contract surface —
// don't rename without bumping the API version.
func (s *Server) registerRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "health",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Liveness probe",
		Description: "Cheap probe for orchestration (systemd, k8s, docker healthcheck). Touches no state.",
		Tags:        []string{"meta"},
	}, s.handleHealth)

	huma.Register(s.api, huma.Operation{
		OperationID: "list-radios",
		Method:      http.MethodGet,
		Path:        "/radios",
		Summary:     "List attached radios",
		Description: "Returns every radio currently registered with the daemon — radio_id, identity, connection status. Clients use this to populate a radio picker before drilling into per-radio resources.",
		Tags:        []string{"radios"},
	}, s.handleListRadios)

	huma.Register(s.api, huma.Operation{
		OperationID: "get-radio",
		Method:      http.MethodGet,
		Path:        "/radios/{radio_id}",
		Summary:     "Per-radio session snapshot",
		Description: "Identity, telemetry, current channel, connection status for one radio. Useful for clients rendering a status bar.",
		Tags:        []string{"radios"},
	}, s.handleGetRadio)

	huma.Register(s.api, huma.Operation{
		OperationID: "list-channels",
		Method:      http.MethodGet,
		Path:        "/radios/{radio_id}/channels",
		Summary:     "List configured channel slots for one radio",
		Description: "Returns the radio's channel table — name, role, slot index, has-PSK flag. PSK bytes are NEVER emitted by this endpoint; they stay on the daemon side.",
		Tags:        []string{"channels"},
	}, s.handleListChannels)

	huma.Register(s.api, huma.Operation{
		OperationID: "list-nodes",
		Method:      http.MethodGet,
		Path:        "/radios/{radio_id}/nodes",
		Summary:     "List known mesh peers for one radio",
		Description: "Returns every peer the radio's NodeDB has seen, with the most-recent telemetry (SNR, RSSI, hops) and derived state (online / offline / muted).",
		Tags:        []string{"nodes"},
	}, s.handleListNodes)

	huma.Register(s.api, huma.Operation{
		OperationID: "list-messages",
		Method:      http.MethodGet,
		Path:        "/radios/{radio_id}/messages",
		Summary:     "List chat messages for one radio",
		Description: "Returns persisted + live in-memory chat rows in chronological order. Optional ?limit= caps response to the most recent N rows.",
		Tags:        []string{"messages"},
	}, s.handleListMessages)

	huma.Register(s.api, huma.Operation{
		OperationID: "send-message",
		Method:      http.MethodPost,
		Path:        "/radios/{radio_id}/messages",
		Summary:     "Send a chat message via one radio",
		Description: "Enqueues an outbound text message on the named channel. Returns the allocated MeshPacket.id; clients correlate this with subsequent ack / fail events from the SSE stream.",
		Tags:        []string{"messages"},
	}, s.handleSendMessage)

	sse.Register(s.api, huma.Operation{
		OperationID: "events-stream",
		Method:      http.MethodGet,
		Path:        "/radios/{radio_id}/events",
		Summary:     "Server-sent events stream for one radio",
		Description: "Live SSE stream of mesh activity for the named radio — text packets, node-info beacons, channel changes, position fixes, routing acks, traceroute / ping replies. Clients consume this for real-time UI updates.",
		Tags:        []string{"events"},
	}, eventsTypeMap, s.handleEvents)

	huma.Register(s.api, huma.Operation{
		OperationID: "list-ble-devices",
		Method:      http.MethodGet,
		Path:        "/transports/ble/devices",
		Summary:     "List paired Bluetooth devices",
		Description: "Returns every BLE device the daemon has saved for auto-connect. Favorite devices are flagged.",
		Tags:        []string{"transports"},
	}, s.handleListBLEDevices)

	huma.Register(s.api, huma.Operation{
		OperationID: "scan-ble",
		Method:      http.MethodPost,
		Path:        "/transports/ble/scan",
		Summary:     "Scan for nearby Meshtastic radios over Bluetooth",
		Description: "Runs a discovery scan for the configured timeout (default 10s) and returns peripherals advertising the Meshtastic GATT service.",
		Tags:        []string{"transports"},
	}, s.handleScanBLE)

	huma.Register(s.api, huma.Operation{
		OperationID: "pair-ble",
		Method:      http.MethodPost,
		Path:        "/transports/ble/devices",
		Summary:     "Pair a Bluetooth radio",
		Description: "Opens an encrypted GATT connection to trigger OS-level bonding (PIN prompt on macOS, agent on Linux), then saves the device to the daemon's pairing table.",
		Tags:        []string{"transports"},
	}, s.handlePairBLE)

	huma.Register(s.api, huma.Operation{
		OperationID: "forget-ble-device",
		Method:      http.MethodDelete,
		Path:        "/transports/ble/devices/{uuid}",
		Summary:     "Remove a paired Bluetooth device",
		Description: "Removes the device from the saved-pairings table. Does not unbind at the OS level.",
		Tags:        []string{"transports"},
	}, s.handleForgetBLE)

	huma.Register(s.api, huma.Operation{
		OperationID: "set-ble-favorite",
		Method:      http.MethodPut,
		Path:        "/transports/ble/devices/{uuid}/favorite",
		Summary:     "Mark a Bluetooth device as the auto-connect favorite",
		Description: "Sets this device as the bare-`meshx` auto-connect target when no USB radio is plugged in.",
		Tags:        []string{"transports"},
	}, s.handleSetBLEFavorite)

	huma.Register(s.api, huma.Operation{
		OperationID: "clear-ble-favorite",
		Method:      http.MethodDelete,
		Path:        "/transports/ble/favorite",
		Summary:     "Clear the auto-connect favorite",
		Description: "Removes the favorite flag from whichever device currently holds it. Auto-connect falls back to the single-saved-device rule.",
		Tags:        []string{"transports"},
	}, s.handleClearBLEFavorite)

	huma.Register(s.api, huma.Operation{
		OperationID: "scan-usb",
		Method:      http.MethodPost,
		Path:        "/transports/usb/scan",
		Summary:     "Identify Meshtastic radios on USB-serial",
		Description: "Walks every candidate USB-serial port, sends a Meshtastic WantConfigId handshake, returns each port's outcome (IsMeshtastic + node identity, or a reason it didn't respond).",
		Tags:        []string{"transports"},
	}, s.handleScanUSB)

	huma.Register(s.api, huma.Operation{
		OperationID: "auto-detect-usb",
		Method:      http.MethodPost,
		Path:        "/transports/usb/auto",
		Summary:     "Auto-detect a single Meshtastic radio on USB",
		Description: "Convenience over /transports/usb/scan — returns the device path of the single Meshtastic radio found. 404 when zero, 409 when multiple.",
		Tags:        []string{"transports"},
	}, s.handleAutoDetectUSB)
}

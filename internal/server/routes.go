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
)

// registerRoutes wires every endpoint Huma should serve. Route
// registration is the seam where method + path + request/response
// types are declared once and the OpenAPI spec falls out for free —
// no hand-written swagger.json, no doc drift.
//
// OperationID strings ("list-channels", etc.) become the operationId
// field in the emitted OpenAPI spec, which generated clients use as
// method names. They're stable contract surface — don't rename
// without bumping the API version.
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
		OperationID: "session-snapshot",
		Method:      http.MethodGet,
		Path:        "/session",
		Summary:     "Current radio session snapshot",
		Description: "Identity, telemetry, current channel, connection status. Useful for clients rendering a status bar.",
		Tags:        []string{"session"},
	}, s.handleSession)

	huma.Register(s.api, huma.Operation{
		OperationID: "list-channels",
		Method:      http.MethodGet,
		Path:        "/channels",
		Summary:     "List configured channel slots",
		Description: "Returns the radio's channel table — name, role, slot index, has-PSK flag. PSK bytes are NEVER emitted by this endpoint; they stay on the daemon side.",
		Tags:        []string{"channels"},
	}, s.handleListChannels)

	huma.Register(s.api, huma.Operation{
		OperationID: "list-nodes",
		Method:      http.MethodGet,
		Path:        "/nodes",
		Summary:     "List known mesh peers",
		Description: "Returns every peer the radio's NodeDB has seen, with the most-recent telemetry (SNR, RSSI, hops) and derived state (online / offline / muted).",
		Tags:        []string{"nodes"},
	}, s.handleListNodes)

	huma.Register(s.api, huma.Operation{
		OperationID: "list-messages",
		Method:      http.MethodGet,
		Path:        "/messages",
		Summary:     "List chat messages",
		Description: "Returns persisted + live in-memory chat rows for the active radio in chronological order.",
		Tags:        []string{"messages"},
	}, s.handleListMessages)
}

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

import "github.com/danielgtaylor/huma/v2"

// Cross-cutting handler glue — the per-radio resolver shared by every
// {radio_id}-scoped route, plus the SSE input shape. Per-route
// handlers + their input/output schemas live in handlers_<topic>.go.
// JSON tags on model types shape the OpenAPI spec; generated client
// SDKs deserialize into structurally identical structs.

// resolveRadio looks up the Driver for an inbound {radio_id} and
// returns 404 when the radio isn't registered.
func (s *Server) resolveRadio(radioID string) (Driver, error) {
	if s == nil || s.radios == nil {
		return nil, huma.Error503ServiceUnavailable("registry uninitialized")
	}
	d, ok := s.radios.Get(radioID)
	if !ok {
		return nil, huma.Error404NotFound("radio not registered: " + radioID)
	}
	return d, nil
}

// eventsInput is the SSE registration's typed input shape — Huma's
// sse.Register reads the path tag to populate the spec's parameters
// block. There's no body / response shape here; sse.Register provides
// the streaming response itself.
//
// LastEventID and Since are the two surfaces clients use to resume
// after a reconnect. Browser EventSource auto-emits Last-Event-ID
// from the most recent SSE id: line; curl / hand-written clients
// generally find ?since= more ergonomic. The handler accepts either
// — see resolveEventCursor for the priority rules.
type eventsInput struct {
	RadioID     string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	LastEventID string `                doc:"resumption cursor auto-emitted by EventSource clients on reconnect; the daemon replays buffered events with id > LastEventID" header:"Last-Event-ID"`
	Since       string `                doc:"explicit resumption cursor (decimal event_id); takes priority over Last-Event-ID. Use 0 to replay the entire ring buffer"                            query:"since"`
}

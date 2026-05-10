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
	"context"

	"github.com/danielgtaylor/huma/v2"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// Radio-op handlers — the HTTP twin of the TUI's /ping, /tr, and
// /sync slash commands. Each route is a thin POST that dispatches a
// typed model.Command to the underlying pump. Mirrors POST /reboot's
// fire-and-forget shape: 202 Accepted, response body echoes the
// allocated MeshPacket.id (for ping / traceroute, so SSE consumers
// can correlate against subsequent events) or just acknowledges the
// dispatch (for sync — fire-and-forget, no correlator).

// PingRequest is the inbound POST body for /ping.
type PingRequest struct {
	ToNum uint32 `json:"to_num" doc:"peer NodeNum to ping; the firmware echoes a REPLY_APP packet back which lands as a 'ping' SSE event correlated by the returned packet_id" format:"int64" minimum:"1"`
}

// PingResult acknowledges the dispatch. Clients correlate against
// the eventual `ping` SSE event using packet_id.
type PingResult struct {
	PacketID uint32 `json:"packet_id" doc:"MeshPacket.id allocated for the ping; matches the request_id field on the eventual ping event"`
	OK       bool   `json:"ok"        doc:"false when the pump's outbound buffer was full or no radio is attached"`
}

// TracerouteRequest is the inbound POST body for /traceroute.
type TracerouteRequest struct {
	ToNum uint32 `json:"to_num" doc:"peer NodeNum to trace a route to; the firmware walks the mesh and echoes a TRACEROUTE_APP packet back which lands as a 'traceroute' SSE event correlated by the returned packet_id" format:"int64" minimum:"1"`
}

// TracerouteResult acknowledges the dispatch. Clients correlate
// against the eventual `traceroute` SSE event using packet_id.
type TracerouteResult struct {
	PacketID uint32 `json:"packet_id" doc:"MeshPacket.id allocated for the traceroute; matches the request_id field on the eventual traceroute event"`
	OK       bool   `json:"ok"        doc:"false when the pump's outbound buffer was full or no radio is attached"`
}

// SyncResult acknowledges the dispatch. RequestSync is
// fire-and-forget at the wire level — the radio re-dumps its
// NodeDB / channels / configs / Metadata, each arriving as its
// own SSE event over the next few seconds.
type SyncResult struct {
	OK bool `json:"ok" doc:"false when the pump's outbound buffer was full or no radio is attached"`
}

type pingInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	Body    PingRequest
}

type pingOutput struct {
	Status int
	Body   PingResult
}

func (s *Server) handlePing(_ context.Context, in *pingInput) (*pingOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	pid, ok := d.Send(mdl.SendPing{TargetNum: in.Body.ToNum})
	if !ok {
		return nil, huma.Error503ServiceUnavailable(
			"radio outbound buffer full or no radio attached",
		)
	}
	out := &pingOutput{Status: 202}
	out.Body = PingResult{PacketID: pid, OK: ok}
	return out, nil
}

type tracerouteInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	Body    TracerouteRequest
}

type tracerouteOutput struct {
	Status int
	Body   TracerouteResult
}

func (s *Server) handleTraceroute(
	_ context.Context,
	in *tracerouteInput,
) (*tracerouteOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	pid, ok := d.Send(mdl.SendTraceroute{TargetNum: in.Body.ToNum})
	if !ok {
		return nil, huma.Error503ServiceUnavailable(
			"radio outbound buffer full or no radio attached",
		)
	}
	out := &tracerouteOutput{Status: 202}
	out.Body = TracerouteResult{PacketID: pid, OK: ok}
	return out, nil
}

type syncInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
}

type syncOutput struct {
	Status int
	Body   SyncResult
}

func (s *Server) handleSync(_ context.Context, in *syncInput) (*syncOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	_, ok := d.Send(mdl.RequestSync{})
	if !ok {
		return nil, huma.Error503ServiceUnavailable(
			"radio outbound buffer full or no radio attached",
		)
	}
	out := &syncOutput{Status: 202}
	out.Body = SyncResult{OK: ok}
	return out, nil
}

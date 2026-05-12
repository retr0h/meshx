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

package radio

import (
	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// Radio-op dispatches — Ping / Traceroute / Sync. Single source of
// truth for /ping / /tr / /sync; HTTP handlers + TUI both call.
//
// Per-consumer bookkeeping (TUI's PendingPing / PendingTraceroute
// timeout tracking, HTTP's SSE correlator) stays in the consumer.
// The session method's job is just dispatch + return the
// PacketID so the caller can correlate.

// PingRequest is the inbound shape for /ping.
type PingRequest struct {
	TargetNum uint32
}

// PingResult echoes the allocated MeshPacket.id so callers can
// correlate with the eventual ping SSE event.
type PingResult struct {
	PacketID uint32
}

// TracerouteRequest is the inbound shape for /traceroute.
type TracerouteRequest struct {
	TargetNum uint32
}

// TracerouteResult echoes the allocated MeshPacket.id so callers
// can correlate with the eventual traceroute SSE event.
type TracerouteResult struct {
	PacketID uint32
}

// SyncResult acknowledges the dispatch. RequestSync is
// fire-and-forget at the wire level — the radio re-dumps its
// NodeDB / channels / configs / Metadata, each arriving as its own
// inbound event over the next few seconds.
type SyncResult struct {
	OK bool
}

// Ping — fires a REPLY_APP packet at the target. Firmware echoes
// the packet back which surfaces as a ping SSE event. Returns the
// PacketID so consumers can correlate.
//
// Refuses TargetNum == MyNodeNum (firmware won't echo to self) with
// 400 — meaningless request, surface a clean signal instead of
// silently timing out.
func (s *Session) Ping(req PingRequest) (PingResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.TargetNum == 0 {
		return PingResult{}, ErrBadRequest("ping target NodeNum required")
	}
	if s != nil && s.State != nil && s.State.MyNodeNum != 0 &&
		req.TargetNum == s.State.MyNodeNum {
		return PingResult{}, ErrBadRequest(
			"cannot ping yourself — firmware won't echo to its own node num",
		)
	}
	pid, ok := s.Send(mdl.SendPing{TargetNum: req.TargetNum})
	if !ok {
		return PingResult{}, ErrUnavailable("radio outbound buffer full or no radio attached")
	}
	return PingResult{PacketID: pid}, nil
}

// Traceroute — fires a TRACEROUTE_APP RouteDiscovery request. The
// firmware walks the mesh and echoes a TRACEROUTE_APP reply back
// which surfaces as a traceroute SSE event.
func (s *Session) Traceroute(req TracerouteRequest) (TracerouteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.TargetNum == 0 {
		return TracerouteResult{}, ErrBadRequest("traceroute target NodeNum required")
	}
	if s != nil && s.State != nil && s.State.MyNodeNum != 0 &&
		req.TargetNum == s.State.MyNodeNum {
		return TracerouteResult{}, ErrBadRequest(
			"cannot traceroute yourself — firmware won't echo to its own node num",
		)
	}
	pid, ok := s.Send(mdl.SendTraceroute{TargetNum: req.TargetNum})
	if !ok {
		return TracerouteResult{}, ErrUnavailable("radio outbound buffer full or no radio attached")
	}
	return TracerouteResult{PacketID: pid}, nil
}

// Sync — fires WantConfigId at the radio so it re-dumps its NodeDB,
// channel table, configs, and Metadata. Each arrives as its own
// inbound event over the next few seconds; no correlator on the
// wire.
func (s *Session) Sync() (SyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Send(mdl.RequestSync{}); !ok {
		return SyncResult{}, ErrUnavailable("radio outbound buffer full or no radio attached")
	}
	return SyncResult{OK: true}, nil
}

// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
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
	"log/slog"

	"github.com/danielgtaylor/huma/v2/sse"
)

// MeshxEvent is the unified-stream wire shape — every event from
// every registered radio funnels through this envelope so a fleet
// consumer can hold one connection across all radios. Per-radio
// /radios/{id}/events keeps the per-kind typeMap so EventSource
// dispatch on `event:` lines stays idiomatic; the unified /events
// gives up that nicety in exchange for one connection across N
// radios. Clients dispatch on the `kind` field instead.
type MeshxEvent struct {
	EventID uint64 `json:"event_id" doc:"per-radio monotonic event id (use ?since= on /radios/{id}/events for resumable replay against one radio)"                              format:"int64" minimum:"0"`
	Kind    string `json:"kind"     doc:"event kind — text | dm_received | node_info | routing | message_status | …; full list at /openapi-3.0.yaml under each per-kind schema"`
	RadioID string `json:"radio_id" doc:"originating radio's canonical identifier"`
	Data    any    `json:"data"     doc:"event payload — schema depends on kind"`
}

// unifiedEventsTypeMap registers a single Go type → SSE event-name
// mapping. The unified stream emits every event under the same
// `event: meshx_event` line; the kind discriminator lives inside
// the JSON envelope.
var unifiedEventsTypeMap = map[string]any{
	"meshx_event": MeshxEvent{},
}

type unifiedEventsInput struct{}

// handleUnifiedEvents fans every registered driver's published
// events onto one SSE stream, wrapping each in a MeshxEvent envelope
// so consumers see kind + radio_id alongside the payload.
//
// Live-only V1: no replay cursor. Per-radio resumption (?since= on
// /radios/{id}/events) covers the catch-up-after-blip case for
// each radio independently; a unified cursor would need a global
// ordering across all radios, which the current per-Session counter
// doesn't provide. Worth revisiting when MCP notifications land.
func (s *Server) handleUnifiedEvents(
	ctx context.Context,
	_ *unifiedEventsInput,
	send sse.Sender,
) {
	if s == nil || s.radios == nil {
		s.logger.Warn("sse-unified: registry uninitialized")
		return
	}
	log := s.logger.With(
		slog.String("subsystem", "sse"),
		slog.String("scope", "unified"),
		slog.String("request_id", RequestIDFromContext(ctx)),
	)
	log.Info("subscribed", slog.Int("radios", len(s.radios.IDs())))
	defer log.Info("unsubscribed")

	ch := s.radios.SubscribeAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			err := send(sse.Message{
				ID: int(ev.ID),
				Data: MeshxEvent{
					EventID: ev.ID,
					Kind:    ev.Kind,
					RadioID: ev.RadioID,
					Data:    ev.Data,
				},
			})
			if err != nil {
				log.Debug(
					"send failed",
					slog.String("kind", ev.Kind),
					slog.String("radio_id", ev.RadioID),
					slog.Uint64("event_id", ev.ID),
					slog.Any("error", err),
				)
				return
			}
		}
	}
}

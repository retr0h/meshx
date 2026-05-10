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
	"log/slog"
	"strconv"

	"github.com/danielgtaylor/huma/v2/sse"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/session"
)

// eventsTypeMap registers each Go type with the SSE event name that
// gets emitted on the wire. Huma matches the Go type passed to
// send.Data against this map and writes `event: <name>\ndata: <json>`.
// Clients (the generated SDK + hand-written) key on the event name.
//
// The kinds match session.Event{Kind} constants so a future
// EventEnvelope-style send (one wire shape, kind discriminator in the
// JSON) can swap in without changing call sites — just update the map
// to register a single envelope type.
var eventsTypeMap = map[string]any{
	session.EventText:           mdl.Text{},
	session.EventNodeInfo:       mdl.NodeInfo{},
	session.EventChannelInfo:    mdl.ChannelInfo{},
	session.EventPosition:       mdl.Position{},
	session.EventRouting:        mdl.Routing{},
	session.EventTraceroute:     mdl.Traceroute{},
	session.EventPing:           mdl.Ping{},
	session.EventMyInfo:         mdl.MyInfo{},
	session.EventMetadata:       mdl.Metadata{},
	session.EventDeviceMetrics:  mdl.DeviceMetrics{},
	session.EventEnvMetrics:     mdl.EnvMetrics{},
	session.EventLoRaConfig:     mdl.LoraConfig{},
	session.EventDeviceConfig:   mdl.DeviceConfig{},
	session.EventConfigComplete: mdl.ConfigComplete{},
	session.EventReconnecting:   mdl.Reconnecting{},
	session.EventDisconnected:   mdl.Disconnected{},
	session.EventTransportError: mdl.TransportError{},
	session.EventMessageStatus:  mdl.MessageStatusUpdate{},
	session.EventDMReceived:     mdl.Text{},
}

// handleEvents subscribes to the resolved Driver's event stream and
// forwards each event as an SSE message to the connected client.
// Returns when the client disconnects (ctx cancels) or the driver is
// torn down.
//
// Honors a resumption cursor on reconnect: SSE's Last-Event-ID
// header (auto-managed by EventSource browser clients) OR an
// explicit ?since=<event_id> query param (for curl, hand-written
// HTTP clients). The cursor names the highest event_id the client
// has already processed; the daemon replays every buffered event
// with id > cursor before subscribing for live ones. Per-event
// SSE id: lines mean EventSource auto-tracks the cursor across
// reconnects with no client-side bookkeeping.
//
// The dispatch on ev.Kind picks the registered Go type for send.Data
// so Huma writes the right `event:` line on the wire — clients can
// switch on the event name without parsing JSON to know the kind.
func (s *Server) handleEvents(
	ctx context.Context,
	in *eventsInput,
	send sse.Sender,
) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		// resolveRadio's huma.Error404NotFound has already been written
		// by Huma's error path before this handler runs in the SSE
		// case... actually no: sse.Register hands us the live response
		// writer. Best we can do is log + bail; the connection will
		// 200 with no events, which is itself a signal.
		s.logger.Warn(
			"sse: resolveRadio failed",
			slog.String("radio_id", in.RadioID),
			slog.Any("error", err),
		)
		return
	}

	cursor, hasCursor := resolveEventCursor(in.LastEventID, in.Since)

	log := s.logger.With(
		slog.String("subsystem", "sse"),
		slog.String("radio_id", in.RadioID),
		slog.String("request_id", RequestIDFromContext(ctx)),
		slog.Uint64("since", cursor),
	)
	log.Info("subscribed")
	defer log.Info("unsubscribed")

	var (
		snapshot []session.Event
		ch       <-chan session.Event
	)
	if hasCursor {
		snapshot, ch = d.SubscribeWithReplay(ctx, cursor)
	} else {
		ch = d.Subscribe(ctx)
	}

	for _, ev := range snapshot {
		if err := send(sse.Message{ID: int(ev.ID), Data: ev.Data}); err != nil {
			log.Debug(
				"replay send failed",
				slog.String("kind", ev.Kind),
				slog.Uint64("event_id", ev.ID),
				slog.Any("error", err),
			)
			return
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := send(sse.Message{ID: int(ev.ID), Data: ev.Data}); err != nil {
				// Most likely the client disconnected mid-write —
				// next ctx tick catches it; log + bail so we don't
				// spam the channel until then.
				log.Debug(
					"send failed",
					slog.String("kind", ev.Kind),
					slog.Uint64("event_id", ev.ID),
					slog.Any("error", err),
				)
				return
			}
		}
	}
}

// resolveEventCursor picks the resumption cursor from inputs, in
// priority order: ?since= query param wins over Last-Event-ID
// header (the explicit query is a deliberate seek; the header is
// the auto-tracked cursor an EventSource client emits on reconnect).
// Returns (cursor, true) when a usable value parsed; (0, false)
// when neither was supplied or both failed to parse — caller skips
// replay and goes straight to live subscribe.
func resolveEventCursor(headerID, querySince string) (uint64, bool) {
	if querySince != "" {
		if id, err := strconv.ParseUint(querySince, 10, 64); err == nil {
			return id, true
		}
	}
	if headerID != "" {
		if id, err := strconv.ParseUint(headerID, 10, 64); err == nil {
			return id, true
		}
	}
	return 0, false
}

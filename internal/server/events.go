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

	"github.com/danielgtaylor/huma/v2/sse"

	"github.com/retr0h/meshx/internal/driver"
	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// eventsTypeMap registers each Go type with the SSE event name that
// gets emitted on the wire. Huma matches the Go type passed to
// send.Data against this map and writes `event: <name>\ndata: <json>`.
// Clients (the generated SDK + hand-written) key on the event name.
//
// The kinds match driver.Event{Kind} constants so a future
// EventEnvelope-style send (one wire shape, kind discriminator in the
// JSON) can swap in without changing call sites — just update the map
// to register a single envelope type.
var eventsTypeMap = map[string]any{
	driver.EventText:           mdl.Text{},
	driver.EventNodeInfo:       mdl.NodeInfo{},
	driver.EventChannelInfo:    mdl.ChannelInfo{},
	driver.EventPosition:       mdl.Position{},
	driver.EventRouting:        mdl.Routing{},
	driver.EventTraceroute:     mdl.Traceroute{},
	driver.EventPing:           mdl.Ping{},
	driver.EventMyInfo:         mdl.MyInfo{},
	driver.EventMetadata:       mdl.Metadata{},
	driver.EventDeviceMetrics:  mdl.DeviceMetrics{},
	driver.EventEnvMetrics:     mdl.EnvMetrics{},
	driver.EventLoRaConfig:     mdl.LoraConfig{},
	driver.EventDeviceConfig:   mdl.DeviceConfig{},
	driver.EventConfigComplete: mdl.ConfigComplete{},
	driver.EventReconnecting:   mdl.Reconnecting{},
	driver.EventDisconnected:   mdl.Disconnected{},
	driver.EventTransportError: mdl.TransportError{},
}

// handleEvents subscribes to the resolved Driver's event stream and
// forwards each event as an SSE message to the connected client.
// Returns when the client disconnects (ctx cancels) or the driver is
// torn down.
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

	log := s.logger.With(
		slog.String("subsystem", "sse"),
		slog.String("radio_id", in.RadioID),
		slog.String("request_id", RequestIDFromContext(ctx)),
	)
	log.Info("subscribed")
	defer log.Info("unsubscribed")

	ch := d.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := send.Data(ev.Data); err != nil {
				// Most likely the client disconnected mid-write —
				// next ctx tick catches it; log + bail so we don't
				// spam the channel until then.
				log.Debug(
					"send failed",
					slog.String("kind", ev.Kind),
					slog.Any("error", err),
				)
				return
			}
		}
	}
}

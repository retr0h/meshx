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

package session

import (
	"context"
	"sync"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// Event is the union the driver publishes whenever the canonical
// session state changes. Each variant is one of the existing model
// event types (Text, NodeInfo, ChannelInfo, Position, Routing, …)
// plus a Kind tag so JSON consumers can route without protobuf-style
// oneofs. The same shape goes onto the SSE stream, into a future
// audit log, and (eventually) feeds the in-process TUI when it
// flips to subscriber-only mode.
type Event struct {
	Kind string `json:"kind"`
	Data any    `json:"data"`
}

// EventKind tags every variant. Stable contract — the SSE consumer
// (generated SDK or hand-written) keys on these strings.
const (
	EventText           = "text"
	EventNodeInfo       = "node_info"
	EventChannelInfo    = "channel_info"
	EventPosition       = "position"
	EventRouting        = "routing"
	EventTraceroute     = "traceroute"
	EventPing           = "ping"
	EventMyInfo         = "my_info"
	EventMetadata       = "metadata"
	EventDeviceMetrics  = "device_metrics"
	EventEnvMetrics     = "env_metrics"
	EventLoRaConfig     = "lora_config"
	EventDeviceConfig   = "device_config"
	EventConfigComplete = "config_complete"
	EventReconnecting   = "reconnecting"
	EventDisconnected   = "disconnected"
	EventTransportError = "transport_error"
)

// subscriberBuffer is the per-subscriber channel depth. Big enough
// that a momentarily-slow consumer (a remote SSE client behind a
// laggy network) doesn't block the publisher; small enough that a
// permanently-stuck one drops events instead of bloating memory.
// Drops are silent — Subscribe contract says "sample, not durable"
// since the canonical state is the source of truth and clients can
// resync via GET /radios/{radio_id} after reconnect.
const subscriberBuffer = 64

// Subscribe returns a channel that receives every Event published
// on this Session until ctx is canceled. Multiple concurrent
// subscribers are supported (one for each SSE client + the future
// in-process TUI). When ctx cancels, Session removes the channel
// from its fan-out and closes it; subscribers that range over the
// channel exit cleanly.
//
// Channel buffer is subscriberBuffer events. A subscriber that
// can't keep up sees DROPPED events, not blocked publishers — fine
// because the canonical state lives in *State and a fresh
// snapshot is one HTTP GET away.
func (s *Session) Subscribe(ctx context.Context) <-chan Event {
	ch := make(chan Event, subscriberBuffer)
	s.subMu.Lock()
	s.subs = append(s.subs, ch)
	s.subMu.Unlock()
	go func() {
		<-ctx.Done()
		s.subMu.Lock()
		for i, c := range s.subs {
			if c == ch {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				break
			}
		}
		s.subMu.Unlock()
		close(ch)
	}()
	return ch
}

// Publish fans an event out to every active subscriber. Non-blocking
// — a slow subscriber drops the event rather than back-pressuring
// the apply* path. Today's call sites are the apply* handlers in
// internal/tui/radio.go (one-line publish per handler); when those
// migrate onto Session in the apply* relocation MR, the call sites
// move with them.
func (s *Session) Publish(ev Event) {
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	for _, ch := range s.subs {
		select {
		case ch <- ev:
		default:
			// Subscriber's buffer is full — drop. The state.go
			// snapshot is canonical; a re-fetch via GET /radios/{id}
			// recovers anything that was dropped here.
		}
	}
}

// PublishText is the typed shortcut for an mdl.Text event.
func (s *Session) PublishText(t mdl.Text) Event {
	ev := Event{Kind: EventText, Data: t}
	s.Publish(ev)
	return ev
}

// PublishNodeInfo is the typed shortcut for an mdl.NodeInfo event.
func (s *Session) PublishNodeInfo(n mdl.NodeInfo) Event {
	ev := Event{Kind: EventNodeInfo, Data: n}
	s.Publish(ev)
	return ev
}

// PublishChannelInfo is the typed shortcut for an mdl.ChannelInfo event.
func (s *Session) PublishChannelInfo(c mdl.ChannelInfo) Event {
	ev := Event{Kind: EventChannelInfo, Data: c}
	s.Publish(ev)
	return ev
}

// PublishPosition is the typed shortcut for an mdl.Position event.
func (s *Session) PublishPosition(p mdl.Position) Event {
	ev := Event{Kind: EventPosition, Data: p}
	s.Publish(ev)
	return ev
}

// PublishRouting is the typed shortcut for an mdl.Routing event.
func (s *Session) PublishRouting(r mdl.Routing) Event {
	ev := Event{Kind: EventRouting, Data: r}
	s.Publish(ev)
	return ev
}

// PublishTraceroute is the typed shortcut for an mdl.Traceroute event.
func (s *Session) PublishTraceroute(t mdl.Traceroute) Event {
	ev := Event{Kind: EventTraceroute, Data: t}
	s.Publish(ev)
	return ev
}

// PublishPing is the typed shortcut for an mdl.Ping event.
func (s *Session) PublishPing(p mdl.Ping) Event {
	ev := Event{Kind: EventPing, Data: p}
	s.Publish(ev)
	return ev
}

// subState is the embedded fan-out registry on Session. Kept as an
// embedded struct (rather than fields directly on Session) so the
// driver.go file stays focused on lifecycle and this file owns the
// subscribe seam end-to-end.
type subState struct {
	subMu sync.RWMutex
	subs  []chan Event
}

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
//
// ID is a per-Session monotonic uint64 assigned at Publish time.
// Consumers track the highest ID they've seen and pass it back as a
// resumption cursor (Last-Event-ID SSE header or ?since=<id> query
// param) on reconnect — the daemon replays buffered events with
// id > since before subscribing for live ones.
type Event struct {
	ID   uint64 `json:"event_id"`
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
	EventMessageStatus  = "message_status"
)

// subscriberBuffer is the per-subscriber channel depth. Big enough
// that a momentarily-slow consumer (a remote SSE client behind a
// laggy network) doesn't block the publisher; small enough that a
// permanently-stuck one drops events instead of bloating memory.
// Drops are silent — Subscribe contract says "sample, not durable"
// since the canonical state is the source of truth and clients can
// resync via GET /radios/{radio_id} after reconnect.
const subscriberBuffer = 64

// eventRingCap is the per-Session replay buffer depth. 1024 covers
// roughly 17 minutes of the busiest mesh activity we've observed —
// far longer than any reasonable client-reconnect blip — while
// keeping the per-radio memory cost trivial (~64 KB for typed
// pointer-sized Data values).
const eventRingCap = 1024

// Subscribe returns a channel that receives every Event published
// on this Session until ctx is canceled. Live-only — does not
// replay buffered history. Multiple concurrent subscribers are
// supported (one for each SSE client + the future in-process TUI).
// When ctx cancels, Session removes the channel from its fan-out
// and closes it; subscribers that range over the channel exit
// cleanly.
//
// Channel buffer is subscriberBuffer events. A subscriber that
// can't keep up sees DROPPED events, not blocked publishers — fine
// because the canonical state lives in *State and a fresh
// snapshot is one HTTP GET away.
func (s *Session) Subscribe(ctx context.Context) <-chan Event {
	_, ch := s.subscribeAt(ctx, eventCursorLatest)
	return ch
}

// SubscribeWithReplay returns any buffered events with id > sinceID
// AND a live channel for events published after the snapshot. The
// snapshot + subscribe are taken under one lock so a publish racing
// with this call can't slip an event into neither slice nor channel.
//
// Pass sinceID=0 to replay the whole buffer (oldest first).
//
// When the cursor is older than the buffer's oldest entry — the
// client was offline longer than the buffer's depth — the snapshot
// is whatever survives in the ring; the client can detect the gap
// by comparing sinceID to the first replayed event's ID.
func (s *Session) SubscribeWithReplay(
	ctx context.Context,
	sinceID uint64,
) ([]Event, <-chan Event) {
	return s.subscribeAt(ctx, sinceID)
}

// eventCursorLatest is the sentinel that subscribeAt interprets as
// "no replay — give me only events published after I subscribe."
// Distinguished from sinceID=0 (which means "give me everything in
// the buffer") because event IDs start at 1.
const eventCursorLatest uint64 = ^uint64(0)

// subscribeAt is the unified subscribe path. Snapshotting the ring
// and adding the new subscriber happen under a single lock so a
// publish racing the subscribe can't end up in neither slice nor
// channel. Above subscriberBuffer events of channel backlog the
// late events are dropped; the snapshot delivery is unbounded so
// reconnect-replay never leaves a hole the client can't see.
func (s *Session) subscribeAt(
	ctx context.Context,
	sinceID uint64,
) ([]Event, <-chan Event) {
	ch := make(chan Event, subscriberBuffer)
	s.subMu.Lock()
	var snapshot []Event
	if sinceID != eventCursorLatest {
		snapshot = s.ringSinceLocked(sinceID)
	}
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
	return snapshot, ch
}

// Publish fans an event out to every active subscriber and records
// it in the per-Session replay ring. Non-blocking on slow
// subscribers — a stuck consumer drops the event rather than
// back-pressuring the apply* path. The ring write happens under the
// fan-out lock so a SubscribeWithReplay racing this Publish either
// sees the event in its snapshot OR receives it on the new live
// channel — never both, never neither.
func (s *Session) Publish(ev Event) {
	s.subMu.Lock()
	s.nextEventID++
	ev.ID = s.nextEventID
	s.ringPushLocked(ev)
	subs := append([]chan Event(nil), s.subs...)
	s.subMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// Subscriber's buffer is full — drop. The state.go
			// snapshot is canonical; a re-fetch via GET /radios/{id}
			// recovers anything that was dropped here.
		}
	}
}

// ringPushLocked appends ev to the circular replay buffer. Caller
// must hold s.subMu.
func (s *Session) ringPushLocked(ev Event) {
	s.ring[s.ringHead] = ev
	s.ringHead = (s.ringHead + 1) % eventRingCap
	if s.ringCount < eventRingCap {
		s.ringCount++
	}
}

// ringSinceLocked returns a chronological-order copy of every
// buffered event with ID > sinceID. Caller must hold s.subMu.
func (s *Session) ringSinceLocked(sinceID uint64) []Event {
	if s.ringCount == 0 {
		return nil
	}
	start := (s.ringHead - s.ringCount + eventRingCap) % eventRingCap
	out := make([]Event, 0, s.ringCount)
	for i := 0; i < s.ringCount; i++ {
		ev := s.ring[(start+i)%eventRingCap]
		if ev.ID > sinceID {
			out = append(out, ev)
		}
	}
	return out
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

// PublishMessageStatus is the typed shortcut for an
// mdl.MessageStatusUpdate event. Fires from ApplyRouting when an
// outbound row flips to terminal Status.
func (s *Session) PublishMessageStatus(u mdl.MessageStatusUpdate) Event {
	ev := Event{Kind: EventMessageStatus, Data: u}
	s.Publish(ev)
	return ev
}

// subState is the embedded fan-out + replay registry on Session.
// Kept as an embedded struct (rather than fields directly on
// Session) so the driver.go file stays focused on lifecycle and
// this file owns the subscribe seam end-to-end.
type subState struct {
	subMu       sync.Mutex
	subs        []chan Event
	ring        [eventRingCap]Event
	ringHead    int    // next write position
	ringCount   int    // 0..eventRingCap
	nextEventID uint64 // last assigned event ID; first event published gets ID=1
}

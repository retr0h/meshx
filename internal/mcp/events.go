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

package mcp

// SSE→MCP notification bridge. When an agent calls subscribe_events,
// a goroutine starts consuming the daemon's SSE stream and forwards
// each event as an MCP Log notification to every connected session.
//
// Two subscription modes:
//   - Per-radio: subscribe_events(radio_id="0xabc") → /radios/{id}/events
//   - Unified:   subscribe_events() (no radio_id)   → /events (all radios,
//     radio_id tagged on every event envelope)
//
// Both support ?since= for resumable reconnect — the agent passes the
// last event_id it received and the daemon replays from its ring buffer.
//
// Log notifications carry a JSON envelope:
//   {"kind":"text","radio_id":"0xabc","event_id":"42","data":{…}}

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// subKeyAll is the map key for the unified /events subscription.
const subKeyAll = "*"

func (s *Server) registerEventTools() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name: "subscribe_events",
		Description: "Start streaming radio events as MCP log notifications. " +
			"Two modes: pass radio_id for a single radio's events " +
			"(/radios/{id}/events), or omit radio_id to subscribe to " +
			"ALL radios via the unified /events stream (each event " +
			"carries radio_id in the envelope). Pass since (a decimal " +
			"event_id) to resume from that cursor — the daemon replays " +
			"buffered events with id > since before streaming new ones. " +
			"Each SSE event becomes a Log notification with level=info " +
			"and JSON body: {\"kind\":\"text\",\"radio_id\":\"0x…\"," +
			"\"event_id\":\"42\",\"data\":{…}}. " +
			"One subscription per radio_id (or one unified). " +
			"Re-subscribing is a no-op.",
	}, s.toolSubscribeEvents)

	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name: "unsubscribe_events",
		Description: "Stop streaming events. Pass radio_id to unsubscribe " +
			"from a single radio, or omit radio_id to cancel the " +
			"unified all-radios subscription. Idempotent.",
	}, s.toolUnsubscribeEvents)
}

type subscribeEventsArgs struct {
	RadioID string `json:"radio_id,omitempty" jsonschema:"canonical radio identifier from list_radios; omit to subscribe to ALL radios via the unified /events stream"`
	Since   string `json:"since,omitempty"    jsonschema:"resume cursor — replay events with id > since from the daemon's ring buffer before streaming new ones; decimal event_id string"`
}

func (s *Server) toolSubscribeEvents(
	_ context.Context,
	_ *mcpsdk.CallToolRequest,
	args subscribeEventsArgs,
) (*mcpsdk.CallToolResult, any, error) {
	key := args.RadioID
	if key == "" {
		key = subKeyAll
	}
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	if _, ok := s.eventSubs[key]; ok {
		label := key
		if key == subKeyAll {
			label = "all radios"
		}
		return textResult(fmt.Sprintf("already subscribed to %s", label)), nil, nil
	}
	subCtx, cancel := context.WithCancel(context.Background())
	s.eventSubs[key] = cancel

	var sseURL string
	if args.RadioID != "" {
		sseURL = strings.TrimRight(s.serverURL, "/") + "/radios/" + args.RadioID + "/events"
	} else {
		sseURL = strings.TrimRight(s.serverURL, "/") + "/events"
	}
	if args.Since != "" {
		sseURL += "?since=" + args.Since
	}

	go s.consumeSSE(subCtx, key, sseURL)

	label := args.RadioID
	if label == "" {
		label = "all radios (unified)"
	}
	msg := fmt.Sprintf("subscribed to events for %s", label)
	if args.Since != "" {
		msg += fmt.Sprintf(" (resuming from event_id > %s)", args.Since)
	}
	return textResult(msg), nil, nil
}

type unsubscribeEventsArgs struct {
	RadioID string `json:"radio_id,omitempty" jsonschema:"radio to unsubscribe; omit to cancel the unified all-radios subscription"`
}

func (s *Server) toolUnsubscribeEvents(
	_ context.Context,
	_ *mcpsdk.CallToolRequest,
	args unsubscribeEventsArgs,
) (*mcpsdk.CallToolResult, any, error) {
	key := args.RadioID
	if key == "" {
		key = subKeyAll
	}
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	if cancel, ok := s.eventSubs[key]; ok {
		cancel()
		delete(s.eventSubs, key)
		label := key
		if key == subKeyAll {
			label = "all radios"
		}
		return textResult(fmt.Sprintf("unsubscribed from %s", label)), nil, nil
	}
	return textResult("not subscribed (no-op)"), nil, nil
}

// consumeSSE connects to the given SSE URL on the daemon and forwards
// each event as an MCP Log notification. key is the subscription map
// key (radio_id or subKeyAll). Runs until ctx cancels or the stream
// closes.
func (s *Server) consumeSSE(ctx context.Context, key, sseURL string) {
	defer func() {
		s.eventsMu.Lock()
		delete(s.eventSubs, key)
		s.eventsMu.Unlock()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return
	}

	rd := bufio.NewReader(resp.Body)
	var (
		kind string
		id   string
		data strings.Builder
	)
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if kind != "" && data.Len() > 0 {
				s.broadcastEvent(ctx, key, kind, id, data.String())
			}
			kind = ""
			id = ""
			data.Reset()
		case strings.HasPrefix(line, "id:"):
			id = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "event:"):
			kind = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			raw := strings.TrimPrefix(line, "data:")
			data.WriteString(strings.TrimLeft(raw, " "))
		}
	}
}

// broadcastEvent sends one MCP Log notification to every connected
// session. For per-radio subscriptions, radio_id is the radio; for
// unified subscriptions, radio_id is embedded in the SSE data by the
// daemon's events_unified handler.
func (s *Server) broadcastEvent(
	ctx context.Context,
	radioID, kind, eventID, payload string,
) {
	var envelope string
	if radioID == subKeyAll {
		// Unified stream — the daemon's envelope already carries
		// radio_id inside the data payload. Pass the raw event.
		envelope = fmt.Sprintf(
			`{"kind":%q,"event_id":%q,"data":%s}`,
			kind, eventID, strings.TrimSpace(payload),
		)
	} else {
		envelope = fmt.Sprintf(
			`{"kind":%q,"radio_id":%q,"event_id":%q,"data":%s}`,
			kind, radioID, eventID, strings.TrimSpace(payload),
		)
	}
	for ss := range s.mcp.Sessions() {
		_ = ss.Log(ctx, &mcpsdk.LoggingMessageParams{
			Level:  "info",
			Logger: "meshx-events",
			Data:   envelope,
		})
	}
}

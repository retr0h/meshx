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

// Package sdk is the consumer-facing companion to internal/sdk/gen.
// gen/ is the generated typed RPC layer over the daemon's HTTP+SSE
// API; this package provides Remote, the *radio.Session-shaped
// implementation of tui.radioSession that the TUI uses when pointed at
// a remote daemon. Remote satisfies the same interface *radio.Session
// satisfies in local mode, so the TUI's Update / apply* / View paths
// don't branch on mode.
package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/radio"
	"github.com/retr0h/meshx/internal/sdk/gen"
)

// Remote is the HTTP+SSE-backed radio session — the *radio.Session
// twin used in remote-client mode (`meshx remote <radio_id>
// --server URL`).
//
// Embeds a *radio.Session with nil Pump and nil Store. That gives
// Remote every Apply* and Publish* method for free, and ensures
// state mutation in remote mode goes through the *exact same code
// path* the daemon uses on its own State. The only methods Remote
// overrides are Send (POST HTTP instead of pump write) and Stop
// (cancel SSE before tearing down). AttachPump is a no-op in
// practice — the TUI's openPumpMsg path doesn't fire when the
// model is in remote mode.
//
// Wire shape:
//   - *gen.ClientWithResponses for typed outbound calls
//   - *radio.State (via the embedded Driver) seeded from initial
//     GETs and projected forward by the SSE consumer through Apply*
//   - a goroutine that reads /radios/{id}/events and forwards each
//     event to teaSend so the TUI's Update sees the same mdl.X
//     tea.Msg the local pump path would have produced
type Remote struct {
	*radio.Session

	client    *gen.ClientWithResponses
	radioID   string
	serverURL string
	authToken string // empty when the daemon is unauthenticated
	cancel    context.CancelFunc

	// teaSend is set by Start once the bubbletea program is up.
	// The SSE goroutine uses it to inject events as tea.Msg into
	// the model's Update loop. Function-typed (rather than holding
	// a *tea.Program) so the sdk package doesn't import bubbletea.
	teaSend func(msg any)
}

// NewRemote builds a Remote pointed at serverURL, verifies the radio
// is registered, and seeds *radio.State from the daemon's snapshot
// endpoints. The returned Remote is ready to satisfy radioSession but
// won't receive live events until Start is called.
//
// authToken is the bearer token the daemon expects; empty = no auth
// header (for loopback daemons running --auth-disabled or unauthed).
// Threads through both the typed HTTP calls (via WithRequestEditorFn)
// and the hand-rolled SSE reader.
func NewRemote(serverURL, authToken, radioID string) (*Remote, error) {
	opts := []gen.ClientOption{}
	if authToken != "" {
		token := authToken
		opts = append(opts, gen.WithRequestEditorFn(
			func(_ context.Context, req *http.Request) error {
				req.Header.Set("Authorization", "Bearer "+token)
				return nil
			},
		))
	}
	c, err := gen.NewClientWithResponses(serverURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("sdk: build client: %w", err)
	}
	r := &Remote{
		Session:   radio.New(radio.NewState(), nil, nil),
		client:    c,
		radioID:   radioID,
		serverURL: serverURL,
		authToken: authToken,
	}
	if err := r.seed(context.Background()); err != nil {
		return nil, err
	}
	return r, nil
}

// seed pulls the four snapshot endpoints in sequence and applies them
// to local state. Order matters only for the session snapshot — it
// sets RadioID before Channels/Nodes/Messages append. Run sequentially
// (not in parallel) because the daemon side is cheap and a 4xx on any
// of them should fail fast.
func (r *Remote) seed(ctx context.Context) error {
	snapResp, err := r.client.GetRadioWithResponse(ctx, r.radioID)
	if err != nil {
		return fmt.Errorf("sdk: get radio: %w", err)
	}
	if snapResp.JSON200 == nil {
		return fmt.Errorf("sdk: get radio %s: %s", r.radioID, snapResp.Status())
	}
	applySessionSnapshot(r.State, *snapResp.JSON200)

	chResp, err := r.client.ListChannelsWithResponse(ctx, r.radioID)
	if err != nil {
		return fmt.Errorf("sdk: list channels: %w", err)
	}
	if chResp.JSON200 != nil && chResp.JSON200.Channels != nil {
		for _, g := range *chResp.JSON200.Channels {
			r.State.Channels = append(r.State.Channels, channelFromGen(g))
		}
	}

	ndResp, err := r.client.ListNodesWithResponse(ctx, r.radioID)
	if err != nil {
		return fmt.Errorf("sdk: list nodes: %w", err)
	}
	if ndResp.JSON200 != nil && ndResp.JSON200.Nodes != nil {
		for _, g := range *ndResp.JSON200.Nodes {
			n := nodeFromGen(g)
			r.State.NodesByNum[n.NodeNum] = len(r.State.Nodes)
			r.State.Nodes = append(r.State.Nodes, n)
		}
	}

	msgResp, err := r.client.ListMessagesWithResponse(ctx, r.radioID, nil)
	if err != nil {
		return fmt.Errorf("sdk: list messages: %w", err)
	}
	if msgResp.JSON200 != nil && msgResp.JSON200.Messages != nil {
		for _, g := range *msgResp.JSON200.Messages {
			m := messageFromGen(g)
			if m.PacketID > 0 {
				r.State.MessagesByPacketID[m.PacketID] = len(r.State.Messages)
			}
			r.State.Messages = append(r.State.Messages, m)
		}
	}
	return nil
}

// Start launches the SSE goroutine. teaSend is the bubbletea program's
// Send method — typed as func(msg any) so this package doesn't import
// bubbletea. Idempotent; calling Stop and then Start re-subscribes.
func (r *Remote) Start(teaSend func(msg any)) {
	r.teaSend = teaSend
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go r.runSSE(ctx)
}

// Stop cancels the SSE goroutine and any in-flight call. Idempotent.
// The embedded Driver.Stop is also called for symmetry, even though
// the embedded driver's Pump is always nil in remote mode and Stop
// is therefore a no-op there — keeping it makes the lifecycle
// uniform with local mode.
func (r *Remote) Stop() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.Session.Stop()
}

// Send dispatches a command to the daemon. Today only mdl.SendText
// is wired — the other Command variants (SetOwner, SetBuzzer,
// RequestSync, …) need their own daemon endpoints first; they fall
// through with ok=false until then. Overrides the embedded
// Driver.Send (which would write to a nil Pump and silently no-op)
// with HTTP POST against the daemon.
func (r *Remote) Send(cmd mdl.Command) (uint32, bool) {
	switch c := cmd.(type) {
	case mdl.SendText:
		body := gen.SendMessageJSONRequestBody{
			Channel: int64(c.Channel),
			Text:    c.Text,
		}
		if c.ReplyID != 0 {
			rid := int64(c.ReplyID)
			body.ReplyId = &rid
		}
		// Bound the outbound POST so an unreachable daemon doesn't
		// freeze the TUI's Update loop. 5 seconds matches the radio
		// pump's WantAck retry window — longer than the daemon's
		// happy-path round-trip, short enough that a connection
		// failure surfaces as a flash within one or two ticks.
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		// nil params — the daemon-side Idempotency-Key dedupe is opt-in;
		// the local-mode TUI's outbound flow has its own sequencing
		// and doesn't retry POSTs, so legacy "every send hits the
		// radio" behavior is correct here.
		resp, err := r.client.SendMessageWithResponse(ctx, r.radioID, nil, body)
		if err != nil || resp.JSON200 == nil {
			return 0, false
		}
		return uint32(resp.JSON200.PacketId), resp.JSON200.Ok
	default:
		return 0, false
	}
}

// sendTimeout caps every outbound HTTP call from Remote so a slow
// or unreachable daemon can't stall the TUI's Update loop.
const sendTimeout = 5 * time.Second

// runSSE is the long-lived consumer. Opens a streaming GET against
// /radios/{id}/events, parses the text/event-stream framing
// (event: <kind>\ndata: <json>\n\n), unmarshals the data into the
// matching mdl type, and forwards it as a tea.Msg into the model's
// Update loop where existing apply* handlers run. On disconnect or
// error, returns — the caller may re-Start to reconnect (TUI-driver
// reconnect loop, separate from the daemon's radio reconnect).
func (r *Remote) runSSE(ctx context.Context) {
	url := strings.TrimRight(r.serverURL, "/") + "/radios/" + r.radioID + "/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if r.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.authToken)
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
			// End of event — dispatch.
			if kind != "" && data.Len() > 0 {
				r.dispatch(kind, data.String())
			}
			kind = ""
			data.Reset()
		case strings.HasPrefix(line, "event:"):
			kind = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(line, "data:"))
			// Huma emits "data: <json>" with one leading space; trim it.
			// Multiple data: lines concatenate per the SSE spec, so we
			// preserve newlines between them. Comment lines (": ...",
			// SSE heartbeat keepalive) and unknown prefixes fall
			// through the switch and are ignored — the next iteration
			// reads the following line.
		}
	}
}

// dispatch unmarshals one SSE event payload into the matching mdl
// type and forwards it to the tea program. Unknown kinds are dropped
// (forward compatibility — daemon may emit kinds the client doesn't
// recognize). teaSend may be nil before Start completes; in that
// window events are silently dropped, which is fine because seed()
// already populated state.
func (r *Remote) dispatch(kind, payload string) {
	if r.teaSend == nil {
		return
	}
	body := strings.TrimSpace(payload)
	send := func(v any) {
		if err := jsonUnmarshalString(body, v); err != nil {
			return
		}
		r.teaSend(derefAny(v))
	}
	switch kind {
	case radio.EventText:
		send(&mdl.Text{})
	case radio.EventNodeInfo:
		send(&mdl.NodeInfo{})
	case radio.EventChannelInfo:
		send(&mdl.ChannelInfo{})
	case radio.EventPosition:
		send(&mdl.Position{})
	case radio.EventRouting:
		send(&mdl.Routing{})
	case radio.EventTraceroute:
		send(&mdl.Traceroute{})
	case radio.EventPing:
		send(&mdl.Ping{})
	case radio.EventMyInfo:
		send(&mdl.MyInfo{})
	case radio.EventMetadata:
		send(&mdl.Metadata{})
	case radio.EventDeviceMetrics:
		send(&mdl.DeviceMetrics{})
	case radio.EventEnvMetrics:
		send(&mdl.EnvMetrics{})
	case radio.EventLoRaConfig:
		send(&mdl.LoraConfig{})
	case radio.EventDeviceConfig:
		send(&mdl.DeviceConfig{})
	case radio.EventConfigComplete:
		send(&mdl.ConfigComplete{})
	case radio.EventReconnecting:
		send(&mdl.Reconnecting{})
	case radio.EventDisconnected:
		send(&mdl.Disconnected{})
	}
}

// jsonUnmarshalString is a tiny helper so dispatch reads as one
// pattern per event kind. Wraps json.Unmarshal so callers can hand a
// pointer to a freshly-zeroed mdl type.
func jsonUnmarshalString(s string, v any) error {
	return json.NewDecoder(strings.NewReader(s)).Decode(v)
}

// derefAny dereferences a *T to T so the tea.Msg the model sees is
// the value type, matching what the local pump path injects.
func derefAny(v any) any {
	switch x := v.(type) {
	case *mdl.Text:
		return *x
	case *mdl.NodeInfo:
		return *x
	case *mdl.ChannelInfo:
		return *x
	case *mdl.Position:
		return *x
	case *mdl.Routing:
		return *x
	case *mdl.Traceroute:
		return *x
	case *mdl.Ping:
		return *x
	case *mdl.MyInfo:
		return *x
	case *mdl.Metadata:
		return *x
	case *mdl.DeviceMetrics:
		return *x
	case *mdl.EnvMetrics:
		return *x
	case *mdl.LoraConfig:
		return *x
	case *mdl.DeviceConfig:
		return *x
	case *mdl.ConfigComplete:
		return *x
	case *mdl.Reconnecting:
		return *x
	case *mdl.Disconnected:
		return *x
	}
	return v
}

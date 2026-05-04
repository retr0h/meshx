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
// API; this package provides Remote, the *driver.Driver-shaped
// implementation of tui.radioDriver that the TUI uses when pointed at
// a remote daemon. Remote satisfies the same interface *driver.Driver
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

	"github.com/retr0h/meshx/internal/driver"
	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/sdk/gen"
)

// Remote is the HTTP+SSE-backed radio session — the *driver.Driver
// twin used in remote-client mode (`meshx remote <radio_id>
// --server URL`). Owns:
//
//   - *gen.ClientWithResponses for typed outbound calls
//   - *driver.State seeded from initial GETs and projected forward
//     by the SSE consumer
//   - a goroutine that reads the daemon's /radios/{id}/events stream
//     and feeds events back into the TUI via tea.Program.Send so the
//     model's existing Update / apply* path runs unchanged
//
// AttachPump / AttachStore are no-ops, PumpHandle / StoreHandle return
// nil — there's no local pump or storage in remote mode (the daemon
// owns both). PublishX returns the event without fan-out — no one
// subscribes to a Remote.
type Remote struct {
	client    *gen.ClientWithResponses
	radioID   string
	serverURL string
	state     *driver.State
	cancel    context.CancelFunc

	// teaSend is set by Start once the bubbletea program is up.
	// The SSE goroutine uses it to inject events as tea.Msg into
	// the model's Update loop. Function-typed (rather than holding
	// a *tea.Program) so the sdk package doesn't import bubbletea.
	teaSend func(msg any)
}

// NewRemote builds a Remote pointed at serverURL, verifies the radio
// is registered, and seeds *driver.State from the daemon's snapshot
// endpoints. The returned Remote is ready to satisfy radioDriver but
// won't receive live events until Start is called.
func NewRemote(serverURL, radioID string) (*Remote, error) {
	c, err := gen.NewClientWithResponses(serverURL)
	if err != nil {
		return nil, fmt.Errorf("sdk: build client: %w", err)
	}
	r := &Remote{
		client:    c,
		radioID:   radioID,
		serverURL: serverURL,
		state:     driver.NewState(),
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
	applySessionSnapshot(r.state, *snapResp.JSON200)

	chResp, err := r.client.ListChannelsWithResponse(ctx, r.radioID)
	if err != nil {
		return fmt.Errorf("sdk: list channels: %w", err)
	}
	if chResp.JSON200 != nil && chResp.JSON200.Channels != nil {
		for _, g := range *chResp.JSON200.Channels {
			r.state.Channels = append(r.state.Channels, channelFromGen(g))
		}
	}

	ndResp, err := r.client.ListNodesWithResponse(ctx, r.radioID)
	if err != nil {
		return fmt.Errorf("sdk: list nodes: %w", err)
	}
	if ndResp.JSON200 != nil && ndResp.JSON200.Nodes != nil {
		for _, g := range *ndResp.JSON200.Nodes {
			n := nodeFromGen(g)
			r.state.NodesByNum[n.NodeNum] = len(r.state.Nodes)
			r.state.Nodes = append(r.state.Nodes, n)
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
				r.state.MessagesByPacketID[m.PacketID] = len(r.state.Messages)
			}
			r.state.Messages = append(r.state.Messages, m)
		}
	}
	return nil
}

// Start launches the SSE goroutine. teaSend is the bubbletea program's
// Send method — typed as func(msg any) so this package doesn't import
// bubbletea. Idempotent; calling Stop and then Start re-subscribes.
func (r *Remote) Start(teaSend func(msg any)) {
	if r == nil {
		return
	}
	r.teaSend = teaSend
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go r.runSSE(ctx)
}

// Stop cancels the SSE goroutine and any in-flight call. Idempotent.
func (r *Remote) Stop() {
	if r == nil || r.cancel == nil {
		return
	}
	r.cancel()
	r.cancel = nil
}

// Session returns the local State projection. Same shape and
// semantics as *driver.Driver.Session() — render code reads it
// unchanged in either mode.
func (r *Remote) Session() *driver.State {
	if r == nil {
		return nil
	}
	return r.state
}

// Send dispatches a command to the daemon. Today only mdl.SendText
// is wired — the other Command variants (SetOwner, SetBuzzer,
// RequestSync, …) need their own daemon endpoints first; they fall
// through with ok=false until then.
func (r *Remote) Send(cmd mdl.Command) (uint32, bool) {
	if r == nil {
		return 0, false
	}
	switch c := cmd.(type) {
	case mdl.SendText:
		body := gen.SendMessageJSONRequestBody{
			Channel: int64(c.Channel),
			Text:    c.Text,
		}
		if c.ReplyID != 0 {
			rid := int32(c.ReplyID)
			body.ReplyId = &rid
		}
		resp, err := r.client.SendMessageWithResponse(context.Background(), r.radioID, body)
		if err != nil || resp.JSON200 == nil {
			return 0, false
		}
		return uint32(resp.JSON200.PacketId), resp.JSON200.Ok
	default:
		return 0, false
	}
}

// AttachPump is a no-op in remote mode — the daemon owns the pump.
// Implemented to satisfy the radioDriver interface; the TUI's
// openPumpMsg path never fires when the model carries a *Remote.
func (r *Remote) AttachPump(_ driver.Pump) {}

// AttachStore is a no-op in remote mode — the daemon owns persistence.
func (r *Remote) AttachStore(_ driver.Store) {}

// PumpHandle returns nil — no local pump in remote mode.
func (r *Remote) PumpHandle() driver.Pump { return nil }

// StoreHandle returns nil — no local storage in remote mode. Apply*
// handlers in internal/tui/radio.go nil-check this before persisting,
// so save calls become no-ops; the daemon already saved before
// emitting the event over SSE.
func (r *Remote) StoreHandle() driver.Store { return nil }

// PublishText is a no-op subscribe-side fan-out — Remote has no
// subscribers. The method exists to satisfy radioDriver. In local
// mode this is the seam the SSE handler subscribes to; in remote
// mode the fan-out already happened on the daemon, the SSE arrival
// is the event, and re-publishing here would be the loopback noise.
func (r *Remote) PublishText(t mdl.Text) driver.Event {
	return driver.Event{Kind: driver.EventText, Data: t}
}

// PublishNodeInfo — see PublishText.
func (r *Remote) PublishNodeInfo(n mdl.NodeInfo) driver.Event {
	return driver.Event{Kind: driver.EventNodeInfo, Data: n}
}

// PublishChannelInfo — see PublishText.
func (r *Remote) PublishChannelInfo(c mdl.ChannelInfo) driver.Event {
	return driver.Event{Kind: driver.EventChannelInfo, Data: c}
}

// PublishPosition — see PublishText.
func (r *Remote) PublishPosition(p mdl.Position) driver.Event {
	return driver.Event{Kind: driver.EventPosition, Data: p}
}

// PublishRouting — see PublishText.
func (r *Remote) PublishRouting(rt mdl.Routing) driver.Event {
	return driver.Event{Kind: driver.EventRouting, Data: rt}
}

// PublishTraceroute — see PublishText.
func (r *Remote) PublishTraceroute(t mdl.Traceroute) driver.Event {
	return driver.Event{Kind: driver.EventTraceroute, Data: t}
}

// PublishPing — see PublishText.
func (r *Remote) PublishPing(p mdl.Ping) driver.Event {
	return driver.Event{Kind: driver.EventPing, Data: p}
}

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
	case driver.EventText:
		send(&mdl.Text{})
	case driver.EventNodeInfo:
		send(&mdl.NodeInfo{})
	case driver.EventChannelInfo:
		send(&mdl.ChannelInfo{})
	case driver.EventPosition:
		send(&mdl.Position{})
	case driver.EventRouting:
		send(&mdl.Routing{})
	case driver.EventTraceroute:
		send(&mdl.Traceroute{})
	case driver.EventPing:
		send(&mdl.Ping{})
	case driver.EventMyInfo:
		send(&mdl.MyInfo{})
	case driver.EventMetadata:
		send(&mdl.Metadata{})
	case driver.EventDeviceMetrics:
		send(&mdl.DeviceMetrics{})
	case driver.EventEnvMetrics:
		send(&mdl.EnvMetrics{})
	case driver.EventLoRaConfig:
		send(&mdl.LoraConfig{})
	case driver.EventDeviceConfig:
		send(&mdl.DeviceConfig{})
	case driver.EventConfigComplete:
		send(&mdl.ConfigComplete{})
	case driver.EventReconnecting:
		send(&mdl.Reconnecting{})
	case driver.EventDisconnected:
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

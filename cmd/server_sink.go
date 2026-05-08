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

package cmd

import (
	"log/slog"

	"github.com/retr0h/meshx/internal/session"
	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/server"
)

// daemonSink is the pump.Sink the meshx server attaches when it
// spawns a Pump for a radio. Every translated FromRadio event lands
// here; we route to the right Driver.Apply* method, which mutates
// State, persists via Store, and publishes over SSE — same code the
// TUI runs in local mode, just without the TUI presentation layer.
//
// The sink also handles the daemon-only Registry rekey: when
// ApplyMyInfo claims the canonical "0xNNNNNNNN" identity, we re-add
// the Driver under the new key so /radios/0xNNNNNNNN starts working
// immediately.
type daemonSink struct {
	drv      *session.Session
	registry *server.Registry
	log      *slog.Logger
}

// Send is the pump.Sink contract — fan-in for every event the
// translation layer produces. Untyped (any) so this package doesn't
// drag bubbletea in; we type-switch on the concrete event variant.
func (s *daemonSink) Send(msg any) {
	if s == nil || s.drv == nil {
		return
	}
	switch ev := msg.(type) {
	case mdl.MyInfo:
		res := s.drv.ApplyMyInfo(ev)
		if res.Changed && s.registry != nil {
			s.registry.Rekey(res.OldRadioID, res.NewRadioID, s.drv)
			s.log.Info(
				"radio identified",
				slog.String("old_radio_id", res.OldRadioID),
				slog.String("new_radio_id", res.NewRadioID),
				slog.Uint64("my_node_num", uint64(ev.NodeNum)),
			)
		}
	case mdl.Metadata:
		s.drv.ApplyMetadata(ev)
	case mdl.LoraConfig:
		s.drv.ApplyLoraConfig(ev)
	case mdl.DeviceConfig:
		s.drv.ApplyDeviceConfig(ev)
	case mdl.DeviceMetrics:
		s.drv.ApplyDeviceMetrics(ev)
	case mdl.EnvMetrics:
		s.drv.ApplyEnvMetrics(ev)
	case mdl.Position:
		// Daemon doesn't compute Maidenhead grid — TUI's render does.
		// Pass empty; consumers that want the grid can derive locally.
		s.drv.ApplyPosition(ev, "")
	case mdl.ChannelInfo:
		s.drv.ApplyChannelInfo(ev)
	case mdl.NodeInfo:
		s.drv.ApplyNodeInfo(ev)
	case mdl.Text:
		// Daemon takes the text as-is — sanitization is a TUI render
		// concern; persistence stores the raw bytes the radio sent.
		s.drv.ApplyText(ev, ev.Body.Text, false)
	case mdl.Routing:
		s.drv.ApplyRouting(ev)
	case mdl.Ping:
		s.drv.ApplyPing(ev)
	case mdl.Traceroute:
		s.drv.ApplyTraceroute(ev)
	case mdl.ConfigComplete:
		wasDisconnected := s.drv.ApplyConfigComplete()
		if wasDisconnected {
			s.log.Info(
				"radio connected",
				slog.String("radio_id", s.drv.State.RadioID),
				slog.Int("nodes", len(s.drv.State.Nodes)),
				slog.Int("channels", len(s.drv.State.Channels)),
				slog.Int("messages", len(s.drv.State.Messages)),
				slog.String("firmware", s.drv.State.RadioFirmware),
			)
		}
	case mdl.Reconnecting:
		s.drv.ApplyReconnecting(ev)
		errStr := ""
		if ev.Err != nil {
			errStr = ev.Err.Error()
		}
		s.log.Warn(
			"transport reconnecting",
			slog.Int("attempt", ev.Attempt),
			slog.Duration("retry_in", ev.After),
			slog.String("error", errStr),
		)
	case mdl.Disconnected:
		s.drv.ApplyDisconnected(ev)
		s.log.Info("transport disconnected")
	case mdl.TransportError:
		s.log.Error("transport error", slog.Any("error", ev.Err))
	default:
		// Unknown event variant — translation layer added a new kind
		// the sink doesn't route yet. Drop silently; no observable
		// failure mode beyond the missing State mutation.
	}
}

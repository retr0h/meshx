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

package tui

import (
	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/radio"
)

// radioSession is the narrow surface the TUI model requires of the
// headless driver layer. Declared at the consumer seam per the
// osapi-io pattern so a test double or a future in-process RPC
// variant can satisfy it without dragging the concrete *radio.Session
// into scope.
//
// Concrete *radio.Session satisfies this structurally — the compiler
// verifies at the assignment site in newModel and RunRadio.
type radioSession interface {
	// Session returns the canonical per-radio state shared between
	// the driver and the TUI. Nil when the driver is uninitialized.
	Snapshot() *radio.State

	// Send dispatches an outbound mdl.Command via the underlying pump.
	// Returns the allocated MeshPacket.id (zero for fire-and-forget)
	// and ok=false when the pump is nil or its outbound buffer is full.
	Send(cmd mdl.Command) (uint32, bool)

	// PumpHandle returns the current Pump. Nil in demo mode or before
	// the first dial. Callers that need to nil-check before sending
	// use this; high-level send paths go through Send.
	PumpHandle() radio.Pump

	// StoreHandle returns the current Store. Nil in in-memory mode.
	// Used by call sites that call Store methods directly during the
	// transition period before those calls move onto Driver methods.
	StoreHandle() radio.Store

	// Stop tears down the pump goroutines and transport. Idempotent.
	Stop()

	// Apply* mutates the canonical State in response to an inbound
	// model event. Each method publishes to subscribers on its way
	// out (defer Publish*), so consumers see the same events the
	// state mutation produced. Local mode uses *radio.Session which
	// also persists via Store; remote mode uses *sdk.Remote (which
	// embeds *radio.Session with nil Pump + nil Store) so Apply*
	// only mutates the local State projection — persistence and
	// SSE fan-out happened daemon-side before the event arrived.
	//
	// The TUI's Update dispatches every inbound mdl.X tea.Msg to
	// the matching ApplyX, then layers TUI-only side effects (flash
	// banner, ding, scrollback nudge). State mutation is single-
	// source: there is exactly one implementation in driver/apply.go.
	ApplyMyInfo(msg mdl.MyInfo) radio.ApplyMyInfoResult
	ApplyMetadata(msg mdl.Metadata)
	ApplyLoraConfig(msg mdl.LoraConfig)
	ApplyDeviceConfig(msg mdl.DeviceConfig)
	ApplyDeviceMetrics(msg mdl.DeviceMetrics)
	ApplyEnvMetrics(msg mdl.EnvMetrics)
	ApplyPosition(msg mdl.Position, grid string) radio.ApplyPositionResult
	ApplyChannelInfo(msg mdl.ChannelInfo)
	ApplyNodeInfo(msg mdl.NodeInfo) radio.ApplyNodeInfoResult
	ApplyText(ev mdl.Text, sanitizedText string, corrupted, alert bool) radio.ApplyTextResult
	ApplyRouting(msg mdl.Routing) radio.ApplyRoutingResult
	ApplyTraceroute(msg mdl.Traceroute)
	ApplyPing(msg mdl.Ping)
	ApplyConfigComplete() bool

	// RecordOutbound mirrors the inbound ApplyText path for messages
	// the user just typed locally — appends a "mine" row, persists,
	// indexes by PacketID, and publishes a synthesized mdl.Text so
	// SSE clients see the outbound row in lockstep with the daemon's
	// State.Messages append.
	RecordOutbound(opts radio.RecordOutboundOptions) radio.ApplyTextResult

	// PutSetting persists a key/value pref through the Store. Failures
	// surface via the driver's OnStoreError callback (default
	// AlertStorageError appends a permanent system row). Used for
	// /mute (ding_muted) and /config buzzer toggles.
	PutSetting(radioID, key, value string)

	// SaveNodePrefs persists a peer's favorite / muted toggle through
	// the Store. Failures surface via OnStoreError.
	SaveNodePrefs(radioID string, nodeNum uint32, favorite, muted bool)

	// Channel ops — single source of truth for /channel new / add /
	// del / share. The TUI commands wrap these with flash messages
	// and system-block output; HTTP handlers in internal/server wrap
	// them with Huma I/O structs. Same primitives, two consumers.
	MintChannel(req radio.MintChannelRequest) (radio.MintChannelResult, error)
	ImportChannel(req radio.ImportChannelRequest) (radio.ImportChannelResult, error)
	DeleteChannel(req radio.DeleteChannelRequest) (radio.DeleteChannelResult, error)
	ShareChannel(req radio.ShareChannelRequest) (radio.ChannelShareResult, error)

	// LookupChannelByName resolves a user-typed name (raw / "#name" /
	// "*name*") to a slot index. Returns -1 when no live channel
	// matches. TUI's findChannelByName uses this so the resolution
	// rule lives in one place.
	LookupChannelByName(typed string) int

	// Config + radio-op dispatches — same shared primitives the HTTP
	// handlers call. TUI commands (/nick, /tag, /config, /reboot,
	// /sync, /ping, /tr) become thin wrappers over these.
	UpdateConfig(req radio.UpdateConfigRequest) (radio.UpdateConfigResult, error)
	Reboot(req radio.RebootRequest) (radio.RebootResult, error)
	Sync() (radio.SyncResult, error)
	Ping(req radio.PingRequest) (radio.PingResult, error)
	Traceroute(req radio.TracerouteRequest) (radio.TracerouteResult, error)

	// SendMessage is the single Send-text primitive — couples pump
	// dispatch + outbound-row append + persist + publish so the TUI's
	// sendDM / sendPlainReply / sendBangReply paths and the daemon's
	// handleSendMessage all reach the same lockstep behavior.
	SendMessage(req radio.SendMessageRequest) radio.SendMessageResult
}

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

package tui

import (
	"github.com/retr0h/meshx/internal/driver"
	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// radioDriver is the narrow surface the TUI model requires of the
// headless driver layer. Declared at the consumer seam per the
// osapi-io pattern so a test double or a future in-process RPC
// variant can satisfy it without dragging the concrete *driver.Driver
// into scope.
//
// Concrete *driver.Driver satisfies this structurally — the compiler
// verifies at the assignment site in newModel and RunRadio.
type radioDriver interface {
	// Session returns the canonical per-radio state shared between
	// the driver and the TUI. Nil when the driver is uninitialized.
	Session() *driver.State

	// Send dispatches an outbound mdl.Command via the underlying pump.
	// Returns the allocated MeshPacket.id (zero for fire-and-forget)
	// and ok=false when the pump is nil or its outbound buffer is full.
	Send(cmd mdl.Command) (uint32, bool)

	// AttachPump sets the pump handle once the tea program is running.
	AttachPump(p driver.Pump)

	// AttachStore sets the storage handle after storage.New succeeds.
	AttachStore(s driver.Store)

	// PumpHandle returns the current Pump. Nil in demo mode or before
	// the first dial. Callers that need to nil-check before sending
	// use this; high-level send paths go through Send.
	PumpHandle() driver.Pump

	// StoreHandle returns the current Store. Nil in in-memory mode.
	// Used by call sites that call Store methods directly during the
	// transition period before those calls move onto Driver methods.
	StoreHandle() driver.Store

	// Stop tears down the pump goroutines and transport. Idempotent.
	Stop()

	// Apply* mutates the canonical State in response to an inbound
	// model event. Each method publishes to subscribers on its way
	// out (defer Publish*), so consumers see the same events the
	// state mutation produced. Local mode uses *driver.Driver which
	// also persists via Store; remote mode uses *sdk.Remote (which
	// embeds *driver.Driver with nil Pump + nil Store) so Apply*
	// only mutates the local State projection — persistence and
	// SSE fan-out happened daemon-side before the event arrived.
	//
	// The TUI's Update dispatches every inbound mdl.X tea.Msg to
	// the matching ApplyX, then layers TUI-only side effects (flash
	// banner, ding, scrollback nudge). State mutation is single-
	// source: there is exactly one implementation in driver/apply.go.
	ApplyMyInfo(msg mdl.MyInfo) driver.ApplyMyInfoResult
	ApplyMetadata(msg mdl.Metadata)
	ApplyLoraConfig(msg mdl.LoraConfig)
	ApplyDeviceConfig(msg mdl.DeviceConfig)
	ApplyDeviceMetrics(msg mdl.DeviceMetrics)
	ApplyEnvMetrics(msg mdl.EnvMetrics)
	ApplyPosition(msg mdl.Position, grid string) driver.ApplyPositionResult
	ApplyChannelInfo(msg mdl.ChannelInfo)
	ApplyNodeInfo(msg mdl.NodeInfo) driver.ApplyNodeInfoResult
	ApplyText(ev mdl.Text, sanitizedText string, corrupted bool) driver.ApplyTextResult
	ApplyRouting(msg mdl.Routing) driver.ApplyRoutingResult
	ApplyTraceroute(msg mdl.Traceroute)
	ApplyPing(msg mdl.Ping)
	ApplyConfigComplete() bool
}

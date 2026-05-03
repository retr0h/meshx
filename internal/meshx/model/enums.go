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

package model

// Typed-string enums for fields that the pump used to surface as
// raw strings. Consumers read .String() on the proto enum at the
// pump boundary, then cast into the typed alias here so the rest of
// the codebase can compare against constants without typos
// silently never matching.
//
// Wire format stays the proto enum name (e.g. "US", "LONG_FAST",
// "CLIENT") because that's what every other Meshtastic client
// renders, what the SQLite settings rows use, and what config-edit
// commands accept. Constants are the well-known values we touch
// today; unknown values still flow through (cast to the typed
// string) — the renderer just shows the raw enum name.

// Region is the LoRa region code from Config.LoRaConfig. Drives
// modem timing + power limits.
type Region string

// Region constants match the firmware's RegionCode enum names.
const (
	RegionUnset Region = "UNSET"
	RegionUS    Region = "US"
	RegionEU868 Region = "EU_868"
	RegionEU433 Region = "EU_433"
	RegionCN    Region = "CN"
	RegionJP    Region = "JP"
	RegionANZ   Region = "ANZ"
)

// ModemPreset is the LoRa data-rate profile — speed vs range
// tradeoff. LongFast is the firmware default and what the public
// mesh runs on.
type ModemPreset string

// ModemPreset constants match the firmware's ModemPreset enum names.
const (
	ModemLongFast     ModemPreset = "LONG_FAST"
	ModemLongSlow     ModemPreset = "LONG_SLOW"
	ModemVeryLongSlow ModemPreset = "VERY_LONG_SLOW"
	ModemMediumSlow   ModemPreset = "MEDIUM_SLOW"
	ModemMediumFast   ModemPreset = "MEDIUM_FAST"
	ModemShortSlow    ModemPreset = "SHORT_SLOW"
	ModemShortFast    ModemPreset = "SHORT_FAST"
	ModemLongModerate ModemPreset = "LONG_MODERATE"
	ModemShortTurbo   ModemPreset = "SHORT_TURBO"
)

// DeviceRole is Config.DeviceConfig.Role — what the radio thinks it
// is on the mesh (client, router, repeater, …). Drives whether the
// node forwards traffic, beacons frequently, etc.
type DeviceRole string

// DeviceRole constants match the firmware's Role enum names.
const (
	RoleClient       DeviceRole = "CLIENT"
	RoleClientMute   DeviceRole = "CLIENT_MUTE"
	RoleClientHidden DeviceRole = "CLIENT_HIDDEN"
	RoleRouter       DeviceRole = "ROUTER"
	RoleRouterClient DeviceRole = "ROUTER_CLIENT"
	RoleRepeater     DeviceRole = "REPEATER"
	RoleTracker      DeviceRole = "TRACKER"
	RoleSensor       DeviceRole = "SENSOR"
	RoleTAK          DeviceRole = "TAK"
	RoleLostAndFound DeviceRole = "LOST_AND_FOUND"
	RoleTAKTracker   DeviceRole = "TAK_TRACKER"
)

// ChannelRole is Channel.Role — DISABLED slots are kept addressable
// so /channel new can re-use them; PRIMARY is the active default
// channel; SECONDARY is anything else.
type ChannelRole string

// ChannelRole constants match the firmware's Channel_Role enum names.
const (
	ChannelDisabled  ChannelRole = "DISABLED"
	ChannelPrimary   ChannelRole = "PRIMARY"
	ChannelSecondary ChannelRole = "SECONDARY"
)

// NodeState is the displayed state of a peer in the nodes pane —
// online / offline / failed / muted. Lives in model/ because the
// daemon emits it over HTTP+SSE to remote clients (it's part of the
// API surface), even though it's never persisted in SQLite. The
// canonical derivation (LastHeardAt → Online/Offline, with Muted as
// a sticky override) lives on the server side; clients just render
// whatever the API returns.
type NodeState int

const (
	// StateUnknown — never heard from this peer (LastHeardAt zero, no
	// fixture seed). Stringifies to "" so display printf("%s") gets
	// nothing visible for a never-heard peer instead of a literal
	// "unknown" leaking into the UI.
	StateUnknown NodeState = iota
	// StateOnline — heard from in the last 15 minutes.
	StateOnline
	// StateOffline — known peer we haven't heard from recently.
	StateOffline
	// StateFailed — peer we've actively failed to reach (currently
	// only set by /tr / /ping timeout flows).
	StateFailed
	// StateMuted — user-sticky preference (the `m` nav key flips
	// this). Always wins over the LastHeardAt-derived states; persists
	// across restarts via the nodes.muted column.
	StateMuted
)

// String returns the human/wire form. StateUnknown → "" by design.
func (s NodeState) String() string {
	switch s {
	case StateOnline:
		return "online"
	case StateOffline:
		return "offline"
	case StateFailed:
		return "failed"
	case StateMuted:
		return "muted"
	default:
		return ""
	}
}

// RoutingError is Routing.error_reason — the firmware's verdict on
// an outbound packet. RoutingNone means delivered.
type RoutingError string

// RoutingError constants match the firmware's Routing.Error enum names.
const (
	RoutingNone           RoutingError = "NONE"
	RoutingNoRoute        RoutingError = "NO_ROUTE"
	RoutingGotNak         RoutingError = "GOT_NAK"
	RoutingTimeout        RoutingError = "TIMEOUT"
	RoutingNoInterface    RoutingError = "NO_INTERFACE"
	RoutingMaxRetransmit  RoutingError = "MAX_RETRANSMIT"
	RoutingNoChannel      RoutingError = "NO_CHANNEL"
	RoutingTooLarge       RoutingError = "TOO_LARGE"
	RoutingNoResponse     RoutingError = "NO_RESPONSE"
	RoutingDutyCycleLimit RoutingError = "DUTY_CYCLE_LIMIT"
	RoutingBadRequest     RoutingError = "BAD_REQUEST"
	RoutingNotAuthorized  RoutingError = "NOT_AUTHORIZED"
)

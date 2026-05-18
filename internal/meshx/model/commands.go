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

package model

// Commands are the outbound twin of Events: typed values the
// consumer hands the pump for transmission. Pump's Send method
// type-switches on the concrete command and assembles the proto
// envelope internally — meshx never touches gomeshproto.
//
// Symmetric with events.go: events flow consumer-ward as bare Go
// values dispatched by Bubble Tea on type identity; commands flow
// pump-ward as bare Go values dispatched by pump on type identity.

// Command is the marker interface every outbound command satisfies.
// The unexported isCommand method keeps consumers from accidentally
// passing arbitrary types to pump.Send (the closed-set property of
// a discriminated union, enforced at compile time).
type Command interface {
	isCommand()
}

// SendText sends a chat message. ToNum=0 is the broadcast default
// (MeshPacket.to=0xFFFFFFFF, the firmware-canonical "everyone on
// this channel"); a non-zero ToNum addresses a specific peer for a
// direct message. ReplyID non-zero threads the message to a parent
// packet (used by /reply, /73, etc.). Send returns a freshly
// generated packetID the consumer can stash on the local
// messageItem for ack correlation.
type SendText struct {
	Channel int
	Text    string
	ReplyID uint32
	ToNum   uint32
}

func (SendText) isCommand() {}

// SendPing fires a REPLY_APP packet at the target so the firmware's
// echo service can bounce it back — the round trip measures
// latency. Send returns a packetID the consumer correlates against
// the eventual mdl.Ping event.
type SendPing struct {
	TargetNum uint32
}

func (SendPing) isCommand() {}

// SendTraceroute fires a TRACEROUTE_APP RouteDiscovery request.
// Send returns the request packetID the consumer correlates against
// the eventual mdl.Traceroute event (Routing.request_id matches).
type SendTraceroute struct {
	TargetNum uint32
}

func (SendTraceroute) isCommand() {}

// SetOwner writes the radio's User record (longname / shortname /
// is_licensed). Firmware ≥ 2.3 accepts hot updates (no reboot
// required). Fire-and-forget — the consumer flashes optimistically
// and observes the resulting NodeInfo broadcast a moment later.
type SetOwner struct {
	LongName   string
	ShortName  string
	IsLicensed bool
}

func (SetOwner) isCommand() {}

// SetBuzzer toggles the radio's ExternalNotification module.
// Snapshot is the full live config the radio reported (or zero
// if we toggled before the live config arrived) — pump preserves
// every field other than Enabled / AlertMessage* on round-trip.
type SetBuzzer struct {
	Enabled  bool
	Snapshot ExternalNotification
}

func (SetBuzzer) isCommand() {}

// SetChannel writes one channel slot. Slot.Index addresses the
// slot (0 is PRIMARY, 1-7 are SECONDARY); Slot.Role decides whether
// the slot is enabled. To wipe a slot, send DeleteChannel instead.
type SetChannel struct {
	Slot ChannelInfo
}

func (SetChannel) isCommand() {}

// DeleteChannel disables a slot — sets Channel.Role to DISABLED
// with empty settings, the firmware-canonical "free this slot"
// gesture. Refuses index 0 (PRIMARY) at the consumer; pump trusts
// the index it's given.
type DeleteChannel struct {
	Index int
}

func (DeleteChannel) isCommand() {}

// RequestSync fires WantConfigId — prompts the radio to re-dump
// its NodeDB, channels, configs, and ConfigComplete. Used by /sync.
type RequestSync struct{}

func (RequestSync) isCommand() {}

// RequestBuzzerConfig asks the radio to send back its
// ExternalNotification module config. Used by ConfigComplete handler
// when the WantConfigId dump didn't include the module config (some
// firmware skips it) so /config has a live snapshot to round-trip.
type RequestBuzzerConfig struct{}

func (RequestBuzzerConfig) isCommand() {}

// Reboot reboots the radio after Seconds (firmware schedules a
// shutdown timer). Used by /reboot. Seconds=0 is "now" per the
// firmware convention.
type Reboot struct {
	Seconds int32
}

func (Reboot) isCommand() {}

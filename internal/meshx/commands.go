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

// commands.go wires the entire slash-command surface:
//
//   - executeCommand — the dispatcher ham / messaging / overlay verbs
//     all route through.
//   - sendBang / sendBangReply / systemLine / systemBlock — the four
//     primitives every slash handler uses to emit a user-visible row.
//   - newTextToRadio / newAdminSetOwner / setOwner — ToRadio envelope
//     builders + the AdminMessage write path used by /nick and /tag.
//   - small helpers (activate, actOnSelectedNode, ackWord,
//     currentChannelIndex, replyTargetFor) that commands lean on.
//
// Model / Update / render surface stays in app.go and ui.go.

package meshx

import (
	"fmt"
	mathrand "math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"
)

func (m *model) sendPlainMessage(text string) {
	m.sendPlainReply(text, 0)
}

// sendPlainReply is sendPlainMessage with an optional Data.reply_id
// for threading. Used by /reply, /msg, /me — anything that's
// semantically "regular chat with a directed flavor," NOT a /bang
// command. Routes through the same TEXT_MESSAGE_APP path sendBang
// uses; the only difference is msg.bang stays empty so the chat row
// renders with the magenta `›` "mine" marker instead of the yellow
// `*` bang flag.
func (m *model) sendPlainReply(text string, replyToID uint32) {
	var pid uint32
	var envelope *pb.ToRadio
	if m.pump != nil {
		envelope, pid = newTextToRadio(text, m.currentChannelIndex(), replyToID)
	}
	item := messageItem{
		time: timeNowHHMM(), from: "me", mine: true, text: text,
		status: "pending", packetID: pid,
		replyID: replyToID,
		fromNum: m.myNodeNum,
		sentAt:  time.Now(),
	}
	m.messages = append(m.messages, item)
	m.selectedMsg = len(m.messages) - 1
	m.flash = fmt.Sprintf("sent in %s", m.currentChannel)

	m.storagePersist(saveMessage(m.db, m.currentChannel, item))

	if envelope != nil {
		m.pump.Enqueue(envelope)
	}
}

// newTextToRadio builds the ToRadio envelope for a plain text chat
// message on a named channel index. Broadcast (to = 0xFFFFFFFF) on
// PortNum TEXT_MESSAGE_APP (1) — the canonical Meshtastic chat path.
// When replyID != 0 the packet threads to the referenced parent
// (Data.reply_id) — this is how /73 <call> and friends tie their
// outgoing text to the specific message from that operator.
//
// Returns both the envelope and the generated MeshPacket.id so the
// caller can stash it on the local messageItem.packetID — that's
// the correlation key the ROUTING_APP reply lands on when the
// radio acks (or errors) delivery.
func newTextToRadio(text string, channel, replyID uint32) (*pb.ToRadio, uint32) {
	pid := randPacketID()
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			Id:      pid,
			To:      0xFFFFFFFF,
			Channel: channel,
			WantAck: true,
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum: pb.PortNum_TEXT_MESSAGE_APP,
				Payload: []byte(text),
				ReplyId: replyID,
			}},
		}},
	}, pid
}

// randPacketID returns a non-zero 32-bit id suitable for
// MeshPacket.id. Zero is reserved (the firmware treats 0 as
// "unassigned") so we re-roll on the unlikely collision.
func randPacketID() uint32 {
	for {
		pid := mathrand.Uint32()
		if pid != 0 {
			return pid
		}
	}
}

// setOwner validates the desired longname / shortname and sends an
// AdminMessage.SetOwner to the radio. `which` is "long" or "short"
// and controls which field the user is targeting — the other field
// is carried through unchanged from the current config (firmware
// overwrites the whole User record, so we have to round-trip both).
// Called by /nick and /tag. Returns a tea.Cmd for consistency with
// the dispatcher's expected shape; today it's always nil.
func (m *model) setOwner(longName, shortName, which string) tea.Cmd {
	// Demo mode has no real radio — just flash a hint.
	if m.isDemo() {
		m.flash = "/" + which + "name takes effect on a real radio (demo mode)"
		return nil
	}
	if m.pump == nil {
		m.flash = "/" + which + "name needs a live radio connection"
		return nil
	}
	target := strings.TrimSpace(longName)
	if which == "short" {
		target = strings.TrimSpace(shortName)
	}
	if target == "" {
		if which == "short" {
			m.flash = "usage: /tag <1-4 chars or emoji>"
		} else {
			m.flash = "usage: /nick <longname>"
		}
		return nil
	}
	// Meshtastic field lengths per the proto — longname up to 36
	// bytes, shortname up to 4 bytes. We measure in bytes not
	// runes because that's what firmware enforces.
	switch which {
	case "long":
		if len(longName) > 36 {
			m.flash = fmt.Sprintf("/nick rejected: longname %d bytes, max 36", len(longName))
			return nil
		}
	case "short":
		if len(shortName) > 4 {
			m.flash = fmt.Sprintf(
				"/tag rejected: shortname %d bytes, max 4 (%d-byte emoji counts as 1 char)",
				len(shortName),
				len(shortName),
			)
			return nil
		}
	}
	isLicensed := false
	if n := m.myNode(); n != nil {
		// Preserve the existing is_licensed flag across renames.
		// We don't expose a /license command today so this just
		// stays however the user configured it via the phone app.
		_ = n
	}
	envelope, err := newAdminSetOwner(m.myNodeNum, longName, shortName, isLicensed)
	if err != nil {
		m.flash = fmt.Sprintf("/%sname failed: %v", which, err)
		return nil
	}
	if !m.pump.Enqueue(envelope) {
		m.flash = "/" + which + "name dropped — outbound buffer full"
		return nil
	}
	switch which {
	case "long":
		m.systemLine(
			fmt.Sprintf("nick → %s (radio will re-broadcast NodeInfo on next cycle)", longName),
		)
	case "short":
		m.systemLine(
			fmt.Sprintf("tag → %s (radio will re-broadcast NodeInfo on next cycle)", shortName),
		)
	}
	return nil
}

// newAdminSetOwner builds an AdminMessage.SetOwner ToRadio envelope,
// wrapped in a MeshPacket addressed to our own node. This is how
// meshX updates the radio's User config (longname / shortname) over
// the wire — same AdminMessage path the official Meshtastic phone
// apps and Python CLI use.
//
// The phone apps chase SetOwner with a reboot; we don't. Meshtastic
// firmware >= 2.3 accepts User updates hot (updates in-memory,
// persists to flash, keeps running). Skipping the reboot saves
// ~20 seconds of downtime per rename and is safe on any current
// build.
func newAdminSetOwner(
	myNodeNum uint32,
	longName, shortName string,
	isLicensed bool,
) (*pb.ToRadio, error) {
	admin := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_SetOwner{
			SetOwner: &pb.User{
				LongName:   longName,
				ShortName:  shortName,
				IsLicensed: isLicensed,
			},
		},
	}
	payload, err := proto.Marshal(admin)
	if err != nil {
		return nil, fmt.Errorf("marshal AdminMessage: %w", err)
	}
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			// AdminMessage is addressed to OUR OWN node num — the
			// radio treats it as a local config write.
			To:      myNodeNum,
			WantAck: true,
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum: pb.PortNum_ADMIN_APP,
				Payload: payload,
			}},
		}},
	}, nil
}

// newAdminSetBuzzer builds an AdminMessage.SetModuleConfig envelope
// for ExternalNotificationConfig. snapshot is the full live config
// the radio reported (or a fresh empty config if we never saw one);
// we copy it and overwrite only Enabled + AlertMessageBuzzer so other
// fields the user might have configured via the phone app (output
// pin, alert_bell, vibra, nag_timeout, …) ride through verbatim.
// Both fields move together: a meaningful "buzzer on" requires
// Enabled=true AND AlertMessageBuzzer=true, and "off" forces both
// false so the module doesn't keep firing on bell or vibra paths
// users probably don't realize were enabled.
//
// Like setOwner this skips the reboot the official phone app issues
// after a module-config write — modern firmware accepts module config
// hot. If a future radio refuses the change without a reboot we'd
// need to chain an AdminMessage_RebootSeconds packet here.
func newAdminSetBuzzer(
	myNodeNum uint32,
	enabled bool,
	snapshot *pb.ModuleConfig_ExternalNotificationConfig,
) (*pb.ToRadio, error) {
	// proto.Clone on the snapshot preserves every field the radio
	// reported (including ones we never surface in /config) without
	// copying the proto's internal MessageState mutex — direct struct
	// assignment trips go-vet's copylocks. Empty fallback covers
	// "user toggled before the live config arrived" — we still get a
	// usable payload, just without round-tripping unrelated fields.
	var ext *pb.ModuleConfig_ExternalNotificationConfig
	if snapshot != nil {
		ext = proto.Clone(snapshot).(*pb.ModuleConfig_ExternalNotificationConfig)
	} else {
		ext = &pb.ModuleConfig_ExternalNotificationConfig{}
	}
	// Set ALL three message-alert output paths together. Meshtastic
	// firmware fires whichever path the hardware has wired — a
	// piezo on output_buzzer, a vibra motor on output_vibra, or a
	// simple LED on output. Setting just AlertMessageBuzzer left
	// radios with the notification on a different pin silent. Bell
	// paths (alert_bell_*) stay at whatever the snapshot had — that
	// flag is for the BEL character on incoming text, separate from
	// the message-arrival alert /config exposes.
	//
	// UsePwm rides with the toggle because most ham-radio boards
	// (T-Beam, Heltec, RAK 4631 with WisBlock buzzer) have a hardware
	// buzzer on the device-level `device.buzzer_gpio` pin, NOT on
	// External Notification's per-module output_pin (which is "Unset"
	// out of the box). With UsePwm=true the module ignores the
	// per-module output pin / duration / active polarity and routes
	// through device.buzzer_gpio — which is what's actually wired.
	// Without it, "buzzer on" sets every alert flag but the firmware
	// has no GPIO to drive and the radio stays silent. The phone app
	// surfaces this same field as "Use PWM Buzzer".
	ext.Enabled = enabled
	ext.AlertMessage = enabled
	ext.AlertMessageBuzzer = enabled
	ext.AlertMessageVibra = enabled
	ext.UsePwm = enabled
	admin := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_SetModuleConfig{
			SetModuleConfig: &pb.ModuleConfig{
				PayloadVariant: &pb.ModuleConfig_ExternalNotification{
					ExternalNotification: ext,
				},
			},
		},
	}
	payload, err := proto.Marshal(admin)
	if err != nil {
		return nil, fmt.Errorf("marshal AdminMessage: %w", err)
	}
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			To:      myNodeNum,
			WantAck: true,
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum: pb.PortNum_ADMIN_APP,
				Payload: payload,
			}},
		}},
	}, nil
}

// meshtasticChannelSlots is the firmware's hard cap on simultaneous
// channels. Slot 0 is always PRIMARY; 1..7 are SECONDARY. /channel
// new + add allocate into the first DISABLED slot >= 1; PRIMARY is
// off-limits because the radio refuses to operate without one.
const meshtasticChannelSlots = 8

// newAdminSetChannel builds an AdminMessage.SetChannel ToRadio
// envelope for a single channel slot. Same wire path as setOwner /
// newAdminSetBuzzer — admin packet addressed to our own node, the
// radio applies it locally and rebroadcasts NodeInfo so peers see the
// new channel name. role is one of pb.Channel_PRIMARY (slot 0 only),
// pb.Channel_SECONDARY (1..7), or pb.Channel_DISABLED (delete).
//
// settings can be nil for the disable case — the radio frees the slot
// and wipes the PSK either way. For add / new, settings carries the
// PSK + name + uplink/downlink flags.
func newAdminSetChannel(
	myNodeNum uint32,
	index int32,
	role pb.Channel_Role,
	settings *pb.ChannelSettings,
) (*pb.ToRadio, error) {
	admin := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_SetChannel{
			SetChannel: &pb.Channel{
				Index:    index,
				Settings: settings,
				Role:     role,
			},
		},
	}
	payload, err := proto.Marshal(admin)
	if err != nil {
		return nil, fmt.Errorf("marshal AdminMessage: %w", err)
	}
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			To:      myNodeNum,
			WantAck: true,
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum: pb.PortNum_ADMIN_APP,
				Payload: payload,
			}},
		}},
	}, nil
}

// findFreeChannelSlot returns the first slot index >= 1 that is
// DISABLED (or beyond the radio's reported set), suitable for /channel
// new + /channel add to allocate into. Returns -1 if all 7 secondary
// slots are taken — caller should flash "no free slots, /channel del
// something first."
//
// Slot 0 is intentionally skipped — that's PRIMARY's home and the
// firmware refuses to operate without one. Adding via slot 0 would
// silently nuke the user's primary channel; refuse and let them
// /channel del + /channel new explicitly if they really want to
// replace the primary.
func (m *model) findFreeChannelSlot() int {
	for i := 1; i < meshtasticChannelSlots; i++ {
		if i >= len(m.channels) {
			return i
		}
		if m.channels[i].role == "DISABLED" || m.channels[i].role == "" {
			return i
		}
	}
	return -1
}

// findChannelByName looks up a channel slot by user-typed name. The
// renderer prefixes names with "#" / "*…*" for display; we accept the
// bare name, the prefixed form, or a leading "#" — whatever the user
// types should resolve. Case-sensitive because PSK lookup ultimately
// matches on bytes. Returns -1 if not found or DISABLED.
func (m *model) findChannelByName(typed string) int {
	want := strings.TrimSpace(typed)
	want = strings.TrimPrefix(want, "#")
	want = strings.Trim(want, "*")
	for i, c := range m.channels {
		if c.role == "DISABLED" {
			continue
		}
		bare := strings.TrimPrefix(c.name, "#")
		bare = strings.Trim(bare, "*")
		if bare == want {
			return i
		}
	}
	return -1
}

// channelDel disables a channel slot via AdminMessage_SetChannel
// with role=DISABLED and nil settings — the radio frees the slot and
// wipes the PSK. Refuses to delete the PRIMARY (slot 0) because the
// firmware requires one to operate; the user can /channel rename or
// /config the primary instead.
//
// No confirmation prompt — the cost of an accidental /channel del is
// just /channel add the URL again (if you have it), and forcing y/n
// on every channel-management command would feel patronizing for what
// is fundamentally a local-state edit. If we ever ship /channel
// backup, we can add a "deleted N channels" undo window.
func (m *model) channelDel(typed string) tea.Cmd {
	if m.isDemo() {
		m.flash = "/channel del takes effect on a real radio (demo mode)"
		return nil
	}
	if m.pump == nil {
		m.flash = "/channel del needs a live radio connection"
		return nil
	}
	idx := m.findChannelByName(typed)
	if idx < 0 {
		m.flash = fmt.Sprintf("/channel del: no channel matching %q", typed)
		return nil
	}
	if m.channels[idx].role == "PRIMARY" {
		m.flash = "/channel del: cannot delete the primary channel — use /config to rename"
		return nil
	}
	envelope, err := newAdminSetChannel(
		m.myNodeNum, int32(idx), pb.Channel_DISABLED, nil,
	)
	if err != nil {
		m.flash = fmt.Sprintf("/channel del failed: %v", err)
		return nil
	}
	if !m.pump.Enqueue(envelope) {
		m.flash = "/channel del dropped — outbound buffer full"
		return nil
	}
	deletedName := m.channels[idx].name
	// Optimistically clear the slot — the radio's ChannelInfo
	// rebroadcast will reconfirm, but the UI shouldn't keep showing a
	// channel the user just deleted while waiting for that round trip.
	m.channels[idx] = channelItem{
		index: idx,
		role:  "DISABLED",
	}
	if m.currentChannel == deletedName {
		// User deleted the channel they were on. Snap back to the
		// primary so the input bar has a valid target.
		for _, c := range m.channels {
			if c.role == "PRIMARY" {
				m.currentChannel = c.name
				break
			}
		}
	}
	m.systemLine(fmt.Sprintf("channel %s deleted (slot %d freed)", deletedName, idx))
	m.flash = fmt.Sprintf("deleted %s", deletedName)
	return nil
}

// channelAdd accepts a meshtastic://e/#... or
// https://meshtastic.org/e/#... URL, decodes the embedded ChannelSet,
// and pushes each channel into the first free secondary slot via
// AdminMessage_SetChannel. Skips channels whose name already exists
// on the radio (additive only — never overwrites). Refuses to push
// into slot 0 (PRIMARY) so a malformed share link can't nuke the
// user's primary channel.
func (m *model) channelAdd(rawURL string) tea.Cmd {
	if m.isDemo() {
		m.flash = "/channel add takes effect on a real radio (demo mode)"
		return nil
	}
	if m.pump == nil {
		m.flash = "/channel add needs a live radio connection"
		return nil
	}
	cs, err := parseChannelShareURL(rawURL)
	if err != nil {
		m.flash = "/channel add: " + err.Error()
		return nil
	}
	added, skipped := 0, 0
	var summary []string
	for _, s := range cs.GetSettings() {
		name := s.GetName()
		if name == "" {
			// Empty-name in a share URL means "default channel" —
			// almost certainly a mistake (sharing the well-known
			// LongFast channel that everyone is already on). Skip
			// rather than silently overwrite slot 0 / dupe LongFast.
			summary = append(summary, "skip: <empty name> (would clobber default)")
			skipped++
			continue
		}
		if m.findChannelByName(name) >= 0 {
			summary = append(summary, fmt.Sprintf("skip: %q already on radio", name))
			skipped++
			continue
		}
		slot := m.findFreeChannelSlot()
		if slot < 0 {
			summary = append(summary,
				fmt.Sprintf("skip: %q — no free slots (max %d, /channel del to free one)",
					name, meshtasticChannelSlots-1))
			skipped++
			continue
		}
		envelope, err := newAdminSetChannel(
			m.myNodeNum, int32(slot), pb.Channel_SECONDARY, s,
		)
		if err != nil {
			summary = append(summary, fmt.Sprintf("fail: %q — %v", name, err))
			skipped++
			continue
		}
		if !m.pump.Enqueue(envelope) {
			summary = append(summary, fmt.Sprintf("fail: %q — outbound buffer full", name))
			skipped++
			continue
		}
		// Optimistically populate the slot so a follow-up /channel new
		// finds the next free slot rather than re-using this one. The
		// radio's ChannelInfo broadcast will overwrite this row with
		// the canonical state shortly.
		display := "#" + name
		if len(s.GetPsk()) > 0 {
			display = "*" + name + "*"
		}
		m.channels[slot] = channelItem{
			name:    display,
			private: len(s.GetPsk()) > 0,
			index:   slot,
			role:    "SECONDARY",
			psk:     s.GetPsk(),
		}
		summary = append(summary, fmt.Sprintf("add:  %s → slot %d", display, slot))
		added++
	}
	if added == 0 && skipped == 0 {
		m.flash = "/channel add: nothing to do"
		return nil
	}
	header := fmt.Sprintf("/channel add — %d added, %d skipped", added, skipped)
	m.systemBlock(header, summary...)
	if added > 0 {
		m.flash = fmt.Sprintf("added %d channel%s", added, plural(added))
	} else {
		m.flash = "no channels added — see log"
	}
	return nil
}

// newAdminReboot builds an AdminMessage.RebootSeconds envelope. The
// firmware reboots after `secs` seconds — 5 gives the radio time to
// finish flushing pending writes (queued ACKs, NodeDB persistence)
// before the restart. Used by /reboot and as a recovery path for
// radios that need a kick after a module-config write.
func newAdminReboot(myNodeNum uint32, secs int32) (*pb.ToRadio, error) {
	admin := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_RebootSeconds{
			RebootSeconds: secs,
		},
	}
	payload, err := proto.Marshal(admin)
	if err != nil {
		return nil, fmt.Errorf("marshal AdminMessage: %w", err)
	}
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			To:      myNodeNum,
			WantAck: true,
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum: pb.PortNum_ADMIN_APP,
				Payload: payload,
			}},
		}},
	}, nil
}

// newAdminGetModuleConfigBuzzer asks the radio to send back its
// current ExternalNotification ModuleConfig. Used as a backstop after
// the WantConfigId handshake — some firmware doesn't push module
// configs proactively, in which case meshx would otherwise render a
// default-true guess in /config until the user hit save. The reply
// flows through the same FromRadio_ModuleConfig path the dump uses,
// so applyModuleBuzzer doesn't have to know whether it was solicited.
func newAdminGetModuleConfigBuzzer(myNodeNum uint32) (*pb.ToRadio, error) {
	admin := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_GetModuleConfigRequest{
			GetModuleConfigRequest: pb.AdminMessage_EXTNOTIF_CONFIG,
		},
	}
	payload, err := proto.Marshal(admin)
	if err != nil {
		return nil, fmt.Errorf("marshal AdminMessage: %w", err)
	}
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			To:      myNodeNum,
			WantAck: true,
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum:      pb.PortNum_ADMIN_APP,
				Payload:      payload,
				WantResponse: true,
			}},
		}},
	}, nil
}

// newTraceroutePacket builds a TRACEROUTE_APP MeshPacket addressed
// to `targetNum` with an empty RouteDiscovery payload. WantResponse
// tells the firmware to relay the discovery back populated with the
// traversed route; WantAck so we still get a Routing receipt to
// distinguish "request landed on the mesh" from "request never made
// it to the wire." Returns the envelope plus the generated packetID
// the model stashes in m.pendingTraceroute so the inbound reply can
// correlate via Data.request_id.
func newTraceroutePacket(targetNum uint32) (*pb.ToRadio, uint32, error) {
	rd := &pb.RouteDiscovery{}
	payload, err := proto.Marshal(rd)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal RouteDiscovery: %w", err)
	}
	pid := randPacketID()
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			Id:       pid,
			To:       targetNum,
			WantAck:  true,
			HopLimit: 7, // Meshtastic firmware default; gives the discovery enough headroom for a typical multi-hop mesh
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum:      pb.PortNum_TRACEROUTE_APP,
				Payload:      payload,
				WantResponse: true,
			}},
		}},
	}, pid, nil
}

// newPingPacket builds a REPLY_APP MeshPacket addressed to
// `targetNum`. Meshtastic's REPLY_APP service is a built-in echo:
// the firmware automatically bounces whatever payload it receives
// back to the sender, which is the closest thing to ICMP echo the
// mesh has. WantAck so we still get a Routing receipt, WantResponse
// so the firmware actually relays the echo back. Returns the
// envelope plus the generated packetID the model stashes in
// m.pendingPing for correlation.
func newPingPacket(targetNum uint32) (*pb.ToRadio, uint32, error) {
	pid := randPacketID()
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			Id:       pid,
			To:       targetNum,
			WantAck:  true,
			HopLimit: 7,
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum:      pb.PortNum_REPLY_APP,
				Payload:      []byte("ping"),
				WantResponse: true,
			}},
		}},
	}, pid, nil
}

// pingTimeoutSeconds bounds how long /ping waits for the REPLY_APP
// echo before declaring the request lost. Same 30s ballpark /tr
// uses — enough for a multi-hop round trip on slow modem presets,
// not so long the user thinks the command silently no-opped.
const pingTimeoutSeconds = 30

func pingTimeoutCmd(packetID uint32) tea.Cmd {
	return tea.Tick(pingTimeoutSeconds*time.Second, func(time.Time) tea.Msg {
		return pingTimeoutMsg{packetID: packetID}
	})
}

// tracerouteTimeoutSeconds bounds how long /tr waits for a
// TRACEROUTE_APP reply before declaring the request lost. 30s covers
// a 6-hop round trip on a slow LongFast mesh with retries — same
// ballpark the official Meshtastic clients use. tracerouteTimeoutCmd
// returns a tea.Cmd that fires tracerouteTimeoutMsg after the
// deadline; the handler short-circuits if pendingTraceroute already
// resolved or got replaced by a newer /tr.
const tracerouteTimeoutSeconds = 30

func tracerouteTimeoutCmd(packetID uint32) tea.Cmd {
	return tea.Tick(tracerouteTimeoutSeconds*time.Second, func(time.Time) tea.Msg {
		return tracerouteTimeoutMsg{packetID: packetID}
	})
}

// resetConfigDraft snapshots the live radio state into m.cfgDraft so
// the /config panel opens populated with current values. Called from
// openOverlay(overlayConfig) so re-opening the panel always starts
// clean — any unsaved edits from a prior session are discarded the
// moment the panel closes (a future "save on close" would invert
// this, but the current design is "explicit Ctrl+S or it didn't
// happen").
func (m *model) resetConfigDraft() {
	m.cfgDraft = configDraft{
		buzzer:    m.radioBuzzerEnabled,
		longName:  m.myCallsign(),
		shortName: m.myShortName(),
	}
	m.cfgEditing = ""
	m.cfgConfirmDiscard = false
}

// configDraftDirty reports whether the draft has any field that
// differs from the live state. Drives the dirty-marker in the panel
// header + each row, and gates the Esc-on-dirty discard prompt.
func (m model) configDraftDirty() bool {
	if m.cfgDraft.buzzer != m.radioBuzzerEnabled {
		return true
	}
	if m.cfgDraft.longName != m.myCallsign() {
		return true
	}
	if m.cfgDraft.shortName != m.myShortName() {
		return true
	}
	return false
}

// commitConfigDraft fires the AdminMessage(s) needed to make the radio
// match the draft, and persists the local mirrors. Walks the diff so
// rows that didn't change don't generate wire traffic. Returns the
// number of changes applied so the caller can flash a sane summary.
//
// Validates string fields against the firmware's byte limits before
// touching the wire — same caps setOwner enforces — so a bad longname
// rejects in-panel without the radio seeing it.
func (m *model) commitConfigDraft() int {
	if m.isDemo() {
		m.flash = "/config: save needs a real radio (demo mode)"
		return 0
	}
	if m.pump == nil {
		m.flash = "/config: save needs a live radio connection"
		return 0
	}
	// Validate before any side effects. Stop on the first failure so
	// the user fixes one at a time — partial commits would land the
	// "valid" half on the radio while the panel still shows the
	// invalid edit, which is confusing.
	if len(m.cfgDraft.longName) > 36 {
		m.flash = fmt.Sprintf("/config: longname %d bytes, max 36", len(m.cfgDraft.longName))
		return 0
	}
	if len(m.cfgDraft.shortName) > 4 {
		m.flash = fmt.Sprintf("/config: shortname %d bytes, max 4", len(m.cfgDraft.shortName))
		return 0
	}

	changes := 0
	// Owner (longname / shortname) — one AdminMessage covers both
	// fields; firmware overwrites the whole User record, so we round-
	// trip both even if only one changed. Same path /nick uses.
	if m.cfgDraft.longName != m.myCallsign() || m.cfgDraft.shortName != m.myShortName() {
		envelope, err := newAdminSetOwner(
			m.myNodeNum, m.cfgDraft.longName, m.cfgDraft.shortName, false,
		)
		if err != nil {
			m.flash = fmt.Sprintf("/config: SetOwner build failed: %v", err)
			return 0
		}
		if !m.pump.Enqueue(envelope) {
			m.flash = "/config: SetOwner dropped — outbound buffer full"
			return 0
		}
		changes++
	}
	// Radio buzzer — separate AdminMessage path (SetModuleConfig).
	if m.cfgDraft.buzzer != m.radioBuzzerEnabled {
		envelope, err := newAdminSetBuzzer(m.myNodeNum, m.cfgDraft.buzzer, m.radioBuzzerSnapshot)
		if err != nil {
			m.flash = fmt.Sprintf("/config: buzzer build failed: %v", err)
			return 0
		}
		if !m.pump.Enqueue(envelope) {
			m.flash = "/config: buzzer dropped — outbound buffer full"
			return 0
		}
		m.radioBuzzerEnabled = m.cfgDraft.buzzer
		v := "on"
		if !m.cfgDraft.buzzer {
			v = "off"
		}
		m.storagePersist(putSetting(m.db, "radio_buzzer", v))
		changes++
	}
	if changes == 0 {
		m.flash = "/config: no changes to save"
		return 0
	}
	m.flash = fmt.Sprintf("/config: %d change%s saved — radio updating", changes, plural(changes))
	m.systemLine(fmt.Sprintf("config: committed %d change%s", changes, plural(changes)))
	return changes
}

// buildVersionLines returns the rows /version dumps as a systemBlock.
// Reads from BuildInfo() (the same goversion.Info `meshx version`
// JSON-prints) so the in-app surface and the CLI surface report
// identical data — caarlos0/go-version backfills sensible defaults
// from runtime/debug.ReadBuildInfo when goreleaser ldflags weren't
// applied (e.g. plain `go build`), so we get the commit SHA + dirty
// flag for free without forking the discovery logic.
//
// Also includes the radio's firmware version when known, so the user
// can see at a glance whether their firmware is current.
func buildVersionLines(m *model) []string {
	v := BuildInfo()
	lines := []string{
		fmt.Sprintf("meshx:    %s", v.GitVersion),
	}
	if v.GitCommit != "" {
		commit := v.GitCommit
		if len(commit) > 7 {
			commit = commit[:7]
		}
		if v.GitTreeState == "dirty" {
			commit += "-dirty"
		}
		lines = append(lines, fmt.Sprintf("commit:   %s", commit))
	}
	if v.BuildDate != "" {
		lines = append(lines, fmt.Sprintf("built:    %s", v.BuildDate))
	}
	if v.BuiltBy != "" {
		lines = append(lines, fmt.Sprintf("by:       %s", v.BuiltBy))
	}
	lines = append(lines, fmt.Sprintf("go:       %s", v.GoVersion))
	switch {
	case m.radioFirmware != "":
		lines = append(lines, fmt.Sprintf("firmware: %s", m.radioFirmware))
	case m.isDemo():
		lines = append(lines, "firmware: (demo)")
	default:
		lines = append(lines, "firmware: (waiting on Metadata packet)")
	}
	return lines
}

// plural returns "s" when n != 1 — micro-helper to keep config save
// flash messages grammatical without inline ternaries littering the
// commit path.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// currentChannelIndex maps m.currentChannel back to the Meshtastic
// channel index used on the wire. Defaults to 0 (PRIMARY) when the
// channel name isn't in our list.
func (m model) currentChannelIndex() uint32 {
	for i, c := range m.channels {
		if c.name == m.currentChannel {
			return uint32(i)
		}
	}
	return 0
}

// timeNowHHMM returns the current wall time in HH:MM for message
// timestamps. Extracted so tests can override if needed.

func (m *model) actOnSelectedNode(fn func(*nodeItem)) {
	if m.focused != paneNodes {
		return
	}
	sorted := m.sortedNodes()
	if m.selectedNd < 0 || m.selectedNd >= len(sorted) {
		return
	}
	target := sorted[m.selectedNd].callsign
	for i := range m.nodes {
		if m.nodes[i].callsign == target {
			fn(&m.nodes[i])
			return
		}
	}
}

// activate is the "open/select" action — Enter and Space in normal mode.
// Meaning depends on which pane is focused:
//   - channels: switch the messages pane to that channel
//   - nodes:    show whois / node info flash
//   - messages: expand selected message (hop, SNR, RSSI, hex id)
func (m *model) activate() tea.Cmd {
	switch m.focused {
	case paneConfig:
		entries := m.configEntries()
		if m.selectedCfg < 0 || m.selectedCfg >= len(entries) {
			return nil
		}
		e := entries[m.selectedCfg]
		switch e.kind {
		case cfgEntryString:
			// Swap into inline-edit mode. The Component checks
			// m.cfgEditing and renders the textinput in place of the
			// static value cell, so the cursor lives right where the
			// row's draft value already does. Pre-fill with the
			// current draft so a "small tweak" doesn't require
			// retyping the whole field.
			m.cfgEditing = e.field
			m.cfgEditInput.SetValue(e.value)
			m.cfgEditInput.CursorEnd()
			m.mode = modeConfigEdit
			return m.cfgEditInput.Focus()
		case cfgEntryToggle:
			if e.action != nil {
				e.action(m)
			}
			return nil
		}
		// Read-only row — Enter is a no-op. selectableConfig...Indices
		// already prevents the cursor from parking here, but guard.
		return nil
	case paneChannels:
		if m.selectedCh < len(m.channels) {
			c := m.channels[m.selectedCh]
			m.currentChannel = c.name
			m.channels[m.selectedCh].unread = 0
			m.flash = fmt.Sprintf("switched to %s", c.name)
			// Land on the input bar in the new channel — same as
			// /join. Without this the user is stuck in nav mode on
			// the (now-closed) channels overlay and has to ESC to
			// type. Reuses closeOverlayToInput so overlay state,
			// focused pane, mode flag, and textinput.Focus() all
			// flip together — matches every other "we just acted on
			// the user's selection, now hand them back the keyboard"
			// transition (revealMessages, etc.).
			return m.closeOverlayToInput()
		}
	case paneNodes:
		sorted := m.sortedNodes()
		if m.selectedNd < len(sorted) {
			n := sorted[m.selectedNd]
			hw := n.hwModel
			if hw == "" {
				hw = "?"
			}
			fw := n.firmware
			if fw == "" {
				fw = "?"
			}
			m.flash = fmt.Sprintf(
				"%s  ·  %s  ·  fw %s  ·  last heard %s  ·  %s",
				n.callsign, hw, fw, n.currentLastHeard(), n.currentState(),
			)
		}
	case paneMessages:
		if m.selectedMsg < len(m.messages) {
			msg := m.messages[m.selectedMsg]
			switch {
			case msg.status == "system":
				m.flash = "system message — no metadata"
			case msg.mine:
				m.flash = fmt.Sprintf("to %s  ·  hop %d  ·  ACK %s",
					m.currentChannel, msg.hops, ackWord(msg.status))
			default:
				parts := []string{"from " + msg.from}
				if msg.hops > 0 {
					parts = append(parts, fmt.Sprintf("hop %d", msg.hops))
				}
				if msg.snr != "" {
					parts = append(parts, "SNR "+msg.snr+" dB")
				}
				m.flash = strings.Join(parts, "  ·  ")
			}
		}
	}
	return nil
}

func ackWord(status string) string {
	switch status {
	case "ack":
		return "ok"
	case "fail":
		return "timeout"
	default:
		return "pending"
	}
}

func (m *model) executeCommand(raw string) tea.Cmd {
	if raw == "" {
		return nil
	}
	// Split into verb + rest for arg-taking commands.
	verb := raw
	rest := ""
	if sp := strings.IndexByte(raw, ' '); sp >= 0 {
		verb = raw[:sp]
		rest = strings.TrimSpace(raw[sp+1:])
	}

	switch verb {
	case "q", "quit", "exit":
		return tea.Quit

	case "pin":
		// Toggle pin on the most recent ephemeral notice. "Ephemeral"
		// = has an expireAt stamp (command-triggered notice, not
		// splash / storage / chat). Typed from input with no explicit
		// selection, so the heuristic is "the thing the user most
		// recently ran."
		idx := m.lastEphemeralNoticeIdx()
		if idx < 0 {
			m.flash = "/pin: nothing pinnable in the log"
			return nil
		}
		pinned := !m.messages[idx].pinned
		m.toggleNoticePin(idx)
		if pinned {
			m.flash = "notice pinned — timer paused"
		} else {
			m.flash = "notice unpinned — timer resumed"
		}

	// ── Ham-radio bang shortcuts ──────────────────────────────────
	// These are quick-command shorthands that compose and send the
	// underlying !bang message. Geeky, fast, and keeps the protocol
	// payload visible as normal message text so every other
	// Meshtastic client sees it as plain chat.

	case "cq":
		// Ham-customary "via <rig/app>" suffix on the beacon so
		// anyone copying the CQ knows what client the caller runs.
		// Only /cq carries this tag — routine chat + reply verbs
		// stay clean so a 237-byte LoRa payload isn't wasted on
		// attribution on every packet.
		call := m.myCallsign()
		body := fmt.Sprintf("CQ CQ CQ de %s via %s — testing signals, please ack", call, clientTag)
		if rest != "" {
			body = fmt.Sprintf("CQ de %s via %s %s", call, clientTag, rest)
		}
		m.sendBang("/cq", body)
		m.flash = "/cq broadcast sent"
	case "cqr":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /cqr <callsign>  (or highlight their CQ in nav mode)"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("no telemetry for %s — node unknown", target)
			return nil
		}
		m.sendBangReply("/cqr "+target, signalReport(n), m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!cqr %s — copy report sent (%s)", target, signalReport(n))
	case "rs":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /rs <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("no telemetry for %s — node unknown", target)
			return nil
		}
		m.sendBangReply("/rs "+target, signalReport(n), m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!rs %s — %s", target, signalReport(n))
	case "73":
		// /73           → broadcast best-regards
		// /73 <call>    → directed "73 <call>" — aimed at a specific
		//                 operator you're signing off to cordially.
		//                 Threads via Data.reply_id to that operator's
		//                 most recent message when we have one.
		target := strings.TrimSpace(rest)
		if target == "" {
			m.sendBang("/73", "73")
			m.flash = "!73 sent"
			return nil
		}
		m.sendBangReply("/73 "+target, "73 "+target, m.replyTargetFor(target))
		m.flash = "!73 " + target + " — best regards"
	case "88":
		m.sendBang("/88", "88")
		m.flash = "!88 sent"
	case "qsl":
		// /qsl           → broadcast acknowledgment
		// /qsl <call>    → directed "QSL <call>" — aimed at a specific
		//                  operator whose last transmission we copied.
		//                  Threads via Data.reply_id to that operator's
		//                  most recent message.
		target := strings.TrimSpace(rest)
		if target == "" {
			m.sendBang("/qsl", "QSL")
			m.flash = "!qsl — acknowledged"
			return nil
		}
		body := "QSL " + target
		m.sendBangReply("/qsl "+target, body, m.replyTargetFor(target))
		m.flash = "!qsl " + target + " — copy confirmed"
	case "qth":
		// PRIVACY — /qth only transmits when the user runs it
		// explicitly, and only the coarse Maidenhead grid (~20 km
		// precision). Never exact lat/long.
		//
		// Two forms:
		//   /qth                → broadcast your own grid (from radio GPS)
		//   /qth <text>         → broadcast a custom QTH string
		//
		// To look up a PEER's QTH, use /whois <call> — keeps send vs.
		// query unambiguous.
		arg := strings.TrimSpace(rest)
		if arg == "" {
			if m.myGrid == "" {
				m.flash = "no GPS fix — /qth <text> to send a custom QTH, or configure position on the radio"
				return nil
			}
			m.sendBang("/qth", "QTH: "+m.myGrid)
			m.flash = "QTH: " + m.myGrid
			return nil
		}
		m.sendBang("/qth", "QTH: "+arg)
		m.flash = "QTH: " + arg
	case "env":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /env <callsign>"
			return nil
		}
		nodeNum := m.nodeNumOf(target)
		if nodeNum == 0 {
			m.systemLine(fmt.Sprintf("env: no record of %s", target))
			return nil
		}
		n := m.lookupNode(target)
		env, ok := m.peerEnv[nodeNum]
		if !ok {
			m.systemLine(fmt.Sprintf("env: %s has no environmental telemetry on file", n.callsign))
			m.systemLine("     (only peers with temp/humidity/pressure sensors broadcast this)")
			return nil
		}
		var lines []string
		if env.temperature != 0 {
			lines = append(lines, fmt.Sprintf("temp:     %.1f °C", env.temperature))
		}
		if env.humidity != 0 {
			lines = append(lines, fmt.Sprintf("humidity: %.0f %%", env.humidity))
		}
		if env.pressure != 0 {
			lines = append(lines, fmt.Sprintf("pressure: %.0f hPa", env.pressure))
		}
		if env.gas != 0 {
			lines = append(lines, fmt.Sprintf("gas:      %.0f Ω", env.gas))
		}
		lines = append(lines, fmt.Sprintf("age:      %s ago", humanDuration(time.Since(env.at))))
		m.systemBlock(fmt.Sprintf("env %s", n.callsign), lines...)

	// ── Extra ham/Meshtastic slang ────────────────────────────────
	case "qrz":
		// "Who is calling me?" — broadcast a prompt for identification.
		m.sendBang("/qrz", "QRZ? who's calling?")
		m.flash = "!qrz — asking for ID"
	case "qrm":
		// "You have man-made interference." Report to a station.
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /qrm <callsign>"
			return nil
		}
		m.sendBangReply(
			"/qrm "+target,
			"QRM — interference on your signal",
			m.replyTargetFor(target),
		)
		m.flash = fmt.Sprintf("!qrm %s — interference reported", target)
	case "qsb":
		// "Your signal is fading."
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /qsb <callsign>"
			return nil
		}
		m.sendBangReply("/qsb "+target, "QSB — signal fading, copy weak", m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!qsb %s — fade reported", target)
	case "sk":
		// Final sign-off — stronger than /73. "Signing off clear."
		// /sk           → broadcast SK
		// /sk <call>    → directed "SK <call>" — aimed at a specific
		//                 operator you're closing a contact with.
		//                 Threads via Data.reply_id to that operator's
		//                 most recent message.
		target := strings.TrimSpace(rest)
		if target == "" {
			m.sendBang("/sk", "SK — clear and out 73")
			m.flash = "!sk — clear"
			return nil
		}
		body := "SK — clear and out 73, " + target
		m.sendBangReply("/sk "+target, body, m.replyTargetFor(target))
		m.flash = "!sk " + target + " — cleared"
	case "wx":
		// Weather at my QTH. Optional argument supplies the conditions;
		// without one we emit a placeholder so the user types their own.
		wx := rest
		if wx == "" {
			wx = "clear 55°F light wind"
		}
		m.sendBang("/wx", "wx: "+wx)
		m.flash = "wx: " + wx + " — broadcast"
	case "grid":
		// Just the Maidenhead locator — shorter / more data-friendly
		// than /qth which also names the city.
		grid := rest
		if grid == "" {
			grid = m.myGrid
		}
		if grid == "" {
			m.flash = "no GPS fix — /grid <locator> to send a custom grid"
			return nil
		}
		m.sendBang("/grid", "grid: "+grid)
		m.flash = "grid: " + grid + " — broadcast"
	case "mesh":
		// Meshtastic-specific — summarize what the mesh looks like
		// from our vantage: number of nodes we can hear, by state.
		online, muted, offline := 0, 0, 0
		for i := range m.nodes {
			switch m.nodes[i].currentState() {
			case "online":
				online++
			case "muted":
				muted++
			case "offline", "failed":
				offline++
			}
		}
		body := fmt.Sprintf("mesh view: %d online, %d muted, %d stale", online, muted, offline)
		m.sendBang("/mesh", body)
		m.flash = body
	case "k":
		// "Over — go ahead." Ragchew turn-taking.
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /k <callsign>"
			return nil
		}
		m.sendBangReply("/k "+target, "K — over, go ahead", m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!k %s — over to you", target)

	// ── IRC-style operational commands ────────────────────────────
	case "tr":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /tr <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.systemLine(fmt.Sprintf("tr: node %s unknown", target))
			return nil
		}
		// Fast paths first — no live wire traffic possible in demo
		// mode or when the pump isn't up yet, so fall back to cached
		// telemetry with a clear "no live path" tag instead of
		// silently no-opping.
		if m.isDemo() || m.pump == nil {
			m.systemBlock(
				fmt.Sprintf("traceroute %s", n.callsign),
				fmt.Sprintf("hops:   %d (cached)", n.lastHops),
				fmt.Sprintf("signal: %s", signalReport(n)),
				"note:   live traceroute needs a real radio connection",
			)
			return nil
		}
		// Self-traceroute is meaningless — firmware drops it.
		if n.nodeNum != 0 && n.nodeNum == m.myNodeNum {
			m.systemLine("tr: that's you — /info for your own config")
			return nil
		}
		// One traceroute in flight at a time. Issuing a second /tr
		// while the first hasn't resolved would orphan the old
		// pendingTraceroute (the new packetID overwrites the field
		// and the original timeout tick never finds a match). Refuse
		// loud rather than silently lose the prior request.
		if m.pendingTraceroute != nil {
			m.flash = fmt.Sprintf(
				"tr: already tracing %s — wait or it'll auto-timeout",
				m.pendingTraceroute.targetCall,
			)
			return nil
		}
		envelope, pid, err := newTraceroutePacket(n.nodeNum)
		if err != nil {
			m.flash = fmt.Sprintf("tr: build failed: %v", err)
			return nil
		}
		if !m.pump.Enqueue(envelope) {
			m.flash = "tr: dropped — outbound buffer full"
			return nil
		}
		m.pendingTraceroute = &pendingTraceroute{
			packetID:    pid,
			targetNum:   n.nodeNum,
			targetCall:  n.callsign,
			requestedAt: time.Now(),
		}
		m.flash = fmt.Sprintf(
			"tr: tracing %s (waiting up to %ds)",
			n.callsign, tracerouteTimeoutSeconds,
		)
		m.systemLine(fmt.Sprintf(
			"traceroute %s — request sent (id 0x%x), awaiting reply",
			n.callsign, pid,
		))
		return tracerouteTimeoutCmd(pid)
	case "ping":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /ping <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.systemLine(fmt.Sprintf("ping: node %s unknown", target))
			return nil
		}
		// Pinging ourselves yields meaningless telemetry (firmware
		// won't echo a packet back to its own node). Refuse with a
		// note rather than emitting a request that will silently
		// timeout.
		if n.nodeNum != 0 && n.nodeNum == m.myNodeNum {
			m.systemLine("ping: that's you — /whois for your own config")
			return nil
		}
		// Demo / offline fall back to cached telemetry — same shape
		// /tr uses. Tag the result so the user knows the radio
		// wasn't actually queried.
		if m.isDemo() || m.pump == nil {
			lines := []string{
				fmt.Sprintf("last heard: %s ago (cached)", n.currentLastHeard()),
				fmt.Sprintf("signal:     %s", signalReport(n)),
				"note:       live ping needs a real radio connection",
			}
			if nodeNum := m.nodeNumOf(target); nodeNum != 0 {
				if pos, ok := m.peerPositions[nodeNum]; ok && m.myGrid != "" {
					if km := haversineKm(m.myLatitude, m.myLongitude, pos.latitude, pos.longitude); km > 0 {
						lines = append(lines,
							fmt.Sprintf("distance:   %.1f km", km),
						)
					}
				}
			}
			m.systemBlock(fmt.Sprintf("ping %s", n.callsign), lines...)
			return nil
		}
		// One ping in flight at a time. Same shape as pendingTraceroute.
		if m.pendingPing != nil {
			m.flash = fmt.Sprintf(
				"ping: already pinging %s — wait or it'll auto-timeout",
				m.pendingPing.targetCall,
			)
			return nil
		}
		envelope, pid, err := newPingPacket(n.nodeNum)
		if err != nil {
			m.flash = fmt.Sprintf("ping: build failed: %v", err)
			return nil
		}
		if !m.pump.Enqueue(envelope) {
			m.flash = "ping: dropped — outbound buffer full"
			return nil
		}
		m.pendingPing = &pendingPing{
			packetID:    pid,
			targetNum:   n.nodeNum,
			targetCall:  n.callsign,
			requestedAt: time.Now(),
		}
		m.flash = fmt.Sprintf(
			"ping: pinging %s (waiting up to %ds)",
			n.callsign, pingTimeoutSeconds,
		)
		m.systemLine(fmt.Sprintf(
			"ping %s — request sent (id 0x%x), awaiting echo",
			n.callsign, pid,
		))
		return pingTimeoutCmd(pid)
	case "w", "whois":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /whois <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.systemLine(fmt.Sprintf("whois: no record of %s", target))
			return nil
		}
		hw := n.hwModel
		if hw == "" {
			hw = "unknown hw"
		}
		fw := n.firmware
		nodeNum := m.nodeNumOf(target)
		isSelf := nodeNum != 0 && nodeNum == m.myNodeNum
		// For our own node, fw lives on m.radioFirmware (from
		// FromRadio.Metadata), not on the nodeItem — MyNodeInfo
		// doesn't carry firmware. Same story for battery /
		// channel-util telemetry, which arrives via DeviceMetrics
		// and is stored on the model root for self only.
		if isSelf && fw == "" {
			fw = m.radioFirmware
		}
		if fw == "" {
			fw = "?"
		}

		// Ghost peer — we have their node num from text packets but
		// have never received their NodeInfo, so the longname is just
		// the placeholder hex form and hw / fw / position are all
		// unknown. Surface that up front rather than pretending a
		// partial whois is a whole one. Detection now goes through
		// the live unresolved flag (since the callsign string for a
		// ghost is the same "node 0x..." form a fully resolved peer
		// could also legitimately have).
		ghost := n.unresolved

		var lines []string
		// Identity block — long, short, hex — at the top of every
		// whois so the user can correlate however the peer was
		// referred to in chat. For resolved peers the long name is
		// whatever they advertised; for ghosts it's the hex placeholder
		// (we deliberately don't synthesize "Meshtastic <hex>").
		lines = append(lines, fmt.Sprintf("name:   %s", n.callsign))
		if n.shortName != "" {
			lines = append(lines, fmt.Sprintf("short:  %s", n.shortName))
		}
		if nodeNum != 0 {
			lines = append(lines, fmt.Sprintf("id:     0x%x", nodeNum))
		}
		lines = append(lines, "")
		if ghost {
			lines = append(lines,
				"👻 no NodeInfo received for this peer",
				"  we've heard text packets from them but never their",
				"  User broadcast, so longname / hw / fw / position are",
				"  unknown. Their NodeInfo may arrive in the next",
				"  ~15 min, or try /sync to force a NodeDB re-dump.",
				"",
			)
		}
		lines = append(lines,
			fmt.Sprintf("hw:     %s", hw),
			fmt.Sprintf("fw:     %s", fw),
			fmt.Sprintf("heard:  %s ago", n.currentLastHeard()),
			fmt.Sprintf("state:  %s", n.currentState()),
			fmt.Sprintf("signal: %s", signalReport(n)),
			fmt.Sprintf("hops:   %s", whoisHops(n, isSelf)),
		)
		// Battery + channel-util are only tracked model-wide for
		// self today. For peers we'd need a per-peer DeviceMetrics
		// cache (TODO). Surface what we have.
		if isSelf && m.hasTelemetry {
			pct := "—"
			switch {
			case m.batteryLevel > 100:
				pct = "pwr (USB / solar — no cell)"
			case m.batteryLevel > 0:
				pct = fmt.Sprintf("%d%%", m.batteryLevel)
			}
			if m.batteryVoltage > 0 {
				lines = append(lines, fmt.Sprintf("battery: %s  %.2f V", pct, m.batteryVoltage))
			} else {
				lines = append(lines, fmt.Sprintf("battery: %s", pct))
			}
			lines = append(lines, fmt.Sprintf("chanutl: %.1f%%", m.channelUtil))
		}
		if nodeNum != 0 {
			if pos, ok := m.peerPositions[nodeNum]; ok {
				lines = append(
					lines,
					fmt.Sprintf("grid:   %s", pos.grid),
					fmt.Sprintf(
						"coord:  %.5f, %.5f  alt %d m",
						pos.latitude,
						pos.longitude,
						pos.altitude,
					),
					fmt.Sprintf("fix age: %s ago", humanDuration(time.Since(pos.at))),
				)
				// Distance from us if we also have a fix — same
				// great-circle helper /ping uses.
				if !isSelf && m.myLatitude != 0 && m.myLongitude != 0 {
					if km := haversineKm(m.myLatitude, m.myLongitude, pos.latitude, pos.longitude); km > 0 {
						lines = append(lines, fmt.Sprintf("dist:   %.1f km from you", km))
					}
				}
			}
		}
		lines = append(lines, "end of /whois")
		m.systemBlock(fmt.Sprintf("whois %s", n.callsign), lines...)
	case "r", "reply":
		if rest == "" {
			target := m.selectedSender()
			if target == "" {
				m.flash = "usage: /reply <callsign> <text>"
				return nil
			}
			return m.prefillInput("/reply " + target + " ")
		}
		// /reply <call> <text>
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			return m.prefillInput("/reply " + rest + " ")
		}
		target := rest[:sp]
		body := strings.TrimSpace(rest[sp+1:])
		if body == "" {
			return m.prefillInput("/reply " + target + " ")
		}
		// Route through sendBangReply so the packet actually hits
		// the pump and picks up Data.reply_id threading to the
		// parent message. Body stays clean — no "→<target>: " chrome
		// in the wire payload; the threading line above the row
		// (rendered from replyID) is how "this replies to X" is
		// surfaced to readers.
		//
		// Prefer m.replyParent (captured by `r` in nav mode against
		// the actually-highlighted row) over replyTargetFor's most-
		// recent-from-sender fallback, so threading anchors to the
		// EXACT message the user navigated to — even when the same
		// callsign has several messages in the log.
		parent := m.replyParent
		if parent == 0 {
			parent = m.replyTargetFor(target)
		}
		m.replyParent = 0
		// Plain chat with a Data.reply_id — NOT a /bang command. The
		// renderer reads msg.bang to decide between yellow `*` and
		// magenta `›` flag glyphs; we want `›` here so a reply
		// looks like the regular outbound chat it actually is.
		m.sendPlainReply(body, parent)
		m.flash = fmt.Sprintf("reply sent to %s", target)
	case "msg":
		// /msg <call> <text> — directed message. Meshtastic has no
		// formal DM on the public channel (this still broadcasts),
		// so convention is to prefix the body with the target's name
		// so humans see the addressing. Unlike /reply there's no
		// parent to thread against, so replyID is zero.
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			m.flash = "usage: /msg <callsign> <text>"
			return nil
		}
		target := rest[:sp]
		body := strings.TrimSpace(rest[sp+1:])
		if body == "" {
			m.flash = "usage: /msg <callsign> <text>"
			return nil
		}
		// Plain chat with the target's nick prefixed in the body —
		// NOT a /bang command. Renders with the magenta `›` flag so
		// it reads as regular outbound chat (which it is — Meshtastic
		// has no actual DM, this is still a channel broadcast with
		// the addressing convention spelled out in the body).
		m.sendPlainReply(target+": "+body, 0)
		m.flash = fmt.Sprintf("DM sent to %s", target)
	case "join":
		if rest == "" {
			m.flash = "usage: /join <channel>"
			return nil
		}
		// Join by matching name; if not found, flash.
		for i, c := range m.channels {
			if c.name == rest || strings.TrimPrefix(c.name, "#") == rest {
				m.switchChannelByIndex(i)
				return nil
			}
		}
		m.flash = fmt.Sprintf("no channel named %s — /channel list", rest)
	case "part":
		// Meshtastic channels aren't IRC channels — they live on the
		// RADIO as "Channel" config slots (each with a name + a shared
		// PSK), not as a per-client membership. There's nothing for
		// meshX to "part" from; removing a channel means deleting the
		// slot on the radio (phone app or `meshtastic --ch-disable
		// <idx>`). Surface the explanation as a systemBlock instead of
		// a one-line flash so the user sees the model spelled out.
		m.systemBlock(
			"/part",
			"Meshtastic channels live on the radio, not the client.",
			"To leave a channel, disable the slot via the phone app or",
			"the meshtastic CLI (`meshtastic --ch-disable <index>`).",
			"meshX will stop seeing it once the radio drops the slot.",
		)
		m.flash = "/part: channels are radio-configured — see the log"
	case "channels", "list":
		// /list is the IRC convention for "show me the channels."
		m.openOverlay(overlayChannels)
	case "nodes", "who":
		// /who is the IRC convention for "show me the user list" —
		// alias for /nodes so muscle memory from IRC clients lands
		// where users expect. "Node" is the canonical Meshtastic
		// term (we dropped /users + /names).
		m.openOverlay(overlayNodes)
	case "nearby":
		// Distance-sorted roster of peers with a GPS fix — "who
		// can I talk to directly" at a glance. The renderer handles
		// the no-self-fix case with an in-pane explainer so the
		// overlay always opens; refusing here with a transient
		// flash made the command look broken when the user's own
		// radio hadn't broadcast a Position packet yet.
		m.openOverlay(overlayNearby)
	case "radar":
		// Polar scope. Same in-pane explainer as /nearby for the
		// no-self-fix case — always open, show why if data is
		// missing.
		m.openOverlay(overlayRadar)
	case "channel":
		if rest == "list" || rest == "" {
			m.openOverlay(overlayChannels)
			return nil
		}
		// /channel add <url>
		// Accept either a meshtastic://e/#... deep link or an
		// https://meshtastic.org/e/#... universal link. The fragment
		// after `#` is a base64-url ChannelSet protobuf — see
		// channel_url.go for the codec. PSK never touches the network;
		// the URL is a portable PSK envelope, not a server call.
		sub := rest
		arg := ""
		if i := strings.IndexByte(rest, ' '); i >= 0 {
			sub = rest[:i]
			arg = strings.TrimSpace(rest[i+1:])
		}
		switch sub {
		case "add":
			if arg == "" {
				m.flash = "usage: /channel add <meshtastic://url>"
				return nil
			}
			return m.channelAdd(arg)
		case "del", "delete", "rm":
			if arg == "" {
				m.flash = "usage: /channel del <name>"
				return nil
			}
			return m.channelDel(arg)
		default:
			m.flash = "usage: /channel list  |  /channel add <url>  |  /channel del <name>"
		}
	case "nick":
		// /nick (no args) — read-only display of the current
		// longname. /nick <name> — immediate write of User.long_name
		// via AdminMessage.SetOwner. No reboot (firmware accepts
		// the write hot); change propagates to peers on the next
		// NodeInfo broadcast. The canonical edit path for both
		// longname and shortname (with draft + Ctrl+S) is /config;
		// /nick stays as the fast inline rename ham operators
		// expect to do without leaving their composing surface.
		// Shortname is round-tripped from the current value so
		// this only changes longname.
		if rest == "" {
			cur := m.myCallsign()
			short := m.myShortName()
			if short != "" {
				m.flash = fmt.Sprintf("nick: %s [%s]  (use /nick <name> to change)", cur, short)
			} else {
				m.flash = fmt.Sprintf("nick: %s  (use /nick <name> to change)", cur)
			}
			return nil
		}
		return m.setOwner(rest, m.myShortName(), "long")
	case "config":
		// Open the radio-config overlay. The interactive panel
		// (configPane) shows an "Radio buzzer: on/off" row at the
		// top — Enter toggles it via AdminMessage.SetModuleConfig.
		// The dump-as-systemBlock variant /config used to do is
		// gone; /info already covers "what does meshx know" and
		// /config is now the consistent path for radio-side knobs.
		m.openOverlay(overlayConfig)
	case "dingtest":
		// Manual BEL verification — returns the exact tea.Cmd
		// applyTextMessage uses on inbound chat. Going through the
		// bubbletea runtime (instead of writing to stdout inline) is
		// what makes the BEL actually reach the terminal under the
		// alt-screen renderer. If the bell still doesn't fire after
		// /dingtest, the cause is your terminal's audible + visual
		// bell preferences (Terminal.app / iTerm Profile → Audible
		// Bell + Visual Bell) — not a meshX bug.
		m.systemBlock("/dingtest",
			"emit:    BEL queued via tea.Cmd",
			"hint:    if no audible/visual bell, check",
			"         Terminal/iTerm Profile → Audible Bell + Visual Bell.",
		)
		m.flash = "/dingtest: BEL queued"
		return ringTerminalBellCmd()
	case "mute":
		// Toggle the meshX terminal ding (BEL on inbound text).
		// Persists to settings.ding_muted so the pref survives
		// restarts. Does NOT touch the radio's onboard buzzer —
		// that's /config → "Radio buzzer". Two separate knobs by
		// design: the radio beeps in your pocket / on your desk,
		// meshX dings inside the terminal.
		m.dingMuted = !m.dingMuted
		v := "off"
		if m.dingMuted {
			v = "on"
		}
		m.storagePersist(putSetting(m.db, "ding_muted", v))
		if m.dingMuted {
			m.flash = "/mute on — terminal ding silenced"
			m.systemLine("ding muted — terminal won't beep on incoming text")
		} else {
			m.flash = "/mute off — terminal ding restored"
			m.systemLine("ding unmuted — terminal will beep on incoming text")
		}
	case "me":
		// IRC ASCII-action convention. /me waves → broadcasts the
		// literal "* waves" as a TEXT_MESSAGE_APP packet on the
		// current channel. Routes through sendPlainMessage (NOT
		// sendBang) so msg.bang stays empty — chatRowRender's
		// action detection requires that to render the row as
		// "* <nick> <action>" in italic, instead of the bang flag
		// /cq, /73, etc. produce. Wire format is just "* <action>"
		// so non-meshx peers see something readable too.
		if rest == "" {
			m.flash = "usage: /me <action>"
			return nil
		}
		m.sendPlainMessage("* " + rest)
		m.flash = fmt.Sprintf("* %s %s", m.myCallsign(), rest)
	case "version":
		// Surface meshX version + radio firmware in one shot. Useful
		// for support tickets, "is my firmware current?" checks, and
		// just-curious. Reads runtime/debug.ReadBuildInfo() so the
		// VCS revision is always accurate without a manual version
		// constant to bump.
		m.systemBlock("/version", buildVersionLines(m)...)
	case "ignore":
		// Local-only filter — hide chat messages from a peer in the
		// messages pane. Doesn't touch the wire (the radio still
		// receives the packets), doesn't persist (in-memory set,
		// cleared on restart). Distinct from nav-m mute which is
		// just a state-marker on the nodes pane. Use /unignore to
		// drop the filter.
		target := rest
		if target == "" {
			m.flash = "usage: /ignore <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("ignore: no node matches %s", target)
			return nil
		}
		if m.ignored == nil {
			m.ignored = make(map[string]bool)
		}
		m.ignored[strings.ToLower(n.callsign)] = true
		m.flash = fmt.Sprintf("ignoring %s — messages hidden until /unignore", n.callsign)
		m.systemLine(fmt.Sprintf("ignore: %s — chat messages will be hidden", n.callsign))
	case "unignore":
		target := rest
		if target == "" {
			if len(m.ignored) == 0 {
				m.flash = "/unignore: nothing on the ignore list"
				return nil
			}
			calls := make([]string, 0, len(m.ignored))
			for k := range m.ignored {
				calls = append(calls, k)
			}
			m.flash = "currently ignoring: " + strings.Join(
				calls,
				", ",
			) + "  (use /unignore <call>)"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("unignore: no node matches %s", target)
			return nil
		}
		key := strings.ToLower(n.callsign)
		if !m.ignored[key] {
			m.flash = fmt.Sprintf("unignore: %s wasn't on the list", n.callsign)
			return nil
		}
		delete(m.ignored, key)
		m.flash = fmt.Sprintf("unignoring %s — messages will show again", n.callsign)
		m.systemLine(fmt.Sprintf("unignore: %s — chat messages restored", n.callsign))
	case "reboot":
		// Sends AdminMessage_RebootSeconds(5) to our own radio. Some
		// firmware needs a reboot for module-config writes to take
		// effect; also a top-level "my radio is wedged" recovery.
		// 5 seconds gives the radio time to flush queued ACKs +
		// persist NodeDB before the restart.
		if m.isDemo() {
			m.flash = "/reboot: needs a real radio (demo mode)"
			return nil
		}
		if m.pump == nil {
			m.flash = "/reboot: needs a live radio connection"
			return nil
		}
		envelope, err := newAdminReboot(m.myNodeNum, 5)
		if err != nil {
			m.flash = fmt.Sprintf("/reboot: build failed: %v", err)
			return nil
		}
		if !m.pump.Enqueue(envelope) {
			m.flash = "/reboot: dropped — outbound buffer full"
			return nil
		}
		m.flash = "/reboot: radio will restart in 5s — meshx will reconnect automatically"
		m.systemLine("reboot: AdminMessage sent — radio restarting in 5s")
	case "info", "whoami":
		// /info — dump meshx's current knowledge to the log so you
		// can diagnose "why don't I have a name for this peer?"
		// without external tooling. Shows our own identity, a
		// peer-count breakdown (real names vs. unresolved "node 0x…"
		// placeholders), session state, and channel summary.
		lines := []string{
			fmt.Sprintf(
				"self:     %s (0x%x)  shortname=%s",
				m.myCallsign(),
				m.myNodeNum,
				m.myShortName(),
			),
		}
		if n := m.myNode(); n != nil {
			lines = append(lines, fmt.Sprintf("hw:       %s  fw=%s", n.hwModel, m.radioFirmware))
		}
		var resolved, ghosts int
		for _, n := range m.nodes {
			if strings.HasPrefix(n.callsign, "node 0x") {
				ghosts++
			} else {
				resolved++
			}
		}
		lines = append(
			lines,
			fmt.Sprintf(
				"peers:    %d total  (%d named, %d placeholder)",
				len(m.nodes),
				resolved,
				ghosts,
			),
			fmt.Sprintf("channels: %d", len(m.channels)),
			fmt.Sprintf("connected: %t  handshake_complete=%t", m.connected, m.connected),
		)
		if m.radioRegion != "" {
			lines = append(lines, fmt.Sprintf("region:   %s  preset=%s  tx=%d dBm  role=%s",
				m.radioRegion, m.radioModemPreset, m.radioTxPower, m.radioRole))
		}
		if ghosts > 0 {
			const maxList = 10
			header := fmt.Sprintf("unresolved peers (%d):", ghosts)
			if ghosts > maxList {
				header = fmt.Sprintf("unresolved peers (first %d of %d):", maxList, ghosts)
			}
			lines = append(lines, header)
			n := 0
			for _, node := range m.nodes {
				if strings.HasPrefix(node.callsign, "node 0x") {
					lines = append(lines, "  "+node.callsign)
					n++
					if n >= maxList {
						break
					}
				}
			}
			lines = append(lines, "(try /sync to re-request the radio's NodeDB)")
		}
		m.systemBlock("info", lines...)
	case "sync":
		// /sync — ask the radio to re-dump its config + NodeDB via a
		// fresh WantConfigId handshake. Use when you suspect the
		// cache is stale, or after the radio just resolved a peer
		// you want surfaced without waiting for the next organic
		// NODEINFO_APP broadcast.
		if m.pump == nil {
			m.flash = "/sync needs a live radio connection (demo mode)"
			return nil
		}
		nonce := mathrand.Uint32()
		if nonce == 0 {
			nonce = 1
		}
		ok := m.pump.Enqueue(&pb.ToRadio{
			PayloadVariant: &pb.ToRadio_WantConfigId{WantConfigId: nonce},
		})
		if !ok {
			m.flash = "/sync dropped — outbound buffer full"
			return nil
		}
		// Snapshot current ghost count so we can report the delta
		// when the matching ConfigComplete lands.
		ghosts := 0
		for _, n := range m.nodes {
			if strings.HasPrefix(n.callsign, "node 0x") {
				ghosts++
			}
		}
		// Store as non-zero sentinel even when zero ghosts exist, so
		// the ConfigComplete handler can tell a pending /sync apart
		// from the startup handshake.
		if ghosts == 0 {
			m.syncPendingGhosts = -1
		} else {
			m.syncPendingGhosts = ghosts
		}
		m.systemBlock("sync",
			fmt.Sprintf("requested NodeDB re-dump (nonce=0x%x)", nonce),
			fmt.Sprintf("baseline: %d unresolved peers", ghosts),
			"watching for incoming NodeInfo — any placeholder that resolves",
			"will fire its own `identified` line; summary lands on completion.",
		)
	case "help", "h":
		// /help             → open the full scrollable overlay
		// /help <verb>      → irssi / BitchX-style per-command usage
		//                     + summary card dropped inline as a
		//                     systemBlock so it lives in the log
		//                     alongside the exchange it's helping
		//                     with (no modal context switch).
		verb := strings.ToLower(strings.TrimSpace(rest))
		if verb == "" {
			m.mode = modeHelp
			return nil
		}
		verb = strings.TrimPrefix(verb, "/")
		entry, ok := helpEntries[verb]
		if !ok {
			m.flash = fmt.Sprintf("no help for /%s — try /help alone for the full index", verb)
			return nil
		}
		m.systemBlock(
			fmt.Sprintf("help /%s", verb),
			"usage:   "+entry.usage,
			"summary: "+entry.summary,
		)
	case "lastlog":
		// /lastlog              — jump to the very last message
		// /lastlog <call|text>  — jump to the last chat message FROM
		//                          <call> (matches the from column,
		//                          not body), or the last row whose
		//                          body contains <text> if no sender
		//                          matches. Substring + case-
		//                          insensitive lookup, same loose
		//                          match /whois uses.
		// Closes any overlay, lands in nav mode on the located row.
		if len(m.messages) == 0 {
			m.flash = "/lastlog: log is empty"
			return nil
		}
		m.overlay = overlayNone
		m.focused = paneMessages
		m.input.Blur()
		idx := -1
		if rest == "" {
			idx = len(m.messages) - 1
		} else {
			needle := strings.ToLower(strings.TrimSpace(rest))
			// First pass: prefer matches in the from column — that's
			// what "the last message FROM gleep" means semantically.
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].status == "system" {
					continue
				}
				if strings.Contains(strings.ToLower(m.messages[i].from), needle) {
					idx = i
					break
				}
			}
			// Second pass: body match if no sender hit. Lets users
			// /lastlog "morning" find the last message containing it.
			if idx < 0 {
				for i := len(m.messages) - 1; i >= 0; i-- {
					if m.messages[i].status == "system" {
						continue
					}
					if strings.Contains(strings.ToLower(m.messages[i].text), needle) {
						idx = i
						break
					}
				}
			}
		}
		if idx < 0 {
			m.flash = fmt.Sprintf("lastlog: no chat row matches %q", rest)
			m.mode = modeNav
			return nil
		}
		m.selectedMsg = idx
		m.mode = modeNav
		hit := m.messages[idx]
		m.flash = fmt.Sprintf("lastlog: %s — %s", hit.time, hit.from)
	case "search":
		if rest == "" {
			// Toggle behavior — clear an active query, otherwise
			// hint at the syntax. "/search" with nothing was the
			// only way to drop a stale query without going through
			// nav-mode `/` then Esc; bind it here so the muscle-
			// memory "type /search to manage search" works both
			// directions.
			if m.searchQuery != "" {
				m.searchQuery = ""
				m.flash = "search cleared"
				return nil
			}
			m.flash = "usage: /search <pattern>  (press / in nav for live-filter)"
			return nil
		}
		m.searchQuery = strings.ToLower(rest)
		// Walk backward from the current selection — chat logs read
		// newest-first, so /search should land on the MOST RECENT
		// match (just-above-the-cursor or near-the-tail), not the
		// oldest one. n/N still step in their bound directions, so
		// once we're on the most-recent hit n walks further back
		// through history and N walks forward toward newer matches.
		if ok, count := m.jumpToSearchHit(-1); ok {
			m.flash = fmt.Sprintf(
				"search: %d matches for %q — n/N to step, /search to clear",
				count,
				rest,
			)
			m.mode = modeNav
			m.input.Blur()
		} else {
			m.flash = fmt.Sprintf("no match for %q", rest)
			m.searchQuery = ""
		}
	case "clear":
		m.messages = nil
		m.selectedMsg = 0
		m.flash = "scrollback cleared"

	default:
		m.flash = fmt.Sprintf("unknown /%s — see /help", verb)
	}
	return nil
}

// sendBang appends an outgoing command-originated message to the
// local log AND (in live-radio mode) transmits it over LoRa via the
// pump. The `bang` field is kept purely for local UI styling — the
// on-wire text is just `body`, clean enough that any other
// Meshtastic client reads it as plain chat.
//
// Used by /cq, /73, /qsl, /qth, /grid, /rs, /cqr, /sk, /qrz, /qrm,
// /qsb, /wx, /k, /mesh. Commands that don't transmit (/whois, /ping,
// /tr, /env, /config) use systemLine() instead.
func (m *model) sendBang(bang, body string) {
	m.sendBangReply(bang, body, 0)
}

// sendBangReply is sendBang with an optional reply target — when
// replyToID is non-zero, the outgoing packet carries Data.reply_id
// pointing at the parent message, and the local log entry records
// the same replyID so the renderer can draw a quoted-parent line
// above the reply.
func (m *model) sendBangReply(bang, body string, replyToID uint32) {
	status := "ack"
	var pid uint32
	var envelope *pb.ToRadio
	if !m.isDemo() {
		status = "pending" // flipped by radioRoutingMsg handler
	}
	if m.pump != nil {
		envelope, pid = newTextToRadio(body, m.currentChannelIndex(), replyToID)
	}
	item := messageItem{
		time:     timeNowHHMM(),
		from:     "me",
		mine:     true,
		bang:     bang,
		text:     body,
		status:   status,
		replyID:  replyToID,
		packetID: pid,
		fromNum:  m.myNodeNum,
		sentAt:   time.Now(),
	}
	m.messages = append(m.messages, item)
	m.selectedMsg = len(m.messages) - 1
	m.focused = paneMessages

	// Persist the outgoing so the log survives restart. Skipped in
	// demo mode (m.db is always nil there).
	m.storagePersist(saveMessage(m.db, m.currentChannel, item))

	if envelope != nil {
		m.pump.Enqueue(envelope)
	}
}

// replyTargetFor returns the packetID of the most recent message
// from the given callsign, or 0 if none exists. Used by directed
// ham verbs (/73 <call>, /qsl <call>, /sk <call>, /rs <call>, etc.)
// to thread the outgoing reply to whatever <call> most recently
// said — the Meshtastic "reply to" semantic.
func (m *model) replyTargetFor(call string) uint32 {
	if call == "" {
		return 0
	}
	target := strings.ToLower(strings.TrimSpace(call))
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.mine || msg.status == "system" || msg.packetID == 0 {
			continue
		}
		if strings.Contains(strings.ToLower(msg.from), target) {
			return msg.packetID
		}
	}
	return 0
}

// updateHelp handles keys while the help overlay is visible. Vim-style
// scroll: j/k lines, d/u half-page, g/G top/bottom, q / ? / Enter /
// ESC dismiss. Ctrl+X still exits the whole app.

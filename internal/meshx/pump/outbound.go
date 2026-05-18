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

package pump

// outbound.go — the consumer-side Send entrypoint plus the
// proto-envelope builders. Every model.Command flows through Send;
// each switch arm assembles a *pb.ToRadio and hands it to enqueue.
// This file is the one place gomeshproto types meet model.Command
// types on the outbound side, mirroring translate.go on the inbound.

import (
	"errors"
	"fmt"
	"math/rand/v2"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"

	"github.com/retr0h/meshx/internal/meshx/model"
)

// Send ships a typed model.Command. Returns the generated packetID
// for commands that allocate one (SendText / SendPing / SendTraceroute);
// returns 0 for fire-and-forget commands. ok is false when the
// outbound buffer is full OR the proto build failed (caller should
// surface an "outbound buffer full / build failed" flash).
//
// myNodeNum is read from the pump's own MyInfo cache — the consumer
// never has to thread it back through the API. AdminMessage variants
// address themselves to the local radio (To = myNodeNum) so the
// firmware applies the change locally instead of broadcasting it.
func (p *Pump) Send(cmd model.Command) (uint32, bool) {
	switch c := cmd.(type) {
	case model.SendText:
		envelope, pid := buildText(c.Text, uint32(c.Channel), c.ReplyID, c.ToNum)
		return pid, p.enqueue(envelope)

	case model.SendPing:
		envelope, pid := buildPing(c.TargetNum)
		return pid, p.enqueue(envelope)

	case model.SendTraceroute:
		envelope, pid, err := buildTraceroute(c.TargetNum)
		if err != nil {
			return 0, false
		}
		return pid, p.enqueue(envelope)

	case model.SetOwner:
		envelope, err := buildAdminSetOwner(p.myNum, c.LongName, c.ShortName, c.IsLicensed)
		if err != nil {
			return 0, false
		}
		return 0, p.enqueue(envelope)

	case model.SetBuzzer:
		envelope, err := buildAdminSetBuzzer(p.myNum, c.Enabled, c.Snapshot)
		if err != nil {
			return 0, false
		}
		return 0, p.enqueue(envelope)

	case model.SetChannel:
		envelope, err := buildAdminSetChannel(p.myNum, c.Slot)
		if err != nil {
			return 0, false
		}
		return 0, p.enqueue(envelope)

	case model.DeleteChannel:
		envelope, err := buildAdminDeleteChannel(p.myNum, c.Index)
		if err != nil {
			return 0, false
		}
		return 0, p.enqueue(envelope)

	case model.RequestSync:
		nonce := rand.Uint32()
		if nonce == 0 {
			nonce = 1
		}
		envelope := &pb.ToRadio{
			PayloadVariant: &pb.ToRadio_WantConfigId{WantConfigId: nonce},
		}
		return 0, p.enqueue(envelope)

	case model.RequestBuzzerConfig:
		envelope, err := buildAdminGetModuleConfigBuzzer(p.myNum)
		if err != nil {
			return 0, false
		}
		return 0, p.enqueue(envelope)

	case model.Reboot:
		envelope, err := buildAdminReboot(p.myNum, c.Seconds)
		if err != nil {
			return 0, false
		}
		return 0, p.enqueue(envelope)
	}
	return 0, false
}

// enqueue is the unexported back-channel Send arms call. Non-blocking
// send onto the outbound chan — drops the message and returns false
// when the buffer is full (caller flashes a hint).
func (p *Pump) enqueue(envelope *pb.ToRadio) bool {
	select {
	case p.outbound <- envelope:
		return true
	default:
		return false
	}
}

// randPacketID returns a non-zero 32-bit id suitable for
// MeshPacket.id. Zero is reserved (the firmware treats 0 as
// "unassigned") so we re-roll on the unlikely collision.
func randPacketID() uint32 {
	for {
		pid := rand.Uint32()
		if pid != 0 {
			return pid
		}
	}
}

// buildText builds the ToRadio envelope for a plain text chat
// packet. toNum=0 routes broadcast (MeshPacket.to=0xFFFFFFFF, the
// firmware-canonical "everyone on this channel"); a non-zero toNum
// addresses a specific peer for a direct message — same packet,
// same TEXT_MESSAGE_APP port, only the To field changes (Meshtastic
// has no separate DM port). When replyID != 0 the packet threads
// to the referenced parent so /reply / /73 etc. link to the
// originating message.
//
// Returns both the envelope and the generated MeshPacket.id so the
// caller can stash it on the local messageItem.packetID for ack
// correlation when the ROUTING_APP receipt lands.
func buildText(text string, channel, replyID, toNum uint32) (*pb.ToRadio, uint32) {
	to := uint32(0xFFFFFFFF)
	if toNum != 0 {
		to = toNum
	}
	pid := randPacketID()
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			Id:      pid,
			To:      to,
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

// buildPing builds a REPLY_APP MeshPacket addressed to targetNum.
// Meshtastic's REPLY_APP service is a built-in echo: the firmware
// automatically bounces whatever payload it receives back to the
// sender, which is the closest thing to ICMP echo the mesh has.
// WantAck so we still get a Routing receipt, WantResponse so the
// firmware actually relays the echo back. Returns the envelope plus
// the generated packetID the consumer stashes in pendingPing for
// correlation.
func buildPing(targetNum uint32) (*pb.ToRadio, uint32) {
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
	}, pid
}

// buildTraceroute builds a TRACEROUTE_APP MeshPacket addressed to
// targetNum with an empty RouteDiscovery payload. WantResponse tells
// the firmware to relay the discovery back populated with the
// traversed route; WantAck so we still get a Routing receipt to
// distinguish "request landed on the mesh" from "request never made
// it to the wire."
func buildTraceroute(targetNum uint32) (*pb.ToRadio, uint32, error) {
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
			HopLimit: 7,
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum:      pb.PortNum_TRACEROUTE_APP,
				Payload:      payload,
				WantResponse: true,
			}},
		}},
	}, pid, nil
}

// buildAdminSetOwner builds an AdminMessage.SetOwner ToRadio
// envelope, wrapped in a MeshPacket addressed to the local node.
// This is how meshX updates the radio's User config (longname /
// shortname / is_licensed) over the wire — same AdminMessage path
// the official Meshtastic phone apps and Python CLI use.
//
// Firmware ≥ 2.3 accepts User updates hot (in-memory + flash, no
// reboot). The phone apps still chase SetOwner with a reboot; we
// don't, saving ~20s of downtime per rename.
func buildAdminSetOwner(
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
	return wrapAdminEnvelope(myNodeNum, payload, false), nil
}

// buildAdminSetBuzzer builds an AdminMessage.SetModuleConfig
// envelope for ExternalNotification. snapshot is the full live
// config the radio reported (or zero if we toggled before the live
// config arrived); we project to proto via
// ExternalNotificationToProto and overwrite only the alert flags so
// other fields the user might have configured via the phone app
// (output pin, alert_bell, vibra, nag_timeout, …) ride through
// verbatim.
//
// Both Enabled+AlertMessageBuzzer move together: a meaningful
// "buzzer on" requires Enabled=true AND AlertMessageBuzzer=true,
// and "off" forces both false so the module doesn't keep firing on
// bell or vibra paths users probably don't realize were enabled.
//
// UsePwm rides with the toggle because most ham-radio boards
// (T-Beam, Heltec, RAK 4631 with WisBlock buzzer) have a hardware
// buzzer on the device-level device.buzzer_gpio pin, NOT on
// External Notification's per-module output_pin (which is "Unset"
// out of the box). With UsePwm=true the module ignores the
// per-module output pin / duration / active polarity and routes
// through device.buzzer_gpio — what's actually wired. Without it,
// "buzzer on" sets every alert flag but the firmware has no GPIO to
// drive and the radio stays silent. The phone app surfaces this
// same field as "Use PWM Buzzer".
//
// Like SetOwner this skips the reboot the official phone app issues
// after a module-config write — modern firmware accepts module
// config hot. If a future radio refuses without a reboot we'd need
// to chain an AdminMessage_RebootSeconds packet here.
func buildAdminSetBuzzer(
	myNodeNum uint32,
	enabled bool,
	snapshot model.ExternalNotification,
) (*pb.ToRadio, error) {
	ext := ExternalNotificationToProto(snapshot)
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
	return wrapAdminEnvelope(myNodeNum, payload, false), nil
}

// buildAdminSetChannel builds an AdminMessage.SetChannel envelope
// from a model.ChannelInfo slot. role is projected from the typed
// string; PSK rides as-is.
func buildAdminSetChannel(myNodeNum uint32, slot model.ChannelInfo) (*pb.ToRadio, error) {
	role, err := channelRoleToProto(slot.Role)
	if err != nil {
		return nil, err
	}
	settings := &pb.ChannelSettings{
		Name: slot.Name,
		Psk:  slot.PSK,
		Id:   slot.ID,
	}
	admin := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_SetChannel{
			SetChannel: &pb.Channel{
				Index:    int32(slot.Index),
				Settings: settings,
				Role:     role,
			},
		},
	}
	payload, err := proto.Marshal(admin)
	if err != nil {
		return nil, fmt.Errorf("marshal AdminMessage: %w", err)
	}
	return wrapAdminEnvelope(myNodeNum, payload, false), nil
}

// buildAdminDeleteChannel sends the same SetChannel envelope with
// role=DISABLED + nil settings — the firmware-canonical "free this
// slot" gesture.
func buildAdminDeleteChannel(myNodeNum uint32, index int) (*pb.ToRadio, error) {
	admin := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_SetChannel{
			SetChannel: &pb.Channel{
				Index: int32(index),
				Role:  pb.Channel_DISABLED,
			},
		},
	}
	payload, err := proto.Marshal(admin)
	if err != nil {
		return nil, fmt.Errorf("marshal AdminMessage: %w", err)
	}
	return wrapAdminEnvelope(myNodeNum, payload, false), nil
}

// buildAdminReboot builds an AdminMessage.RebootSeconds envelope.
// Firmware reboots after secs seconds; 5 gives the radio time to
// finish flushing pending writes (queued ACKs, NodeDB persistence)
// before the restart.
func buildAdminReboot(myNodeNum uint32, secs int32) (*pb.ToRadio, error) {
	admin := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_RebootSeconds{
			RebootSeconds: secs,
		},
	}
	payload, err := proto.Marshal(admin)
	if err != nil {
		return nil, fmt.Errorf("marshal AdminMessage: %w", err)
	}
	return wrapAdminEnvelope(myNodeNum, payload, false), nil
}

// buildAdminGetModuleConfigBuzzer asks the radio to send back its
// current ExternalNotification ModuleConfig. Used as a backstop
// after the WantConfigId handshake — some firmware doesn't push
// module configs proactively, in which case meshx would otherwise
// render a default-true guess in /config until the user hit save.
// The reply flows through the same FromRadio_ModuleConfig path the
// dump uses, so the consumer's ModuleBuzzer handler doesn't have to
// know whether it was solicited.
func buildAdminGetModuleConfigBuzzer(myNodeNum uint32) (*pb.ToRadio, error) {
	admin := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_GetModuleConfigRequest{
			GetModuleConfigRequest: pb.AdminMessage_EXTNOTIF_CONFIG,
		},
	}
	payload, err := proto.Marshal(admin)
	if err != nil {
		return nil, fmt.Errorf("marshal AdminMessage: %w", err)
	}
	return wrapAdminEnvelope(myNodeNum, payload, true), nil
}

// wrapAdminEnvelope wraps a marshalled AdminMessage payload in a
// MeshPacket addressed to the local radio (To=myNodeNum). All
// AdminMessage variants share this shape — the only knob is whether
// we want the firmware to send a response (true for queries like
// GetModuleConfigRequest, false for fire-and-forget Set* writes).
func wrapAdminEnvelope(myNodeNum uint32, payload []byte, wantResponse bool) *pb.ToRadio {
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			To:      myNodeNum,
			WantAck: true,
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum:      pb.PortNum_ADMIN_APP,
				Payload:      payload,
				WantResponse: wantResponse,
			}},
		}},
	}
}

// channelRoleToProto projects model.ChannelRole (typed string) to
// the proto enum. Unknown values are rejected — better to fail loud
// at the boundary than silently send a DISABLED slot the user
// didn't ask for.
func channelRoleToProto(role model.ChannelRole) (pb.Channel_Role, error) {
	switch role {
	case model.ChannelDisabled:
		return pb.Channel_DISABLED, nil
	case model.ChannelPrimary:
		return pb.Channel_PRIMARY, nil
	case model.ChannelSecondary:
		return pb.Channel_SECONDARY, nil
	}
	return 0, errors.New("unknown channel role: " + string(role))
}

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

// outbound_test.go — unit tests for the proto-envelope builder functions and
// the pump.Send dispatcher. All builders are package-level functions that
// return proto envelopes; we inspect the envelope shape instead of round-
// tripping through a real transport.

import (
	"testing"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"

	"github.com/retr0h/meshx/internal/meshx/model"
)

// extractPacket unwraps the MeshPacket from a ToRadio envelope.
func extractPacket(t *testing.T, env *pb.ToRadio) *pb.MeshPacket {
	t.Helper()
	tv, ok := env.GetPayloadVariant().(*pb.ToRadio_Packet)
	if !ok {
		t.Fatalf("expected ToRadio_Packet, got %T", env.GetPayloadVariant())
	}
	return tv.Packet
}

// extractDecoded unwraps the Data from a MeshPacket.
func extractDecoded(t *testing.T, pkt *pb.MeshPacket) *pb.Data {
	t.Helper()
	dv, ok := pkt.GetPayloadVariant().(*pb.MeshPacket_Decoded)
	if !ok {
		t.Fatalf("expected MeshPacket_Decoded, got %T", pkt.GetPayloadVariant())
	}
	if dv.Decoded == nil {
		t.Fatal("decoded is nil")
	}
	return dv.Decoded
}

// ---- buildText --------------------------------------------------------------

func TestBuildText_Broadcast(t *testing.T) {
	env, pid := buildText("hello", 0, 0, 0)
	if pid == 0 {
		t.Fatal("expected non-zero packetID")
	}
	pkt := extractPacket(t, env)
	if pkt.GetTo() != 0xFFFFFFFF {
		t.Errorf("To: got 0x%x, want 0xFFFFFFFF", pkt.GetTo())
	}
	if pkt.GetChannel() != 0 {
		t.Errorf("Channel: got %d", pkt.GetChannel())
	}
	if !pkt.GetWantAck() {
		t.Error("WantAck: want true")
	}
	dec := extractDecoded(t, pkt)
	if dec.GetPortnum() != pb.PortNum_TEXT_MESSAGE_APP {
		t.Errorf("Portnum: got %v", dec.GetPortnum())
	}
	if string(dec.GetPayload()) != "hello" {
		t.Errorf("Payload: got %q", dec.GetPayload())
	}
}

func TestBuildText_DirectMessage(t *testing.T) {
	env, pid := buildText("dm text", 1, 0, 0xDEAD)
	if pid == 0 {
		t.Fatal("expected non-zero packetID")
	}
	pkt := extractPacket(t, env)
	if pkt.GetTo() != 0xDEAD {
		t.Errorf("To: got 0x%x, want 0xDEAD", pkt.GetTo())
	}
	if pkt.GetChannel() != 1 {
		t.Errorf("Channel: got %d", pkt.GetChannel())
	}
}

func TestBuildText_WithReplyID(t *testing.T) {
	env, _ := buildText("reply", 0, 0xBEEF, 0)
	pkt := extractPacket(t, env)
	dec := extractDecoded(t, pkt)
	if dec.GetReplyId() != 0xBEEF {
		t.Errorf("ReplyId: got 0x%x", dec.GetReplyId())
	}
}

func TestBuildText_PacketIDIsNonZero(t *testing.T) {
	// Run multiple times — randPacketID should never produce zero.
	for i := range 20 {
		_, pid := buildText("test", 0, 0, 0)
		if pid == 0 {
			t.Errorf("iteration %d: got zero packetID", i)
		}
	}
}

// ---- buildPing --------------------------------------------------------------

func TestBuildPing(t *testing.T) {
	env, pid := buildPing(0x1234)
	if pid == 0 {
		t.Fatal("expected non-zero packetID")
	}
	pkt := extractPacket(t, env)
	if pkt.GetTo() != 0x1234 {
		t.Errorf("To: got 0x%x", pkt.GetTo())
	}
	if !pkt.GetWantAck() {
		t.Error("WantAck: want true")
	}
	if pkt.GetHopLimit() != 7 {
		t.Errorf("HopLimit: got %d, want 7", pkt.GetHopLimit())
	}
	dec := extractDecoded(t, pkt)
	if dec.GetPortnum() != pb.PortNum_REPLY_APP {
		t.Errorf("Portnum: got %v", dec.GetPortnum())
	}
	if !dec.GetWantResponse() {
		t.Error("WantResponse: want true")
	}
	if string(dec.GetPayload()) != "ping" {
		t.Errorf("Payload: got %q", dec.GetPayload())
	}
}

// ---- buildTraceroute --------------------------------------------------------

func TestBuildTraceroute(t *testing.T) {
	env, pid, err := buildTraceroute(0x5678)
	if err != nil {
		t.Fatalf("buildTraceroute: %v", err)
	}
	if pid == 0 {
		t.Fatal("expected non-zero packetID")
	}
	pkt := extractPacket(t, env)
	if pkt.GetTo() != 0x5678 {
		t.Errorf("To: got 0x%x", pkt.GetTo())
	}
	if pkt.GetHopLimit() != 7 {
		t.Errorf("HopLimit: got %d, want 7", pkt.GetHopLimit())
	}
	dec := extractDecoded(t, pkt)
	if dec.GetPortnum() != pb.PortNum_TRACEROUTE_APP {
		t.Errorf("Portnum: got %v", dec.GetPortnum())
	}
	if !dec.GetWantResponse() {
		t.Error("WantResponse: want true")
	}
	// Payload must be a valid (empty) RouteDiscovery.
	var rd pb.RouteDiscovery
	if err := proto.Unmarshal(dec.GetPayload(), &rd); err != nil {
		t.Errorf("RouteDiscovery payload invalid: %v", err)
	}
}

// ---- buildAdminSetOwner -----------------------------------------------------

func TestBuildAdminSetOwner(t *testing.T) {
	env, err := buildAdminSetOwner(0xABCD, "Alice Longname", "ALIC", true)
	if err != nil {
		t.Fatalf("buildAdminSetOwner: %v", err)
	}
	pkt := extractPacket(t, env)
	if pkt.GetTo() != 0xABCD {
		t.Errorf("To: got 0x%x, want 0xABCD", pkt.GetTo())
	}
	if !pkt.GetWantAck() {
		t.Error("WantAck: want true")
	}
	dec := extractDecoded(t, pkt)
	if dec.GetPortnum() != pb.PortNum_ADMIN_APP {
		t.Errorf("Portnum: got %v", dec.GetPortnum())
	}
	// Decode and inspect the AdminMessage.
	var adm pb.AdminMessage
	if err := proto.Unmarshal(dec.GetPayload(), &adm); err != nil {
		t.Fatalf("unmarshal AdminMessage: %v", err)
	}
	owner, ok := adm.GetPayloadVariant().(*pb.AdminMessage_SetOwner)
	if !ok {
		t.Fatalf("expected SetOwner, got %T", adm.GetPayloadVariant())
	}
	if owner.SetOwner.GetLongName() != "Alice Longname" {
		t.Errorf("LongName: got %q", owner.SetOwner.GetLongName())
	}
	if !owner.SetOwner.GetIsLicensed() {
		t.Error("IsLicensed: want true")
	}
}

// ---- buildAdminSetBuzzer ----------------------------------------------------

func TestBuildAdminSetBuzzer_Enable(t *testing.T) {
	snap := model.ExternalNotification{OutputMs: 100, Output: 5}
	env, err := buildAdminSetBuzzer(0x0001, true, snap)
	if err != nil {
		t.Fatalf("buildAdminSetBuzzer: %v", err)
	}
	pkt := extractPacket(t, env)
	dec := extractDecoded(t, pkt)
	var adm pb.AdminMessage
	if err := proto.Unmarshal(dec.GetPayload(), &adm); err != nil {
		t.Fatalf("unmarshal AdminMessage: %v", err)
	}
	sm, ok := adm.GetPayloadVariant().(*pb.AdminMessage_SetModuleConfig)
	if !ok {
		t.Fatalf("expected SetModuleConfig, got %T", adm.GetPayloadVariant())
	}
	en, ok := sm.SetModuleConfig.GetPayloadVariant().(*pb.ModuleConfig_ExternalNotification)
	if !ok {
		t.Fatalf("expected ExternalNotification, got %T", sm.SetModuleConfig.GetPayloadVariant())
	}
	ext := en.ExternalNotification
	if !ext.GetEnabled() {
		t.Error("Enabled: want true")
	}
	if !ext.GetAlertMessageBuzzer() {
		t.Error("AlertMessageBuzzer: want true")
	}
	if !ext.GetUsePwm() {
		t.Error("UsePwm: want true")
	}
	// Snapshot field must survive round-trip.
	if ext.GetOutputMs() != 100 {
		t.Errorf("OutputMs: got %d, want 100", ext.GetOutputMs())
	}
}

func TestBuildAdminSetBuzzer_Disable(t *testing.T) {
	snap := model.ExternalNotification{Enabled: true, AlertMessageBuzzer: true}
	env, err := buildAdminSetBuzzer(0x0002, false, snap)
	if err != nil {
		t.Fatalf("buildAdminSetBuzzer: %v", err)
	}
	pkt := extractPacket(t, env)
	dec := extractDecoded(t, pkt)
	var adm pb.AdminMessage
	if err := proto.Unmarshal(dec.GetPayload(), &adm); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sm := adm.GetPayloadVariant().(*pb.AdminMessage_SetModuleConfig)
	en := sm.SetModuleConfig.GetPayloadVariant().(*pb.ModuleConfig_ExternalNotification)
	ext := en.ExternalNotification
	if ext.GetEnabled() {
		t.Error("Enabled: want false after disable")
	}
	if ext.GetAlertMessageBuzzer() {
		t.Error("AlertMessageBuzzer: want false after disable")
	}
}

// ---- buildAdminSetChannel ---------------------------------------------------

func TestBuildAdminSetChannel_Secondary(t *testing.T) {
	slot := model.ChannelInfo{
		Index: 1,
		Name:  "Backup",
		Role:  model.ChannelSecondary,
		PSK:   []byte{0xAA, 0xBB},
		ID:    0x1234,
	}
	env, err := buildAdminSetChannel(0xFFFF, slot)
	if err != nil {
		t.Fatalf("buildAdminSetChannel: %v", err)
	}
	pkt := extractPacket(t, env)
	dec := extractDecoded(t, pkt)
	var adm pb.AdminMessage
	if err := proto.Unmarshal(dec.GetPayload(), &adm); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sc, ok := adm.GetPayloadVariant().(*pb.AdminMessage_SetChannel)
	if !ok {
		t.Fatalf("expected SetChannel, got %T", adm.GetPayloadVariant())
	}
	ch := sc.SetChannel
	if ch.GetIndex() != 1 {
		t.Errorf("Index: got %d", ch.GetIndex())
	}
	if ch.GetRole() != pb.Channel_SECONDARY {
		t.Errorf("Role: got %v", ch.GetRole())
	}
	if ch.GetSettings().GetName() != "Backup" {
		t.Errorf("Name: got %q", ch.GetSettings().GetName())
	}
}

func TestBuildAdminSetChannel_Primary(t *testing.T) {
	slot := model.ChannelInfo{
		Index: 0,
		Name:  "",
		Role:  model.ChannelPrimary,
	}
	env, err := buildAdminSetChannel(0x0001, slot)
	if err != nil {
		t.Fatalf("buildAdminSetChannel PRIMARY: %v", err)
	}
	pkt := extractPacket(t, env)
	dec := extractDecoded(t, pkt)
	var adm pb.AdminMessage
	_ = proto.Unmarshal(dec.GetPayload(), &adm)
	sc := adm.GetPayloadVariant().(*pb.AdminMessage_SetChannel)
	if sc.SetChannel.GetRole() != pb.Channel_PRIMARY {
		t.Errorf("Role: got %v", sc.SetChannel.GetRole())
	}
}

func TestBuildAdminSetChannel_UnknownRole_Error(t *testing.T) {
	slot := model.ChannelInfo{
		Index: 2,
		Name:  "test",
		Role:  model.ChannelRole("UNKNOWN_ROLE"),
	}
	_, err := buildAdminSetChannel(0x0001, slot)
	if err == nil {
		t.Fatal("expected error for unknown channel role")
	}
}

// ---- buildAdminDeleteChannel ------------------------------------------------

func TestBuildAdminDeleteChannel(t *testing.T) {
	env, err := buildAdminDeleteChannel(0x9999, 3)
	if err != nil {
		t.Fatalf("buildAdminDeleteChannel: %v", err)
	}
	pkt := extractPacket(t, env)
	dec := extractDecoded(t, pkt)
	var adm pb.AdminMessage
	if err := proto.Unmarshal(dec.GetPayload(), &adm); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sc, ok := adm.GetPayloadVariant().(*pb.AdminMessage_SetChannel)
	if !ok {
		t.Fatalf("expected SetChannel, got %T", adm.GetPayloadVariant())
	}
	if sc.SetChannel.GetIndex() != 3 {
		t.Errorf("Index: got %d", sc.SetChannel.GetIndex())
	}
	if sc.SetChannel.GetRole() != pb.Channel_DISABLED {
		t.Errorf("Role: got %v, want DISABLED", sc.SetChannel.GetRole())
	}
}

// ---- buildAdminReboot -------------------------------------------------------

func TestBuildAdminReboot(t *testing.T) {
	env, err := buildAdminReboot(0x1111, 5)
	if err != nil {
		t.Fatalf("buildAdminReboot: %v", err)
	}
	pkt := extractPacket(t, env)
	dec := extractDecoded(t, pkt)
	var adm pb.AdminMessage
	if err := proto.Unmarshal(dec.GetPayload(), &adm); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	rb, ok := adm.GetPayloadVariant().(*pb.AdminMessage_RebootSeconds)
	if !ok {
		t.Fatalf("expected RebootSeconds, got %T", adm.GetPayloadVariant())
	}
	if rb.RebootSeconds != 5 {
		t.Errorf("RebootSeconds: got %d, want 5", rb.RebootSeconds)
	}
}

// ---- buildAdminGetModuleConfigBuzzer ----------------------------------------

func TestBuildAdminGetModuleConfigBuzzer(t *testing.T) {
	env, err := buildAdminGetModuleConfigBuzzer(0x2222)
	if err != nil {
		t.Fatalf("buildAdminGetModuleConfigBuzzer: %v", err)
	}
	pkt := extractPacket(t, env)
	if pkt.GetTo() != 0x2222 {
		t.Errorf("To: got 0x%x", pkt.GetTo())
	}
	dec := extractDecoded(t, pkt)
	if !dec.GetWantResponse() {
		t.Error("WantResponse: want true for config query")
	}
	var adm pb.AdminMessage
	if err := proto.Unmarshal(dec.GetPayload(), &adm); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	gr, ok := adm.GetPayloadVariant().(*pb.AdminMessage_GetModuleConfigRequest)
	if !ok {
		t.Fatalf("expected GetModuleConfigRequest, got %T", adm.GetPayloadVariant())
	}
	if gr.GetModuleConfigRequest != pb.AdminMessage_EXTNOTIF_CONFIG {
		t.Errorf("GetModuleConfigRequest: got %v", gr.GetModuleConfigRequest)
	}
}

// ---- channelRoleToProto -----------------------------------------------------

func TestChannelRoleToProto(t *testing.T) {
	tests := []struct {
		role     model.ChannelRole
		wantRole pb.Channel_Role
		wantErr  bool
	}{
		{model.ChannelDisabled, pb.Channel_DISABLED, false},
		{model.ChannelPrimary, pb.Channel_PRIMARY, false},
		{model.ChannelSecondary, pb.Channel_SECONDARY, false},
		{model.ChannelRole("GARBAGE"), 0, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			got, err := channelRoleToProto(tt.role)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.wantRole {
				t.Errorf("got %v, want %v", got, tt.wantRole)
			}
		})
	}
}

// ---- pump.Send dispatcher ---------------------------------------------------

func TestPumpSend_SendText(t *testing.T) {
	p := newTestPump()
	pid, ok := p.Send(model.SendText{Text: "hi", Channel: 0})
	if !ok {
		t.Fatal("Send: returned false (buffer full?)")
	}
	if pid == 0 {
		t.Fatal("Send SendText: expected non-zero packetID")
	}
	// Envelope must be on the outbound channel.
	select {
	case env := <-p.outbound:
		if env == nil {
			t.Fatal("nil envelope on outbound")
		}
	default:
		t.Fatal("outbound channel empty after Send")
	}
}

func TestPumpSend_SendPing(t *testing.T) {
	p := newTestPump()
	pid, ok := p.Send(model.SendPing{TargetNum: 0x1234})
	if !ok {
		t.Fatal("Send: returned false")
	}
	if pid == 0 {
		t.Fatal("expected non-zero packetID")
	}
}

func TestPumpSend_SendTraceroute(t *testing.T) {
	p := newTestPump()
	pid, ok := p.Send(model.SendTraceroute{TargetNum: 0x5678})
	if !ok {
		t.Fatal("Send: returned false")
	}
	if pid == 0 {
		t.Fatal("expected non-zero packetID")
	}
}

func TestPumpSend_SetOwner(t *testing.T) {
	p := newTestPump()
	pid, ok := p.Send(model.SetOwner{LongName: "Alice", ShortName: "ALIC"})
	if !ok {
		t.Fatal("Send: returned false")
	}
	if pid != 0 {
		t.Errorf("SetOwner packetID: want 0, got %d", pid)
	}
}

func TestPumpSend_SetBuzzer(t *testing.T) {
	p := newTestPump()
	_, ok := p.Send(model.SetBuzzer{Enabled: true})
	if !ok {
		t.Fatal("Send SetBuzzer: returned false")
	}
}

func TestPumpSend_SetChannel(t *testing.T) {
	p := newTestPump()
	slot := model.ChannelInfo{Index: 1, Name: "Test", Role: model.ChannelSecondary}
	_, ok := p.Send(model.SetChannel{Slot: slot})
	if !ok {
		t.Fatal("Send SetChannel: returned false")
	}
}

func TestPumpSend_SetChannel_BadRole_ReturnsFalse(t *testing.T) {
	p := newTestPump()
	slot := model.ChannelInfo{Index: 1, Role: model.ChannelRole("BAD")}
	pid, ok := p.Send(model.SetChannel{Slot: slot})
	if ok {
		t.Fatal("Send SetChannel bad role: expected false")
	}
	if pid != 0 {
		t.Errorf("expected 0 packetID on error, got %d", pid)
	}
}

func TestPumpSend_DeleteChannel(t *testing.T) {
	p := newTestPump()
	_, ok := p.Send(model.DeleteChannel{Index: 2})
	if !ok {
		t.Fatal("Send DeleteChannel: returned false")
	}
}

func TestPumpSend_RequestSync(t *testing.T) {
	p := newTestPump()
	pid, ok := p.Send(model.RequestSync{})
	if !ok {
		t.Fatal("Send RequestSync: returned false")
	}
	if pid != 0 {
		t.Errorf("RequestSync packetID: want 0, got %d", pid)
	}
}

func TestPumpSend_RequestBuzzerConfig(t *testing.T) {
	p := newTestPump()
	_, ok := p.Send(model.RequestBuzzerConfig{})
	if !ok {
		t.Fatal("Send RequestBuzzerConfig: returned false")
	}
}

func TestPumpSend_Reboot(t *testing.T) {
	p := newTestPump()
	_, ok := p.Send(model.Reboot{Seconds: 5})
	if !ok {
		t.Fatal("Send Reboot: returned false")
	}
}

// ---- Enqueue / outbound buffer full -----------------------------------------

func TestPumpEnqueue_BufferFull_ReturnsFalse(t *testing.T) {
	p := newTestPump() // outbound has capacity 16
	env := &pb.ToRadio{}
	// Fill the buffer.
	for i := 0; i < 16; i++ {
		if !p.enqueue(env) {
			t.Fatalf("enqueue failed before buffer full at index %d", i)
		}
	}
	// Now buffer is full — next enqueue must drop.
	if p.enqueue(env) {
		t.Fatal("expected enqueue to fail when buffer full")
	}
}

func TestPumpEnqueue_Enqueue_Success(t *testing.T) {
	p := newTestPump()
	env := &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_WantConfigId{WantConfigId: 42},
	}
	if !p.enqueue(env) {
		t.Fatal("enqueue failed on empty buffer")
	}
	select {
	case got := <-p.outbound:
		wc, ok := got.GetPayloadVariant().(*pb.ToRadio_WantConfigId)
		if !ok || wc.WantConfigId != 42 {
			t.Errorf("unexpected envelope: %v", got)
		}
	default:
		t.Fatal("outbound empty after enqueue")
	}
}

// ---- randPacketID -----------------------------------------------------------

func TestRandPacketID_NeverZero(t *testing.T) {
	for i := range 100 {
		pid := randPacketID()
		if pid == 0 {
			t.Fatalf("iteration %d: got zero packetID", i)
		}
	}
}

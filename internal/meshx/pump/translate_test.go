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

// translate_test.go exercises the translate / translatePacket methods.
// These are the pure-function heart of the pump — FromRadio→model.*
// projection — and are directly callable without a live transport.
//
// The tests call the unexported methods directly because they live in
// the same package (package pump, not package pump_test). This keeps
// the test surface honest: we're testing internal projection logic,
// not a public API.

import (
	"testing"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"

	"github.com/retr0h/meshx/internal/meshx/model"
)

// newTestPump returns a zero-value Pump suitable for calling translate /
// translatePacket. No goroutines are started; no transport is touched.
func newTestPump() *Pump {
	return &Pump{
		outbound: make(chan *pb.ToRadio, 16),
	}
}

// ---- helpers ----------------------------------------------------------------

func marshalPayload(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// ---- translate: MyInfo ------------------------------------------------------

func TestTranslate_MyInfo(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_MyInfo{
			MyInfo: &pb.MyNodeInfo{MyNodeNum: 0xdeadbeef},
		},
	}
	events := p.translate(msg)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	mi, ok := events[0].(model.MyInfo)
	if !ok {
		t.Fatalf("want model.MyInfo, got %T", events[0])
	}
	if mi.NodeNum != 0xdeadbeef {
		t.Errorf("NodeNum: got 0x%x, want 0xdeadbeef", mi.NodeNum)
	}
	// myNum must be stored on the pump.
	if p.myNum != 0xdeadbeef {
		t.Errorf("pump.myNum: got 0x%x, want 0xdeadbeef", p.myNum)
	}
}

// ---- translate: NodeInfo (no position) --------------------------------------

func TestTranslate_NodeInfo_NoPosition(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_NodeInfo{
			NodeInfo: &pb.NodeInfo{
				Num: 0x1234,
				User: &pb.User{
					LongName:  "Alice",
					ShortName: "ALIC",
				},
				Snr:       7.5,
				LastHeard: 1700000000,
			},
		},
	}
	events := p.translate(msg)
	if len(events) != 1 {
		t.Fatalf("want 1 event (no position), got %d", len(events))
	}
	ni, ok := events[0].(model.NodeInfo)
	if !ok {
		t.Fatalf("want model.NodeInfo, got %T", events[0])
	}
	if ni.NodeNum != 0x1234 {
		t.Errorf("NodeNum: got 0x%x", ni.NodeNum)
	}
	if ni.LongName != "Alice" {
		t.Errorf("LongName: got %q", ni.LongName)
	}
	if ni.ShortName != "ALIC" {
		t.Errorf("ShortName: got %q", ni.ShortName)
	}
}

// ---- translate: NodeInfo with position --------------------------------------

func TestTranslate_NodeInfo_WithPosition(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_NodeInfo{
			NodeInfo: &pb.NodeInfo{
				Num: 0xabcd,
				User: &pb.User{
					LongName:  "Bob",
					ShortName: "BOB_",
				},
				Position: &pb.Position{
					LatitudeI:  int32(37.7749 * 1e7),
					LongitudeI: int32(-122.4194 * 1e7),
					Altitude:   15,
					Time:       1700000000,
				},
			},
		},
	}
	events := p.translate(msg)
	if len(events) != 2 {
		t.Fatalf("want 2 events (NodeInfo + Position), got %d", len(events))
	}
	if _, ok := events[0].(model.NodeInfo); !ok {
		t.Errorf("first event: want model.NodeInfo, got %T", events[0])
	}
	pos, ok := events[1].(model.Position)
	if !ok {
		t.Fatalf("second event: want model.Position, got %T", events[1])
	}
	if pos.FromNodeNum != 0xabcd {
		t.Errorf("Position.FromNodeNum: got 0x%x", pos.FromNodeNum)
	}
	if pos.Altitude != 15 {
		t.Errorf("Position.Altitude: got %d", pos.Altitude)
	}
}

// ---- translate: NodeInfo with zero position (skipped) -----------------------

func TestTranslate_NodeInfo_ZeroPosition_Skipped(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_NodeInfo{
			NodeInfo: &pb.NodeInfo{
				Num:  0x1111,
				User: &pb.User{LongName: "Carol", ShortName: "CARL"},
				Position: &pb.Position{
					// LatitudeI and LongitudeI both zero — no fix.
					LatitudeI:  0,
					LongitudeI: 0,
				},
			},
		},
	}
	events := p.translate(msg)
	// Only NodeInfo — zero position is dropped.
	if len(events) != 1 {
		t.Fatalf("want 1 event (no position), got %d: %#v", len(events), events)
	}
}

// ---- translate: Channel -----------------------------------------------------

func TestTranslate_Channel(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_Channel{
			Channel: &pb.Channel{
				Index: 0,
				Role:  pb.Channel_PRIMARY,
				Settings: &pb.ChannelSettings{
					Name: "LongFast",
					Psk:  []byte{0x01, 0x02, 0x03},
					Id:   0xbeef,
				},
			},
		},
	}
	events := p.translate(msg)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	ci, ok := events[0].(model.ChannelInfo)
	if !ok {
		t.Fatalf("want model.ChannelInfo, got %T", events[0])
	}
	if ci.Index != 0 {
		t.Errorf("Index: got %d", ci.Index)
	}
	if ci.Name != "LongFast" {
		t.Errorf("Name: got %q", ci.Name)
	}
	if !ci.HasPSK {
		t.Error("HasPSK: want true")
	}
	if len(ci.PSK) != 3 {
		t.Errorf("PSK len: got %d", len(ci.PSK))
	}
	if ci.ID != 0xbeef {
		t.Errorf("ID: got 0x%x", ci.ID)
	}
}

func TestTranslate_Channel_NoPSK(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_Channel{
			Channel: &pb.Channel{
				Index: 1,
				Role:  pb.Channel_SECONDARY,
				Settings: &pb.ChannelSettings{
					Name: "Admin",
				},
			},
		},
	}
	events := p.translate(msg)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	ci := events[0].(model.ChannelInfo)
	if ci.HasPSK {
		t.Error("HasPSK: want false for empty PSK")
	}
}

// ---- translate: ConfigCompleteId --------------------------------------------

func TestTranslate_ConfigCompleteId(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_ConfigCompleteId{ConfigCompleteId: 42},
	}
	events := p.translate(msg)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	if _, ok := events[0].(model.ConfigComplete); !ok {
		t.Fatalf("want model.ConfigComplete, got %T", events[0])
	}
}

// ---- translate: Metadata ----------------------------------------------------

func TestTranslate_Metadata(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_Metadata{
			Metadata: &pb.DeviceMetadata{
				FirmwareVersion:    "2.5.1.abcd",
				DeviceStateVersion: 23,
				HasWifi:            true,
				HasBluetooth:       true,
			},
		},
	}
	events := p.translate(msg)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	md, ok := events[0].(model.Metadata)
	if !ok {
		t.Fatalf("want model.Metadata, got %T", events[0])
	}
	if md.FirmwareVersion != "2.5.1.abcd" {
		t.Errorf("FirmwareVersion: got %q", md.FirmwareVersion)
	}
	if !md.HasWifi || !md.HasBluetooth {
		t.Error("HasWifi / HasBluetooth: want both true")
	}
}

// ---- translate: Config_Lora -------------------------------------------------

func TestTranslate_Config_Lora(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_Config{
			Config: &pb.Config{
				PayloadVariant: &pb.Config_Lora{
					Lora: &pb.Config_LoRaConfig{
						TxPower:     20,
						Region:      pb.Config_LoRaConfig_US,
						ModemPreset: pb.Config_LoRaConfig_LONG_FAST,
					},
				},
			},
		},
	}
	events := p.translate(msg)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	lc, ok := events[0].(model.LoraConfig)
	if !ok {
		t.Fatalf("want model.LoraConfig, got %T", events[0])
	}
	if lc.TxPowerDBm != 20 {
		t.Errorf("TxPowerDBm: got %d", lc.TxPowerDBm)
	}
	if lc.Region != model.RegionUS {
		t.Errorf("Region: got %q", lc.Region)
	}
	if lc.ModemPreset != model.ModemLongFast {
		t.Errorf("ModemPreset: got %q", lc.ModemPreset)
	}
}

func TestTranslate_Config_Lora_Nil(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_Config{
			Config: &pb.Config{
				PayloadVariant: &pb.Config_Lora{
					Lora: nil,
				},
			},
		},
	}
	events := p.translate(msg)
	if len(events) != 0 {
		t.Fatalf("want 0 events for nil Lora, got %d", len(events))
	}
}

// ---- translate: Config_Device -----------------------------------------------

func TestTranslate_Config_Device(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_Config{
			Config: &pb.Config{
				PayloadVariant: &pb.Config_Device{
					Device: &pb.Config_DeviceConfig{
						Role: pb.Config_DeviceConfig_ROUTER,
					},
				},
			},
		},
	}
	events := p.translate(msg)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	dc, ok := events[0].(model.DeviceConfig)
	if !ok {
		t.Fatalf("want model.DeviceConfig, got %T", events[0])
	}
	if dc.Role != model.RoleRouter {
		t.Errorf("Role: got %q", dc.Role)
	}
}

func TestTranslate_Config_Device_Nil(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_Config{
			Config: &pb.Config{
				PayloadVariant: &pb.Config_Device{
					Device: nil,
				},
			},
		},
	}
	events := p.translate(msg)
	if len(events) != 0 {
		t.Fatalf("want 0 events for nil Device, got %d", len(events))
	}
}

func TestTranslate_Config_UnknownVariant_ReturnsNil(t *testing.T) {
	p := newTestPump()
	// A Config with no recognised sub-variant drops silently.
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_Config{
			Config: &pb.Config{},
		},
	}
	events := p.translate(msg)
	if len(events) != 0 {
		t.Fatalf("want 0 events, got %d: %T", len(events), events)
	}
}

// ---- translate: ModuleConfig_ExternalNotification ---------------------------

func TestTranslate_ModuleConfig_ExternalNotification(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_ModuleConfig{
			ModuleConfig: &pb.ModuleConfig{
				PayloadVariant: &pb.ModuleConfig_ExternalNotification{
					ExternalNotification: &pb.ModuleConfig_ExternalNotificationConfig{
						Enabled:            true,
						AlertMessageBuzzer: true,
					},
				},
			},
		},
	}
	events := p.translate(msg)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	mb, ok := events[0].(model.ModuleBuzzer)
	if !ok {
		t.Fatalf("want model.ModuleBuzzer, got %T", events[0])
	}
	if !mb.Enabled {
		t.Error("Enabled: want true")
	}
	if !mb.AlertMessageBuzzer {
		t.Error("AlertMessageBuzzer: want true")
	}
}

func TestTranslate_ModuleConfig_Nil(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_ModuleConfig{
			ModuleConfig: nil,
		},
	}
	events := p.translate(msg)
	if len(events) != 0 {
		t.Fatalf("want 0 events, got %d", len(events))
	}
}

func TestTranslate_ModuleConfig_ExternalNotification_NilPayload(t *testing.T) {
	p := newTestPump()
	msg := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_ModuleConfig{
			ModuleConfig: &pb.ModuleConfig{
				PayloadVariant: &pb.ModuleConfig_ExternalNotification{
					ExternalNotification: nil,
				},
			},
		},
	}
	events := p.translate(msg)
	if len(events) != 0 {
		t.Fatalf("want 0 events for nil ExternalNotification, got %d", len(events))
	}
}

// ---- translate: unknown variant → nil --------------------------------------

func TestTranslate_UnknownVariant_ReturnsNil(t *testing.T) {
	p := newTestPump()
	// FromRadio with no payload variant set.
	msg := &pb.FromRadio{}
	events := p.translate(msg)
	if len(events) != 0 {
		t.Fatalf("want 0 events for unknown variant, got %d", len(events))
	}
}

// ---- translatePacket: TEXT_MESSAGE_APP -------------------------------------

func TestTranslatePacket_TextMessage(t *testing.T) {
	p := newTestPump()
	at := uint32(1700000000)
	pkt := &pb.MeshPacket{
		From:     0xaabb,
		To:       0xFFFFFFFF,
		Channel:  2,
		Id:       0x1234,
		RxTime:   at,
		RxSnr:    5.5,
		RxRssi:   -90,
		HopStart: 7,
		HopLimit: 5,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_TEXT_MESSAGE_APP,
			Payload: []byte("hello mesh"),
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	txt, ok := events[0].(model.Text)
	if !ok {
		t.Fatalf("want model.Text, got %T", events[0])
	}
	if txt.Body.Text != "hello mesh" {
		t.Errorf("Text: got %q", txt.Body.Text)
	}
	if txt.Body.PacketID != 0x1234 {
		t.Errorf("PacketID: got 0x%x", txt.Body.PacketID)
	}
	if txt.Channel != 2 {
		t.Errorf("Channel: got %d", txt.Channel)
	}
	if txt.Body.Hops != 2 { // 7-5
		t.Errorf("Hops: got %d, want 2", txt.Body.Hops)
	}
	if txt.Body.Status != model.StatusAck {
		t.Errorf("Status: got %q", txt.Body.Status)
	}
}

func TestTranslatePacket_TextMessage_DM(t *testing.T) {
	p := newTestPump()
	pkt := &pb.MeshPacket{
		From:    0x1111,
		To:      0x2222,
		Channel: 0,
		Id:      0x9999,
		RxTime:  1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum:      pb.PortNum_TEXT_MESSAGE_APP,
			Payload:      []byte("direct message"),
			ReplyId:      0xabcd,
			WantResponse: false,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	txt := events[0].(model.Text)
	if txt.ToNum != 0x2222 {
		t.Errorf("ToNum: got 0x%x", txt.ToNum)
	}
	if txt.Body.ReplyID != 0xabcd {
		t.Errorf("ReplyID: got 0x%x", txt.Body.ReplyID)
	}
}

// ---- translatePacket: TELEMETRY_APP — DeviceMetrics -------------------------

func TestTranslatePacket_Telemetry_DeviceMetrics(t *testing.T) {
	p := newTestPump()
	tel := &pb.Telemetry{
		Variant: &pb.Telemetry_DeviceMetrics{
			DeviceMetrics: &pb.DeviceMetrics{
				BatteryLevel:       85,
				Voltage:            3.7,
				ChannelUtilization: 12.5,
				AirUtilTx:          4.2,
			},
		},
	}
	payload := marshalPayload(t, tel)
	pkt := &pb.MeshPacket{
		From:   0x5555,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_TELEMETRY_APP,
			Payload: payload,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	dm, ok := events[0].(model.DeviceMetrics)
	if !ok {
		t.Fatalf("want model.DeviceMetrics, got %T", events[0])
	}
	if dm.BatteryLevel != 85 {
		t.Errorf("BatteryLevel: got %d", dm.BatteryLevel)
	}
	if dm.FromNodeNum != 0x5555 {
		t.Errorf("FromNodeNum: got 0x%x", dm.FromNodeNum)
	}
}

// ---- translatePacket: TELEMETRY_APP — EnvironmentMetrics -------------------

func TestTranslatePacket_Telemetry_EnvironmentMetrics(t *testing.T) {
	p := newTestPump()
	tel := &pb.Telemetry{
		Variant: &pb.Telemetry_EnvironmentMetrics{
			EnvironmentMetrics: &pb.EnvironmentMetrics{
				Temperature:        22.5,
				RelativeHumidity:   60.0,
				BarometricPressure: 1013.25,
				GasResistance:      50000.0,
			},
		},
	}
	payload := marshalPayload(t, tel)
	pkt := &pb.MeshPacket{
		From:   0x6666,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_TELEMETRY_APP,
			Payload: payload,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	em, ok := events[0].(model.EnvMetrics)
	if !ok {
		t.Fatalf("want model.EnvMetrics, got %T", events[0])
	}
	if em.FromNodeNum != 0x6666 {
		t.Errorf("FromNodeNum: got 0x%x", em.FromNodeNum)
	}
	// Float comparisons — allow ±0.1 tolerance.
	if em.Temperature < 22.0 || em.Temperature > 23.0 {
		t.Errorf("Temperature: got %f", em.Temperature)
	}
}

func TestTranslatePacket_Telemetry_InvalidPayload_ReturnsNil(t *testing.T) {
	p := newTestPump()
	pkt := &pb.MeshPacket{
		From:   0x7777,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_TELEMETRY_APP,
			Payload: []byte("not valid protobuf xxxxxxxxxxx"),
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 0 {
		t.Fatalf("want 0 events for corrupt telemetry payload, got %d", len(events))
	}
}

func TestTranslatePacket_Telemetry_UnknownVariant_ReturnsNil(t *testing.T) {
	// A Telemetry proto with no recognized variant (e.g. only unknown
	// future fields) should produce zero events.
	p := newTestPump()
	// An empty Telemetry message has no variant set.
	tel := &pb.Telemetry{}
	payload := marshalPayload(t, tel)
	pkt := &pb.MeshPacket{
		From:   0x8888,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_TELEMETRY_APP,
			Payload: payload,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 0 {
		t.Fatalf("want 0 events for empty Telemetry variant, got %d", len(events))
	}
}

// ---- translatePacket: POSITION_APP -----------------------------------------

func TestTranslatePacket_Position(t *testing.T) {
	p := newTestPump()
	pos := &pb.Position{
		LatitudeI:  int32(51.5074 * 1e7),
		LongitudeI: int32(-0.1278 * 1e7),
		Altitude:   50,
		Time:       1700000000,
	}
	payload := marshalPayload(t, pos)
	pkt := &pb.MeshPacket{
		From:   0x3333,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_POSITION_APP,
			Payload: payload,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	pp, ok := events[0].(model.Position)
	if !ok {
		t.Fatalf("want model.Position, got %T", events[0])
	}
	if pp.FromNodeNum != 0x3333 {
		t.Errorf("FromNodeNum: got 0x%x", pp.FromNodeNum)
	}
	if pp.Altitude != 50 {
		t.Errorf("Altitude: got %d", pp.Altitude)
	}
}

func TestTranslatePacket_Position_ZeroLatLon_Skipped(t *testing.T) {
	p := newTestPump()
	pos := &pb.Position{
		LatitudeI:  0,
		LongitudeI: 0,
		Altitude:   0,
	}
	payload := marshalPayload(t, pos)
	pkt := &pb.MeshPacket{
		From:   0x4444,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_POSITION_APP,
			Payload: payload,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 0 {
		t.Fatalf("want 0 events for zero position, got %d", len(events))
	}
}

func TestTranslatePacket_Position_InvalidPayload_ReturnsNil(t *testing.T) {
	p := newTestPump()
	pkt := &pb.MeshPacket{
		From:   0x4445,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_POSITION_APP,
			Payload: []byte("not protobuf garbage xxxxxxxx"),
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 0 {
		t.Fatalf("want 0 events for corrupt position payload, got %d", len(events))
	}
}

// ---- translatePacket: NODEINFO_APP -----------------------------------------

func TestTranslatePacket_NodeInfo(t *testing.T) {
	p := newTestPump()
	user := &pb.User{
		LongName:  "Dave",
		ShortName: "DAVE",
	}
	payload := marshalPayload(t, user)
	pkt := &pb.MeshPacket{
		From:     0xd00d,
		RxTime:   1700000000,
		RxSnr:    3.0,
		RxRssi:   -80,
		HopStart: 7,
		HopLimit: 6,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_NODEINFO_APP,
			Payload: payload,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	ni, ok := events[0].(model.NodeInfo)
	if !ok {
		t.Fatalf("want model.NodeInfo, got %T", events[0])
	}
	if ni.NodeNum != 0xd00d {
		t.Errorf("NodeNum: got 0x%x", ni.NodeNum)
	}
	if ni.LongName != "Dave" {
		t.Errorf("LongName: got %q", ni.LongName)
	}
	if ni.Hops != 1 { // 7-6
		t.Errorf("Hops: got %d, want 1", ni.Hops)
	}
}

func TestTranslatePacket_NodeInfo_InvalidPayload_ReturnsNil(t *testing.T) {
	p := newTestPump()
	pkt := &pb.MeshPacket{
		From:   0xd00e,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_NODEINFO_APP,
			Payload: []byte("garbage payload xxxxxxxxxx"),
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 0 {
		t.Fatalf("want 0 events for corrupt nodeinfo payload, got %d", len(events))
	}
}

// ---- translatePacket: ROUTING_APP ------------------------------------------

func TestTranslatePacket_Routing_OK(t *testing.T) {
	p := newTestPump()
	r := &pb.Routing{
		Variant: &pb.Routing_ErrorReason{ErrorReason: pb.Routing_NONE},
	}
	payload := marshalPayload(t, r)
	pkt := &pb.MeshPacket{
		From:     0xaaaa,
		RxTime:   1700000000,
		HopStart: 7,
		HopLimit: 7,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum:   pb.PortNum_ROUTING_APP,
			Payload:   payload,
			RequestId: 0x1234,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	rt, ok := events[0].(model.Routing)
	if !ok {
		t.Fatalf("want model.Routing, got %T", events[0])
	}
	if !rt.OK {
		t.Error("OK: want true for NONE error reason")
	}
	if rt.RequestID != 0x1234 {
		t.Errorf("RequestID: got 0x%x", rt.RequestID)
	}
	if rt.FromNum != 0xaaaa {
		t.Errorf("FromNum: got 0x%x", rt.FromNum)
	}
}

func TestTranslatePacket_Routing_Timeout(t *testing.T) {
	p := newTestPump()
	r := &pb.Routing{
		Variant: &pb.Routing_ErrorReason{ErrorReason: pb.Routing_TIMEOUT},
	}
	payload := marshalPayload(t, r)
	pkt := &pb.MeshPacket{
		From:   0xbbbb,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_ROUTING_APP,
			Payload: payload,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	rt := events[0].(model.Routing)
	if rt.OK {
		t.Error("OK: want false for TIMEOUT")
	}
	if rt.Reason != model.RoutingTimeout {
		t.Errorf("Reason: got %q, want %q", rt.Reason, model.RoutingTimeout)
	}
}

func TestTranslatePacket_Routing_InvalidPayload_ReturnsNil(t *testing.T) {
	p := newTestPump()
	pkt := &pb.MeshPacket{
		From:   0xcccc,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_ROUTING_APP,
			Payload: []byte("not protobuf routing data xxxx"),
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 0 {
		t.Fatalf("want 0 events for corrupt routing payload, got %d", len(events))
	}
}

// ---- translatePacket: ADMIN_APP — GetModuleConfigResponse ------------------

func TestTranslatePacket_AdminApp_ModuleConfigResponse(t *testing.T) {
	p := newTestPump()
	adm := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_GetModuleConfigResponse{
			GetModuleConfigResponse: &pb.ModuleConfig{
				PayloadVariant: &pb.ModuleConfig_ExternalNotification{
					ExternalNotification: &pb.ModuleConfig_ExternalNotificationConfig{
						Enabled:            true,
						AlertMessageBuzzer: true,
					},
				},
			},
		},
	}
	payload := marshalPayload(t, adm)
	pkt := &pb.MeshPacket{
		From:   0xe0e0,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_ADMIN_APP,
			Payload: payload,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	mb, ok := events[0].(model.ModuleBuzzer)
	if !ok {
		t.Fatalf("want model.ModuleBuzzer, got %T", events[0])
	}
	if !mb.Enabled || !mb.AlertMessageBuzzer {
		t.Error("ModuleBuzzer flags: want both true")
	}
}

func TestTranslatePacket_AdminApp_NonModuleConfigResponse_ReturnsNil(t *testing.T) {
	// An AdminMessage with a different variant (e.g. SetOwner response)
	// must drop silently.
	p := newTestPump()
	adm := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_SetOwner{
			SetOwner: &pb.User{LongName: "test"},
		},
	}
	payload := marshalPayload(t, adm)
	pkt := &pb.MeshPacket{
		From:   0xf0f0,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_ADMIN_APP,
			Payload: payload,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 0 {
		t.Fatalf("want 0 events, got %d", len(events))
	}
}

func TestTranslatePacket_AdminApp_InvalidPayload_ReturnsNil(t *testing.T) {
	p := newTestPump()
	pkt := &pb.MeshPacket{
		From:   0xf0f1,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_ADMIN_APP,
			Payload: []byte("not admin protobuf garbage xxx"),
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 0 {
		t.Fatalf("want 0 events for corrupt admin payload, got %d", len(events))
	}
}

// ---- translatePacket: REPLY_APP (ping response) ----------------------------

func TestTranslatePacket_ReplyApp_Ping(t *testing.T) {
	p := newTestPump()
	pkt := &pb.MeshPacket{
		From:     0x7890,
		RxTime:   1700000000,
		RxSnr:    2.5,
		RxRssi:   -75,
		HopStart: 7,
		HopLimit: 6,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum:   pb.PortNum_REPLY_APP,
			Payload:   []byte("ping"),
			RequestId: 0x9876,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	pg, ok := events[0].(model.Ping)
	if !ok {
		t.Fatalf("want model.Ping, got %T", events[0])
	}
	if pg.RequestID != 0x9876 {
		t.Errorf("RequestID: got 0x%x", pg.RequestID)
	}
	if pg.FromNum != 0x7890 {
		t.Errorf("FromNum: got 0x%x", pg.FromNum)
	}
	if pg.Hops != 1 {
		t.Errorf("Hops: got %d, want 1", pg.Hops)
	}
}

// ---- translatePacket: TRACEROUTE_APP ---------------------------------------

func TestTranslatePacket_Traceroute(t *testing.T) {
	p := newTestPump()
	rd := &pb.RouteDiscovery{
		Route: []uint32{0x1111, 0x2222},
	}
	payload := marshalPayload(t, rd)
	pkt := &pb.MeshPacket{
		From:   0x3210,
		To:     0x0123,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum:   pb.PortNum_TRACEROUTE_APP,
			Payload:   payload,
			RequestId: 0x5678,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	tr, ok := events[0].(model.Traceroute)
	if !ok {
		t.Fatalf("want model.Traceroute, got %T", events[0])
	}
	if tr.RequestID != 0x5678 {
		t.Errorf("RequestID: got 0x%x", tr.RequestID)
	}
	if len(tr.Route) != 2 || tr.Route[0] != 0x1111 {
		t.Errorf("Route: got %v", tr.Route)
	}
}

func TestTranslatePacket_Traceroute_InvalidPayload_ReturnsNil(t *testing.T) {
	p := newTestPump()
	pkt := &pb.MeshPacket{
		From:   0x3211,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_TRACEROUTE_APP,
			Payload: []byte("not traceroute protobuf xxxxxxx"),
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 0 {
		t.Fatalf("want 0 events for corrupt traceroute payload, got %d", len(events))
	}
}

// ---- translatePacket: ADMIN_APP — nil ext in GetModuleConfigResponse -------

func TestTranslatePacket_AdminApp_NilExtNotification_ReturnsNil(t *testing.T) {
	p := newTestPump()
	// GetModuleConfigResponse with a non-ExternalNotification variant —
	// the ext == nil branch.
	adm := &pb.AdminMessage{
		PayloadVariant: &pb.AdminMessage_GetModuleConfigResponse{
			GetModuleConfigResponse: &pb.ModuleConfig{
				// No ExternalNotification sub-variant — GetExternalNotification() returns nil.
				PayloadVariant: &pb.ModuleConfig_Mqtt{},
			},
		},
	}
	payload := marshalPayload(t, adm)
	pkt := &pb.MeshPacket{
		From:   0xf0f2,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_ADMIN_APP,
			Payload: payload,
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 0 {
		t.Fatalf("want 0 events for nil ext notification, got %d", len(events))
	}
}

// ---- translatePacket: nil Decoded → nil ------------------------------------

func TestTranslatePacket_NilDecoded_ReturnsNil(t *testing.T) {
	p := newTestPump()
	pkt := &pb.MeshPacket{
		From:           0x1234,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: nil},
	}
	events := p.translatePacket(pkt)
	if len(events) != 0 {
		t.Fatalf("want 0 events for nil Decoded, got %d", len(events))
	}
}

// ---- translatePacket: unrecognised portnum → nil ---------------------------

func TestTranslatePacket_UnknownPortnum_ReturnsNil(t *testing.T) {
	p := newTestPump()
	pkt := &pb.MeshPacket{
		From:   0x1234,
		RxTime: 1700000000,
		PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
			Portnum: pb.PortNum_SERIAL_APP,
			Payload: []byte("whatever"),
		}},
	}
	events := p.translatePacket(pkt)
	if len(events) != 0 {
		t.Fatalf("want 0 events for unhandled portnum, got %d", len(events))
	}
}

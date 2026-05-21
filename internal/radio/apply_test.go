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

package radio

import (
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// TestSession_ApplyMyInfo verifies that ApplyMyInfo stamps MyNodeNum,
// updates RadioID when the store is nil (no-op in that path), and
// reports Changed correctly.
func TestSession_ApplyMyInfo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		initID      string
		nodeNum     uint32
		wantMyNum   uint32
		wantNewID   string
		wantChanged bool
	}{
		{
			name:        "stamps-mynodenum-no-store",
			initID:      "pending:usb:/dev/cu0",
			nodeNum:     0xdeadbeef,
			wantMyNum:   0xdeadbeef,
			wantNewID:   "pending:usb:/dev/cu0", // store nil, no ClaimRadioIdentity
			wantChanged: false,
		},
		{
			name:        "same-id-not-changed",
			initID:      "",
			nodeNum:     1,
			wantMyNum:   1,
			wantNewID:   "",
			wantChanged: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(_ *testing.T) {
			s := newTestSession()
			s.State.RadioID = tc.initID

			res := s.ApplyMyInfo(mdl.MyInfo{NodeNum: tc.nodeNum})

			if s.State.MyNodeNum != tc.wantMyNum {
				t.Fatalf("MyNodeNum = %d, want %d", s.State.MyNodeNum, tc.wantMyNum)
			}
			if res.NewRadioID != tc.wantNewID {
				t.Fatalf("NewRadioID = %q, want %q", res.NewRadioID, tc.wantNewID)
			}
			if res.Changed != tc.wantChanged {
				t.Fatalf("Changed = %v, want %v", res.Changed, tc.wantChanged)
			}
			if res.OldRadioID != tc.initID {
				t.Fatalf("OldRadioID = %q, want %q", res.OldRadioID, tc.initID)
			}
		})
	}
}

// TestSession_ApplyMetadata verifies all four firmware-metadata fields
// are written to State.
func TestSession_ApplyMetadata(t *testing.T) {
	t.Parallel()

	s := newTestSession()
	s.ApplyMetadata(mdl.Metadata{
		FirmwareVersion: "2.5.0.abcdef",
		DeviceStateVer:  7,
		HasWifi:         true,
		HasBluetooth:    false,
	})

	if s.State.RadioFirmware != "2.5.0.abcdef" {
		t.Fatalf("RadioFirmware = %q, want 2.5.0.abcdef", s.State.RadioFirmware)
	}
	if s.State.RadioDeviceState != 7 {
		t.Fatalf("RadioDeviceState = %d, want 7", s.State.RadioDeviceState)
	}
	if !s.State.RadioHasWifi {
		t.Fatal("RadioHasWifi = false, want true")
	}
	if s.State.RadioHasBT {
		t.Fatal("RadioHasBT = true, want false")
	}
}

// TestSession_ApplyLoraConfig verifies LoRa config fields are stamped.
func TestSession_ApplyLoraConfig(t *testing.T) {
	t.Parallel()

	s := newTestSession()
	s.ApplyLoraConfig(mdl.LoraConfig{
		TxPowerDBm:  30,
		Region:      mdl.RegionUS,
		ModemPreset: mdl.ModemLongFast,
	})

	if s.State.RadioTxPower != 30 {
		t.Fatalf("RadioTxPower = %d, want 30", s.State.RadioTxPower)
	}
	if s.State.RadioRegion != "US" {
		t.Fatalf("RadioRegion = %q, want US", s.State.RadioRegion)
	}
	if s.State.RadioModemPreset != "LONG_FAST" {
		t.Fatalf("RadioModemPreset = %q, want LONG_FAST", s.State.RadioModemPreset)
	}
}

// TestSession_ApplyDeviceConfig verifies the role field is stamped.
func TestSession_ApplyDeviceConfig(t *testing.T) {
	t.Parallel()

	s := newTestSession()
	s.ApplyDeviceConfig(mdl.DeviceConfig{Role: mdl.RoleRouter})

	if s.State.RadioRole != "ROUTER" {
		t.Fatalf("RadioRole = %q, want ROUTER", s.State.RadioRole)
	}
}

// TestSession_ApplyDeviceMetrics verifies own-node telemetry is applied
// and peer telemetry is ignored.
func TestSession_ApplyDeviceMetrics(t *testing.T) {
	t.Parallel()

	const myNum = uint32(0xdeadbeef)

	cases := []struct {
		name        string
		fromNodeNum uint32
		wantApplied bool
	}{
		{
			name:        "own-node-applies",
			fromNodeNum: myNum,
			wantApplied: true,
		},
		{
			name:        "zero-node-num-applies",
			fromNodeNum: 0,
			wantApplied: true,
		},
		{
			name:        "peer-node-ignored",
			fromNodeNum: 0xc0ffee,
			wantApplied: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(_ *testing.T) {
			s := newTestSession()
			s.State.MyNodeNum = myNum
			s.ApplyDeviceMetrics(mdl.DeviceMetrics{
				FromNodeNum:  tc.fromNodeNum,
				BatteryLevel: 80,
				Voltage:      3.7,
				ChannelUtil:  12.5,
				AirUtilTx:    3.2,
			})
			if tc.wantApplied {
				if s.State.BatteryLevel != 80 {
					t.Fatalf("BatteryLevel = %d, want 80", s.State.BatteryLevel)
				}
				if !s.State.HasTelemetry {
					t.Fatal("HasTelemetry = false, want true")
				}
			} else {
				if s.State.BatteryLevel != 0 {
					t.Fatalf("BatteryLevel = %d, want 0 (peer ignored)", s.State.BatteryLevel)
				}
				if s.State.HasTelemetry {
					t.Fatal("HasTelemetry = true, want false (peer ignored)")
				}
			}
		})
	}
}

// TestSession_ApplyEnvMetrics verifies env telemetry is stored in
// PeerEnv keyed by FromNodeNum, and that PeerEnv is lazily initialized.
func TestSession_ApplyEnvMetrics(t *testing.T) {
	t.Parallel()

	s := newTestSession()
	s.State.PeerEnv = nil // simulate uninitialized map

	const peer = uint32(0xaabbccdd)
	s.ApplyEnvMetrics(mdl.EnvMetrics{
		FromNodeNum: peer,
		Temperature: 23.5,
		Humidity:    55.0,
		Pressure:    1013.25,
		Gas:         100000,
	})

	if s.State.PeerEnv == nil {
		t.Fatal("PeerEnv is nil after ApplyEnvMetrics")
	}
	env, ok := s.State.PeerEnv[peer]
	if !ok {
		t.Fatal("PeerEnv entry missing for FromNodeNum")
	}
	if env.Temperature != 23.5 {
		t.Fatalf("Temperature = %v, want 23.5", env.Temperature)
	}
	if env.Humidity != 55.0 {
		t.Fatalf("Humidity = %v, want 55.0", env.Humidity)
	}
}

// TestSession_ApplyPosition verifies self and peer position paths.
func TestSession_ApplyPosition(t *testing.T) {
	t.Parallel()

	const myNum = uint32(0xdeadbeef)
	const peerNum = uint32(0xc0ffee)

	cases := []struct {
		name       string
		fromNum    uint32
		wantIsSelf bool
		wantMyLat  float64
	}{
		{
			name:       "self-position-updates-my-fields",
			fromNum:    myNum,
			wantIsSelf: true,
			wantMyLat:  37.7749,
		},
		{
			name:       "peer-position-does-not-update-my-fields",
			fromNum:    peerNum,
			wantIsSelf: false,
			wantMyLat:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(_ *testing.T) {
			s := newTestSession()
			s.State.MyNodeNum = myNum

			res := s.ApplyPosition(mdl.Position{
				FromNodeNum: tc.fromNum,
				Latitude:    37.7749,
				Longitude:   -122.4194,
				Altitude:    10,
				At:          time.Now(),
			}, "CM87ww")

			if res.IsSelf != tc.wantIsSelf {
				t.Fatalf("IsSelf = %v, want %v", res.IsSelf, tc.wantIsSelf)
			}
			if s.State.MyLatitude != tc.wantMyLat {
				t.Fatalf("MyLatitude = %v, want %v", s.State.MyLatitude, tc.wantMyLat)
			}
			if _, ok := s.State.PeerPositions[tc.fromNum]; !ok {
				t.Fatalf("PeerPositions missing entry for node %d", tc.fromNum)
			}
		})
	}
}

// TestSession_ApplyChannelInfo verifies slot growth, name decoration,
// DISABLED preservation, and CurrentChannel seeding.
func TestSession_ApplyChannelInfo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		msg         mdl.ChannelInfo
		wantName    string
		wantRole    string
		wantPrivate bool
		wantCurrent string // expected State.CurrentChannel after apply
	}{
		{
			name: "primary-empty-name-becomes-default",
			msg: mdl.ChannelInfo{
				Index: 0,
				Name:  "",
				Role:  mdl.ChannelPrimary,
			},
			wantName:    "#default",
			wantRole:    "PRIMARY",
			wantPrivate: false,
			wantCurrent: "#default",
		},
		{
			name: "secondary-with-name-gets-hash-prefix",
			msg: mdl.ChannelInfo{
				Index: 1,
				Name:  "ham",
				Role:  mdl.ChannelSecondary,
			},
			wantName:    "#ham",
			wantRole:    "SECONDARY",
			wantPrivate: false,
			wantCurrent: "", // CurrentChannel already set by prior slot
		},
		{
			name: "private-channel-gets-star-decoration",
			msg: mdl.ChannelInfo{
				Index:  2,
				Name:   "secret",
				Role:   mdl.ChannelSecondary,
				HasPSK: true,
				PSK:    []byte("k"),
			},
			wantName:    "*secret*",
			wantRole:    "SECONDARY",
			wantPrivate: true,
			wantCurrent: "",
		},
		{
			name: "disabled-slot-preserved",
			msg: mdl.ChannelInfo{
				Index: 3,
				Role:  mdl.ChannelDisabled,
			},
			wantName:    "",
			wantRole:    "DISABLED",
			wantPrivate: false,
			wantCurrent: "",
		},
	}

	s := newTestSession()
	for _, tc := range cases {
		t.Run(tc.name, func(_ *testing.T) {
			s.ApplyChannelInfo(tc.msg)
			if tc.msg.Index >= len(s.State.Channels) {
				t.Fatalf(
					"Channels too short (%d) after apply of slot %d",
					len(s.State.Channels),
					tc.msg.Index,
				)
			}
			c := s.State.Channels[tc.msg.Index]
			if c.Name != tc.wantName {
				t.Fatalf("Channels[%d].Name = %q, want %q", tc.msg.Index, c.Name, tc.wantName)
			}
			if c.Role != tc.wantRole {
				t.Fatalf("Channels[%d].Role = %q, want %q", tc.msg.Index, c.Role, tc.wantRole)
			}
			if c.Private != tc.wantPrivate {
				t.Fatalf(
					"Channels[%d].Private = %v, want %v",
					tc.msg.Index,
					c.Private,
					tc.wantPrivate,
				)
			}
			// CurrentChannel is seeded to the first non-disabled channel seen.
			if tc.wantCurrent != "" && s.State.CurrentChannel != tc.wantCurrent {
				t.Fatalf("CurrentChannel = %q, want %q", s.State.CurrentChannel, tc.wantCurrent)
			}
		})
	}
}

// TestSession_ApplyChannelInfo_PreservesUnread confirms that re-applying
// a channel slot doesn't reset the unread counter.
func TestSession_ApplyChannelInfo_PreservesUnread(t *testing.T) {
	t.Parallel()

	s := newTestSession()
	s.ApplyChannelInfo(mdl.ChannelInfo{Index: 0, Name: "default", Role: mdl.ChannelPrimary})
	s.State.Channels[0].Unread = 7

	s.ApplyChannelInfo(mdl.ChannelInfo{Index: 0, Name: "default", Role: mdl.ChannelPrimary})
	if s.State.Channels[0].Unread != 7 {
		t.Fatalf("Unread = %d, want 7 (must survive re-apply)", s.State.Channels[0].Unread)
	}
}

// TestSession_ApplyNodeInfo covers upsert, ghost-upgrade detection, and
// Fav preservation across re-apply.
func TestSession_ApplyNodeInfo(t *testing.T) {
	t.Parallel()

	t.Run("new-node-appended", func(_ *testing.T) {
		s := newTestSession()
		res := s.ApplyNodeInfo(mdl.NodeInfo{
			NodeNum:   0x1234,
			LongName:  "Alice",
			ShortName: "ALIC",
		})
		if res.GhostUpgrade {
			t.Fatal("GhostUpgrade = true for new node")
		}
		if len(s.State.Nodes) != 1 {
			t.Fatalf("Nodes len = %d, want 1", len(s.State.Nodes))
		}
		if s.State.Nodes[0].Callsign != "Alice" {
			t.Fatalf("Callsign = %q, want Alice", s.State.Nodes[0].Callsign)
		}
	})

	t.Run("ghost-upgrade-detected", func(_ *testing.T) {
		s := newTestSession()
		// Seed an unresolved ghost first.
		long, short := mdl.DefaultCallsign(0x5678)
		s.State.Nodes = append(s.State.Nodes, mdl.NodeItem{
			Callsign:   long,
			ShortName:  short,
			NodeNum:    0x5678,
			Unresolved: true,
		})
		s.State.NodesByNum[0x5678] = 0

		res := s.ApplyNodeInfo(mdl.NodeInfo{
			NodeNum:   0x5678,
			LongName:  "Bob",
			ShortName: "BOB!",
		})
		if !res.GhostUpgrade {
			t.Fatal("GhostUpgrade = false, want true (unresolved → real callsign)")
		}
		if res.NewCallsign != "Bob" {
			t.Fatalf("NewCallsign = %q, want Bob", res.NewCallsign)
		}
	})

	t.Run("fav-preserved-on-update", func(_ *testing.T) {
		s := newTestSession()
		// Insert and mark as fav.
		s.ApplyNodeInfo(mdl.NodeInfo{NodeNum: 0xABCD, LongName: "Carol", ShortName: "CARL"})
		s.State.Nodes[0].Fav = true

		// Re-apply the same node.
		s.ApplyNodeInfo(mdl.NodeInfo{NodeNum: 0xABCD, LongName: "Carol", ShortName: "CARL"})
		if !s.State.Nodes[0].Fav {
			t.Fatal("Fav = false after re-apply; must be preserved")
		}
	})

	t.Run("unresolved-callsign-synthesized", func(_ *testing.T) {
		s := newTestSession()
		const n = uint32(0x0000ABCD)
		s.ApplyNodeInfo(mdl.NodeInfo{NodeNum: n}) // no names
		if len(s.State.Nodes) != 1 {
			t.Fatalf("Nodes len = %d, want 1", len(s.State.Nodes))
		}
		long, _ := mdl.DefaultCallsign(n)
		if s.State.Nodes[0].Callsign != long {
			t.Fatalf("Callsign = %q, want %q (synthesized)", s.State.Nodes[0].Callsign, long)
		}
		if !s.State.Nodes[0].Unresolved {
			t.Fatal("Unresolved = false; want true for no-name node")
		}
	})
}

// TestSession_ApplyConfigComplete verifies the Connected/Reconnect
// state machine and the wasDisconnected return value.
func TestSession_ApplyConfigComplete(t *testing.T) {
	t.Parallel()

	t.Run("first-complete-returns-true", func(_ *testing.T) {
		s := newTestSession()
		s.State.Connected = false
		wasDisc := s.ApplyConfigComplete()
		if !wasDisc {
			t.Fatal("wasDisconnected = false on first ConfigComplete")
		}
		if !s.State.Connected {
			t.Fatal("Connected = false after ConfigComplete")
		}
		if s.State.Reconnect != nil {
			t.Fatal("Reconnect should be nil after ConfigComplete")
		}
	})

	t.Run("already-connected-returns-false", func(_ *testing.T) {
		s := newTestSession()
		s.ApplyConfigComplete()            // first
		wasDisc := s.ApplyConfigComplete() // second
		if wasDisc {
			t.Fatal("wasDisconnected = true when already connected")
		}
	})

	t.Run("clears-reconnect-state", func(_ *testing.T) {
		s := newTestSession()
		s.State.Reconnect = &ReconnectState{Attempt: 3}
		s.ApplyConfigComplete()
		if s.State.Reconnect != nil {
			t.Fatal("Reconnect not cleared by ConfigComplete")
		}
	})
}

// TestSession_ApplyDisconnected verifies Connected flips to false while
// leaving Reconnect intact.
func TestSession_ApplyDisconnected(t *testing.T) {
	t.Parallel()

	s := newTestSession()
	s.State.Connected = true
	s.State.Reconnect = &ReconnectState{Attempt: 1}

	s.ApplyDisconnected(mdl.Disconnected{})

	if s.State.Connected {
		t.Fatal("Connected = true after ApplyDisconnected")
	}
	if s.State.Reconnect == nil {
		t.Fatal("Reconnect cleared by ApplyDisconnected; should survive")
	}
}

// TestSession_ApplyReconnecting verifies the reconnect banner is set.
func TestSession_ApplyReconnecting(t *testing.T) {
	t.Parallel()

	s := newTestSession()
	s.ApplyReconnecting(mdl.Reconnecting{
		Attempt: 2,
		After:   3 * time.Second,
		Err:     nil,
	})

	if s.State.Reconnect == nil {
		t.Fatal("Reconnect is nil after ApplyReconnecting")
	}
	if s.State.Reconnect.Attempt != 2 {
		t.Fatalf("Reconnect.Attempt = %d, want 2", s.State.Reconnect.Attempt)
	}
	if s.State.Reconnect.ReadyAt.IsZero() {
		t.Fatal("Reconnect.ReadyAt is zero")
	}
}

// TestSession_RecordOutbound verifies mine-rows are appended correctly
// and indexed in MessagesByPacketID.
func TestSession_RecordOutbound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		opts     RecordOutboundOptions
		wantPIDs bool
	}{
		{
			name: "with-packet-id-indexes",
			opts: RecordOutboundOptions{
				Channel:  0,
				Text:     "hello",
				PacketID: 42,
				ToNum:    0,
			},
			wantPIDs: true,
		},
		{
			name: "zero-packet-id-not-indexed",
			opts: RecordOutboundOptions{
				Channel:  0,
				Text:     "demo",
				PacketID: 0,
			},
			wantPIDs: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(_ *testing.T) {
			s := newTestSession()
			s.State.Channels = []mdl.ChannelItem{
				{Index: 0, Name: "#default", Role: "PRIMARY"},
			}
			s.State.CurrentChannel = "#default"

			before := len(s.State.Messages)
			res := s.RecordOutbound(tc.opts)
			after := len(s.State.Messages)

			if after != before+1 {
				t.Fatalf("Messages len delta = %d, want 1", after-before)
			}
			if !res.FromMine {
				t.Fatal("FromMine = false, want true")
			}
			row := s.State.Messages[res.Index]
			if !row.Mine {
				t.Fatal("row.Mine = false, want true")
			}
			if row.Text != tc.opts.Text {
				t.Fatalf("row.Text = %q, want %q", row.Text, tc.opts.Text)
			}
			if row.Status != mdl.StatusPending {
				t.Fatalf("row.Status = %q, want pending", row.Status)
			}
			if tc.wantPIDs {
				if _, ok := s.State.MessagesByPacketID[tc.opts.PacketID]; !ok {
					t.Fatal("PacketID not indexed in MessagesByPacketID")
				}
			}
		})
	}
}

// TestSession_ApplyPing verifies the node's telemetry fields are
// updated when the FromNum is in NodesByNum, and is a no-op otherwise.
func TestSession_ApplyPing(t *testing.T) {
	t.Parallel()

	t.Run("known-node-updates-telemetry", func(_ *testing.T) {
		s := newTestSession()
		s.State.Nodes = []mdl.NodeItem{
			{NodeNum: 0x1234, Callsign: "Alice"},
		}
		s.State.NodesByNum[0x1234] = 0

		s.ApplyPing(mdl.Ping{
			FromNum: 0x1234,
			Hops:    2,
			SNR:     "-8.5",
			RSSI:    "-92",
		})

		n := s.State.Nodes[0]
		if n.LastHops != 2 {
			t.Fatalf("LastHops = %d, want 2", n.LastHops)
		}
		if n.LastSNR != "-8.5" {
			t.Fatalf("LastSNR = %q, want -8.5", n.LastSNR)
		}
		if n.LastRSSI != "-92" {
			t.Fatalf("LastRSSI = %q, want -92", n.LastRSSI)
		}
	})

	t.Run("unknown-node-no-panic", func(_ *testing.T) {
		s := newTestSession()
		s.ApplyPing(mdl.Ping{FromNum: 0xdeadbeef, Hops: 1})
		// No crash = pass.
	})
}

// TestSession_ApplyTraceroute verifies last-hops is updated from the
// route length for known peers.
func TestSession_ApplyTraceroute(t *testing.T) {
	t.Parallel()

	s := newTestSession()
	s.State.Nodes = []mdl.NodeItem{{NodeNum: 0x9999}}
	s.State.NodesByNum[0x9999] = 0

	s.ApplyTraceroute(mdl.Traceroute{
		FromNum: 0x9999,
		Route:   []uint32{1, 2, 3},
	})

	if s.State.Nodes[0].LastHops != 3 {
		t.Fatalf("LastHops = %d, want 3", s.State.Nodes[0].LastHops)
	}
}

// TestHumanDuration validates the display formatter for different
// durations — uses the unexported function directly since the test is
// in the same package.
func TestHumanDuration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h"},
		{50 * time.Hour, "2d"},
	}

	for _, tc := range cases {
		got := humanDuration(tc.d)
		if got != tc.want {
			t.Fatalf("humanDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

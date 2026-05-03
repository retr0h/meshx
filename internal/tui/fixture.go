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
	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// Demo is meshx's tier-1 fixture primitive. When the model holds a
// non-nil `*Demo`, the app is running in demo mode — every renderer
// and command that needs "what's my radio identity" consults this
// struct instead of live-transport state. When the model's demo
// pointer is nil, the app is in live-radio mode and pulls from the
// transport pump.
//
// This single source of truth means:
//
//   - Every canned "T-Beam v1.1" / "fw 2.3.4" / "CN85" literal lives
//     here, not duplicated across status-bar / /config / /qth / seed
//     data.
//   - Adding a field to Demo automatically flows through the UI once
//     the corresponding renderer branch reads m.demo.X.
//   - Users / test code can supply a custom Demo to drive alternate
//     personas ("a QRP operator on battery", "a router node in the
//     UK") without touching the model code.
//
// Fields are organised by what slot of the UI they drive so it's
// obvious at a glance what a given value shows up in.
type Demo struct {
	// Identity — top status bar + /config + outbound CQ/73 messages.
	Callsign  string // what goes in `//\ <here>` and "de <here>"
	LongName  string // unused today; future NodeInfo parity
	ShortName string // unused today; future NodeInfo parity
	NodeNum   uint32 // what the radio's MyInfo.my_node_num would be

	// Hardware + firmware — top status bar + /config + /whois of self.
	HWModel  string // "T-Beam v1.1"
	Firmware string // "2.3.4"
	HasWifi  bool
	HasBT    bool

	// LoRa config — top status bar + /config.
	CurrentChannel string // "#default"
	ModemPreset    string // "LONG_FAST"
	Region         string // "US"
	TxPowerDBm     int32  // e.g. 14
	Role           string // "CLIENT"

	// Live telemetry (DeviceMetrics equivalent) — top status bar + /config.
	BatteryLevel uint32  // 0-100 (>100 = powered)
	Voltage      float32 // volts
	ChannelUtil  float32 // percent
	AirUtilTx    float32 // percent
	NoiseFloor   string  // "-92dB"

	// Position — top status bar (☖ grid) + /whois of self.
	Grid      string
	Latitude  float64
	Longitude float64

	// Seed content — message log, channels list, users grid.
	Channels []channelItem
	Nodes    []nodeItem
	Messages []messageItem
}

// DefaultDemo returns the canonical "💀 retr0h" HELTEC persona that
// ships with meshx. Modeled on a real-world HELTEC_V3_E so the demo
// screenshots look like what a user actually sees on-air. Kept as a
// function so each caller gets a fresh mutable copy — the model
// modifies message status etc. and we don't want demo launches to
// share state.
func DefaultDemo() *Demo {
	return &Demo{
		Callsign:       "retr0h",
		LongName:       "retr0h",
		ShortName:      "💀",
		NodeNum:        0x103d20cd,
		HWModel:        "HELTEC_V3_E",
		Firmware:       "2.7.15.567b8ea",
		HasWifi:        true,
		HasBT:          true,
		CurrentChannel: "#default",
		ModemPreset:    "LONG_FAST",
		Region:         "US",
		TxPowerDBm:     30,
		Role:           "CLIENT",
		BatteryLevel:   87,
		Voltage:        4.21,
		ChannelUtil:    4.2,
		NoiseFloor:     "-92dB",
		Grid:           "CN85ow",
		Latitude:       45.5231,
		Longitude:      -122.6765,

		Channels: []channelItem{
			{name: "#default", unread: 3},
			{name: "#admin", unread: 0},
			{name: "#emcomm", unread: 0},
			{name: "*secret*", private: true, unread: 1},
		},

		// Peer names + hw / signal values are fictional — similar
		// shape to a real mesh (mix of BBS-ish handles, punny
		// shortnames, a range of firmware versions) but
		// deliberately NOT drawn from any actual on-air operator's
		// identity. Only retr0h / 💀 (the demo "me") mirrors a
		// real radio; everyone else is made up.
		Nodes: []nodeItem{
			// Index 0 is "me" — matches NodeNum above so myNode()
			// resolves via m.NodesByNum[0x103d20cd] = 0.
			{
				callsign: "retr0h", shortName: "💀", state: stateOnline, fav: true,
				lastHeard: "now", heardRank: 0, lastSNR: "0.0", lastRSSI: "0",
				lastHops: 0, hwModel: "HELTEC_V3_E", firmware: "2.7.15.567b8ea",
			},
			{
				callsign: "TangoBravo_7", shortName: "TB7", state: stateOnline,
				lastHeard: "2m", heardRank: 2, lastSNR: "4.2", lastRSSI: "-92",
				lastHops: 5, hwModel: "RAK4631", firmware: "2.6.11",
			},
			{
				callsign: "MeshLab - plrmsh.io", shortName: "MLAB", state: stateOnline,
				lastHeard: "14s", heardRank: 1, lastSNR: "5.5", lastRSSI: "-89",
				lastHops: 2, hwModel: "HELTEC_V3", firmware: "2.7.15",
			},
			{
				callsign: "SolarRelay_HillNode", shortName: "SOLR", state: stateOnline,
				lastHeard: "1m", heardRank: 3, lastSNR: "3.2", lastRSSI: "-103",
				lastHops: 3, hwModel: "Station-G2", firmware: "2.7.15",
			},
			{
				callsign: "Helmsdeep", shortName: "HDP", state: stateOnline,
				lastHeard: "4m", heardRank: 4, lastSNR: "-5.0", lastRSSI: "-98",
				lastHops: 2, hwModel: "TRACKER_T1000_E", firmware: "2.7.15",
			},
			{
				callsign: "BoarSense 1f4a", shortName: "🐗", state: stateOnline,
				lastHeard: "5m", heardRank: 5, lastSNR: "-8.5", lastRSSI: "-101",
				lastHops: 3, hwModel: "T-Beam v1.1", firmware: "2.6.11",
			},
			{
				callsign: "KE9NIL", shortName: "NIL", state: stateOffline,
				lastHeard: "2h", heardRank: 99, lastSNR: "-16.0", lastRSSI: "-115",
				lastHops: 5, hwModel: "T-Deck", firmware: "2.1.0",
			},
		},

		// Demo conversation flow chosen to show off as many UI
		// features as possible in ~12 rows:
		//   1. Own /cq beacon with the "via meshx" attribution
		//      — bang-styled yellow flag + github URL on-wire.
		//   2. Threaded /cqr reply pointing back at the CQ packet
		//      — pink ┌ quote line + hop/SNR right column + ack ✓.
		//   3. /whois card as a 7-line systemBlock — group-zebra
		//      binding, irssi-style indented continuation rows.
		//   4. Ghost peer message — 👻 prefix, drained callsign.
		//   5. `-!- identified …` system line — the moment a ghost
		//      peer upgrades to a real name.
		//   6. A failed /73 — shows the pink ✗ status variant.
		// Ten distinct UI features per screenshot.
		Messages: []messageItem{
			{Message: mdl.Message{
				Time: "14:09", From: "TangoBravo_7", FromNum: 0x5a00aa01,
				Text: "just booted, saying hi 👋", Hops: 3, SNR: "-5.2",
			}},
			{Message: mdl.Message{
				Time: "14:10", From: "retr0h", Mine: true, Bang: "/cq",
				Text: "CQ CQ CQ de retr0h via meshx (github.com/retr0h/meshx) — " +
					"testing signals, please ack",
				Status: mdl.StatusAck, PacketID: 7001,
			}},
			{Message: mdl.Message{
				Time: "14:11", From: "TangoBravo_7", FromNum: 0x5a00aa01,
				Bang: "/cqr",
				Text: "copy 9/9 from Cascadia, SNR -5.2 hop 3",
				Hops: 3, SNR: "-5.2", PacketID: 7002, ReplyID: 7001,
			}},
			{Message: mdl.Message{
				Time: "14:12", From: "MeshLab - plrmsh.io", FromNum: 0x5a00aa02,
				Text: "strong copy from my side too — nice tx",
				Hops: 2, SNR: "5.5",
			}},
			{Message: mdl.Message{
				Time: "14:13", From: "Helmsdeep", FromNum: 0x5a00aa03,
				Text: "anyone going to the club meetup saturday?",
				Hops: 2, SNR: "-5.0",
			}},
			// /whois card — systemBlock emits rows sharing one group
			// id so the zebra stripe treats them as one visual card.
			// Hand-seeded with group=1 and matching timestamps so the
			// render loop groups them the same way.
			{
				Message: mdl.Message{
					Time:   "14:14",
					Text:   "-!- whois TangoBravo_7",
					Status: mdl.StatusSystem,
				},
				group: 1,
			},
			{
				Message: mdl.Message{
					Time:   "14:14",
					Text:   "-!-    hw:     RAK4631",
					Status: mdl.StatusSystem,
				},
				group: 1,
			},
			{
				Message: mdl.Message{
					Time:   "14:14",
					Text:   "-!-    fw:     2.6.11",
					Status: mdl.StatusSystem,
				},
				group: 1,
			},
			{
				Message: mdl.Message{
					Time:   "14:14",
					Text:   "-!-    heard:  3m ago",
					Status: mdl.StatusSystem,
				},
				group: 1,
			},
			{
				Message: mdl.Message{
					Time:   "14:14",
					Text:   "-!-    state:  online",
					Status: mdl.StatusSystem,
				},
				group: 1,
			},
			{
				Message: mdl.Message{
					Time:   "14:14",
					Text:   "-!-    signal: hop 3, SNR -5.2 dB, RSSI -92 dBm",
					Status: mdl.StatusSystem,
				},
				group: 1,
			},
			{
				Message: mdl.Message{
					Time:   "14:14",
					Text:   "-!-    end of /whois",
					Status: mdl.StatusSystem,
				},
				group: 1,
			},
			{Message: mdl.Message{
				Time: "14:16", From: "retr0h", Mine: true,
				Text:   "running the 30w build again, let me know if it's too loud",
				Status: mdl.StatusAck,
			}},
			// Ghost peer — fromNum populated but NOT in m.nodes /
			// m.NodesByNum, so displayFrom falls back to msg.From and
			// renderMessageRow adds the 👻 prefix + drained color.
			{Message: mdl.Message{
				Time: "14:17", From: "node 0x6f66d09d", FromNum: 0x6f66d09d,
				Text: "anyone in the east valley?", Hops: 4, SNR: "-9.1",
				PacketID: 7010,
			}},
			// Multi-line canary — real radios embed \n in telemetry
			// reports (like solar-node end-of-day summaries). Keeps
			// the hanging-indent layout honest when we re-render
			// demo mode after touching renderMessageRow.
			{Message: mdl.Message{
				Time: "14:17", From: "mmca solar test", FromNum: 0x5a00aa03,
				Text: "End of Day Report:\nMax Power: 1375.2576 mW at Pot setting: 133, Voltage: 6.0160 V, Current: 228.6000 mA\nWas the battery fully charged during the day? Yes",
				Hops: 4, SNR: "-3.5",
				Status: mdl.StatusAck,
			}},
			{Message: mdl.Message{
				Time: "14:18", From: "MeshLab - plrmsh.io", FromNum: 0x5a00aa02,
				Text: "6 🐰", Hops: 2, SNR: "5.5", ReplyID: 7010,
			}},
			// Real meshx event — fires from upsertNode when a
			// previously "node 0x…" placeholder finally has its
			// NodeInfo packet arrive and we can resolve the
			// longname. Lavender-italic drained — "this happened
			// behind the scenes, FYI."
			{Message: mdl.Message{
				Time: "14:19", From: "",
				Text:   "-!- identified BoarSense 1f4a (was node 0xe7f4aa01)",
				Status: mdl.StatusSystem,
			}},
			{Message: mdl.Message{
				Time: "14:21", From: "retr0h", Mine: true,
				Text: "brb lunch 🌮", Status: mdl.StatusAck,
			}},
			// Failed send — renders with pink ✗ in the status col,
			// shows the fail variant of the status column.
			{Message: mdl.Message{
				Time: "14:23", From: "retr0h", Mine: true, Bang: "/73",
				Text: "73 👋", Status: mdl.StatusFail,
			}},
		},
	}
}

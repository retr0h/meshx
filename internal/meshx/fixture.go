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

package meshx

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
			// resolves via m.nodesByNum[0x103d20cd] = 0.
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
			{
				time: "14:09", from: "TangoBravo_7", fromNum: 0x5a00aa01,
				text: "just booted, saying hi 👋", hops: 3, snr: "-5.2",
			},
			{
				time: "14:10", from: "retr0h", mine: true, bang: "/cq",
				text: "CQ CQ CQ de retr0h via meshx (github.com/retr0h/meshx) — " +
					"testing signals, please ack",
				status: statusAck, packetID: 7001,
			},
			{
				time: "14:11", from: "TangoBravo_7", fromNum: 0x5a00aa01,
				bang: "/cqr",
				text: "copy 9/9 from Cascadia, SNR -5.2 hop 3",
				hops: 3, snr: "-5.2", packetID: 7002, replyID: 7001,
			},
			{
				time: "14:12", from: "MeshLab - plrmsh.io", fromNum: 0x5a00aa02,
				text: "strong copy from my side too — nice tx",
				hops: 2, snr: "5.5",
			},
			{
				time: "14:13", from: "Helmsdeep", fromNum: 0x5a00aa03,
				text: "anyone going to the club meetup saturday?",
				hops: 2, snr: "-5.0",
			},
			// /whois card — systemBlock emits rows sharing one group
			// id so the zebra stripe treats them as one visual card.
			// Hand-seeded with group=1 and matching timestamps so the
			// render loop groups them the same way.
			{time: "14:14", text: "-!- whois TangoBravo_7", status: statusSystem, group: 1},
			{time: "14:14", text: "-!-    hw:     RAK4631", status: statusSystem, group: 1},
			{time: "14:14", text: "-!-    fw:     2.6.11", status: statusSystem, group: 1},
			{time: "14:14", text: "-!-    heard:  3m ago", status: statusSystem, group: 1},
			{time: "14:14", text: "-!-    state:  online", status: statusSystem, group: 1},
			{
				time:   "14:14",
				text:   "-!-    signal: hop 3, SNR -5.2 dB, RSSI -92 dBm",
				status: statusSystem,
				group:  1,
			},
			{time: "14:14", text: "-!-    end of /whois", status: statusSystem, group: 1},
			{
				time: "14:16", from: "retr0h", mine: true,
				text:   "running the 30w build again, let me know if it's too loud",
				status: statusAck,
			},
			// Ghost peer — fromNum populated but NOT in m.nodes /
			// m.nodesByNum, so displayFrom falls back to msg.from and
			// renderMessageRow adds the 👻 prefix + drained color.
			{
				time: "14:17", from: "node 0x6f66d09d", fromNum: 0x6f66d09d,
				text: "anyone in the east valley?", hops: 4, snr: "-9.1",
				packetID: 7010,
			},
			// Multi-line canary — real radios embed \n in telemetry
			// reports (like solar-node end-of-day summaries). Keeps
			// the hanging-indent layout honest when we re-render
			// demo mode after touching renderMessageRow.
			{
				time: "14:17", from: "mmca solar test", fromNum: 0x5a00aa03,
				text: "End of Day Report:\nMax Power: 1375.2576 mW at Pot setting: 133, Voltage: 6.0160 V, Current: 228.6000 mA\nWas the battery fully charged during the day? Yes",
				hops: 4, snr: "-3.5",
				status: statusAck,
			},
			{
				time: "14:18", from: "MeshLab - plrmsh.io", fromNum: 0x5a00aa02,
				text: "6 🐰", hops: 2, snr: "5.5", replyID: 7010,
			},
			// Real meshx event — fires from upsertNode when a
			// previously "node 0x…" placeholder finally has its
			// NodeInfo packet arrive and we can resolve the
			// longname. Lavender-italic drained — "this happened
			// behind the scenes, FYI."
			{
				time: "14:19", from: "",
				text:   "-!- identified BoarSense 1f4a (was node 0xe7f4aa01)",
				status: statusSystem,
			},
			{
				time: "14:21", from: "retr0h", mine: true,
				text: "brb lunch 🌮", status: statusAck,
			},
			// Failed send — renders with pink ✗ in the status col,
			// shows the fail variant of the status column.
			{
				time: "14:23", from: "retr0h", mine: true, bang: "/73",
				text: "73 👋", status: statusFail,
			},
		},
	}
}

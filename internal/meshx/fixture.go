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

		Nodes: []nodeItem{
			// Index 0 is "me" — matches NodeNum above so myNode()
			// resolves via m.nodesByNum[0x103d20cd] = 0.
			{
				callsign: "retr0h", shortName: "💀", state: "online", fav: true,
				lastHeard: "now", heardRank: 0, lastSNR: "0.0", lastRSSI: "0",
				lastHops: 0, hwModel: "HELTEC_V3_E", firmware: "2.7.15.567b8ea",
			},
			{
				callsign: "WiobooJones", shortName: "Wiib", state: "online",
				lastHeard: "2m", heardRank: 2, lastSNR: "4.2", lastRSSI: "-92",
				lastHops: 5, hwModel: "RAK4631", firmware: "2.6.11",
			},
			{
				callsign: "Gleep - socalme.sh", shortName: "GLP1", state: "online",
				lastHeard: "14s", heardRank: 1, lastSNR: "5.5", lastRSSI: "-89",
				lastHops: 2, hwModel: "HELTEC_V3", firmware: "2.7.15",
			},
			{
				callsign: "AmputiLayag_MeshNodeQTHlab", shortName: "AMPL", state: "online",
				lastHeard: "1m", heardRank: 3, lastSNR: "3.2", lastRSSI: "-103",
				lastHops: 3, hwModel: "Station-G2", firmware: "2.7.15",
			},
			{
				callsign: "Edoras", shortName: "Bkin", state: "online",
				lastHeard: "4m", heardRank: 4, lastSNR: "-5.0", lastRSSI: "-98",
				lastHops: 2, hwModel: "TRACKER_T1000_E", firmware: "2.7.15",
			},
			{
				callsign: "Hogman e7f4", shortName: "🐗", state: "online",
				lastHeard: "5m", heardRank: 5, lastSNR: "-8.5", lastRSSI: "-101",
				lastHops: 3, hwModel: "T-Beam v1.1", firmware: "2.6.11",
			},
			{
				callsign: "N7DEF", shortName: "DEF", state: "offline",
				lastHeard: "2h", heardRank: 99, lastSNR: "-16.0", lastRSSI: "-115",
				lastHops: 5, hwModel: "T-Deck", firmware: "2.1.0",
			},
		},

		Messages: []messageItem{
			{
				time: "14:39", from: "WiobooJones", fromNum: 3595239870,
				text: "Afternoon test if my messages are getting out there?",
				hops: 5, snr: "4.2", packetID: 195849301,
			},
			{
				time: "14:39", from: "Gleep - socalme.sh", fromNum: 1280985301,
				text: "6 🐰", hops: 2, snr: "5.5", packetID: 1237329592,
				replyID: 195849301,
			},
			{
				time: "14:40", from: "retr0h", mine: true, bang: "/cqr",
				text: "copy you at hop 5, SNR 4.2 dB — you're getting out",
				status: "ack",
			},
			{
				time: "14:40", from: "WiobooJones", fromNum: 3595239870,
				text: "Tyty, I guess people just don't like responding to me lol",
				hops: 6, snr: "5.5",
			},
			// Ghost peer — fromNum populated but not in m.nodes /
			// m.nodesByNum, so displayFrom falls back to msg.from
			// and renderMessageRow adds the 👻 prefix + drained color.
			{
				time: "15:01", from: "node 0x6f66d09d", fromNum: 0x6f66d09d,
				text: "anyone near Pasadena?", hops: 4, snr: "-9.1",
			},
			{
				time: "15:35", from: "AmputiLayag_MeshNodeQTHlab", fromNum: 2058163254,
				text: "2hops from BALDWIN PARK", hops: 3, snr: "3.2",
			},
			{
				time: "15:42", from: "retr0h", mine: true, bang: "/73",
				text: "73 AmputiLayag_MeshNodeQTHlab", status: "ack",
			},
			{time: "15:47", from: "", text: "N7DEF went offline", status: "system"},
		},
	}
}

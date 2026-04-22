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

// DefaultDemo returns the canonical "KC7XYZ Retr0h Base" persona
// that ships with meshx. Used for --demo mode. Kept as a function so
// the caller gets a fresh mutable copy — the model modifies message
// status etc. and we don't want demo launches to share state.
func DefaultDemo() *Demo {
	return &Demo{
		Callsign:       "KC7XYZ",
		LongName:       "Retr0h Base",
		ShortName:      "KC7",
		NodeNum:        0xcafef00d,
		HWModel:        "T-Beam v1.1",
		Firmware:       "2.3.4",
		HasWifi:        true,
		HasBT:          true,
		CurrentChannel: "#primary",
		ModemPreset:    "LONG_FAST",
		Region:         "US",
		TxPowerDBm:     14,
		Role:           "CLIENT",
		BatteryLevel:   87,
		Voltage:        3.94,
		ChannelUtil:    4.2,
		NoiseFloor:     "-92dB",
		Grid:           "CN85ow",
		Latitude:       45.5231,
		Longitude:      -122.6765,

		Channels: []channelItem{
			{name: "#primary", unread: 3},
			{name: "#admin", unread: 0},
			{name: "#emcomm", unread: 0},
			{name: "*secret*", private: true, unread: 1},
		},

		Nodes: []nodeItem{
			{callsign: "KC7XYZ 🦀", state: "online", fav: true, lastHeard: "2m", heardRank: 2,
				lastSNR: "-8.5", lastRSSI: "-92", lastHops: 2, hwModel: "T-Beam v1.1", firmware: "2.3.4"},
			{callsign: "N0CALL", state: "online", lastHeard: "14s", heardRank: 0,
				lastSNR: "-5.0", lastRSSI: "-87", lastHops: 1, hwModel: "Heltec v3", firmware: "2.3.4"},
			{callsign: "W1ABC ⚡", state: "online", lastHeard: "1m", heardRank: 1,
				lastSNR: "-5.0", lastRSSI: "-89", lastHops: 1, hwModel: "RAK4631", firmware: "2.3.4"},
			{callsign: "KE0ABC", state: "failed", lastHeard: "8m", heardRank: 5,
				lastSNR: "-14.2", lastRSSI: "-108", lastHops: 4, hwModel: "T-Beam v1.1", firmware: "2.2.1"},
			{callsign: "Rural Signal 📡", state: "muted", lastHeard: "4m", heardRank: 3,
				lastSNR: "-11.2", lastRSSI: "-103", lastHops: 3, hwModel: "Station-G2", firmware: "2.3.4"},
			{callsign: "W9XYZ 🏔", state: "offline", lastHeard: "2h", heardRank: 99,
				lastSNR: "-16.0", lastRSSI: "-115", lastHops: 5, hwModel: "T-Deck", firmware: "2.1.0"},
		},

		Messages: []messageItem{
			{time: "14:02", from: "KC7XYZ 🦀", text: "hello world", hops: 2, snr: "-8.5"},
			{time: "14:03", from: "me", mine: true, text: "hi", status: "ack", hops: 0},
			{time: "14:05", from: "Rural Signal 📡", bang: "/cq", text: "who's out there?",
				acks: "↳ 3 acks — KC7XYZ -8dB  W1ABC -11dB  N0CALL -14dB", hops: 3, snr: "-11.2"},
			{time: "14:06", from: "me", mine: true, bang: "/cqr", text: "copy 9/9, SNR -8.5, hop 1", status: "ack"},
			{time: "14:07", from: "W1ABC ⚡", text: "thanks for the test", hops: 1, snr: "-5.0"},
			{time: "14:08", from: "me", mine: true, text: "73 👋", status: "fail"},
			{time: "14:09", from: "KC7XYZ 🦀", bang: "/qth", text: "QTH: CN87 Seattle", hops: 2, snr: "-9.1"},
			{time: "14:10", from: "me", mine: true, text: "roger, CN85 Portland here 🌲", status: "ack"},
			{time: "14:12", from: "", text: "N7DEF went offline", status: "system"},
		},
	}
}

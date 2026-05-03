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

package model

// BLEDevice is the slim persisted shape of a Bluetooth-paired
// Meshtastic radio. Populated from the storage layer's LoadBLEDevices
// and consumed by the `meshx ble` subcommand tree (list, fav, forget)
// plus the bare-meshx fallback resolution (autoconnect to favorite
// when no USB radio is plugged in).
type BLEDevice struct {
	// UUID is the BLE peripheral UUID — what the OS Bluetooth stack
	// uses to address the radio. Stable per radio, persists across
	// reboots, doesn't roll across pairing sessions. Primary key.
	UUID string

	// LongName is what the radio's OLED prints — the friendly label
	// users actually recognize (e.g. "T-Beam Mobile"). Captured
	// during pairing, refreshed on re-pair.
	LongName string

	// ShortName is the 4-byte shortname Meshtastic surfaces
	// alongside the longname.
	ShortName string

	// HWModel is the firmware-reported hardware type, used in
	// `meshx ble list` to show what's paired.
	HWModel string

	// Favorite marks exactly one device as the auto-connect target
	// for bare `meshx`. Setting a new favorite atomically clears
	// the flag on every other row — see SetBLEFavorite.
	Favorite bool
}

// DisplayName returns the human-facing label the CLI prints in
// `meshx ble list` and in "connecting to …" messages. Prefers the
// longname (the name printed on the radio's OLED), falls back to
// shortname, then the raw uuid. Always non-empty.
func (d BLEDevice) DisplayName() string {
	switch {
	case d.LongName != "":
		return d.LongName
	case d.ShortName != "":
		return d.ShortName
	default:
		return d.UUID
	}
}

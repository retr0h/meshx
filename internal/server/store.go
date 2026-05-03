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

package server

import mdl "github.com/retr0h/meshx/internal/meshx/model"

// Store is the narrow persistence surface this package consumes,
// declared at the consumer seam per the osapi-io pattern. The
// transport-management endpoints use it for BLE-pairing CRUD; future
// endpoints (channel PSK round-tripping, message search) will
// extend it with the methods they actually need.
//
// Concrete *storage.Sqlite from internal/meshx/storage satisfies
// this structurally — Go's structural typing means we don't
// `implements` anywhere; the compiler verifies.
type Store interface {
	LoadBLEDevices() ([]mdl.BLEDevice, error)
	LookupBLEDevice(needle string) (*mdl.BLEDevice, error)
	SaveBLEDevice(d mdl.BLEDevice) error
	SetBLEFavorite(uuid string) error
	ForgetBLEDevice(uuid string) error
}

// BLESighting is one peripheral observed during a BLE scan.
type BLESighting struct {
	UUID      string `json:"uuid"       doc:"peripheral identifier — pass to /transports/ble/devices to pair"`
	LocalName string `json:"local_name" doc:"name advertised by the radio; empty when unset"`
	RSSI      int16  `json:"rssi"       doc:"signal strength in dBm; closer to zero = stronger"`
}

// BLEScanner is the narrow scan surface — runs a discovery scan for
// the configured timeout and returns peripherals advertising the
// Meshtastic GATT service. Implemented by internal/meshx/transport
// (or a stub for tests). Nil = scanning unavailable (returns 503).
type BLEScanner interface {
	ScanMeshtastic(timeoutMS int) ([]BLESighting, error)
}

// BLEPairer dials a peripheral briefly to trigger OS-level bonding
// (PIN prompt on macOS, agent prompt on Linux), then closes the
// handle. Returning nil means the bond was established.
type BLEPairer interface {
	PairMeshtastic(uuid string) error
}

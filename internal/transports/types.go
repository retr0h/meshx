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

package transports

import mdl "github.com/retr0h/meshx/internal/meshx/model"

// Consumer interfaces — narrow surfaces the package declares for the
// adapters it consumes. Concrete adapters live in cmd/ (CLI deps) or
// the server's wiring layer; both satisfy these structurally. Keeping
// the interfaces here means the package is self-contained — when it
// moves to a separate meshtastic-agent module, no imports need to
// follow it.

// Store is the persistence surface — BLE pairing CRUD.
type Store interface {
	LoadBLEDevices() ([]mdl.BLEDevice, error)
	LookupBLEDevice(needle string) (*mdl.BLEDevice, error)
	SaveBLEDevice(d mdl.BLEDevice) error
	SetBLEFavorite(uuid string) error
	ForgetBLEDevice(uuid string) error
}

// BLEScanner discovers nearby Meshtastic peripherals.
type BLEScanner interface {
	ScanMeshtastic(timeoutMS int) ([]BLESighting, error)
}

// BLEPairer dials a peripheral briefly to trigger OS-level bonding
// (PIN prompt on macOS, agent on Linux), then closes the handle.
// Returning nil means the bond was established.
type BLEPairer interface {
	PairMeshtastic(uuid string) error
}

// USBScanner walks every candidate USB-serial port, sends a non-
// destructive Meshtastic handshake, returns every port's outcome.
type USBScanner interface {
	IdentifyAllSerial(timeoutMS int) ([]USBSighting, error)
}

// Wire types — the public shapes Manager methods return. JSON tags
// shape both the OpenAPI spec the daemon emits AND the CLI's
// tabwriter output (CLI reads the same struct fields without going
// through JSON). Keeping them in this package means consumers don't
// have to invent their own DTOs.

// BLEDeviceView is the slim wire shape for a saved paired device.
// Lighter than mdl.BLEDevice — identity + favorite, no internals.
type BLEDeviceView struct {
	UUID      string `json:"uuid"`
	LongName  string `json:"long_name,omitempty"`
	ShortName string `json:"short_name,omitempty"`
	HWModel   string `json:"hw_model,omitempty"`
	Favorite  bool   `json:"favorite"`
}

// BLESighting is one peripheral observed during a BLE scan.
type BLESighting struct {
	UUID      string `json:"uuid"       doc:"peripheral identifier — pass to /transports/ble/devices to pair"`
	LocalName string `json:"local_name" doc:"name advertised by the radio; empty when unset"`
	RSSI      int16  `json:"rssi"       doc:"signal strength in dBm; closer to zero = stronger"`
}

// USBSighting is one candidate USB-serial port observed during a
// scan, with whether it responded to a Meshtastic handshake and the
// node identity if it did.
type USBSighting struct {
	Port         string `json:"port"                 doc:"serial device path (/dev/cu.usbmodem*, /dev/ttyUSB*)"`
	IsMeshtastic bool   `json:"is_meshtastic"        doc:"true when the port responded to a Meshtastic WantConfigId handshake"`
	NodeNum      uint32 `json:"node_num,omitempty"`
	ShortName    string `json:"short_name,omitempty"`
	LongName     string `json:"long_name,omitempty"`
	HWModel      string `json:"hw_model,omitempty"   doc:"e.g. T-Beam v1.1, HELTEC_V3"`
	Reason       string `json:"reason,omitempty"     doc:"why identification failed; empty when IsMeshtastic"`
}

// viewFromModel projects a persisted mdl.BLEDevice into the wire-
// shape BLEDeviceView used by every consumer.
func viewFromModel(d mdl.BLEDevice) BLEDeviceView {
	return BLEDeviceView{
		UUID:      d.UUID,
		LongName:  d.LongName,
		ShortName: d.ShortName,
		HWModel:   d.HWModel,
		Favorite:  d.Favorite,
	}
}

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

package cmd

import (
	"time"

	"github.com/retr0h/meshx/internal/meshx/storage"
	"github.com/retr0h/meshx/internal/meshx/transport"
	"github.com/retr0h/meshx/internal/transports"
)

// transports_deps.go wires the cmd-side adapters that satisfy the
// narrow consumer interfaces declared in internal/transports. Both
// the daemon (`meshx server start`) and the CLI one-shots (`meshx
// ble *`, `meshx usb *`) construct a *transports.Manager from these
// adapters — that's the single source of truth for hardware ops; no
// other path reaches transport.* or storage.* directly.
//
// The adapters are stateless (no fields) so a zero-value satisfies
// the interface; we declare each as a struct type just to attach
// the methods.

// bleScannerAdapter satisfies transports.BLEScanner by delegating
// to transport.ScanBLE and lifting the result into the transports
// wire shape.
type bleScannerAdapter struct{}

func (bleScannerAdapter) ScanMeshtastic(timeoutMS int) ([]transports.BLESighting, error) {
	hits, err := transport.ScanBLE(time.Duration(timeoutMS) * time.Millisecond)
	if err != nil {
		return nil, err
	}
	out := make([]transports.BLESighting, 0, len(hits))
	for _, h := range hits {
		out = append(out, transports.BLESighting{
			UUID:      h.UUID,
			LocalName: h.LocalName,
			RSSI:      h.RSSI,
		})
	}
	return out, nil
}

// blePairerAdapter satisfies transports.BLEPairer by delegating to
// transport.PairBLE. The brief encrypted GATT connection it dials
// triggers OS-level Bluetooth bonding (PIN prompt on macOS, agent
// prompt on Linux).
type blePairerAdapter struct{}

func (blePairerAdapter) PairMeshtastic(uuid string) error {
	return transport.PairBLE(uuid)
}

// usbScannerAdapter satisfies transports.USBScanner by delegating
// to transport.IdentifyAllSerial and lifting each
// transport.DeviceInfo into the wire shape.
type usbScannerAdapter struct{}

func (usbScannerAdapter) IdentifyAllSerial(timeoutMS int) ([]transports.USBSighting, error) {
	infos, err := transport.IdentifyAllSerial(time.Duration(timeoutMS) * time.Millisecond)
	if err != nil {
		return nil, err
	}
	out := make([]transports.USBSighting, 0, len(infos))
	for _, d := range infos {
		hit := transports.USBSighting{
			Port:         d.Port,
			IsMeshtastic: d.IsMeshtastic,
			NodeNum:      d.NodeNum,
			ShortName:    d.ShortName,
			LongName:     d.LongName,
			HWModel:      d.HWModel,
		}
		if d.Err != nil {
			hit.Reason = d.Err.Error()
		}
		out = append(out, hit)
	}
	return out, nil
}

// newTransportsManager wires a *transports.Manager with whatever
// store handle the caller has on hand. nil store is acceptable —
// scan-only callers (CLI `meshx ble scan` / `meshx usb scan`) skip
// the sqlite open; store-needing methods (List, Pair, Fav, Forget)
// surface 503 at call time.
func newTransportsManager(s *storage.Sqlite) *transports.Manager {
	var store transports.Store
	if s != nil {
		store = s
	}
	return transports.New(transports.Config{
		Store:      store,
		Scanner:    bleScannerAdapter{},
		Pairer:     blePairerAdapter{},
		USBScanner: usbScannerAdapter{},
	})
}

// cliTransports opens a fresh sqlite handle and returns a
// *transports.Manager wired to it. Returns a close func the caller
// must defer — this is the one-shot pattern the CLI uses (open per
// invocation; the daemon constructs its Manager once at boot
// instead).
//
// Errors are surfaced verbatim — the caller wraps with a contextual
// "open store" message.
func cliTransports() (*transports.Manager, func(), error) {
	path, err := storage.DefaultPath()
	if err != nil {
		return nil, func() {}, err
	}
	s, err := storage.New(path)
	if err != nil {
		return nil, func() {}, err
	}
	return newTransportsManager(s), func() { _ = s.Close() }, nil
}

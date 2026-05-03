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

package transport

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

// meshtasticServiceUUID is the BLE GATT service every Meshtastic
// radio advertises. Scan results filter on it so unrelated
// peripherals don't pollute results.
const meshtasticServiceUUID = "6ba1b218-15a8-461f-9fa8-5dcae273eafd"

// BLESighting is one peripheral observed during a Meshtastic-filtered
// BLE scan. Lives in transport because the underlying bluetooth
// adapter is here too — both `meshx ble scan` and the daemon's
// /transports/ble/scan handler call ScanBLE and translate this shape
// into their own wire types.
type BLESighting struct {
	UUID      string
	LocalName string
	RSSI      int16
}

// ScanBLE runs a one-shot BLE discovery for timeout duration and
// returns every peripheral that advertised the Meshtastic service
// UUID. Non-blocking-friendly: stops the adapter when the timeout
// elapses, drains the callback goroutine, then returns the
// deduplicated set.
func ScanBLE(timeout time.Duration) ([]BLESighting, error) {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return nil, fmt.Errorf("enable bluetooth adapter: %w", err)
	}

	wantUUID, err := bluetooth.ParseUUID(meshtasticServiceUUID)
	if err != nil {
		return nil, fmt.Errorf("parse service uuid: %w", err)
	}

	type hit struct {
		address   string
		localName string
		rssi      int16
	}
	var (
		mu   sync.Mutex
		hits = map[string]*hit{}
	)

	scanDone := make(chan error, 1)
	go func() {
		scanDone <- adapter.Scan(func(_ *bluetooth.Adapter, res bluetooth.ScanResult) {
			if !slices.Contains(res.ServiceUUIDs(), wantUUID) {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			addr := res.Address.String()
			h, ok := hits[addr]
			if !ok {
				h = &hit{address: addr}
				hits[addr] = h
			}
			h.localName = res.LocalName()
			h.rssi = res.RSSI
		})
	}()

	select {
	case err := <-scanDone:
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
	case <-time.After(timeout):
		if err := adapter.StopScan(); err != nil {
			return nil, fmt.Errorf("stop scan: %w", err)
		}
		<-scanDone
	}

	mu.Lock()
	defer mu.Unlock()
	out := make([]BLESighting, 0, len(hits))
	for _, h := range hits {
		out = append(out, BLESighting{
			UUID:      h.address,
			LocalName: h.localName,
			RSSI:      h.rssi,
		})
	}
	return out, nil
}

// PairBLE triggers OS-level bonding by opening a brief encrypted GATT
// connection to addr (which fires the system PIN prompt on macOS, the
// BlueZ agent on Linux), then closes the link. The bond is
// established at the OS layer regardless of how cleanly the link
// unwinds, so the close error is wrapped but doesn't undo the pair.
func PairBLE(addr string) error {
	client, err := DialBLE(addr)
	if err != nil {
		return fmt.Errorf("pair: %w", err)
	}
	if cerr := client.Close(); cerr != nil {
		return fmt.Errorf("close after pair: %w", cerr)
	}
	return nil
}

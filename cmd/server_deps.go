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
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"tinygo.org/x/bluetooth"

	"github.com/retr0h/meshx/internal/meshx/storage"
	"github.com/retr0h/meshx/internal/meshx/transport"
	"github.com/retr0h/meshx/internal/server"
)

// meshtasticServiceUUID is the BLE GATT service every Meshtastic
// radio advertises. Scan results filter on it so unrelated
// peripherals don't pollute results.
const meshtasticServiceUUID = "6ba1b218-15a8-461f-9fa8-5dcae273eafd"

// serverDeps wires the optional server dependencies — sqlite store,
// BLE scanner, BLE pairer. Each can fail independently; the server
// returns 503 from endpoints that need a missing dep so callers see
// a real signal instead of silent breakage. Errors get logged but
// don't abort daemon startup.
func serverDeps(
	cmd *cobra.Command,
	log *slog.Logger,
) (server.Store, server.BLEScanner, server.BLEPairer) {
	store := openStorage(cmd, log)
	scanner := bleScanner{}
	pairer := blePairer{}
	return store, scanner, pairer
}

// openStorage opens the shared sqlite handle (~/.meshx/meshx.db),
// running migrations as needed. Returns nil on failure with a
// structured warning — the daemon still serves read-only routes that
// don't need persistence.
func openStorage(_ *cobra.Command, log *slog.Logger) server.Store {
	path, err := storage.DefaultPath()
	if err != nil {
		log.Warn("storage disabled: cannot resolve path", slog.Any("error", err))
		return nil
	}
	s, err := storage.New(path)
	if err != nil {
		log.Warn("storage disabled: open failed",
			slog.String("path", path),
			slog.Any("error", err),
		)
		return nil
	}
	log.Info("storage opened", slog.String("path", path))
	return s
}

// bleScanner satisfies server.BLEScanner — runs a tinygo bluetooth
// scan, filters to peripherals advertising the Meshtastic service,
// stops after the timeout, returns the unique peripherals.
type bleScanner struct{}

func (bleScanner) ScanMeshtastic(timeoutMS int) ([]server.BLESighting, error) {
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
	case <-time.After(time.Duration(timeoutMS) * time.Millisecond):
		if err := adapter.StopScan(); err != nil {
			return nil, fmt.Errorf("stop scan: %w", err)
		}
		<-scanDone
	}

	mu.Lock()
	defer mu.Unlock()
	out := make([]server.BLESighting, 0, len(hits))
	for _, h := range hits {
		out = append(out, server.BLESighting{
			UUID:      h.address,
			LocalName: h.localName,
			RSSI:      h.rssi,
		})
	}
	return out, nil
}

// blePairer satisfies server.BLEPairer — opens a brief encrypted
// GATT connection (which triggers OS-level bonding / PIN prompt),
// then closes it. The follow-up `connect` re-opens against the
// now-bonded peripheral without a fresh prompt.
type blePairer struct{}

func (blePairer) PairMeshtastic(uuid string) error {
	client, err := transport.DialBLE(uuid)
	if err != nil {
		return fmt.Errorf("pair: %w", err)
	}
	if cerr := client.Close(); cerr != nil {
		// Best-effort close — the bond is established at the OS layer
		// regardless of how cleanly the link unwinds.
		return fmt.Errorf("close after pair: %w", cerr)
	}
	return nil
}

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

// Package transports is the hardware-management layer — BLE scan /
// pair / list / forget / favorite, USB scan / auto-detect. It is the
// single source of truth: HTTP handlers, the local CLI, and any
// future MCP tool all call methods on *Manager rather than
// duplicating the validation + dispatch logic across consumers.
//
// The package is intentionally self-contained — it declares its own
// narrow consumer interfaces (Store, BLEScanner, BLEPairer,
// USBScanner) and depends only on internal/meshx/model. No upward
// references to internal/server or internal/tui. The concrete
// adapters (storage.Sqlite, transport.ScanBLE, etc.) live one layer
// down in cmd/ and internal/meshx/* and are injected at construction
// time. When this package eventually moves to a separate
// meshtastic-agent module, it travels alone — nothing in here knows
// about HTTP or the TUI.
//
// All long-running operations (Scan, Pair) accept a context but
// today the underlying tinygo-bluetooth library doesn't honor
// cancellation; the context is plumbed through so the contract
// stays stable when the library catches up.
package transports

import "github.com/danielgtaylor/huma/v2"

// Config bundles every dependency *Manager needs. Each field is
// optional — methods that need a missing dep return 503 (the same
// huma error the HTTP layer already uses). This lets a daemon
// running without sqlite still serve /healthz and /radios cleanly
// while rejecting /transports/* calls; lets a CLI command that only
// needs USB scanning skip wiring BLE deps.
type Config struct {
	// Store persists BLE pairings. Required for list / pair / forget
	// / favorite operations.
	Store Store
	// Scanner discovers nearby BLE peripherals. Required for ScanBLE.
	Scanner BLEScanner
	// Pairer triggers OS-level Bluetooth bonding. Required for
	// PairBLE.
	Pairer BLEPairer
	// USBScanner walks USB-serial ports and identifies Meshtastic
	// responders. Required for USB ops.
	USBScanner USBScanner
}

// Manager is the operation surface. Construct one with New and call
// its methods directly. Concurrent-safe — every method delegates to
// dependencies that are themselves safe (sqlite handle, OS BLE
// stack), and the manager itself holds no mutable state.
type Manager struct {
	store      Store
	scanner    BLEScanner
	pairer     BLEPairer
	usbScanner USBScanner
}

// New returns a Manager wired with whatever deps the caller has on
// hand. Missing deps are tolerated; methods that need them surface
// a 503 at call time. Returns a non-nil *Manager unconditionally.
func New(cfg Config) *Manager {
	return &Manager{
		store:      cfg.Store,
		scanner:    cfg.Scanner,
		pairer:     cfg.Pairer,
		usbScanner: cfg.USBScanner,
	}
}

func (m *Manager) requireStore() (Store, error) {
	if m == nil || m.store == nil {
		return nil, huma.Error503ServiceUnavailable("persistence not wired")
	}
	return m.store, nil
}

func (m *Manager) requireScanner() (BLEScanner, error) {
	if m == nil || m.scanner == nil {
		return nil, huma.Error503ServiceUnavailable("BLE scanner not wired")
	}
	return m.scanner, nil
}

func (m *Manager) requirePairer() (BLEPairer, error) {
	if m == nil || m.pairer == nil {
		return nil, huma.Error503ServiceUnavailable("BLE pairer not wired")
	}
	return m.pairer, nil
}

func (m *Manager) requireUSBScanner() (USBScanner, error) {
	if m == nil || m.usbScanner == nil {
		return nil, huma.Error503ServiceUnavailable("USB scanner not wired")
	}
	return m.usbScanner, nil
}

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

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// defaultBLEScanTimeoutMS is the per-call default when callers pass
// 0 or omit timeout_ms. 10s gives the scanner enough time to see
// every peripheral advertise at least once at the typical 1-2s
// advertisement cadence.
const defaultBLEScanTimeoutMS = 10000

// ErrNoTransport is the sentinel returned by ResolveAutoConnect when
// no dialable BLE transport is available. Callers (CLI / daemon's
// /autoconnect handler) translate the wrapped error message into a
// user-facing message verbatim.
var ErrNoTransport = errors.New("no transport available")

// ListBLEDevices returns every saved BLE pairing as a slim view.
// Empty slice (non-nil) when nothing is paired — so callers can range
// without nil-check.
func (m *Manager) ListBLEDevices(_ context.Context) ([]BLEDeviceView, error) {
	store, err := m.requireStore()
	if err != nil {
		return nil, err
	}
	raw, err := store.LoadBLEDevices()
	if err != nil {
		return nil, fmt.Errorf("load ble devices: %w", err)
	}
	out := make([]BLEDeviceView, 0, len(raw))
	for _, d := range raw {
		out = append(out, viewFromModel(d))
	}
	return out, nil
}

// ScanBLE runs a discovery scan for the configured timeout and
// returns peripherals advertising the Meshtastic GATT service.
// timeoutMS <= 0 falls back to defaultBLEScanTimeoutMS.
func (m *Manager) ScanBLE(_ context.Context, timeoutMS int) ([]BLESighting, error) {
	scanner, err := m.requireScanner()
	if err != nil {
		return nil, err
	}
	if timeoutMS <= 0 {
		timeoutMS = defaultBLEScanTimeoutMS
	}
	hits, err := scanner.ScanMeshtastic(timeoutMS)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	return hits, nil
}

// PairBLE dials the named peripheral to trigger OS-level Bluetooth
// bonding, then persists the device to the pairing table. Returns
// the saved view so callers don't have to re-look-it-up.
//
// Empty uuid returns a 400; pairing failure / save failure surfaces
// as a wrapped error.
func (m *Manager) PairBLE(_ context.Context, uuid string) (BLEDeviceView, error) {
	store, err := m.requireStore()
	if err != nil {
		return BLEDeviceView{}, err
	}
	pairer, err := m.requirePairer()
	if err != nil {
		return BLEDeviceView{}, err
	}
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return BLEDeviceView{}, huma.Error400BadRequest("uuid required")
	}
	if err := pairer.PairMeshtastic(uuid); err != nil {
		return BLEDeviceView{}, fmt.Errorf("pair: %w", err)
	}
	if err := store.SaveBLEDevice(mdl.BLEDevice{UUID: uuid}); err != nil {
		return BLEDeviceView{}, fmt.Errorf("save ble device: %w", err)
	}
	return BLEDeviceView{UUID: uuid}, nil
}

// ForgetBLE removes a saved device. target accepts a UUID, longname,
// or shortname; resolution goes through Store.LookupBLEDevice.
// 404 when nothing matches — propagated via huma.Error404NotFound so
// HTTP callers get the right status without further translation.
func (m *Manager) ForgetBLE(_ context.Context, target string) error {
	store, err := m.requireStore()
	if err != nil {
		return err
	}
	d, err := store.LookupBLEDevice(target)
	if err != nil {
		return fmt.Errorf("lookup ble device: %w", err)
	}
	if d == nil {
		return huma.Error404NotFound("no saved device matches " + target)
	}
	if err := store.ForgetBLEDevice(d.UUID); err != nil {
		return fmt.Errorf("forget ble device: %w", err)
	}
	return nil
}

// SetBLEFavorite marks the named device as the auto-connect favorite.
// target accepts a UUID, longname, or shortname. SetBLEFavorite at
// the store layer is atomic — exactly one row carries the flag after
// the call (the named device); every other row's flag is cleared.
func (m *Manager) SetBLEFavorite(_ context.Context, target string) (BLEDeviceView, error) {
	store, err := m.requireStore()
	if err != nil {
		return BLEDeviceView{}, err
	}
	d, err := store.LookupBLEDevice(target)
	if err != nil {
		return BLEDeviceView{}, fmt.Errorf("lookup ble device: %w", err)
	}
	if d == nil {
		return BLEDeviceView{}, huma.Error404NotFound("no saved device matches " + target)
	}
	if err := store.SetBLEFavorite(d.UUID); err != nil {
		return BLEDeviceView{}, fmt.Errorf("set favorite: %w", err)
	}
	view := viewFromModel(*d)
	view.Favorite = true
	return view, nil
}

// ClearBLEFavorite removes the favorite flag from whichever device
// currently holds it. No-op (still 204) when nothing is favored.
func (m *Manager) ClearBLEFavorite(_ context.Context) error {
	store, err := m.requireStore()
	if err != nil {
		return err
	}
	if err := store.SetBLEFavorite(""); err != nil {
		return fmt.Errorf("clear favorite: %w", err)
	}
	return nil
}

// ResolveBLE looks up a saved BLE device by UUID, longname, or
// shortname and returns the canonical UUID. CLI uses this to convert
// a user-typed name into a "ble:<uuid>" dial string before handing
// off to the connect path.
func (m *Manager) ResolveBLE(_ context.Context, target string) (string, error) {
	store, err := m.requireStore()
	if err != nil {
		return "", err
	}
	d, err := store.LookupBLEDevice(target)
	if err != nil {
		return "", fmt.Errorf("lookup ble device: %w", err)
	}
	if d == nil {
		return "", fmt.Errorf(
			"no saved device matches %q — run `meshx ble list` to see what's paired",
			target,
		)
	}
	return d.UUID, nil
}

// ResolveBLEAutoConnect implements the bare-`meshx` BLE resolution
// chain — used when no radio is specified at startup and the daemon
// / TUI needs to pick one automatically. Returns ("ble:<uuid>",
// reason, nil) on success; wraps ErrNoTransport with detail on
// failure so callers can surface a specific message.
//
// Resolution order:
//  1. Exactly one saved device → use it (reason: single-saved-ble).
//  2. Multiple saved, one favored → use the favorite (reason:
//     ble-favorite).
//  3. Otherwise → ErrNoTransport with the list of candidates so the
//     operator knows what to favorite.
func (m *Manager) ResolveBLEAutoConnect() (target, reason string, err error) {
	store, err := m.requireStore()
	if err != nil {
		return "", "", err
	}
	devs, err := store.LoadBLEDevices()
	if err != nil {
		return "", "", fmt.Errorf("load ble devices: %w", err)
	}
	if len(devs) == 0 {
		return "", "", fmt.Errorf(
			"%w: no saved BLE devices — pair one first",
			ErrNoTransport,
		)
	}
	if len(devs) == 1 {
		return "ble:" + devs[0].UUID, "single-saved-ble", nil
	}
	for _, d := range devs {
		if d.Favorite {
			return "ble:" + d.UUID, "ble-favorite", nil
		}
	}
	names := make([]string, 0, len(devs))
	for _, d := range devs {
		names = append(names, d.DisplayName())
	}
	return "", "", fmt.Errorf(
		"%w: multiple saved BLE devices and no favorite (%s)",
		ErrNoTransport,
		strings.Join(names, ", "),
	)
}

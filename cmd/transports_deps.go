// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/storage"
	"github.com/retr0h/meshx/internal/meshx/transport"
)

// transports_deps.go wires the cmd-side adapters that directly call
// internal/meshx/transport and internal/meshx/storage for BLE and USB
// operations. The server/transports package has been removed; all CLI
// one-shots use cliManager directly.

// BLESighting is one peripheral observed during a BLE scan.
type BLESighting struct {
	UUID      string
	LocalName string
	RSSI      int16
}

// BLEDeviceView is the slim view for a saved paired device.
type BLEDeviceView struct {
	UUID      string
	LongName  string
	ShortName string
	HWModel   string
	Favorite  bool
}

// USBSighting is one candidate USB-serial port observed during a scan.
type USBSighting struct {
	Port         string
	IsMeshtastic bool
	NodeNum      uint32
	ShortName    string
	LongName     string
	HWModel      string
	Reason       string
}

// cliManager is a thin adapter that calls transport.* and storage.*
// directly. It replaces the deleted internal/transports.Manager for
// CLI one-shot use. nil store means scan-only; store-requiring ops
// return an error when store is nil.
type cliManager struct {
	store *storage.Bolt
}

func (m *cliManager) requireStore() (*storage.Bolt, error) {
	if m == nil || m.store == nil {
		return nil, fmt.Errorf("persistence not wired")
	}
	return m.store, nil
}

// ScanBLE runs a discovery scan and returns Meshtastic BLE peripherals.
func (m *cliManager) ScanBLE(_ context.Context, timeoutMS int) ([]BLESighting, error) {
	hits, err := transport.ScanBLE(time.Duration(timeoutMS) * time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	out := make([]BLESighting, 0, len(hits))
	for _, h := range hits {
		out = append(out, BLESighting{
			UUID:      h.UUID,
			LocalName: h.LocalName,
			RSSI:      h.RSSI,
		})
	}
	return out, nil
}

// PairBLE triggers OS-level Bluetooth bonding and persists the device.
func (m *cliManager) PairBLE(_ context.Context, uuid string) (BLEDeviceView, error) {
	store, err := m.requireStore()
	if err != nil {
		return BLEDeviceView{}, err
	}
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return BLEDeviceView{}, fmt.Errorf("uuid required")
	}
	if err := transport.PairBLE(uuid); err != nil {
		return BLEDeviceView{}, fmt.Errorf("pair: %w", err)
	}
	if err := store.SaveBLEDevice(mdl.BLEDevice{UUID: uuid}); err != nil {
		return BLEDeviceView{}, fmt.Errorf("save ble device: %w", err)
	}
	return BLEDeviceView{UUID: uuid}, nil
}

// ListBLEDevices returns every saved BLE pairing as a slim view.
func (m *cliManager) ListBLEDevices(_ context.Context) ([]BLEDeviceView, error) {
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
		out = append(out, BLEDeviceView{
			UUID:      d.UUID,
			LongName:  d.LongName,
			ShortName: d.ShortName,
			HWModel:   d.HWModel,
			Favorite:  d.Favorite,
		})
	}
	return out, nil
}

// ForgetBLE removes a saved device by UUID, longname, or shortname.
func (m *cliManager) ForgetBLE(_ context.Context, target string) error {
	store, err := m.requireStore()
	if err != nil {
		return err
	}
	d, err := store.LookupBLEDevice(target)
	if err != nil {
		return fmt.Errorf("lookup ble device: %w", err)
	}
	if d == nil {
		return fmt.Errorf("no saved device matches %s", target)
	}
	if err := store.ForgetBLEDevice(d.UUID); err != nil {
		return fmt.Errorf("forget ble device: %w", err)
	}
	return nil
}

// SetBLEFavorite marks the named device as the auto-connect favorite.
func (m *cliManager) SetBLEFavorite(_ context.Context, target string) (BLEDeviceView, error) {
	store, err := m.requireStore()
	if err != nil {
		return BLEDeviceView{}, err
	}
	d, err := store.LookupBLEDevice(target)
	if err != nil {
		return BLEDeviceView{}, fmt.Errorf("lookup ble device: %w", err)
	}
	if d == nil {
		return BLEDeviceView{}, fmt.Errorf("no saved device matches %s", target)
	}
	if err := store.SetBLEFavorite(d.UUID); err != nil {
		return BLEDeviceView{}, fmt.Errorf("set favorite: %w", err)
	}
	return BLEDeviceView{
		UUID:      d.UUID,
		LongName:  d.LongName,
		ShortName: d.ShortName,
		HWModel:   d.HWModel,
		Favorite:  true,
	}, nil
}

// ClearBLEFavorite removes the favorite flag from whichever device holds it.
func (m *cliManager) ClearBLEFavorite(_ context.Context) error {
	store, err := m.requireStore()
	if err != nil {
		return err
	}
	if err := store.SetBLEFavorite(""); err != nil {
		return fmt.Errorf("clear favorite: %w", err)
	}
	return nil
}

// ResolveBLE looks up a saved BLE device and returns its canonical UUID.
func (m *cliManager) ResolveBLE(_ context.Context, target string) (string, error) {
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

// ScanUSB walks every candidate USB-serial port and returns each port's outcome.
func (m *cliManager) ScanUSB(_ context.Context, timeoutMS int) ([]USBSighting, error) {
	infos, err := transport.IdentifyAllSerial(time.Duration(timeoutMS) * time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	out := make([]USBSighting, 0, len(infos))
	for _, d := range infos {
		hit := USBSighting{
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

// AutoDetectUSB walks USB-serial ports and returns the port of the single
// Meshtastic radio found. Errors when zero or multiple Meshtastic radios respond.
func (m *cliManager) AutoDetectUSB(ctx context.Context, timeoutMS int) (string, error) {
	hits, err := m.ScanUSB(ctx, timeoutMS)
	if err != nil {
		return "", err
	}
	var meshtastic []USBSighting
	for _, h := range hits {
		if h.IsMeshtastic {
			meshtastic = append(meshtastic, h)
		}
	}
	switch len(meshtastic) {
	case 0:
		if len(hits) == 0 {
			return "", fmt.Errorf(
				"no USB-serial device found — plug in a DATA cable, verify the radio is powered",
			)
		}
		return "", fmt.Errorf(
			"no Meshtastic radio responded on any serial port — try `meshx usb scan` to see candidates",
		)
	case 1:
		return meshtastic[0].Port, nil
	default:
		ports := make([]string, 0, len(meshtastic))
		for _, h := range meshtastic {
			ports = append(ports, h.Port)
		}
		return "", fmt.Errorf(
			"multiple Meshtastic radios found (%s) — pass the device path explicitly",
			strings.Join(ports, ", "),
		)
	}
}

// newTransportsManager wires a *cliManager with whatever store handle
// the caller has on hand. nil store is acceptable — scan-only callers
// skip bolt; store-needing methods surface an error at call time.
func newTransportsManager(s *storage.Bolt) *cliManager {
	return &cliManager{store: s}
}

// cliTransports opens a fresh bolt store and returns a *cliManager
// wired to it. Returns a close func the caller must defer.
func cliTransports() (*cliManager, func(), error) {
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

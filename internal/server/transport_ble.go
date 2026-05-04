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

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// BLEDeviceView is the wire shape for a paired BLE device. Lighter
// than the on-disk mdl.BLEDevice — just identity + favorite.
type BLEDeviceView struct {
	UUID      string `json:"uuid"`
	LongName  string `json:"long_name,omitempty"`
	ShortName string `json:"short_name,omitempty"`
	HWModel   string `json:"hw_model,omitempty"`
	Favorite  bool   `json:"favorite"`
}

// AutoConnectTarget is the wire shape returned by GET /autoconnect —
// the bare-`meshx` resolution chain projected as a transport string
// the caller can dial.
type AutoConnectTarget struct {
	// Target is the dial string: a serial device path, host:port, or
	// "ble:<uuid>". Empty when no transport could be resolved.
	Target string `json:"target"`
	// Reason describes why this target was chosen — "usb-autodetect",
	// "single-saved-ble", "ble-favorite". Empty on error.
	Reason string `json:"reason,omitempty"`
}

// requireStore returns 503 when persistence isn't wired (the daemon
// runs without a sqlite handle). Pulling this through one helper
// keeps each handler small.
func (s *Server) requireStore() (Store, error) {
	if s == nil || s.store == nil {
		return nil, huma.Error503ServiceUnavailable("persistence not wired on this daemon")
	}
	return s.store, nil
}

func (s *Server) requireScanner() (BLEScanner, error) {
	if s == nil || s.scanner == nil {
		return nil, huma.Error503ServiceUnavailable("BLE scanner not wired on this daemon")
	}
	return s.scanner, nil
}

func (s *Server) requirePairer() (BLEPairer, error) {
	if s == nil || s.pairer == nil {
		return nil, huma.Error503ServiceUnavailable("BLE pairer not wired on this daemon")
	}
	return s.pairer, nil
}

type listBLEDevicesInput struct{}

type listBLEDevicesOutput struct {
	Body struct {
		Devices []BLEDeviceView `json:"devices"`
	}
}

func (s *Server) handleListBLEDevices(
	_ context.Context,
	_ *listBLEDevicesInput,
) (*listBLEDevicesOutput, error) {
	store, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	raw, err := store.LoadBLEDevices()
	if err != nil {
		return nil, fmt.Errorf("load ble devices: %w", err)
	}
	out := &listBLEDevicesOutput{}
	out.Body.Devices = make([]BLEDeviceView, 0, len(raw))
	for _, d := range raw {
		out.Body.Devices = append(out.Body.Devices, BLEDeviceView{
			UUID:      d.UUID,
			LongName:  d.LongName,
			ShortName: d.ShortName,
			HWModel:   d.HWModel,
			Favorite:  d.Favorite,
		})
	}
	return out, nil
}

type pairBLEInput struct {
	Body struct {
		UUID string `json:"uuid" minLength:"1" doc:"peripheral identifier from a /transports/ble/scan result"`
	}
}

type pairBLEOutput struct {
	Body BLEDeviceView
}

func (s *Server) handlePairBLE(_ context.Context, in *pairBLEInput) (*pairBLEOutput, error) {
	store, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	pairer, err := s.requirePairer()
	if err != nil {
		return nil, err
	}
	uuid := strings.TrimSpace(in.Body.UUID)
	if uuid == "" {
		return nil, huma.Error400BadRequest("uuid required")
	}
	if err := pairer.PairMeshtastic(uuid); err != nil {
		return nil, fmt.Errorf("pair: %w", err)
	}
	if err := store.SaveBLEDevice(mdl.BLEDevice{UUID: uuid}); err != nil {
		return nil, fmt.Errorf("save ble device: %w", err)
	}
	out := &pairBLEOutput{}
	out.Body = BLEDeviceView{UUID: uuid}
	return out, nil
}

type forgetBLEInput struct {
	UUID string `path:"uuid" doc:"peripheral identifier or saved long/short name"`
}

type forgetBLEOutput struct{}

func (s *Server) handleForgetBLE(
	_ context.Context,
	in *forgetBLEInput,
) (*forgetBLEOutput, error) {
	store, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	d, err := store.LookupBLEDevice(in.UUID)
	if err != nil {
		return nil, fmt.Errorf("lookup ble device: %w", err)
	}
	if d == nil {
		return nil, huma.Error404NotFound("no saved device matches " + in.UUID)
	}
	if err := store.ForgetBLEDevice(d.UUID); err != nil {
		return nil, fmt.Errorf("forget ble device: %w", err)
	}
	return &forgetBLEOutput{}, nil
}

type setBLEFavoriteInput struct {
	UUID string `path:"uuid" doc:"peripheral identifier or saved long/short name"`
}

type setBLEFavoriteOutput struct {
	Body BLEDeviceView
}

func (s *Server) handleSetBLEFavorite(
	_ context.Context,
	in *setBLEFavoriteInput,
) (*setBLEFavoriteOutput, error) {
	store, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	d, err := store.LookupBLEDevice(in.UUID)
	if err != nil {
		return nil, fmt.Errorf("lookup ble device: %w", err)
	}
	if d == nil {
		return nil, huma.Error404NotFound("no saved device matches " + in.UUID)
	}
	if err := store.SetBLEFavorite(d.UUID); err != nil {
		return nil, fmt.Errorf("set favorite: %w", err)
	}
	out := &setBLEFavoriteOutput{}
	out.Body = BLEDeviceView{
		UUID:      d.UUID,
		LongName:  d.LongName,
		ShortName: d.ShortName,
		HWModel:   d.HWModel,
		Favorite:  true,
	}
	return out, nil
}

type clearBLEFavoriteInput struct{}

type clearBLEFavoriteOutput struct{}

func (s *Server) handleClearBLEFavorite(
	_ context.Context,
	_ *clearBLEFavoriteInput,
) (*clearBLEFavoriteOutput, error) {
	store, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	if err := store.SetBLEFavorite(""); err != nil {
		return nil, fmt.Errorf("clear favorite: %w", err)
	}
	return &clearBLEFavoriteOutput{}, nil
}

type scanBLEInput struct {
	Body struct {
		TimeoutMS int `json:"timeout_ms,omitempty" doc:"scan duration in milliseconds; default 10000"`
	}
}

type scanBLEOutput struct {
	Body struct {
		Devices []BLESighting `json:"devices"`
	}
}

func (s *Server) handleScanBLE(_ context.Context, in *scanBLEInput) (*scanBLEOutput, error) {
	scanner, err := s.requireScanner()
	if err != nil {
		return nil, err
	}
	timeout := in.Body.TimeoutMS
	if timeout <= 0 {
		timeout = 10000
	}
	hits, err := scanner.ScanMeshtastic(timeout)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	out := &scanBLEOutput{}
	out.Body.Devices = hits
	return out, nil
}

// ListBLEDevices is the in-process counterpart of GET
// /transports/ble/devices — same body, no HTTP plumbing. The CLI
// wrappers in cmd/ use these methods so the local user goes through
// the same code path remote clients hit.
func (s *Server) ListBLEDevices(_ context.Context) ([]BLEDeviceView, error) {
	store, err := s.requireStore()
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

// ScanBLE is the in-process counterpart of POST /transports/ble/scan.
func (s *Server) ScanBLE(_ context.Context, timeoutMS int) ([]BLESighting, error) {
	scanner, err := s.requireScanner()
	if err != nil {
		return nil, err
	}
	if timeoutMS <= 0 {
		timeoutMS = 10000
	}
	return scanner.ScanMeshtastic(timeoutMS)
}

// PairBLE is the in-process counterpart of POST /transports/ble/devices.
func (s *Server) PairBLE(_ context.Context, uuid string) error {
	store, err := s.requireStore()
	if err != nil {
		return err
	}
	pairer, err := s.requirePairer()
	if err != nil {
		return err
	}
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return fmt.Errorf("uuid required")
	}
	if err := pairer.PairMeshtastic(uuid); err != nil {
		return fmt.Errorf("pair: %w", err)
	}
	return store.SaveBLEDevice(mdl.BLEDevice{UUID: uuid})
}

// ForgetBLEDevice is the in-process counterpart of DELETE
// /transports/ble/devices/{uuid}.
func (s *Server) ForgetBLEDevice(_ context.Context, target string) error {
	store, err := s.requireStore()
	if err != nil {
		return err
	}
	d, err := store.LookupBLEDevice(target)
	if err != nil {
		return fmt.Errorf("lookup ble device: %w", err)
	}
	if d == nil {
		return fmt.Errorf("no saved device matches %q (run `meshx ble list`)", target)
	}
	return store.ForgetBLEDevice(d.UUID)
}

// SetBLEFavoriteByName is the in-process counterpart of PUT
// /transports/ble/devices/{uuid}/favorite — accepts uuid OR name.
func (s *Server) SetBLEFavoriteByName(_ context.Context, target string) (BLEDeviceView, error) {
	store, err := s.requireStore()
	if err != nil {
		return BLEDeviceView{}, err
	}
	d, err := store.LookupBLEDevice(target)
	if err != nil {
		return BLEDeviceView{}, fmt.Errorf("lookup ble device: %w", err)
	}
	if d == nil {
		return BLEDeviceView{}, fmt.Errorf(
			"no saved device matches %q (run `meshx ble list`)",
			target,
		)
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

// ClearBLEFavorite is the in-process counterpart of DELETE
// /transports/ble/favorite.
func (s *Server) ClearBLEFavorite(_ context.Context) error {
	store, err := s.requireStore()
	if err != nil {
		return err
	}
	return store.SetBLEFavorite("")
}

// ResolveBLE looks up a saved BLE device by uuid or name and returns
// its uuid. CLI uses this before handing off to the TUI's RunRadio
// (the dial string is "ble:<uuid>").
func (s *Server) ResolveBLE(_ context.Context, target string) (string, error) {
	store, err := s.requireStore()
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

// ErrNoTransport is the sentinel returned by ResolveAutoConnect when
// no dialable transport is available. Callers (cmd/root,
// /autoconnect handler) translate it into a user-facing message.
var ErrNoTransport = errors.New("no transport available")

// ResolveBLEAutoConnect implements the bare-`meshx` resolution chain
// used by the /autoconnect endpoint. The USB autodetect step is the
// caller's responsibility (cmd/root passes its own dial string in
// when USB succeeds); this function focuses on the BLE fallback. It
// returns ("ble:<uuid>", reason, nil) on success or ErrNoTransport
// wrapped with detail on failure.
func (s *Server) ResolveBLEAutoConnect() (target, reason string, err error) {
	store, err := s.requireStore()
	if err != nil {
		return "", "", err
	}
	devs, err := store.LoadBLEDevices()
	if err != nil {
		return "", "", fmt.Errorf("load ble devices: %w", err)
	}
	if len(devs) == 0 {
		return "", "", fmt.Errorf(
			"%w: no saved BLE devices — pair one with /transports/ble/devices",
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

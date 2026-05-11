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
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/transports"
)

// In-memory fakes for the four transports.* consumer interfaces.
// Each captures the calls it received so tests can assert on side
// effects (e.g., "pair-ble saved the UUID to the store") without
// touching real hardware. Concurrent-safe so harnesses running under
// t.Parallel can share them.

// fakeStore implements transports.Store with a slice of BLEDevices.
// Methods record their args; LoadBLEDevices returns a copy so callers
// can't mutate the in-memory slice.
type fakeStore struct {
	mu      sync.Mutex
	devices []mdl.BLEDevice
	loaded  int    // # times LoadBLEDevices was called
	saved   int    // # times SaveBLEDevice was called
	forgot  string // last UUID passed to ForgetBLEDevice
	favSet  string // last UUID passed to SetBLEFavorite ("" means clear)
	loadErr error  // when set, LoadBLEDevices returns this error
}

func newFakeStore(seed ...mdl.BLEDevice) *fakeStore {
	return &fakeStore{devices: append([]mdl.BLEDevice{}, seed...)}
}

func (s *fakeStore) LoadBLEDevices() ([]mdl.BLEDevice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaded++
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	out := make([]mdl.BLEDevice, len(s.devices))
	copy(out, s.devices)
	return out, nil
}

// LookupBLEDevice matches the storage layer's contract: needle can be
// a UUID, LongName, or ShortName; case-insensitive.
func (s *fakeStore) LookupBLEDevice(needle string) (*mdl.BLEDevice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := strings.ToLower(strings.TrimSpace(needle))
	if n == "" {
		return nil, nil
	}
	for i := range s.devices {
		d := s.devices[i]
		if strings.EqualFold(d.UUID, n) ||
			strings.EqualFold(d.LongName, n) ||
			strings.EqualFold(d.ShortName, n) {
			return &d, nil
		}
	}
	return nil, nil
}

func (s *fakeStore) SaveBLEDevice(d mdl.BLEDevice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved++
	for i := range s.devices {
		if s.devices[i].UUID == d.UUID {
			s.devices[i] = d
			return nil
		}
	}
	s.devices = append(s.devices, d)
	return nil
}

// SetBLEFavorite mirrors the storage layer's atomic "exactly-one-favorite"
// semantics: passing "" clears every flag; a non-empty UUID sets that
// device's flag and clears every other.
func (s *fakeStore) SetBLEFavorite(uuid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.favSet = uuid
	for i := range s.devices {
		s.devices[i].Favorite = (uuid != "" && s.devices[i].UUID == uuid)
	}
	return nil
}

func (s *fakeStore) ForgetBLEDevice(uuid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forgot = uuid
	out := s.devices[:0]
	for _, d := range s.devices {
		if d.UUID != uuid {
			out = append(out, d)
		}
	}
	s.devices = out
	return nil
}

// fakeBLEScanner implements transports.BLEScanner with a canned hit
// list + scriptable error.
type fakeBLEScanner struct {
	mu      sync.Mutex
	hits    []transports.BLESighting
	err     error
	calls   int
	lastTMO int // last timeout_ms passed to ScanMeshtastic
}

func newFakeBLEScanner(hits ...transports.BLESighting) *fakeBLEScanner {
	return &fakeBLEScanner{hits: append([]transports.BLESighting{}, hits...)}
}

func (b *fakeBLEScanner) ScanMeshtastic(timeoutMS int) ([]transports.BLESighting, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	b.lastTMO = timeoutMS
	if b.err != nil {
		return nil, b.err
	}
	out := make([]transports.BLESighting, len(b.hits))
	copy(out, b.hits)
	return out, nil
}

// fakeBLEPairer implements transports.BLEPairer.
type fakeBLEPairer struct {
	mu       sync.Mutex
	err      error
	calls    int
	lastUUID string
}

func newFakeBLEPairer() *fakeBLEPairer {
	return &fakeBLEPairer{}
}

func (b *fakeBLEPairer) PairMeshtastic(uuid string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	b.lastUUID = uuid
	return b.err
}

// fakeUSBScanner implements transports.USBScanner.
type fakeUSBScanner struct {
	mu      sync.Mutex
	hits    []transports.USBSighting
	err     error
	calls   int
	lastTMO int
}

func newFakeUSBScanner(hits ...transports.USBSighting) *fakeUSBScanner {
	return &fakeUSBScanner{hits: append([]transports.USBSighting{}, hits...)}
}

func (u *fakeUSBScanner) IdentifyAllSerial(timeoutMS int) ([]transports.USBSighting, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls++
	u.lastTMO = timeoutMS
	if u.err != nil {
		return nil, u.err
	}
	out := make([]transports.USBSighting, len(u.hits))
	copy(out, u.hits)
	return out, nil
}

// transportHarnessOpts lets a test wire only the consumer surfaces it
// needs. Nil fields stay unwired so the matching require* helper
// returns 503 — exactly what the production daemon does when storage
// or hardware isn't available.
type transportHarnessOpts struct {
	store      transports.Store
	scanner    transports.BLEScanner
	pairer     transports.BLEPairer
	usbScanner transports.USBScanner
}

func newTransportHarness(t *testing.T, opts transportHarnessOpts) *httptest.Server {
	t.Helper()
	mgr := transports.New(transports.Config{
		Store:      opts.store,
		Scanner:    opts.scanner,
		Pairer:     opts.pairer,
		USBScanner: opts.usbScanner,
	})
	s := New(Config{
		Radios:     NewRegistry(),
		Transports: mgr,
	})
	srv := httptest.NewServer(s.http.Handler)
	t.Cleanup(srv.Close)
	return srv
}

// errSentinel lets tests assert that a fake-injected error reaches
// the handler boundary (handlers wrap with fmt.Errorf so we check
// errors.Is on the response body's substring instead of pointer
// identity).
var errSentinel = errors.New("sentinel")

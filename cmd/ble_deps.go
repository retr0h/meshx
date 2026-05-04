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

	"github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/storage"
	"github.com/retr0h/meshx/internal/meshx/transport"
)

// Narrow consumer interfaces for the BLE one-shot subcommands. Each
// declares only the method shape the cmd actually uses, per the
// osapi-io pattern — concrete adapters live below and tests can
// swap the package-level vars.

type bleScanner interface {
	Scan(timeoutMS int) ([]transport.BLESighting, error)
}

type blePairer interface {
	Pair(uuid string) error
}

type bleStore interface {
	SaveBLEDevice(d model.BLEDevice) error
	LoadBLEDevices() ([]model.BLEDevice, error)
	LookupBLEDevice(needle string) (*model.BLEDevice, error)
	SetBLEFavorite(uuid string) error
	ForgetBLEDevice(uuid string) error
	Close() error
}

// transportBLEScanner satisfies bleScanner by delegating to the
// transport package's tinygo-bluetooth scan.
type transportBLEScanner struct{}

func (transportBLEScanner) Scan(timeoutMS int) ([]transport.BLESighting, error) {
	return transport.ScanBLE(time.Duration(timeoutMS) * time.Millisecond)
}

// transportBLEPairer satisfies blePairer by delegating to
// transport.PairBLE (brief encrypted GATT connection that triggers
// OS-level bonding).
type transportBLEPairer struct{}

func (transportBLEPairer) Pair(uuid string) error { return transport.PairBLE(uuid) }

// Package-level wiring. Tests can substitute these to fake out the
// host before invoking RunE.
var (
	cliBLEScanner bleScanner = transportBLEScanner{}
	cliBLEPairer  blePairer  = transportBLEPairer{}

	// cliOpenBLEStore returns a fresh sqlite handle typed as the
	// narrow bleStore consumer interface. Caller is responsible for
	// Close().
	cliOpenBLEStore = func() (bleStore, error) {
		path, err := storage.DefaultPath()
		if err != nil {
			return nil, err
		}
		return storage.New(path)
	}
)

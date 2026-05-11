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
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/meshx/storage"
	"github.com/retr0h/meshx/internal/meshx/transport"
	"github.com/retr0h/meshx/internal/transports"
)

// newTransportsManager wires the daemon-side *transports.Manager —
// the single hardware-management surface shared by the HTTP daemon
// (server.Config.Transports) and (in a follow-up) the local `meshx
// ble *` / `meshx usb *` CLI subcommands. Each adapter delegates to
// internal/meshx/transport.* / internal/meshx/storage.*; missing
// deps (e.g. sqlite open failed) flow through as nil and become 503
// at request time.
func newTransportsManager(s *storage.Sqlite) *transports.Manager {
	var store transports.Store
	if s != nil {
		store = s
	}
	return transports.New(transports.Config{
		Store:      store,
		Scanner:    daemonBLEScanner{},
		Pairer:     daemonBLEPairer{},
		USBScanner: daemonUSBScanner{},
	})
}

// openStore opens the shared sqlite handle (~/.meshx/meshx.db),
// running migrations as needed. Returns nil on failure with a
// structured warning — read-only HTTP routes still serve.
func openStore(_ *cobra.Command, log *slog.Logger) *storage.Sqlite {
	path, err := storage.DefaultPath()
	if err != nil {
		log.Warn("storage disabled: cannot resolve path", slog.Any("error", err))
		return nil
	}
	s, err := storage.New(path)
	if err != nil {
		log.Warn(
			"storage disabled: open failed",
			slog.String("path", path),
			slog.Any("error", err),
		)
		return nil
	}
	log.Info("storage opened", slog.String("path", path))
	return s
}

// daemonBLEScanner satisfies transports.BLEScanner by delegating to
// transport.ScanBLE and lifting the result into the transports
// package's wire shape.
type daemonBLEScanner struct{}

func (daemonBLEScanner) ScanMeshtastic(timeoutMS int) ([]transports.BLESighting, error) {
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

// daemonBLEPairer satisfies transports.BLEPairer by delegating to
// transport.PairBLE.
type daemonBLEPairer struct{}

func (daemonBLEPairer) PairMeshtastic(uuid string) error {
	return transport.PairBLE(uuid)
}

// daemonUSBScanner satisfies transports.USBScanner by delegating to
// transport.IdentifyAllSerial and lifting each transport.DeviceInfo
// into the wire shape (Err → Reason as a string).
type daemonUSBScanner struct{}

func (daemonUSBScanner) IdentifyAllSerial(timeoutMS int) ([]transports.USBSighting, error) {
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

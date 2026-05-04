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
	"github.com/retr0h/meshx/internal/server"
)

// serverDeps wires the optional dependencies the daemon's HTTP
// surface (transports/ble/*, transports/usb/*) needs. Each can fail
// independently; the server returns 503 from endpoints that need a
// missing dep so callers see a real signal instead of silent
// breakage. Errors get logged but don't abort daemon startup.
//
// Note: the local CLI (cmd/ble.go, cmd/usb.go one-shots) does NOT go
// through this — those subcommands call the transport + storage
// packages directly through their own narrow consumer interfaces.
// This function exists only for `meshx server start`.
// serverDepsWithStore lets the caller
// pre-open the concrete *storage.Sqlite when it needs both the
// narrow server.Store surface AND the wider driver.Store surface
// (the daemon's pump path needs ClaimRadioIdentity / SaveMessage,
// which aren't part of server.Store). server_start.go uses this so
// it doesn't open the SQLite handle twice.
func serverDepsWithStore(
	s *storage.Sqlite,
) (server.Store, server.BLEScanner, server.BLEPairer, server.USBScanner) {
	var store server.Store
	if s != nil {
		store = s
	}
	return store, daemonBLEScanner{}, daemonBLEPairer{}, daemonUSBScanner{}
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

// daemonBLEScanner satisfies server.BLEScanner by delegating to
// transport.ScanBLE and lifting the result into the server's wire
// shape.
type daemonBLEScanner struct{}

func (daemonBLEScanner) ScanMeshtastic(timeoutMS int) ([]server.BLESighting, error) {
	hits, err := transport.ScanBLE(time.Duration(timeoutMS) * time.Millisecond)
	if err != nil {
		return nil, err
	}
	out := make([]server.BLESighting, 0, len(hits))
	for _, h := range hits {
		out = append(out, server.BLESighting{
			UUID:      h.UUID,
			LocalName: h.LocalName,
			RSSI:      h.RSSI,
		})
	}
	return out, nil
}

// daemonBLEPairer satisfies server.BLEPairer by delegating to
// transport.PairBLE.
type daemonBLEPairer struct{}

func (daemonBLEPairer) PairMeshtastic(uuid string) error {
	return transport.PairBLE(uuid)
}

// daemonUSBScanner satisfies server.USBScanner by delegating to
// transport.IdentifyAllSerial and lifting each transport.DeviceInfo
// into the server's wire shape (Err → Reason as a string).
type daemonUSBScanner struct{}

func (daemonUSBScanner) IdentifyAllSerial(timeoutMS int) ([]server.USBSighting, error) {
	infos, err := transport.IdentifyAllSerial(time.Duration(timeoutMS) * time.Millisecond)
	if err != nil {
		return nil, err
	}
	out := make([]server.USBSighting, 0, len(infos))
	for _, d := range infos {
		hit := server.USBSighting{
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

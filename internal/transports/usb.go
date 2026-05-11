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
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// defaultUSBScanTimeoutMS is the per-port identification timeout
// when callers pass 0. 1500ms is enough for the Meshtastic
// WantConfigId handshake to round-trip on a connected radio without
// dragging the total scan time too high when many candidate ports
// don't respond.
const defaultUSBScanTimeoutMS = 1500

// ScanUSB walks every candidate USB-serial port and returns each
// port's outcome — Meshtastic-or-not plus identity when yes.
// timeoutMS is per-port. Zero falls back to defaultUSBScanTimeoutMS.
func (m *Manager) ScanUSB(_ context.Context, timeoutMS int) ([]USBSighting, error) {
	scanner, err := m.requireUSBScanner()
	if err != nil {
		return nil, err
	}
	if timeoutMS <= 0 {
		timeoutMS = defaultUSBScanTimeoutMS
	}
	hits, err := scanner.IdentifyAllSerial(timeoutMS)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	return hits, nil
}

// AutoDetectUSB walks every candidate USB-serial port and projects
// the result down to a single port path: success when exactly one
// Meshtastic radio responded. Returns huma-typed errors so HTTP
// callers get the right status without translation:
//
//   - 404 when no ports were found at all (one message)
//   - 404 when ports were found but none Meshtastic (different
//     message — tells the operator to check the firmware)
//   - 409 when more than one Meshtastic radio responded (caller
//     must disambiguate, e.g. via --device)
func (m *Manager) AutoDetectUSB(ctx context.Context, timeoutMS int) (string, error) {
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
			return "", huma.Error404NotFound(
				"no USB-serial device found — plug in a DATA cable, verify the radio is powered",
			)
		}
		return "", huma.Error404NotFound(
			"no Meshtastic radio responded on any serial port — try `meshx usb scan` to see candidates",
		)
	case 1:
		return meshtastic[0].Port, nil
	default:
		ports := make([]string, 0, len(meshtastic))
		for _, h := range meshtastic {
			ports = append(ports, h.Port)
		}
		return "", huma.Error409Conflict(
			"multiple Meshtastic radios found (" + strings.Join(ports, ", ") +
				") — pass --device <path> to pick one",
		)
	}
}

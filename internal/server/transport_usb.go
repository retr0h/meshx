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
	"fmt"

	"github.com/danielgtaylor/huma/v2"
)

// requireUSBScanner returns 503 when the USB scanner adapter isn't
// wired (the daemon serves without serial-port access). Mirrors
// requireScanner / requirePairer for BLE.
func (s *Server) requireUSBScanner() (USBScanner, error) {
	if s == nil || s.usbScanner == nil {
		return nil, huma.Error503ServiceUnavailable("USB scanner not wired on this daemon")
	}
	return s.usbScanner, nil
}

type scanUSBInput struct {
	Body struct {
		TimeoutMS int `json:"timeout_ms,omitempty" doc:"per-port identify timeout in milliseconds; default 1500"`
	}
}

type scanUSBOutput struct {
	Body struct {
		Devices []USBSighting `json:"devices"`
	}
}

func (s *Server) handleScanUSB(_ context.Context, in *scanUSBInput) (*scanUSBOutput, error) {
	scanner, err := s.requireUSBScanner()
	if err != nil {
		return nil, err
	}
	timeout := in.Body.TimeoutMS
	if timeout <= 0 {
		timeout = 1500
	}
	hits, err := scanner.IdentifyAllSerial(timeout)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	out := &scanUSBOutput{}
	out.Body.Devices = hits
	return out, nil
}

type autoUSBInput struct {
	Body struct {
		TimeoutMS int `json:"timeout_ms,omitempty" doc:"per-port identify timeout in milliseconds; default 1500"`
	}
}

type autoUSBOutput struct {
	Body struct {
		Port string `json:"port" doc:"serial device path of the single Meshtastic radio found"`
	}
}

func (s *Server) handleAutoDetectUSB(
	ctx context.Context,
	in *autoUSBInput,
) (*autoUSBOutput, error) {
	port, err := s.AutoDetectUSB(ctx, in.Body.TimeoutMS)
	if err != nil {
		return nil, err
	}
	out := &autoUSBOutput{}
	out.Body.Port = port
	return out, nil
}

// ScanUSB is the in-process counterpart of POST /transports/usb/scan.
// CLI wrappers in cmd/ use this directly so the local user goes
// through the same code path remote clients would hit.
func (s *Server) ScanUSB(_ context.Context, timeoutMS int) ([]USBSighting, error) {
	scanner, err := s.requireUSBScanner()
	if err != nil {
		return nil, err
	}
	if timeoutMS <= 0 {
		timeoutMS = 1500
	}
	return scanner.IdentifyAllSerial(timeoutMS)
}

// AutoDetectUSB is the in-process counterpart of POST
// /transports/usb/auto. Returns a single Meshtastic port path on
// success; errors with a user-readable message when zero or more
// than one is found (callers surface the message verbatim).
func (s *Server) AutoDetectUSB(_ context.Context, timeoutMS int) (string, error) {
	scanner, err := s.requireUSBScanner()
	if err != nil {
		return "", err
	}
	if timeoutMS <= 0 {
		timeoutMS = 1500
	}
	hits, err := scanner.IdentifyAllSerial(timeoutMS)
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
		ports := ""
		for i, h := range meshtastic {
			if i > 0 {
				ports += ", "
			}
			ports += h.Port
		}
		return "", huma.Error409Conflict(
			"multiple Meshtastic radios found (" + ports + ") — pass --device <path> to pick one",
		)
	}
}

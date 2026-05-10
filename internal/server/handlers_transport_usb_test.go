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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestEndpointScanUSB — POST /transports/usb/scan. Forwards the
// requested timeout (defaulting to 1500 when zero/missing) to the
// USBScanner and returns every candidate port's identification
// outcome — including ports that didn't respond to a Meshtastic
// handshake (so callers can distinguish "no Meshtastic radio" from
// "no serial ports at all").
func TestEndpointScanUSB(t *testing.T) {
	t.Parallel()

	hits := []USBSighting{
		{Port: "/dev/cu.usbmodem-1", IsMeshtastic: true, NodeNum: 0xc0ffee, ShortName: "BEAM"},
		{Port: "/dev/cu.usbmodem-2", IsMeshtastic: false, Reason: "no Meshtastic response"},
	}

	cases := []struct {
		name       string
		scanner    USBScanner
		body       string
		wantStatus int
		wantCount  int
		wantTMO    int
		checkCalls bool
	}{
		{
			name:       "explicit-timeout-passes-through-and-returns-mixed-results",
			scanner:    newFakeUSBScanner(hits...),
			body:       `{"timeout_ms":2500}`,
			wantStatus: http.StatusOK,
			wantCount:  2,
			wantTMO:    2500,
			checkCalls: true,
		},
		{
			name:       "zero-timeout-defaults-to-1500",
			scanner:    newFakeUSBScanner(hits...),
			body:       `{"timeout_ms":0}`,
			wantStatus: http.StatusOK,
			wantCount:  2,
			wantTMO:    1500,
			checkCalls: true,
		},
		{
			name:       "missing-timeout-defaults-to-1500",
			scanner:    newFakeUSBScanner(hits...),
			body:       `{}`,
			wantStatus: http.StatusOK,
			wantCount:  2,
			wantTMO:    1500,
			checkCalls: true,
		},
		{
			name:       "no-ports-returns-empty-devices-array",
			scanner:    newFakeUSBScanner(),
			body:       `{}`,
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name: "scanner-error-returns-500",
			scanner: func() USBScanner {
				s := newFakeUSBScanner()
				s.err = errSentinel
				return s
			}(),
			body:       `{}`,
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "scanner-not-wired-returns-503",
			scanner:    nil,
			body:       `{}`,
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTransportHarness(t, transportHarnessOpts{usbScanner: tc.scanner})

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPost,
				srv.URL+"/transports/usb/scan",
				bytes.NewReader([]byte(tc.body)),
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("content-type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var body struct {
				Devices []USBSighting `json:"devices"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(body.Devices) != tc.wantCount {
				t.Fatalf("len(devices) = %d, want %d", len(body.Devices), tc.wantCount)
			}
			if tc.checkCalls {
				fs := tc.scanner.(*fakeUSBScanner)
				if fs.calls != 1 {
					t.Fatalf("scanner.calls = %d, want 1", fs.calls)
				}
				if fs.lastTMO != tc.wantTMO {
					t.Fatalf("scanner.lastTMO = %d, want %d", fs.lastTMO, tc.wantTMO)
				}
			}
		})
	}
}

// TestEndpointAutoDetectUSB — POST /transports/usb/auto. Walks every
// candidate port via the USBScanner, then projects the result down to
// a single port path: 200 with port when exactly one Meshtastic radio
// responded, 404 when zero (with a different message for "no ports
// at all" vs "ports but none Meshtastic"), 409 when more than one
// (so the caller knows to disambiguate via --device).
func TestEndpointAutoDetectUSB(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		scanner     USBScanner
		body        string
		wantStatus  int
		wantPort    string
		wantInError string // substring expected in error.detail
	}{
		{
			name: "single-meshtastic-port-returns-200-with-path",
			scanner: newFakeUSBScanner(
				USBSighting{Port: "/dev/cu.usbmodem-1", IsMeshtastic: true, NodeNum: 0xc0ffee},
				USBSighting{Port: "/dev/cu.usbmodem-2", IsMeshtastic: false},
			),
			body:       `{}`,
			wantStatus: http.StatusOK,
			wantPort:   "/dev/cu.usbmodem-1",
		},
		{
			name:        "zero-ports-found-returns-404-with-no-device-message",
			scanner:     newFakeUSBScanner(),
			body:        `{}`,
			wantStatus:  http.StatusNotFound,
			wantInError: "no USB-serial device found",
		},
		{
			name: "ports-but-none-meshtastic-returns-404-with-different-message",
			scanner: newFakeUSBScanner(
				USBSighting{Port: "/dev/cu.usbmodem-9", IsMeshtastic: false, Reason: "timeout"},
			),
			body:        `{}`,
			wantStatus:  http.StatusNotFound,
			wantInError: "no Meshtastic radio responded",
		},
		{
			name: "multiple-meshtastic-ports-returns-409-with-port-list",
			scanner: newFakeUSBScanner(
				USBSighting{Port: "/dev/cu.usbmodem-1", IsMeshtastic: true},
				USBSighting{Port: "/dev/cu.usbmodem-2", IsMeshtastic: true},
			),
			body:        `{}`,
			wantStatus:  http.StatusConflict,
			wantInError: "multiple Meshtastic radios",
		},
		{
			name: "scanner-error-returns-500",
			scanner: func() USBScanner {
				s := newFakeUSBScanner()
				s.err = errSentinel
				return s
			}(),
			body:       `{}`,
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "scanner-not-wired-returns-503",
			scanner:    nil,
			body:       `{}`,
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTransportHarness(t, transportHarnessOpts{usbScanner: tc.scanner})

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPost,
				srv.URL+"/transports/usb/auto",
				bytes.NewReader([]byte(tc.body)),
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("content-type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusOK {
				var body struct {
					Port string `json:"port"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if body.Port != tc.wantPort {
					t.Fatalf("port = %q, want %q", body.Port, tc.wantPort)
				}
				return
			}
			if tc.wantInError != "" {
				var errBody struct {
					Detail string `json:"detail"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&errBody)
				if !strings.Contains(errBody.Detail, tc.wantInError) {
					t.Fatalf(
						"error.detail = %q, want substring %q",
						errBody.Detail, tc.wantInError,
					)
				}
			}
		})
	}
}

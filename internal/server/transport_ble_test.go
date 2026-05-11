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
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/transports"
)

// TestEndpointListBLEDevices — GET /transports/ble/devices. Projects
// the persisted mdl.BLEDevice into the slim transports.BLEDeviceView wire shape.
// 503 when the store isn't wired (a daemon running without sqlite).
func TestEndpointListBLEDevices(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		store      transports.Store
		wantStatus int
		wantUUIDs  []string
		wantFavs   []bool
	}{
		{
			name: "returns-saved-devices-projected-to-transports.BLEDeviceView",
			store: newFakeStore(
				mdl.BLEDevice{UUID: "11", LongName: "T-Beam Mobile", Favorite: true},
				mdl.BLEDevice{UUID: "22", LongName: "Heltec Base"},
			),
			wantStatus: http.StatusOK,
			wantUUIDs:  []string{"11", "22"},
			wantFavs:   []bool{true, false},
		},
		{
			name:       "empty-store-returns-empty-array-not-null",
			store:      newFakeStore(),
			wantStatus: http.StatusOK,
			wantUUIDs:  []string{},
			wantFavs:   []bool{},
		},
		{
			name:       "store-not-wired-returns-503",
			store:      nil,
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTransportHarness(t, transportHarnessOpts{store: tc.store})

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodGet, srv.URL+"/transports/ble/devices", nil,
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
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
				Devices []transports.BLEDeviceView `json:"devices"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(body.Devices) != len(tc.wantUUIDs) {
				t.Fatalf("len(devices) = %d, want %d", len(body.Devices), len(tc.wantUUIDs))
			}
			for i, want := range tc.wantUUIDs {
				if body.Devices[i].UUID != want {
					t.Fatalf("devices[%d].uuid = %q, want %q", i, body.Devices[i].UUID, want)
				}
				if body.Devices[i].Favorite != tc.wantFavs[i] {
					t.Fatalf(
						"devices[%d].favorite = %v, want %v",
						i, body.Devices[i].Favorite, tc.wantFavs[i],
					)
				}
			}
		})
	}
}

// TestEndpointScanBLE — POST /transports/ble/scan. Forwards the
// requested timeout to the scanner (defaulting to 10000 when zero or
// missing) and returns the deduplicated peripheral list. 503 when the
// scanner isn't wired.
func TestEndpointScanBLE(t *testing.T) {
	t.Parallel()

	hits := []transports.BLESighting{
		{UUID: "aaa", LocalName: "T-Beam", RSSI: -55},
		{UUID: "bbb", LocalName: "Heltec", RSSI: -83},
	}

	cases := []struct {
		name       string
		scanner    transports.BLEScanner
		body       string
		wantStatus int
		wantCount  int
		wantTMO    int  // expected timeoutMS the handler forwarded to the scanner
		checkCalls bool // verify scanner.calls == 1
	}{
		{
			name:       "explicit-timeout-passes-through-and-returns-hits",
			scanner:    newFakeBLEScanner(hits...),
			body:       `{"timeout_ms":3000}`,
			wantStatus: http.StatusOK,
			wantCount:  2,
			wantTMO:    3000,
			checkCalls: true,
		},
		{
			name:       "zero-timeout-defaults-to-10000",
			scanner:    newFakeBLEScanner(hits...),
			body:       `{"timeout_ms":0}`,
			wantStatus: http.StatusOK,
			wantCount:  2,
			wantTMO:    10000,
			checkCalls: true,
		},
		{
			name:       "missing-timeout-defaults-to-10000",
			scanner:    newFakeBLEScanner(hits...),
			body:       `{}`,
			wantStatus: http.StatusOK,
			wantCount:  2,
			wantTMO:    10000,
			checkCalls: true,
		},
		{
			name:       "empty-scanner-result-returns-empty-devices-array",
			scanner:    newFakeBLEScanner(),
			body:       `{"timeout_ms":1500}`,
			wantStatus: http.StatusOK,
			wantCount:  0,
			wantTMO:    1500,
			checkCalls: true,
		},
		{
			name: "scanner-error-returns-500",
			scanner: func() transports.BLEScanner {
				s := newFakeBLEScanner()
				s.err = errSentinel
				return s
			}(),
			body:       `{"timeout_ms":1500}`,
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "scanner-not-wired-returns-503",
			scanner:    nil,
			body:       `{"timeout_ms":1500}`,
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTransportHarness(t, transportHarnessOpts{scanner: tc.scanner})

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPost,
				srv.URL+"/transports/ble/scan",
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
				Devices []transports.BLESighting `json:"devices"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(body.Devices) != tc.wantCount {
				t.Fatalf("len(devices) = %d, want %d", len(body.Devices), tc.wantCount)
			}
			if tc.checkCalls {
				fs := tc.scanner.(*fakeBLEScanner)
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

// TestEndpointPairBLE — POST /transports/ble/devices. Calls
// pairer.PairMeshtastic to trigger OS bonding, then persists the
// device via store.SaveBLEDevice. Both store + pairer are required;
// either missing → 503.
func TestEndpointPairBLE(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		store       transports.Store
		pairer      transports.BLEPairer
		body        string
		wantStatus  int
		wantUUID    string
		wantSaved   bool // expect store.saved == 1
		wantPaired  bool // expect pairer.calls == 1
		errSentinel bool // wire pairer to return errSentinel
	}{
		{
			name:       "happy-path-pairs-and-saves",
			store:      newFakeStore(),
			pairer:     newFakeBLEPairer(),
			body:       `{"uuid":"abc-uuid"}`,
			wantStatus: http.StatusOK,
			wantUUID:   "abc-uuid",
			wantSaved:  true,
			wantPaired: true,
		},
		{
			name:        "pairer-error-returns-500-and-does-not-save",
			store:       newFakeStore(),
			pairer:      newFakeBLEPairer(),
			body:        `{"uuid":"abc-uuid"}`,
			wantStatus:  http.StatusInternalServerError,
			wantPaired:  true, // pairer was called even though it errored
			wantSaved:   false,
			errSentinel: true,
		},
		{
			name:       "missing-uuid-rejected-by-huma-with-422",
			store:      newFakeStore(),
			pairer:     newFakeBLEPairer(),
			body:       `{}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "empty-uuid-rejected-by-huma-with-422",
			store:      newFakeStore(),
			pairer:     newFakeBLEPairer(),
			body:       `{"uuid":""}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "store-not-wired-returns-503",
			store:      nil,
			pairer:     newFakeBLEPairer(),
			body:       `{"uuid":"abc"}`,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "pairer-not-wired-returns-503",
			store:      newFakeStore(),
			pairer:     nil,
			body:       `{"uuid":"abc"}`,
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.errSentinel {
				tc.pairer.(*fakeBLEPairer).err = errSentinel
			}
			srv := newTransportHarness(t, transportHarnessOpts{
				store:  tc.store,
				pairer: tc.pairer,
			})

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPost,
				srv.URL+"/transports/ble/devices",
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

			// Side-effect assertions when the harness was fully wired.
			if fp, ok := tc.pairer.(*fakeBLEPairer); ok && tc.pairer != nil {
				gotPaired := fp.calls == 1
				if gotPaired != tc.wantPaired {
					t.Fatalf("pairer.calls = %d, want %d", fp.calls, btoi(tc.wantPaired))
				}
				if tc.wantPaired && fp.lastUUID != "abc-uuid" && fp.lastUUID != "abc" {
					t.Fatalf("pairer.lastUUID = %q, want abc-uuid", fp.lastUUID)
				}
			}
			if fs, ok := tc.store.(*fakeStore); ok && tc.store != nil {
				gotSaved := fs.saved == 1
				if gotSaved != tc.wantSaved {
					t.Fatalf("store.saved = %d, want %d", fs.saved, btoi(tc.wantSaved))
				}
			}

			if tc.wantStatus != http.StatusOK {
				return
			}
			var body transports.BLEDeviceView
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.UUID != tc.wantUUID {
				t.Fatalf("body.uuid = %q, want %q", body.UUID, tc.wantUUID)
			}
		})
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestEndpointForgetBLE — DELETE /transports/ble/devices/{uuid}.
// Resolves the path arg via transports.Store.LookupBLEDevice (accepts UUID or
// name); 404 when no saved device matches.
func TestEndpointForgetBLE(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		store      transports.Store
		path       string
		wantStatus int
		wantForgot string // expected store.forgot
	}{
		{
			name: "uuid-match-deletes-from-store",
			store: newFakeStore(
				mdl.BLEDevice{UUID: "abc-uuid", LongName: "Beam"},
			),
			path:       "abc-uuid",
			wantStatus: http.StatusNoContent,
			wantForgot: "abc-uuid",
		},
		{
			name: "longname-match-resolves-and-deletes",
			store: newFakeStore(
				mdl.BLEDevice{UUID: "abc-uuid", LongName: "Beam"},
			),
			path:       "Beam",
			wantStatus: http.StatusNoContent,
			wantForgot: "abc-uuid",
		},
		{
			name:       "no-match-returns-404",
			store:      newFakeStore(),
			path:       "ghost-uuid",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store-not-wired-returns-503",
			store:      nil,
			path:       "abc-uuid",
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTransportHarness(t, transportHarnessOpts{store: tc.store})

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodDelete,
				srv.URL+"/transports/ble/devices/"+tc.path, nil,
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if fs, ok := tc.store.(*fakeStore); ok && tc.store != nil {
				if fs.forgot != tc.wantForgot {
					t.Fatalf("store.forgot = %q, want %q", fs.forgot, tc.wantForgot)
				}
			}
		})
	}
}

// TestEndpointSetBLEFavorite — PUT /transports/ble/devices/{uuid}/favorite.
// Resolves path arg via lookup, then SetBLEFavorite atomically clears
// every other flag.
func TestEndpointSetBLEFavorite(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		store      transports.Store
		path       string
		wantStatus int
		wantFavSet string // expected store.favSet
		wantUUID   string // expected response body uuid
	}{
		{
			name: "marks-named-device-as-favorite",
			store: newFakeStore(
				mdl.BLEDevice{UUID: "11", LongName: "Mobile"},
				mdl.BLEDevice{UUID: "22", LongName: "Base", Favorite: true},
			),
			path:       "Mobile",
			wantStatus: http.StatusOK,
			wantFavSet: "11",
			wantUUID:   "11",
		},
		{
			name:       "no-match-returns-404",
			store:      newFakeStore(),
			path:       "ghost",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "store-not-wired-returns-503",
			store:      nil,
			path:       "abc",
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTransportHarness(t, transportHarnessOpts{store: tc.store})

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPut,
				srv.URL+"/transports/ble/devices/"+tc.path+"/favorite", nil,
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if fs, ok := tc.store.(*fakeStore); ok && tc.store != nil {
				if fs.favSet != tc.wantFavSet {
					t.Fatalf("store.favSet = %q, want %q", fs.favSet, tc.wantFavSet)
				}
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var body transports.BLEDeviceView
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.UUID != tc.wantUUID {
				t.Fatalf("body.uuid = %q, want %q", body.UUID, tc.wantUUID)
			}
			if !body.Favorite {
				t.Fatalf("body.favorite = false, want true")
			}
		})
	}
}

// TestEndpointClearBLEFavorite — DELETE /transports/ble/favorite.
// Calls SetBLEFavorite("") which clears every flag in the store.
func TestEndpointClearBLEFavorite(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		store      transports.Store
		wantStatus int
		wantFavSet string // empty string when clear was called
		wantClear  bool   // verify previously-favored device is no longer flagged
	}{
		{
			name: "clears-existing-favorite-flag",
			store: newFakeStore(
				mdl.BLEDevice{UUID: "11", LongName: "Mobile", Favorite: true},
				mdl.BLEDevice{UUID: "22", LongName: "Base"},
			),
			wantStatus: http.StatusNoContent,
			wantFavSet: "",
			wantClear:  true,
		},
		{
			name:       "no-op-when-no-favorite-set-still-204",
			store:      newFakeStore(mdl.BLEDevice{UUID: "11"}),
			wantStatus: http.StatusNoContent,
			wantFavSet: "",
		},
		{
			name:       "store-not-wired-returns-503",
			store:      nil,
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTransportHarness(t, transportHarnessOpts{store: tc.store})

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodDelete,
				srv.URL+"/transports/ble/favorite", nil,
			)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if fs, ok := tc.store.(*fakeStore); ok && tc.store != nil {
				if fs.favSet != tc.wantFavSet {
					t.Fatalf("store.favSet = %q, want %q", fs.favSet, tc.wantFavSet)
				}
				if tc.wantClear {
					for _, d := range fs.devices {
						if d.Favorite {
							t.Fatalf("device %q still favored after clear", d.UUID)
						}
					}
				}
			}
		})
	}
}

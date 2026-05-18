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

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/retr0h/meshx/internal/radio"
)

// makeMultiRadioSrv wires a Server with two radios so list-radios can
// assert the sort order and per-radio summary projection. radioA gets
// distinct ConnectDest so the wire field is visible end-to-end.
func makeMultiRadioSrv(t *testing.T) *httptest.Server {
	t.Helper()
	s := New(Config{Radios: NewRegistry()})

	a := radio.New(nil, nil, nil)
	a.State.RadioID = "0xradio_a"
	a.State.MyNodeNum = 0xaaaa
	a.State.Connected = true
	a.State.ConnectDest = "/dev/cu.usb-a"
	s.radios.Add(a.State.RadioID, a)

	b := radio.New(nil, nil, nil)
	b.State.RadioID = "0xradio_b"
	b.State.MyNodeNum = 0xbbbb
	b.State.Connected = false
	b.State.ConnectDest = "tcp://10.0.0.7:4403"
	s.radios.Add(b.State.RadioID, b)

	srv := httptest.NewServer(s.http.Handler)
	t.Cleanup(srv.Close)
	return srv
}

// TestEndpointListRadios — GET /radios. Returns every registered
// radio sorted by RadioID, projected into the RadioSummary wire shape
// (the daemon's per-radio summary, not the full SessionSnapshot).
func TestEndpointListRadios(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		newSrv   func(t *testing.T) *httptest.Server
		wantIDs  []string // expected radio_id sequence (in response order)
		wantDest map[string]string
	}{
		{
			name: "empty-registry-returns-empty-radios-array",
			newSrv: func(t *testing.T) *httptest.Server {
				t.Helper()
				s := New(Config{Radios: NewRegistry()})
				srv := httptest.NewServer(s.http.Handler)
				t.Cleanup(srv.Close)
				return srv
			},
			wantIDs: []string{}, // empty slice, not null
		},
		{
			name:    "two-radios-returned-sorted-by-id-with-projected-fields",
			newSrv:  makeMultiRadioSrv,
			wantIDs: []string{"0xradio_a", "0xradio_b"},
			wantDest: map[string]string{
				"0xradio_a": "/dev/cu.usb-a",
				"0xradio_b": "tcp://10.0.0.7:4403",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := tc.newSrv(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/radios", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			var body struct {
				Radios []RadioSummary `json:"radios"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(body.Radios) != len(tc.wantIDs) {
				t.Fatalf("len(radios) = %d, want %d", len(body.Radios), len(tc.wantIDs))
			}
			for i, want := range tc.wantIDs {
				if body.Radios[i].RadioID != want {
					t.Fatalf("radios[%d].radio_id = %q, want %q", i, body.Radios[i].RadioID, want)
				}
				if dest, ok := tc.wantDest[want]; ok {
					if body.Radios[i].ConnectDest != dest {
						t.Fatalf(
							"radios[%d].connect_dest = %q, want %q",
							i, body.Radios[i].ConnectDest, dest,
						)
					}
				}
			}
		})
	}
}

// TestEndpointGetRadio — GET /radios/{id}. Projects the full
// SessionSnapshot wire shape; resolveRadio gates unknown radio_id
// with 404 before the handler runs.
func TestEndpointGetRadio(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		radioID        string // path arg
		wantStatus     int
		wantMyNodeNum  uint32
		wantFirmware   string
		wantRegion     string
		wantBatteryLvl uint32
	}{
		{
			name:           "returns-snapshot-with-full-wire-shape",
			radioID:        "the-radio",
			wantStatus:     http.StatusOK,
			wantMyNodeNum:  0xdeadbeef,
			wantFirmware:   "2.5.0",
			wantRegion:     "US",
			wantBatteryLvl: 87,
		},
		{
			name:       "unknown-radio-returns-404",
			radioID:    "nope-no-such-radio",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Config{Radios: NewRegistry()})
			sess := radio.New(nil, nil, nil)
			sess.State.RadioID = "the-radio"
			sess.State.MyNodeNum = 0xdeadbeef
			sess.State.Connected = true
			sess.State.RadioFirmware = "2.5.0"
			sess.State.RadioRegion = "US"
			sess.State.BatteryLevel = 87
			s.radios.Add(sess.State.RadioID, sess)
			srv := httptest.NewServer(s.http.Handler)
			t.Cleanup(srv.Close)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(
				ctx, http.MethodGet,
				srv.URL+"/radios/"+tc.radioID, nil,
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
			var body SessionSnapshot
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.MyNodeNum != tc.wantMyNodeNum {
				t.Fatalf("my_node_num = %d, want %d", body.MyNodeNum, tc.wantMyNodeNum)
			}
			if body.RadioFirmware != tc.wantFirmware {
				t.Fatalf("radio_firmware = %q, want %q", body.RadioFirmware, tc.wantFirmware)
			}
			if body.RadioRegion != tc.wantRegion {
				t.Fatalf("radio_region = %q, want %q", body.RadioRegion, tc.wantRegion)
			}
			if body.BatteryLevel != tc.wantBatteryLvl {
				t.Fatalf("battery_level = %d, want %d", body.BatteryLevel, tc.wantBatteryLvl)
			}
			if !body.Connected {
				t.Fatalf("connected = false, want true")
			}
		})
	}
}

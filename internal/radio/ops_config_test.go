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

package radio

import (
	"errors"
	"testing"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// configFakePump satisfies Pump for ops_config tests.
type configFakePump struct{ rejected bool }

func (p *configFakePump) Send(mdl.Command) (uint32, bool) {
	if p.rejected {
		return 0, false
	}
	return 1, true
}
func (p *configFakePump) Stop() {}

// ptr returns a pointer to any value, simplifying optional-field
// construction in table tests.
func ptr[T any](v T) *T { return &v }

// TestSession_UpdateConfig validates long/shortname bounds, dispatch
// coalescing, buzzer-only updates, and pump-unavailable errors.
func TestSession_UpdateConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		req         UpdateConfigRequest
		pump        Pump
		wantCode    int // 0 = expect success
		wantApplied []string
	}{
		{
			name:     "empty-longname-rejected",
			req:      UpdateConfigRequest{LongName: ptr("")},
			pump:     &configFakePump{},
			wantCode: 400,
		},
		{
			name: "longname-too-long-rejected",
			req: UpdateConfigRequest{
				LongName: ptr("this name is way too long for meshtastic radio"),
			},
			pump:     &configFakePump{},
			wantCode: 400,
		},
		{
			name:     "empty-shortname-rejected",
			req:      UpdateConfigRequest{ShortName: ptr("")},
			pump:     &configFakePump{},
			wantCode: 400,
		},
		{
			name:     "shortname-too-long-rejected",
			req:      UpdateConfigRequest{ShortName: ptr("TOOLONG")},
			pump:     &configFakePump{},
			wantCode: 400,
		},
		{
			name:     "no-pump-unavailable",
			req:      UpdateConfigRequest{LongName: ptr("Alice")},
			pump:     nil,
			wantCode: 503,
		},
		{
			name:     "pump-rejected-unavailable",
			req:      UpdateConfigRequest{LongName: ptr("Alice")},
			pump:     &configFakePump{rejected: true},
			wantCode: 503,
		},
		{
			name:        "longname-only-applied",
			req:         UpdateConfigRequest{LongName: ptr("Alice")},
			pump:        &configFakePump{},
			wantCode:    0,
			wantApplied: []string{"longname"},
		},
		{
			name:        "shortname-only-applied",
			req:         UpdateConfigRequest{ShortName: ptr("ALIC")},
			pump:        &configFakePump{},
			wantCode:    0,
			wantApplied: []string{"shortname"},
		},
		{
			name:        "is-licensed-applied",
			req:         UpdateConfigRequest{IsLicensed: ptr(true)},
			pump:        &configFakePump{},
			wantCode:    0,
			wantApplied: []string{"is_licensed"},
		},
		{
			name: "all-owner-fields-coalesced",
			req: UpdateConfigRequest{
				LongName:   ptr("Alice"),
				ShortName:  ptr("ALIC"),
				IsLicensed: ptr(false),
			},
			pump:        &configFakePump{},
			wantCode:    0,
			wantApplied: []string{"longname", "shortname", "is_licensed"},
		},
		{
			name:        "buzzer-only-applied",
			req:         UpdateConfigRequest{Buzzer: ptr(true)},
			pump:        &configFakePump{},
			wantCode:    0,
			wantApplied: []string{"buzzer"},
		},
		{
			name: "all-fields-applied",
			req: UpdateConfigRequest{
				LongName:  ptr("Bob"),
				ShortName: ptr("BOB!"),
				Buzzer:    ptr(false),
			},
			pump:        &configFakePump{},
			wantCode:    0,
			wantApplied: []string{"longname", "shortname", "buzzer"},
		},
		{
			name:        "empty-request-no-applied",
			req:         UpdateConfigRequest{},
			pump:        &configFakePump{},
			wantCode:    0,
			wantApplied: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(nil, tc.pump, nil)

			res, err := s.UpdateConfig(tc.req)
			if tc.wantCode != 0 {
				var opErr *OpError
				if !errors.As(err, &opErr) || opErr.Code != tc.wantCode {
					t.Fatalf("want %d OpError, got %v", tc.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(res.Applied) != len(tc.wantApplied) {
				t.Fatalf("Applied = %v, want %v", res.Applied, tc.wantApplied)
			}
			for i, want := range tc.wantApplied {
				if res.Applied[i] != want {
					t.Fatalf("Applied[%d] = %q, want %q", i, res.Applied[i], want)
				}
			}
		})
	}
}

// TestSession_UpdateConfig_currentOwner exercises UpdateConfig when the
// session has a known node in the NodeDB so currentOwner returns real
// callsign/shortname data. This covers the happy path of currentOwner's
// own-node lookup branch that is bypassed when NodesByNum is empty.
func TestSession_UpdateConfig_currentOwner(t *testing.T) {
	t.Parallel()

	s := New(nil, &configFakePump{}, nil)
	s.State.MyNodeNum = 0xdeadbeef
	s.State.Nodes = []mdl.NodeItem{
		{NodeNum: 0xdeadbeef, Callsign: "Alice", ShortName: "ALIC"},
	}
	s.State.NodesByNum[0xdeadbeef] = 0

	// Patch only ShortName — currentOwner must supply the existing
	// longname so the SetOwner round-trip preserves it.
	res, err := s.UpdateConfig(UpdateConfigRequest{ShortName: ptr("BLIC")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Applied) != 1 || res.Applied[0] != "shortname" {
		t.Fatalf("Applied = %v, want [shortname]", res.Applied)
	}
}

// TestSession_Reboot verifies the default 5-second grace window and an
// explicit Seconds value, plus pump-unavailable path.
func TestSession_Reboot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		req         RebootRequest
		pump        Pump
		wantCode    int
		wantSeconds int32
	}{
		{
			name:     "no-pump-unavailable",
			req:      RebootRequest{},
			pump:     nil,
			wantCode: 503,
		},
		{
			name:     "pump-rejected-unavailable",
			req:      RebootRequest{Seconds: 10},
			pump:     &configFakePump{rejected: true},
			wantCode: 503,
		},
		{
			name:        "zero-seconds-defaults-to-5",
			req:         RebootRequest{Seconds: 0},
			pump:        &configFakePump{},
			wantSeconds: 5,
		},
		{
			name:        "negative-seconds-defaults-to-5",
			req:         RebootRequest{Seconds: -1},
			pump:        &configFakePump{},
			wantSeconds: 5,
		},
		{
			name:        "explicit-seconds-used",
			req:         RebootRequest{Seconds: 30},
			pump:        &configFakePump{},
			wantSeconds: 30,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(nil, tc.pump, nil)
			res, err := s.Reboot(tc.req)
			if tc.wantCode != 0 {
				var opErr *OpError
				if !errors.As(err, &opErr) || opErr.Code != tc.wantCode {
					t.Fatalf("want %d OpError, got %v", tc.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Seconds != tc.wantSeconds {
				t.Fatalf("Seconds = %d, want %d", res.Seconds, tc.wantSeconds)
			}
		})
	}
}

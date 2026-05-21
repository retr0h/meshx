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

package transport

import (
	"fmt"
	"strings"
	"testing"
)

// TestHwModelName verifies the public HwModelName helper returns the correct
// string for known hardware model integers and falls back to the "hw %d" form
// for unknown values.
func TestHwModelName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input int
		want  string
	}{
		// Known entries — sample across the range.
		{name: "UNSET (0)", input: 0, want: "UNSET"},
		{name: "TLORA_V2 (1)", input: 1, want: "TLORA_V2"},
		{name: "TBEAM (4)", input: 4, want: "TBEAM"},
		{name: "HELTEC_V3 (43)", input: 43, want: "HELTEC_V3"},
		{name: "T_DECK (50)", input: 50, want: "T_DECK"},
		{name: "PRIVATE_HW (101)", input: 101, want: "PRIVATE_HW"},
		// Gap in the table — 24 is not mapped.
		{name: "unknown value 24 — numeric fallback", input: 24, want: "hw 24"},
		// Clearly out-of-range values.
		{name: "negative value", input: -1, want: "hw -1"},
		{name: "large unknown value", input: 9999, want: "hw 9999"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := HwModelName(tc.input)
			if got != tc.want {
				t.Errorf("HwModelName(%d) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestHwModelName_FallbackFormat verifies that every unknown integer produces
// the exact "hw %d" form (not empty, not a Go default).
func TestHwModelName_FallbackFormat(t *testing.T) {
	t.Parallel()

	unknowns := []int{24, 98, 100, 200, 500}
	for _, n := range unknowns {
		t.Run(fmt.Sprintf("unknown %d", n), func(t *testing.T) {
			t.Parallel()
			want := fmt.Sprintf("hw %d", n)
			got := HwModelName(n)
			if got != want {
				t.Errorf("HwModelName(%d) = %q, want %q", n, got, want)
			}
		})
	}
}

// TestDeviceInfo_String verifies the one-line rendering for each of the three
// DeviceInfo states: confirmed Meshtastic with full names, confirmed with only
// a NodeNum, and non-Meshtastic (with and without an error).
func TestDeviceInfo_String(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		info     DeviceInfo
		wantFrag string // substring that must appear in String()
	}{
		{
			name: "not Meshtastic without error",
			info: DeviceInfo{
				Port:         "/dev/cu.usbserial-0001",
				IsMeshtastic: false,
			},
			wantFrag: "not Meshtastic",
		},
		{
			name: "not Meshtastic with error",
			info: DeviceInfo{
				Port:         "/dev/cu.usbserial-0001",
				IsMeshtastic: false,
				Err:          fmt.Errorf("timeout after 2s"),
			},
			wantFrag: "timeout after 2s",
		},
		{
			name: "Meshtastic — long name wins",
			info: DeviceInfo{
				Port:         "/dev/cu.usbmodem2101",
				IsMeshtastic: true,
				LongName:     "T-Beam Mobile",
				ShortName:    "TBEM",
				HWModel:      "TBEAM",
			},
			wantFrag: "T-Beam Mobile",
		},
		{
			name: "Meshtastic — short name fallback when no long name",
			info: DeviceInfo{
				Port:         "/dev/cu.usbmodem2101",
				IsMeshtastic: true,
				ShortName:    "TBEM",
				HWModel:      "TBEAM",
			},
			wantFrag: "TBEM",
		},
		{
			name: "Meshtastic — node hex fallback when no names",
			info: DeviceInfo{
				Port:         "/dev/cu.usbmodem2101",
				IsMeshtastic: true,
				NodeNum:      0xdeadbeef,
				HWModel:      "TBEAM",
			},
			wantFrag: "deadbeef",
		},
		{
			name: "Meshtastic — port appears in output",
			info: DeviceInfo{
				Port:         "/dev/cu.usbmodem9999",
				IsMeshtastic: true,
				LongName:     "Relay Node",
				HWModel:      "HELTEC_V3",
			},
			wantFrag: "/dev/cu.usbmodem9999",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.info.String()
			if got == "" {
				t.Fatal("String() returned empty string")
			}
			if tc.wantFrag != "" {
				if !strings.Contains(got, tc.wantFrag) {
					t.Errorf("String() = %q, want it to contain %q", got, tc.wantFrag)
				}
			}
		})
	}
}

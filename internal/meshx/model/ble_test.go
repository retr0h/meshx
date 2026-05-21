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

package model

import "testing"

// TestBLEDevice_DisplayName verifies the name-fallback chain: LongName first,
// then ShortName, then the raw UUID. Always returns a non-empty string.
func TestBLEDevice_DisplayName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		device BLEDevice
		want   string
	}{
		{
			name: "long-name present — returned as-is",
			device: BLEDevice{
				UUID:      "aaaabbbb-cccc-dddd-eeee-ffffffffffff",
				LongName:  "T-Beam Mobile",
				ShortName: "TBEM",
			},
			want: "T-Beam Mobile",
		},
		{
			name: "no long-name — falls back to short-name",
			device: BLEDevice{
				UUID:      "aaaabbbb-cccc-dddd-eeee-ffffffffffff",
				LongName:  "",
				ShortName: "TBEM",
			},
			want: "TBEM",
		},
		{
			name: "no long or short — falls back to UUID",
			device: BLEDevice{
				UUID:      "aaaabbbb-cccc-dddd-eeee-ffffffffffff",
				LongName:  "",
				ShortName: "",
			},
			want: "aaaabbbb-cccc-dddd-eeee-ffffffffffff",
		},
		{
			name: "all empty — returns UUID (only field available)",
			device: BLEDevice{
				UUID: "12345678-0000-0000-0000-000000000000",
			},
			want: "12345678-0000-0000-0000-000000000000",
		},
		{
			name: "favorite flag does not affect display name",
			device: BLEDevice{
				UUID:     "ffffffff-ffff-ffff-ffff-ffffffffffff",
				LongName: "Favorite Radio",
				Favorite: true,
			},
			want: "Favorite Radio",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.device.DisplayName()
			if got != tc.want {
				t.Errorf("DisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}

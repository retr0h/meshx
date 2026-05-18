// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit specific persons to whom the
// Software is furnished to do so, subject to the following conditions:
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

package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/retr0h/meshx/internal/radio"
)

// TestRunClientStatus — `meshx client status` prints daemon health
// + every attached radio. Single happy-path row plus an empty-radios
// row exercise the two output branches (table vs "no radios").
func TestRunClientStatus(t *testing.T) {
	cases := []struct {
		name        string
		radios      []*radio.Session
		wantSubstrs []string
	}{
		{
			name:   "happy-path-one-radio-prints-table-row",
			radios: []*radio.Session{fakeRadio("0xabcdef01", "ble:48d917af-8a1f-e43e-4735")},
			wantSubstrs: []string{
				"daemon: ok",
				"RADIO_ID",
				"0xabcdef01",
				"0xdeadbeef",
				"ble:48d917af",
				"yes", // Connected
			},
		},
		{
			name:        "empty-registry-prints-no-radios-line",
			radios:      nil,
			wantSubstrs: []string{"daemon: ok", "no radios attached"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := clientHarness(t, tc.radios...)
			var buf bytes.Buffer
			if err := runClientStatus(context.Background(), c, &buf); err != nil {
				t.Fatalf("runClientStatus: %v", err)
			}
			got := buf.String()
			for _, want := range tc.wantSubstrs {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\n--- got ---\n%s", want, got)
				}
			}
		})
	}
}

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

package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/retr0h/meshx/internal/radio"
)

// TestResolveClientRadio — the auto-target + needle-resolution rules
// for `meshx client connect [radio]`. Each row mutates the registry
// to exercise a different lookup path.
func TestResolveClientRadio(t *testing.T) {
	type seed struct {
		id, dest string
	}
	cases := []struct {
		name    string
		seeds   []seed
		needle  string
		wantID  string
		wantErr string // substring; "" means expect no error
	}{
		{
			name:    "empty-registry-empty-needle-errors",
			seeds:   nil,
			needle:  "",
			wantErr: "no radios attached",
		},
		{
			name:   "one-radio-empty-needle-auto-targets",
			seeds:  []seed{{id: "0xabcdef01", dest: "/dev/cu.usb"}},
			needle: "",
			wantID: "0xabcdef01",
		},
		{
			name: "multiple-radios-empty-needle-errors-with-hint",
			seeds: []seed{
				{id: "0xabcdef01", dest: "/dev/cu.usb1"},
				{id: "0xabcdef02", dest: "/dev/cu.usb2"},
			},
			needle:  "",
			wantErr: "specify which",
		},
		{
			name:   "exact-radio_id-match-wins",
			seeds:  []seed{{id: "0xabcdef01", dest: "ble:foo"}},
			needle: "0xabcdef01",
			wantID: "0xabcdef01",
		},
		{
			name:   "exact-connect_dest-match-wins",
			seeds:  []seed{{id: "0xabcdef01", dest: "ble:48d917af"}},
			needle: "ble:48d917af",
			wantID: "0xabcdef01",
		},
		{
			name:   "substring-of-connect_dest-matches",
			seeds:  []seed{{id: "0xabcdef01", dest: "ble:48d917af-8a1f-..."}},
			needle: "48d917af",
			wantID: "0xabcdef01",
		},
		{
			name:    "no-match-returns-clear-error",
			seeds:   []seed{{id: "0xabcdef01", dest: "ble:foo"}},
			needle:  "nope",
			wantErr: "no radio matching",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			radios := make([]*radio.Session, 0, len(tc.seeds))
			for _, sd := range tc.seeds {
				radios = append(radios, fakeRadio(sd.id, sd.dest))
			}
			c := clientHarness(t, radios...)

			got, err := resolveClientRadio(context.Background(), c, "http://test", tc.needle)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (radio=%q)", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q missing substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantID {
				t.Fatalf("radio_id = %q, want %q", got, tc.wantID)
			}
		})
	}
}

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
)

// TestOpError validates OpError.Error() and the constructor helpers that
// produce each HTTP-like status code. Table rows cover the plain-string
// constructors; a separate sub-test verifies the fmt.Sprintf variants
// produce the expected formatted message and preserve the code.
func TestOpError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		err      error
		wantMsg  string
		wantCode int
	}{
		{
			name:     "ErrBadRequest",
			err:      ErrBadRequest("bad input"),
			wantMsg:  "bad input",
			wantCode: 400,
		},
		{
			name:     "ErrNotFound",
			err:      ErrNotFound("not found"),
			wantMsg:  "not found",
			wantCode: 404,
		},
		{
			name:     "ErrConflict",
			err:      ErrConflict("already exists"),
			wantMsg:  "already exists",
			wantCode: 409,
		},
		{
			name:     "ErrInternal",
			err:      ErrInternal("server fault"),
			wantMsg:  "server fault",
			wantCode: 500,
		},
		{
			name:     "ErrUnavailable",
			err:      ErrUnavailable("no radio"),
			wantMsg:  "no radio",
			wantCode: 503,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Error() != tc.wantMsg {
				t.Fatalf("Error() = %q, want %q", tc.err.Error(), tc.wantMsg)
			}
			var opErr *OpError
			if !errors.As(tc.err, &opErr) {
				t.Fatal("error is not *OpError")
			}
			if opErr.Code != tc.wantCode {
				t.Fatalf("Code = %d, want %d", opErr.Code, tc.wantCode)
			}
		})
	}

	// Sprintf variants produce formatted messages and carry the same codes.
	t.Run("sprintf-variants", func(t *testing.T) {
		cases := []struct {
			name     string
			err      error
			wantMsg  string
			wantCode int
		}{
			{
				name:     "ErrBadRequestf",
				err:      ErrBadRequestf("slot %d out of range", 9),
				wantMsg:  "slot 9 out of range",
				wantCode: 400,
			},
			{
				name:     "ErrNotFoundf",
				err:      ErrNotFoundf("channel %d missing", 3),
				wantMsg:  "channel 3 missing",
				wantCode: 404,
			},
			{
				name:     "ErrConflictf",
				err:      ErrConflictf("channel %q exists", "ham"),
				wantMsg:  `channel "ham" exists`,
				wantCode: 409,
			},
			{
				name:     "ErrInternalf",
				err:      ErrInternalf("rand: %s", "entropy exhausted"),
				wantMsg:  "rand: entropy exhausted",
				wantCode: 500,
			},
			{
				name:     "ErrUnavailablef",
				err:      ErrUnavailablef("pump %s", "down"),
				wantMsg:  "pump down",
				wantCode: 503,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if tc.err.Error() != tc.wantMsg {
					t.Fatalf("Error() = %q, want %q", tc.err.Error(), tc.wantMsg)
				}
				var opErr *OpError
				if !errors.As(tc.err, &opErr) {
					t.Fatal("error is not *OpError")
				}
				if opErr.Code != tc.wantCode {
					t.Fatalf("Code = %d, want %d", opErr.Code, tc.wantCode)
				}
			})
		}
	})
}

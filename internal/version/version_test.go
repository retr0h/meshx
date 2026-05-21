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

package version

import "testing"

// TestBuildInfo verifies that BuildInfo returns a populated goversion.Info
// and that goreleaser ldflag variables are reflected when set. Subtests run
// sequentially (no t.Parallel inside) because they mutate the same
// package-level vars — parallelising them would race.
func TestBuildInfo(t *testing.T) {
	// save and restore all package-level vars for the whole test.
	origVersion, origCommit, origTreeState, origDate, origBuiltBy :=
		Version, Commit, TreeState, Date, BuiltBy
	t.Cleanup(func() {
		Version, Commit, TreeState, Date, BuiltBy =
			origVersion, origCommit, origTreeState, origDate, origBuiltBy
	})

	t.Run("devel defaults — all vars empty", func(t *testing.T) {
		Version = ""
		Commit = ""
		TreeState = ""
		Date = ""
		BuiltBy = ""

		info := BuildInfo()
		// caarlos0/go-version backfills "devel" from debug.ReadBuildInfo
		// when Version is blank — only assert the field is non-empty.
		if info.GitVersion == "" {
			t.Error("GitVersion should be non-empty (expect devel default)")
		}
	})

	t.Run("stamped values are reflected in Info", func(t *testing.T) {
		Version = "v1.2.3"
		Commit = "abc1234"
		TreeState = "clean"
		Date = "2026-01-01T00:00:00Z"
		BuiltBy = "goreleaser"

		info := BuildInfo()

		cases := []struct {
			field string
			got   string
			want  string
		}{
			{"GitVersion", info.GitVersion, "v1.2.3"},
			{"GitCommit", info.GitCommit, "abc1234"},
			{"GitTreeState", info.GitTreeState, "clean"},
			{"BuildDate", info.BuildDate, "2026-01-01T00:00:00Z"},
			{"BuiltBy", info.BuiltBy, "goreleaser"},
		}

		for _, tc := range cases {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.field, tc.got, tc.want)
			}
		}
	})
}

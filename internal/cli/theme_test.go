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

package cli

import (
	"bytes"
	"strings"
	"testing"
)

// resetActive saves and restores the package-level active pointer so
// tests that call SetTheme do not bleed state into other tests.
func resetActive(t *testing.T) {
	t.Helper()
	orig := active
	t.Cleanup(func() { active = orig })
}

// TestSetTheme verifies that known names activate the theme and unknown
// names are rejected. Tests run sequentially — they mutate the
// package-level active pointer which has no mutex in production code.
func TestSetTheme(t *testing.T) {
	resetActive(t)

	cases := []struct {
		name   string
		input  string
		wantOK bool
		wantFn string // expected active.Name when ok
	}{
		{
			name:   "exact match — maxheadroom",
			input:  "maxheadroom",
			wantOK: true,
			wantFn: "maxheadroom",
		},
		{
			name:   "case-insensitive match",
			input:  "MaxHeadroom",
			wantOK: true,
			wantFn: "maxheadroom",
		},
		{
			name:   "leading/trailing spaces are trimmed",
			input:  "  maxheadroom  ",
			wantOK: true,
			wantFn: "maxheadroom",
		},
		{
			name:   "unknown name returns false",
			input:  "doesnotexist",
			wantOK: false,
		},
		{
			name:   "empty string returns false",
			input:  "",
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Sequential: SetTheme writes the package-level active pointer.
			got := SetTheme(tc.input)
			if got != tc.wantOK {
				t.Errorf("SetTheme(%q) = %v, want %v", tc.input, got, tc.wantOK)
			}
			if tc.wantOK && ActiveTheme().Name != tc.wantFn {
				t.Errorf("ActiveTheme().Name = %q, want %q", ActiveTheme().Name, tc.wantFn)
			}
		})
	}
}

// TestActiveTheme verifies that ActiveTheme returns non-nil and reflects
// the most recent SetTheme call.
func TestActiveTheme(t *testing.T) {
	th := ActiveTheme()
	if th == nil {
		t.Fatal("ActiveTheme() returned nil")
	}
	if th.Name == "" {
		t.Error("ActiveTheme().Name is empty")
	}
}

// TestThemeNames verifies the ordering contract: first element is the
// default theme, remainder are alphabetically sorted.
func TestThemeNames(t *testing.T) {
	names := ThemeNames()
	if len(names) == 0 {
		t.Fatal("ThemeNames() returned empty slice")
	}
	// Default theme must always be first.
	if names[0] != "maxheadroom" {
		t.Errorf("ThemeNames()[0] = %q, want %q", names[0], "maxheadroom")
	}
	// Remainder must be sorted.
	for i := 2; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("ThemeNames() not sorted at index %d: %q < %q", i, names[i], names[i-1])
		}
	}
}

// TestRenderFunctions verifies Mute, Accent, OK, Err, Info, Banner,
// Success, and Failure all return non-empty strings for non-empty input,
// exercising the render path with a bytes.Buffer (non-TTY renderer).
// Sequential — all functions read the package-level active pointer.
func TestRenderFunctions(t *testing.T) {
	w := &bytes.Buffer{}

	cases := []struct {
		name string
		fn   func() string
	}{
		{"Mute", func() string { return Mute(w, "label") }},
		{"Accent", func() string { return Accent(w, "value") }},
		{"OK", func() string { return OK(w, "success") }},
		{"Err", func() string { return Err(w, "failure") }},
		{"Info", func() string { return Info(w, "hint") }},
		{"Banner", func() string { return Banner(w) }},
		{"Success", func() string { return Success(w, "done") }},
		{"Failure", func() string { return Failure(w, "oops") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.fn()
			if strings.TrimSpace(got) == "" {
				t.Errorf("%s() returned empty string", tc.name)
			}
		})
	}
}

// TestSuccess_PlainFallback confirms that Success returns a string containing
// the original message regardless of whether ANSI is active.
func TestSuccess_PlainFallback(t *testing.T) {
	w := &bytes.Buffer{}
	got := Success(w, "all good")
	if !strings.Contains(got, "all good") {
		t.Errorf("Success() = %q — does not contain message", got)
	}
}

// TestFailure_PlainFallback mirrors TestSuccess_PlainFallback for Failure.
func TestFailure_PlainFallback(t *testing.T) {
	w := &bytes.Buffer{}
	got := Failure(w, "went wrong")
	if !strings.Contains(got, "went wrong") {
		t.Errorf("Failure() = %q — does not contain message", got)
	}
}

// TestBanner_TwoLines verifies the Banner output contains exactly two
// non-empty lines (the block-letter logo rows).
func TestBanner_TwoLines(t *testing.T) {
	w := &bytes.Buffer{}
	got := Banner(w)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("Banner() produced %d lines, want 2", len(lines))
	}
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			t.Errorf("Banner() line %d is blank", i)
		}
	}
}

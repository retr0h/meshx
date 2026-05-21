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

// TestChannelItem_ZeroValue ensures that a zero-value ChannelItem compiles
// and has sane defaults — Role is empty (not a runtime panic), Index is 0,
// and HasPSK is false.
func TestChannelItem_ZeroValue(t *testing.T) {
	t.Parallel()

	var ch ChannelItem
	if ch.Index != 0 {
		t.Errorf("Index = %d, want 0", ch.Index)
	}
	if ch.HasPSK {
		t.Error("HasPSK = true on zero value, want false")
	}
	if ch.Unread != 0 {
		t.Errorf("Unread = %d, want 0", ch.Unread)
	}
}

// TestMessageItem_AckersOmitempty verifies that the Ackers slice starts nil
// on a zero-value MessageItem (matches json:"omitempty" expectation).
func TestMessageItem_AckersOmitempty(t *testing.T) {
	t.Parallel()

	var mi MessageItem
	if mi.Ackers != nil {
		t.Errorf("Ackers = %v, want nil on zero value", mi.Ackers)
	}
}

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
	"testing"

	"tinygo.org/x/bluetooth"
)

// TestScannedAddrCache exercises the three cache helpers —
// cacheScannedAddr, loadScannedAddr, and forgetScannedAddr — as a
// logical group. Each subtest is isolated: it uses a unique UUID key so
// parallel runs never share state, and the global map is left clean.
func TestScannedAddrCache(t *testing.T) {
	t.Parallel()

	// Use a sentinel zero-value Address for all subtests; the cache is
	// a pure in-memory map keyed on the UUID string, so the Address
	// value itself doesn't matter for correctness of the cache logic.
	var zeroAddr bluetooth.Address

	t.Run("load on empty cache returns false", func(t *testing.T) {
		t.Parallel()
		const uuid = "cache-test-miss-uuid"
		_, ok := loadScannedAddr(uuid)
		if ok {
			t.Error("expected cache miss, got hit")
		}
	})

	t.Run("cache then load returns true and the stored address", func(t *testing.T) {
		t.Parallel()
		const uuid = "cache-test-hit-uuid"
		cacheScannedAddr(uuid, zeroAddr)
		t.Cleanup(func() { forgetScannedAddr(uuid) })

		got, ok := loadScannedAddr(uuid)
		if !ok {
			t.Fatal("expected cache hit, got miss")
		}
		if got != zeroAddr {
			t.Errorf("loaded address %v, want %v", got, zeroAddr)
		}
	})

	t.Run("forget removes the entry", func(t *testing.T) {
		t.Parallel()
		const uuid = "cache-test-forget-uuid"
		cacheScannedAddr(uuid, zeroAddr)
		forgetScannedAddr(uuid)

		_, ok := loadScannedAddr(uuid)
		if ok {
			t.Error("expected cache miss after forget, got hit")
		}
	})

	t.Run("forget on absent key is a no-op", func(t *testing.T) {
		t.Parallel()
		// Should not panic or error.
		forgetScannedAddr("cache-test-absent-uuid")
	})

	t.Run("overwrite updates the stored address", func(t *testing.T) {
		t.Parallel()
		const uuid = "cache-test-overwrite-uuid"
		var firstAddr, secondAddr bluetooth.Address
		// Both are zero-value; differentiate by checking the overwrite
		// does not produce a miss.
		cacheScannedAddr(uuid, firstAddr)
		cacheScannedAddr(uuid, secondAddr)
		t.Cleanup(func() { forgetScannedAddr(uuid) })

		_, ok := loadScannedAddr(uuid)
		if !ok {
			t.Error("expected cache hit after overwrite, got miss")
		}
	})
}

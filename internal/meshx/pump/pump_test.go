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

package pump

// pump_test.go — unit tests for pump-level helpers (reconnect backoff,
// debug log open, setClient / getClient under the mutex, Enqueue).
// The run loop itself talks to real transports so it is not tested here.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
)

// ---- reconnectBackoff -------------------------------------------------------

func TestReconnectBackoff_TableDriven(t *testing.T) {
	tests := []struct {
		attempt int
		wantMin time.Duration
		wantMax time.Duration
	}{
		{1, 1 * time.Second, 1 * time.Second},
		{2, 2 * time.Second, 2 * time.Second},
		{3, 4 * time.Second, 4 * time.Second},
		{4, 8 * time.Second, 8 * time.Second},
		{5, 16 * time.Second, 16 * time.Second},
		{6, 30 * time.Second, 30 * time.Second}, // capped
		{10, 30 * time.Second, 30 * time.Second},
		{100, 30 * time.Second, 30 * time.Second}, // absurd input still capped
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := reconnectBackoff(tt.attempt)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("attempt=%d: got %v, want [%v, %v]",
					tt.attempt, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestReconnectBackoff_ZeroAndNegative(t *testing.T) {
	// attempt < 1 is clamped to 1 → minReconnectBackoff.
	for _, attempt := range []int{0, -1, -100} {
		got := reconnectBackoff(attempt)
		if got != minReconnectBackoff {
			t.Errorf("attempt=%d: got %v, want %v", attempt, got, minReconnectBackoff)
		}
	}
}

func TestReconnectBackoff_Monotonic(t *testing.T) {
	// Each step must be >= the previous (capped at max).
	prev := time.Duration(0)
	for i := 1; i <= 10; i++ {
		d := reconnectBackoff(i)
		if d < prev {
			t.Errorf("attempt %d: %v < previous %v (not monotonic)", i, d, prev)
		}
		if d > maxReconnectBackoff {
			t.Errorf("attempt %d: %v > maxReconnectBackoff %v", i, d, maxReconnectBackoff)
		}
		prev = d
	}
}

// ---- openPumpDebugLog -------------------------------------------------------

func TestOpenPumpDebugLog_EnvUnset_ReturnsNil(t *testing.T) {
	t.Setenv("MESHX_DEBUG", "")
	f := openPumpDebugLog()
	if f != nil {
		_ = f.Close()
		t.Fatal("expected nil when MESHX_DEBUG is empty")
	}
}

func TestOpenPumpDebugLog_EnvOne_UsesDefaultPath(t *testing.T) {
	// "1" → /tmp/meshx-pump.log. We can't guarantee /tmp is writable in
	// all CI environments, so just check the function returns non-nil and
	// close the file immediately.
	t.Setenv("MESHX_DEBUG", "1")
	f := openPumpDebugLog()
	if f == nil {
		t.Skip("could not open /tmp/meshx-pump.log (filesystem limitation)")
	}
	_ = f.Close()
	// Clean up after the test.
	_ = os.Remove("/tmp/meshx-pump.log")
}

func TestOpenPumpDebugLog_CustomPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pump-debug.log")
	t.Setenv("MESHX_DEBUG", path)
	f := openPumpDebugLog()
	if f == nil {
		t.Fatalf("expected non-nil file for custom path %q", path)
	}
	_ = f.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
}

func TestOpenPumpDebugLog_BadPath_ReturnsNil(t *testing.T) {
	// A path under a non-existent directory cannot be created.
	t.Setenv("MESHX_DEBUG", "/no/such/directory/pump.log")
	f := openPumpDebugLog()
	if f != nil {
		_ = f.Close()
		t.Fatal("expected nil for unwritable path")
	}
}

// ---- setClient / getClient --------------------------------------------------

func TestPump_SetGetClient(t *testing.T) {
	p := &Pump{outbound: make(chan *pb.ToRadio, 16)}
	if p.getClient() != nil {
		t.Fatal("initial client should be nil")
	}

	// Use a nil Transport value — the interface itself is not nil but
	// the underlying concrete type would be. For the purpose of testing
	// the mutex logic, a non-nil interface value is what we need.
	// We pass a concrete *mockTransport defined below.
	mt := &mockTransport{}
	prev := p.setClient(mt)
	if prev != nil {
		t.Fatalf("prev on first set: want nil, got %T", prev)
	}
	if got, ok := p.getClient().(*mockTransport); !ok || got != mt {
		t.Fatal("getClient after setClient returned wrong value")
	}

	mt2 := &mockTransport{}
	prev2 := p.setClient(mt2)
	if prev2mt, ok := prev2.(*mockTransport); !ok || prev2mt != mt {
		t.Fatalf("prev on second set: want mt, got %T", prev2)
	}
	if got, ok := p.getClient().(*mockTransport); !ok || got != mt2 {
		t.Fatal("getClient after second setClient returned wrong value")
	}
}

// ---- Enqueue ----------------------------------------------------------------

func TestPump_Enqueue_Success(t *testing.T) {
	p := &Pump{outbound: make(chan *pb.ToRadio, 16)}
	env := &pb.ToRadio{}
	if !p.Enqueue(env) {
		t.Fatal("Enqueue failed on empty buffer")
	}
	if len(p.outbound) != 1 {
		t.Fatalf("outbound len: want 1, got %d", len(p.outbound))
	}
}

func TestPump_Enqueue_Full_ReturnsFalse(t *testing.T) {
	p := &Pump{outbound: make(chan *pb.ToRadio, 4)}
	env := &pb.ToRadio{}
	for i := range 4 {
		if !p.Enqueue(env) {
			t.Fatalf("Enqueue failed at index %d (should still have room)", i)
		}
	}
	if p.Enqueue(env) {
		t.Fatal("Enqueue returned true on full buffer")
	}
}

// ---- mockTransport ----------------------------------------------------------

// mockTransport is the minimal Transport implementation used to test
// setClient / getClient without starting a real connection.
type mockTransport struct{}

func (*mockTransport) Run(
	_ context.Context,
	_ chan<- *pb.FromRadio,
	_ <-chan *pb.ToRadio,
) error {
	return nil
}

func (*mockTransport) Close() error { return nil }

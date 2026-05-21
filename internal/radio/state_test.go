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
	"testing"
)

// TestNewState verifies NewState returns a properly initialized State
// with all maps non-nil and all slice fields empty so callers can
// append and index without nil-map panics on first use.
func TestNewState(t *testing.T) {
	t.Parallel()

	s := NewState()
	if s == nil {
		t.Fatal("NewState() returned nil")
	}

	// All four maps must be non-nil so the first write doesn't panic.
	if s.NodesByNum == nil {
		t.Error("NodesByNum is nil; expected initialized map")
	}
	if s.MessagesByPacketID == nil {
		t.Error("MessagesByPacketID is nil; expected initialized map")
	}
	if s.PeerPositions == nil {
		t.Error("PeerPositions is nil; expected initialized map")
	}
	if s.PeerEnv == nil {
		t.Error("PeerEnv is nil; expected initialized map")
	}
	if s.Ignored == nil {
		t.Error("Ignored is nil; expected initialized map")
	}

	// Slice fields must be nil (empty) — not allocated until first append.
	if len(s.Channels) != 0 {
		t.Errorf("Channels len = %d, want 0", len(s.Channels))
	}
	if len(s.Nodes) != 0 {
		t.Errorf("Nodes len = %d, want 0", len(s.Nodes))
	}
	if len(s.Messages) != 0 {
		t.Errorf("Messages len = %d, want 0", len(s.Messages))
	}

	// Zero-value fields should remain at their defaults.
	if s.Connected {
		t.Error("Connected = true, want false")
	}
	if s.MyNodeNum != 0 {
		t.Errorf("MyNodeNum = %d, want 0", s.MyNodeNum)
	}
	if s.RadioID != "" {
		t.Errorf("RadioID = %q, want empty", s.RadioID)
	}
}

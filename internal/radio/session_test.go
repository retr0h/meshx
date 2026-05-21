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
	"sync"
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// sessionFakePump satisfies Pump for session-level tests. Tracks
// Stop calls and holds a toggle to simulate buffer-full rejection.
type sessionFakePump struct {
	mu       sync.Mutex
	stopped  bool
	rejected bool
	nextID   uint32
}

func (p *sessionFakePump) Send(mdl.Command) (uint32, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rejected {
		return 0, false
	}
	p.nextID++
	return p.nextID, true
}

func (p *sessionFakePump) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
}

// sessionFakeStore satisfies Store with configurable error injection for
// every method the session layer calls.
type sessionFakeStore struct {
	mu            sync.Mutex
	putSettingErr error
	saveNodeErr   error
	calls         []string
}

func (s *sessionFakeStore) record(m string) {
	s.mu.Lock()
	s.calls = append(s.calls, m)
	s.mu.Unlock()
}

func (s *sessionFakeStore) ResolveRadioByConnection(_, _ string) (string, error) {
	s.record("ResolveRadioByConnection")
	return "", nil
}

func (s *sessionFakeStore) ClaimRadioIdentity(_ string, _ uint32) (string, error) {
	s.record("ClaimRadioIdentity")
	return "", nil
}

func (s *sessionFakeStore) SaveMessage(_, _ string, _ mdl.Message) error {
	s.record("SaveMessage")
	return nil
}

func (s *sessionFakeStore) LoadMessages(_, _ string, _ int) ([]mdl.Message, error) {
	s.record("LoadMessages")
	return nil, nil
}

func (s *sessionFakeStore) ExpireStalePendingMessages(_ string, _ time.Duration) (int, error) {
	s.record("ExpireStalePendingMessages")
	return 0, nil
}

func (s *sessionFakeStore) SaveNode(_ string, _ mdl.CachedNode) error {
	s.record("SaveNode")
	return s.saveNodeErr
}

func (s *sessionFakeStore) LoadNodes(_ string) ([]mdl.CachedNode, error) {
	s.record("LoadNodes")
	return nil, nil
}

func (s *sessionFakeStore) SaveNodePrefs(_ string, _ uint32, _, _ bool) error {
	s.record("SaveNodePrefs")
	return nil
}

func (s *sessionFakeStore) GetSetting(_, _ string) (string, bool, error) {
	s.record("GetSetting")
	return "", false, nil
}

func (s *sessionFakeStore) PutSetting(_, _, _ string) error {
	s.record("PutSetting")
	return s.putSettingErr
}

func (s *sessionFakeStore) ConsumeBootNotes() []string {
	s.record("ConsumeBootNotes")
	return nil
}

func (s *sessionFakeStore) Close() error {
	s.record("Close")
	return nil
}

// TestNew verifies New wires the provided pump, store, and state
// correctly; nil state triggers automatic NewState() allocation.
func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("nil-state-allocates-new", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		if sess == nil {
			t.Fatal("New returned nil")
		}
		if sess.State == nil {
			t.Fatal("State is nil after New(nil, nil, nil)")
		}
		if sess.State.NodesByNum == nil {
			t.Fatal("State.NodesByNum is nil — NewState() not called")
		}
	})

	t.Run("provided-state-is-used", func(_ *testing.T) {
		st := NewState()
		st.RadioID = "0xdeadbeef"
		sess := New(st, nil, nil)
		if sess.State != st {
			t.Fatal("Session.State != provided State pointer")
		}
		if sess.State.RadioID != "0xdeadbeef" {
			t.Fatalf("RadioID = %q, want 0xdeadbeef", sess.State.RadioID)
		}
	})

	t.Run("pump-and-store-wired", func(_ *testing.T) {
		pump := &sessionFakePump{}
		store := &sessionFakeStore{}
		sess := New(nil, pump, store)
		if sess.pump != pump {
			t.Fatal("pump not wired")
		}
		if sess.store != store {
			t.Fatal("store not wired")
		}
	})
}

// TestSession_GetState verifies the pointer returned by GetState is
// the same object the Session was constructed with.
func TestSession_GetState(t *testing.T) {
	t.Parallel()

	st := NewState()
	sess := New(st, nil, nil)
	if got := sess.GetState(); got != st {
		t.Fatalf("GetState() returned different pointer; want identity")
	}
}

// TestSession_Send exercises the nil-pump path (returns 0, false) and
// the live-pump path (returns a non-zero packet id).
func TestSession_Send(t *testing.T) {
	t.Parallel()

	t.Run("nil-pump-returns-zero-false", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		pid, ok := sess.Send(mdl.SendText{Text: "hi"})
		if ok || pid != 0 {
			t.Fatalf("Send with nil pump: got (%d, %v), want (0, false)", pid, ok)
		}
	})

	t.Run("live-pump-returns-id-true", func(_ *testing.T) {
		pump := &sessionFakePump{}
		sess := New(nil, pump, nil)
		pid, ok := sess.Send(mdl.SendText{Text: "hi"})
		if !ok || pid == 0 {
			t.Fatalf("Send with live pump: got (%d, %v), want (>0, true)", pid, ok)
		}
	})

	t.Run("rejected-pump-returns-zero-false", func(_ *testing.T) {
		pump := &sessionFakePump{rejected: true}
		sess := New(nil, pump, nil)
		pid, ok := sess.Send(mdl.SendText{Text: "hi"})
		if ok || pid != 0 {
			t.Fatalf("Send with rejected pump: got (%d, %v), want (0, false)", pid, ok)
		}
	})
}

// TestSession_Stop verifies Stop is idempotent on a nil pump and
// calls pump.Stop() exactly once on a live pump.
func TestSession_Stop(t *testing.T) {
	t.Parallel()

	t.Run("nil-pump-no-panic", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		sess.Stop() // must not panic
	})

	t.Run("live-pump-stopped", func(_ *testing.T) {
		pump := &sessionFakePump{}
		sess := New(nil, pump, nil)
		sess.Stop()
		if !pump.stopped {
			t.Fatal("pump.Stop() not called")
		}
	})
}

// TestSession_AttachPump verifies AttachPump replaces the current pump.
func TestSession_AttachPump(t *testing.T) {
	t.Parallel()

	sess := New(nil, nil, nil)
	pump := &sessionFakePump{}
	sess.AttachPump(pump)
	if sess.pump != pump {
		t.Fatal("pump not replaced after AttachPump")
	}
	// Confirm Send now works through the new pump.
	_, ok := sess.Send(mdl.RequestSync{})
	if !ok {
		t.Fatal("Send returned false after AttachPump with live pump")
	}
}

// TestSession_AttachStore verifies AttachStore replaces the store handle.
func TestSession_AttachStore(t *testing.T) {
	t.Parallel()

	sess := New(nil, nil, nil)
	store := &sessionFakeStore{}
	sess.AttachStore(store)
	if sess.store != store {
		t.Fatal("store not replaced after AttachStore")
	}
}

// TestSession_PumpHandle verifies PumpHandle returns the current pump
// (which may be nil before AttachPump is called).
func TestSession_PumpHandle(t *testing.T) {
	t.Parallel()

	t.Run("nil-before-attach", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		if sess.PumpHandle() != nil {
			t.Fatal("PumpHandle() should be nil before AttachPump")
		}
	})

	t.Run("non-nil-after-attach", func(_ *testing.T) {
		pump := &sessionFakePump{}
		sess := New(nil, pump, nil)
		if got := sess.PumpHandle(); got != pump {
			t.Fatal("PumpHandle() returned wrong pump")
		}
	})
}

// TestSession_StoreHandle verifies StoreHandle returns the current store.
func TestSession_StoreHandle(t *testing.T) {
	t.Parallel()

	t.Run("nil-before-attach", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		if sess.StoreHandle() != nil {
			t.Fatal("StoreHandle() should be nil before AttachStore")
		}
	})

	t.Run("non-nil-after-attach", func(_ *testing.T) {
		store := &sessionFakeStore{}
		sess := New(nil, nil, store)
		if got := sess.StoreHandle(); got != store {
			t.Fatal("StoreHandle() returned wrong store")
		}
	})
}

// TestSession_AlertStorageError verifies the once-per-session gate:
// the first error appends a system row and sets StorageAlerted; every
// subsequent call is a no-op. A nil error is always a no-op.
func TestSession_AlertStorageError(t *testing.T) {
	t.Parallel()

	t.Run("nil-error-is-noop", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		sess.AlertStorageError(nil)
		if sess.State.StorageAlerted {
			t.Fatal("StorageAlerted = true after nil error")
		}
		if len(sess.State.Messages) != 0 {
			t.Fatal("Messages non-empty after nil error")
		}
	})

	t.Run("first-error-appends-system-row", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		sess.AlertStorageError(errors.New("bolt: database too large"))
		if !sess.State.StorageAlerted {
			t.Fatal("StorageAlerted = false after first error")
		}
		if len(sess.State.Messages) != 1 {
			t.Fatalf("Messages len = %d, want 1", len(sess.State.Messages))
		}
		row := sess.State.Messages[0]
		if row.Status != mdl.StatusSystem {
			t.Fatalf("row.Status = %q, want system", row.Status)
		}
	})

	t.Run("second-error-is-noop", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		sess.AlertStorageError(errors.New("first"))
		sess.AlertStorageError(errors.New("second"))
		if len(sess.State.Messages) != 1 {
			t.Fatalf(
				"Messages len = %d, want 1 (second error must not append)",
				len(sess.State.Messages),
			)
		}
	})
}

// TestSession_PutSetting verifies PutSetting is a no-op when the store
// is nil and delegates to the store when it is present.
func TestSession_PutSetting(t *testing.T) {
	t.Parallel()

	t.Run("nil-store-noop", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		sess.PutSetting("", "key", "value") // must not panic
	})

	t.Run("live-store-delegates", func(_ *testing.T) {
		store := &sessionFakeStore{}
		sess := New(nil, nil, store)
		sess.PutSetting("0xrad", "ding_muted", "off")
		store.mu.Lock()
		defer store.mu.Unlock()
		found := false
		for _, c := range store.calls {
			if c == "PutSetting" {
				found = true
			}
		}
		if !found {
			t.Fatal("store.PutSetting not called")
		}
	})
}

// TestSession_StoreError verifies the storeError helper fires
// OnStoreError only when err != nil, and is silent when OnStoreError
// is nil even for real errors.
func TestSession_StoreError(t *testing.T) {
	t.Parallel()

	t.Run("nil-error-no-callback", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		var called bool
		sess.OnStoreError = func(error) { called = true }
		sess.storeError(nil)
		if called {
			t.Fatal("OnStoreError called for nil error")
		}
	})

	t.Run("real-error-fires-callback", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		var got error
		sess.OnStoreError = func(e error) { got = e }
		sess.storeError(errors.New("bolt: write failed"))
		if got == nil {
			t.Fatal("OnStoreError not called for real error")
		}
	})

	t.Run("nil-callback-no-panic-on-real-error", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		sess.OnStoreError = nil
		sess.storeError(errors.New("harmless")) // must not panic
	})
}

// TestSession_SaveNodePrefs verifies SaveNodePrefs is a no-op when the
// store is nil and delegates to the store when it is present.
func TestSession_SaveNodePrefs(t *testing.T) {
	t.Parallel()

	t.Run("nil-store-noop", func(_ *testing.T) {
		sess := New(nil, nil, nil)
		sess.SaveNodePrefs("0xrad", 42, true, false) // must not panic
	})

	t.Run("live-store-delegates", func(_ *testing.T) {
		store := &sessionFakeStore{}
		sess := New(nil, nil, store)
		sess.SaveNodePrefs("0xrad", 99, false, true)
		store.mu.Lock()
		defer store.mu.Unlock()
		found := false
		for _, c := range store.calls {
			if c == "SaveNodePrefs" {
				found = true
			}
		}
		if !found {
			t.Fatal("store.SaveNodePrefs not called")
		}
	})
}

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
	"sync"
	"testing"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// hydrateFakeStore is a full-Store stub the HydrateFromStore tests
// drive. Every mutable return value is settable from the test so
// individual scenarios can exercise each hydration step independently.
type hydrateFakeStore struct {
	mu sync.Mutex

	// Settable fields — set before calling HydrateFromStore.
	radioID     string
	nodes       []mdl.CachedNode
	nodesErr    error
	messages    []mdl.Message
	messagesErr error
	staleCount  int
	staleErr    error
	bootNotes   []string
}

func (s *hydrateFakeStore) ResolveRadioByConnection(_, _ string) (string, error) {
	return s.radioID, nil
}

func (s *hydrateFakeStore) ClaimRadioIdentity(_ string, _ uint32) (string, error) {
	return s.radioID, nil
}
func (s *hydrateFakeStore) SaveMessage(_, _ string, _ mdl.Message) error { return nil }
func (s *hydrateFakeStore) LoadMessages(_, _ string, _ int) ([]mdl.Message, error) {
	return s.messages, s.messagesErr
}

func (s *hydrateFakeStore) ExpireStalePendingMessages(_ string, _ time.Duration) (int, error) {
	return s.staleCount, s.staleErr
}
func (s *hydrateFakeStore) SaveNode(_ string, _ mdl.CachedNode) error { return nil }
func (s *hydrateFakeStore) LoadNodes(_ string) ([]mdl.CachedNode, error) {
	return s.nodes, s.nodesErr
}
func (s *hydrateFakeStore) SaveNodePrefs(_ string, _ uint32, _, _ bool) error { return nil }

func (s *hydrateFakeStore) GetSetting(
	_, _ string,
) (string, bool, error) {
	return "", false, nil
}
func (s *hydrateFakeStore) PutSetting(_, _, _ string) error { return nil }
func (s *hydrateFakeStore) ConsumeBootNotes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bootNotes
}
func (s *hydrateFakeStore) Close() error { return nil }

// TestSession_HydrateFromStore covers each step of the hydration
// pipeline: nil-store short-circuit, node loading, stale-pending sweep,
// message loading + ghost creation, last-heard backfill, and boot notes.
func TestSession_HydrateFromStore(t *testing.T) {
	t.Parallel()

	t.Run("nil-store-returns-empty-result", func(t *testing.T) {
		s := newTestSession()
		res := s.HydrateFromStore(HydrationOptions{})
		if res.MessagesLoaded != 0 || res.NodesLoaded != 0 || res.GhostsCreated != 0 {
			t.Fatalf("expected empty result; got %+v", res)
		}
	})

	t.Run("nodes-loaded-and-indexed", func(t *testing.T) {
		store := &hydrateFakeStore{
			nodes: []mdl.CachedNode{
				{NodeNum: 0x1111, LongName: "Alice", ShortName: "ALIC"},
				{NodeNum: 0x2222, LongName: "Bob", ShortName: "BOB!"},
			},
		}
		s := New(nil, nil, store)

		res := s.HydrateFromStore(HydrationOptions{})

		if res.NodesLoaded != 2 {
			t.Fatalf("NodesLoaded = %d, want 2", res.NodesLoaded)
		}
		if len(s.State.Nodes) != 2 {
			t.Fatalf("State.Nodes len = %d, want 2", len(s.State.Nodes))
		}
		if _, ok := s.State.NodesByNum[0x1111]; !ok {
			t.Fatal("node 0x1111 not indexed in NodesByNum")
		}
	})

	t.Run("stale-pending-count-returned", func(t *testing.T) {
		store := &hydrateFakeStore{staleCount: 3}
		s := New(nil, nil, store)

		res := s.HydrateFromStore(HydrationOptions{})
		if res.StalePendingExpired != 3 {
			t.Fatalf("StalePendingExpired = %d, want 3", res.StalePendingExpired)
		}
	})

	t.Run("messages-loaded-and-indexed", func(t *testing.T) {
		store := &hydrateFakeStore{
			messages: []mdl.Message{
				{PacketID: 100, Text: "hi", FromNum: 0x3333},
				{PacketID: 200, Text: "yo", FromNum: 0x4444},
			},
		}
		s := New(nil, nil, store)

		res := s.HydrateFromStore(HydrationOptions{})

		if res.MessagesLoaded != 2 {
			t.Fatalf("MessagesLoaded = %d, want 2", res.MessagesLoaded)
		}
		if _, ok := s.State.MessagesByPacketID[100]; !ok {
			t.Fatal("PacketID 100 not indexed")
		}
	})

	t.Run("ghost-created-for-unknown-sender", func(t *testing.T) {
		const unknownNum = uint32(0xBEEF)
		store := &hydrateFakeStore{
			messages: []mdl.Message{
				{PacketID: 1, FromNum: unknownNum, Text: "hello"},
			},
		}
		s := New(nil, nil, store)

		res := s.HydrateFromStore(HydrationOptions{})

		if res.GhostsCreated != 1 {
			t.Fatalf("GhostsCreated = %d, want 1", res.GhostsCreated)
		}
		if _, ok := s.State.NodesByNum[unknownNum]; !ok {
			t.Fatal("ghost not indexed in NodesByNum")
		}
	})

	t.Run("known-sender-does-not-create-ghost", func(t *testing.T) {
		const knownNum = uint32(0xCAFE)
		store := &hydrateFakeStore{
			nodes: []mdl.CachedNode{
				{NodeNum: knownNum, LongName: "Known"},
			},
			messages: []mdl.Message{
				{PacketID: 1, FromNum: knownNum, Text: "hello"},
			},
		}
		s := New(nil, nil, store)

		res := s.HydrateFromStore(HydrationOptions{})
		if res.GhostsCreated != 0 {
			t.Fatalf("GhostsCreated = %d, want 0 (known sender)", res.GhostsCreated)
		}
	})

	t.Run("last-heard-backfilled-from-messages", func(t *testing.T) {
		const nodeNum = uint32(0xFEED)
		sentAt := time.Now().Add(-10 * time.Minute)
		store := &hydrateFakeStore{
			nodes: []mdl.CachedNode{
				{NodeNum: nodeNum, LongName: "Feed"},
			},
			messages: []mdl.Message{
				{PacketID: 1, FromNum: nodeNum, SentAt: sentAt, Text: "hi"},
			},
		}
		s := New(nil, nil, store)

		res := s.HydrateFromStore(HydrationOptions{})
		if res.LastHeardBackfilled != 1 {
			t.Fatalf("LastHeardBackfilled = %d, want 1", res.LastHeardBackfilled)
		}
		idx := s.State.NodesByNum[nodeNum]
		if !s.State.Nodes[idx].LastHeardAt.Equal(sentAt) {
			t.Fatalf("LastHeardAt not backfilled from message SentAt")
		}
	})

	t.Run("sanitizer-applied-to-messages", func(t *testing.T) {
		store := &hydrateFakeStore{
			messages: []mdl.Message{
				{PacketID: 1, FromNum: 0x1234, Text: "raw\x07text"},
			},
		}
		s := New(nil, nil, store)

		var sanitizerCalled bool
		res := s.HydrateFromStore(HydrationOptions{
			SanitizeText: func(_ string) (string, bool, bool) {
				sanitizerCalled = true
				return "cleaned", false, true
			},
		})

		if !sanitizerCalled {
			t.Fatal("sanitizer not called")
		}
		if res.MessagesLoaded != 1 {
			t.Fatalf("MessagesLoaded = %d, want 1", res.MessagesLoaded)
		}
		if s.State.Messages[0].Text != "cleaned" {
			t.Fatalf("Messages[0].Text = %q, want cleaned", s.State.Messages[0].Text)
		}
	})

	t.Run("boot-notes-returned", func(t *testing.T) {
		store := &hydrateFakeStore{
			bootNotes: []string{"migrated schema v3 → v4"},
		}
		s := New(nil, nil, store)

		res := s.HydrateFromStore(HydrationOptions{})
		if len(res.BootNotes) != 1 || res.BootNotes[0] != "migrated schema v3 → v4" {
			t.Fatalf("BootNotes = %v, want 1 entry", res.BootNotes)
		}
	})

	t.Run("dest-resolver-sets-radio-id", func(t *testing.T) {
		store := &hydrateFakeStore{radioID: "0xdeadbeef"}
		s := New(nil, nil, store)

		s.HydrateFromStore(HydrationOptions{
			Dest: "usb:/dev/cu.usbserial-0001",
			ResolveRadioByConnection: func(_, _ string) (string, error) {
				return "0xdeadbeef", nil
			},
			ParseRadioDest: func(_ string) (string, string) {
				return "usb", "/dev/cu.usbserial-0001"
			},
		})

		if s.State.RadioID != "0xdeadbeef" {
			t.Fatalf("RadioID = %q, want 0xdeadbeef", s.State.RadioID)
		}
		// MyNodeNum should be parsed from the hex suffix.
		if s.State.MyNodeNum != 0xdeadbeef {
			t.Fatalf("MyNodeNum = 0x%x, want 0xdeadbeef", s.State.MyNodeNum)
		}
	})

	t.Run("zero-packet-id-messages-not-indexed", func(t *testing.T) {
		store := &hydrateFakeStore{
			messages: []mdl.Message{
				{PacketID: 0, Text: "system", FromNum: 0},
			},
		}
		s := New(nil, nil, store)
		s.HydrateFromStore(HydrationOptions{})
		if _, ok := s.State.MessagesByPacketID[0]; ok {
			t.Fatal("PacketID=0 must not be indexed")
		}
	})
}

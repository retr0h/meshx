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

package storage_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/storage"
)

// newTestBolt opens a Bolt store under t.TempDir and registers Close via
// t.Cleanup.
func newTestBolt(t *testing.T) *storage.Bolt {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.bolt")
	b, err := storage.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// ---- helpers ----------------------------------------------------------------

func mustSaveMessage(t *testing.T, b *storage.Bolt, radioID, channel string, msg model.Message) {
	t.Helper()
	if err := b.SaveMessage(radioID, channel, msg); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
}

func mustSaveNode(t *testing.T, b *storage.Bolt, radioID string, n model.CachedNode) {
	t.Helper()
	if err := b.SaveNode(radioID, n); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
}

// ---- New / Close ------------------------------------------------------------

func TestNewAndClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meshx.bolt")
	b, err := storage.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNilReceiver_Close(t *testing.T) {
	var b *storage.Bolt
	if err := b.Close(); err != nil {
		t.Fatalf("nil Close: %v", err)
	}
}

// ---- ConsumeBootNotes -------------------------------------------------------

func TestConsumeBootNotes_AlwaysNil(t *testing.T) {
	b := newTestBolt(t)
	notes := b.ConsumeBootNotes()
	if notes != nil {
		t.Fatalf("expected nil, got %v", notes)
	}
}

// ---- ParseRadioDest --------------------------------------------------------

func TestParseRadioDest(t *testing.T) {
	tests := []struct {
		name      string
		dest      string
		wantTrans string
		wantAddr  string
	}{
		{"empty", "", "unknown", "unknown"},
		{"ble", "ble:AABB-CCDD", "ble", "AABB-CCDD"},
		{"tcp", "radio.local:4403", "tcp", "radio.local:4403"},
		{"usb", "/dev/cu.usbserial-0001", "usb", "/dev/cu.usbserial-0001"},
		{"whitespace", "  ble:uuid ", "ble", "uuid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, addr := storage.ParseRadioDest(tt.dest)
			if tr != tt.wantTrans {
				t.Errorf("transport: got %q, want %q", tr, tt.wantTrans)
			}
			if addr != tt.wantAddr {
				t.Errorf("addr: got %q, want %q", addr, tt.wantAddr)
			}
		})
	}
}

// ---- RadioIDFromNodeNum / PendingRadioID / IsPlaceholderRadioID ------------

func TestRadioIDFromNodeNum(t *testing.T) {
	got := storage.RadioIDFromNodeNum(0x103e034d)
	if got != "0x103e034d" {
		t.Fatalf("got %q, want %q", got, "0x103e034d")
	}
}

func TestPendingRadioID(t *testing.T) {
	got := storage.PendingRadioID("ble", "uuid-1")
	if got != "pending:ble:uuid-1" {
		t.Fatalf("got %q", got)
	}
}

func TestIsPlaceholderRadioID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"pending:ble:uuid", true},
		{"pending:tcp:host:4403", true},
		{"0x103e034d", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := storage.IsPlaceholderRadioID(tt.id); got != tt.want {
			t.Errorf("IsPlaceholderRadioID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

// ---- ResolveRadioByConnection / ClaimRadioIdentity -------------------------

func TestResolveRadioByConnection_NewConnection(t *testing.T) {
	b := newTestBolt(t)
	id, err := b.ResolveRadioByConnection("ble", "uuid-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if id != "pending:ble:uuid-1" {
		t.Fatalf("got %q, want pending placeholder", id)
	}
}

func TestResolveRadioByConnection_Idempotent(t *testing.T) {
	b := newTestBolt(t)
	id1, _ := b.ResolveRadioByConnection("ble", "uuid-1")
	id2, _ := b.ResolveRadioByConnection("ble", "uuid-1")
	if id1 != id2 {
		t.Fatalf("expected same id, got %q vs %q", id1, id2)
	}
}

func TestResolveRadioByConnection_DifferentConnections(t *testing.T) {
	b := newTestBolt(t)
	id1, _ := b.ResolveRadioByConnection("ble", "uuid-1")
	id2, _ := b.ResolveRadioByConnection("usb", "/dev/tty.usb0")
	if id1 == id2 {
		t.Fatalf("expected distinct ids, both got %q", id1)
	}
}

func TestResolveRadioByConnection_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	id, err := b.ResolveRadioByConnection("ble", "x")
	if err != nil {
		t.Fatal(err)
	}
	if id != "" {
		t.Fatalf("expected empty, got %q", id)
	}
}

func TestClaimRadioIdentity_PromotesPlaceholder(t *testing.T) {
	b := newTestBolt(t)
	pending, _ := b.ResolveRadioByConnection("ble", "uuid-1")
	// Save some data under the pending id so we can verify migration.
	mustSaveMessage(t, b, pending, "LongFast", model.Message{
		Time:   "09:00",
		From:   "Alice",
		Text:   "hello",
		SentAt: time.Now(),
	})

	canonical, err := b.ClaimRadioIdentity(pending, 0x103e034d)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if canonical != "0x103e034d" {
		t.Fatalf("got %q", canonical)
	}

	// Message should be accessible under the canonical id.
	msgs, err := b.LoadMessages(canonical, "LongFast", 10)
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Text != "hello" {
		t.Fatalf("expected migrated message, got %v", msgs)
	}
}

func TestClaimRadioIdentity_AlreadyCanonical(t *testing.T) {
	b := newTestBolt(t)
	canonical := "0x103e034d"
	got, err := b.ClaimRadioIdentity(canonical, 0x103e034d)
	if err != nil {
		t.Fatal(err)
	}
	if got != canonical {
		t.Fatalf("got %q, want %q", got, canonical)
	}
}

func TestClaimRadioIdentity_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	got, err := b.ClaimRadioIdentity("pending:ble:x", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0x00000001" {
		t.Fatalf("got %q", got)
	}
}

func TestClaimRadioIdentity_CanonicalAlreadyExists(t *testing.T) {
	b := newTestBolt(t)
	// Pre-create the canonical bucket by resolving and claiming once.
	pending1, _ := b.ResolveRadioByConnection("ble", "uuid-1")
	_, _ = b.ClaimRadioIdentity(pending1, 0x103e034d)

	// Now a second connection races and also tries to claim the same
	// node num from a different placeholder.
	pending2, _ := b.ResolveRadioByConnection("usb", "/dev/tty.x")
	got, err := b.ClaimRadioIdentity(pending2, 0x103e034d)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if got != "0x103e034d" {
		t.Fatalf("got %q", got)
	}
}

// ---- SaveMessage / LoadMessages / ExpireStalePendingMessages ---------------

func TestSaveAndLoadMessages_BasicRoundtrip(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000001"
	ch := "LongFast"

	msgs := []model.Message{
		{Time: "08:00", From: "Alice", Text: "first", SentAt: time.Now()},
		{Time: "08:01", From: "Bob", Text: "second", SentAt: time.Now()},
		{Time: "08:02", From: "Alice", Text: "third", SentAt: time.Now()},
	}
	for _, m := range msgs {
		mustSaveMessage(t, b, radio, ch, m)
	}

	got, err := b.LoadMessages(radio, ch, 10)
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 messages, got %d", len(got))
	}
	// Oldest first.
	if got[0].Text != "first" || got[2].Text != "third" {
		t.Fatalf("wrong order: %v", got)
	}
}

func TestLoadMessages_Limit(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000002"
	ch := "LongFast"
	for i := range 10 {
		mustSaveMessage(t, b, radio, ch, model.Message{
			Time:   "09:00",
			From:   "X",
			Text:   string(rune('a' + i)),
			SentAt: time.Now(),
		})
	}
	got, err := b.LoadMessages(radio, ch, 3)
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
}

func TestLoadMessages_ZeroLimitReturnsNil(t *testing.T) {
	b := newTestBolt(t)
	mustSaveMessage(t, b, "0xabc", "ch", model.Message{
		Time: "09:00", From: "X", Text: "hi", SentAt: time.Now(),
	})
	got, err := b.LoadMessages("0xabc", "ch", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestLoadMessages_ChannelFilter(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000003"
	mustSaveMessage(
		t,
		b,
		radio,
		"ch1",
		model.Message{Time: "09:00", From: "A", Text: "in-ch1", SentAt: time.Now()},
	)
	mustSaveMessage(
		t,
		b,
		radio,
		"ch2",
		model.Message{Time: "09:01", From: "B", Text: "in-ch2", SentAt: time.Now()},
	)

	got, err := b.LoadMessages(radio, "ch1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "in-ch1" {
		t.Fatalf("channel filter failed: %v", got)
	}
}

func TestLoadMessages_EmptyChannelReturnsAll(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000004"
	mustSaveMessage(
		t,
		b,
		radio,
		"ch1",
		model.Message{Time: "09:00", From: "A", Text: "a", SentAt: time.Now()},
	)
	mustSaveMessage(
		t,
		b,
		radio,
		"ch2",
		model.Message{Time: "09:01", From: "B", Text: "b", SentAt: time.Now()},
	)

	got, err := b.LoadMessages(radio, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
}

func TestSaveMessage_SkipsSystemRows(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000005"
	mustSaveMessage(t, b, radio, "LongFast", model.Message{
		Status: model.StatusSystem,
		Text:   "should not persist",
	})
	mustSaveMessage(t, b, radio, "LongFast", model.Message{
		Status: model.StatusNotice,
		Text:   "also skipped",
	})
	got, err := b.LoadMessages(radio, "LongFast", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no messages, got %v", got)
	}
}

func TestSaveMessage_UpdatesExistingByPacketID(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000006"
	ch := "LongFast"
	now := time.Now()

	mustSaveMessage(t, b, radio, ch, model.Message{
		Time:     "09:00",
		From:     "Alice",
		Text:     "hello",
		PacketID: 42,
		Status:   model.StatusPending,
		SentAt:   now,
	})
	// Update delivery state.
	mustSaveMessage(t, b, radio, ch, model.Message{
		Time:     "09:00",
		From:     "Alice",
		Text:     "hello",
		PacketID: 42,
		Status:   model.StatusAck,
		SentAt:   now,
	})

	got, err := b.LoadMessages(radio, ch, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 message, got %d", len(got))
	}
	if got[0].Status != model.StatusAck {
		t.Fatalf("want StatusAck, got %v", got[0].Status)
	}
}

func TestLoadMessages_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	msgs, err := b.LoadMessages("x", "y", 10)
	if err != nil {
		t.Fatal(err)
	}
	if msgs != nil {
		t.Fatalf("expected nil, got %v", msgs)
	}
}

func TestExpireStalePendingMessages(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000007"
	ch := "LongFast"

	staleTime := time.Now().Add(-10 * time.Minute)
	freshTime := time.Now()

	mustSaveMessage(t, b, radio, ch, model.Message{
		PacketID: 1,
		Status:   model.StatusPending,
		SentAt:   staleTime,
		Text:     "stale",
	})
	mustSaveMessage(t, b, radio, ch, model.Message{
		PacketID: 2,
		Status:   model.StatusPending,
		SentAt:   freshTime,
		Text:     "fresh",
	})
	mustSaveMessage(t, b, radio, ch, model.Message{
		PacketID: 3,
		Status:   model.StatusAck,
		SentAt:   staleTime,
		Text:     "acked — should not change",
	})

	n, err := b.ExpireStalePendingMessages(radio, 5*time.Minute)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 expired, got %d", n)
	}

	msgs, _ := b.LoadMessages(radio, ch, 10)
	statuses := map[uint32]model.MessageStatus{}
	for _, m := range msgs {
		statuses[m.PacketID] = m.Status
	}
	if statuses[1] != model.StatusFail {
		t.Errorf("stale row: want fail, got %q", statuses[1])
	}
	if statuses[2] != model.StatusPending {
		t.Errorf("fresh row: want pending, got %q", statuses[2])
	}
	if statuses[3] != model.StatusAck {
		t.Errorf("acked row: want ack, got %q", statuses[3])
	}
}

func TestExpireStalePendingMessages_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	n, err := b.ExpireStalePendingMessages("x", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

// ---- SaveNode / LoadNodes / SaveNodePrefs -----------------------------------

func TestSaveAndLoadNodes(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000010"

	nodes := []model.CachedNode{
		{NodeNum: 1, LongName: "Alice", ShortName: "ALIC", HwModel: "HELTEC_V3"},
		{NodeNum: 2, LongName: "Bob", ShortName: "BOB_", HwModel: "TBEAM"},
	}
	for _, n := range nodes {
		mustSaveNode(t, b, radio, n)
	}

	got, err := b.LoadNodes(radio)
	if err != nil {
		t.Fatalf("LoadNodes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(got))
	}
}

func TestSaveNode_UpdatesExisting(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000011"
	mustSaveNode(t, b, radio, model.CachedNode{NodeNum: 7, LongName: "Old", ShortName: "OLD_"})
	mustSaveNode(t, b, radio, model.CachedNode{NodeNum: 7, LongName: "New", ShortName: "NEW_"})

	got, err := b.LoadNodes(radio)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].LongName != "New" {
		t.Fatalf("expected updated node, got %v", got)
	}
}

func TestSaveNode_SkipsEmptyCallsigns(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000012"
	if err := b.SaveNode(radio, model.CachedNode{NodeNum: 5}); err != nil {
		t.Fatal(err)
	}
	got, err := b.LoadNodes(radio)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no nodes, got %v", got)
	}
}

func TestSaveNode_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	if err := b.SaveNode("x", model.CachedNode{NodeNum: 1, LongName: "A", ShortName: "A"}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadNodes_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	got, err := b.LoadNodes("x")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestSaveNodePrefs_UpdatesPrefsOnly(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000013"
	mustSaveNode(t, b, radio, model.CachedNode{NodeNum: 9, LongName: "Carol", ShortName: "CARL"})

	if err := b.SaveNodePrefs(radio, 9, true, false); err != nil {
		t.Fatalf("SaveNodePrefs: %v", err)
	}
	got, err := b.LoadNodes(radio)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Favorite || got[0].Muted {
		t.Fatalf("unexpected prefs: %v", got)
	}
	// LongName must be preserved.
	if got[0].LongName != "Carol" {
		t.Fatalf("LongName clobbered: %q", got[0].LongName)
	}
}

func TestSaveNodePrefs_GhostPeer(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000014"
	// No SaveNode first — prefs-only write for an unknown peer.
	if err := b.SaveNodePrefs(radio, 77, false, true); err != nil {
		t.Fatalf("SaveNodePrefs ghost: %v", err)
	}
	got, err := b.LoadNodes(radio)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].NodeNum != 77 || !got[0].Muted {
		t.Fatalf("ghost prefs: %v", got)
	}
}

func TestSaveNodePrefs_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	if err := b.SaveNodePrefs("x", 1, true, true); err != nil {
		t.Fatal(err)
	}
}

// ---- GetSetting / PutSetting -----------------------------------------------

func TestPutAndGetSetting_RadioScoped(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000020"

	if err := b.PutSetting(radio, "radio_buzzer", "on"); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	v, ok, _ := b.GetSetting(radio, "radio_buzzer")
	if !ok {
		t.Fatal("GetSetting: not found")
	}
	if v != "on" {
		t.Fatalf("got %q, want %q", v, "on")
	}
}

func TestPutAndGetSetting_Global(t *testing.T) {
	b := newTestBolt(t)

	if err := b.PutSetting("", "ding_muted", "on"); err != nil {
		t.Fatalf("PutSetting global: %v", err)
	}
	v, ok, _ := b.GetSetting("", "ding_muted")
	if !ok {
		t.Fatal("GetSetting global: not found")
	}
	if v != "on" {
		t.Fatalf("got %q, want %q", v, "on")
	}
}

func TestGetSetting_MissingReturnsEmpty(t *testing.T) {
	b := newTestBolt(t)
	v, ok, _ := b.GetSetting("0xabc", "no_such_key")
	if ok || v != "" {
		t.Fatalf("expected not-found, got %q %v", v, ok)
	}
}

func TestPutSetting_OverwritesValue(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000021"
	_ = b.PutSetting(radio, "k", "old")
	_ = b.PutSetting(radio, "k", "new")
	v, ok, _ := b.GetSetting(radio, "k")
	if !ok || v != "new" {
		t.Fatalf("got %q %v", v, ok)
	}
}

func TestGetSetting_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	v, ok, _ := b.GetSetting("x", "k")
	if ok || v != "" {
		t.Fatalf("expected not-found, got %q %v", v, ok)
	}
}

func TestPutSetting_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	if err := b.PutSetting("x", "k", "v"); err != nil {
		t.Fatal(err)
	}
}

// ---- BLE devices ------------------------------------------------------------

func mustSaveBLE(t *testing.T, b *storage.Bolt, d model.BLEDevice) {
	t.Helper()
	if err := b.SaveBLEDevice(d); err != nil {
		t.Fatalf("SaveBLEDevice: %v", err)
	}
}

func TestSaveAndLoadBLEDevices(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-1", LongName: "Alpha", ShortName: "ALPH"})
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-2", LongName: "Beta", ShortName: "BETA"})

	devs, err := b.LoadBLEDevices()
	if err != nil {
		t.Fatalf("LoadBLEDevices: %v", err)
	}
	if len(devs) != 2 {
		t.Fatalf("want 2 devices, got %d", len(devs))
	}
}

func TestSaveBLEDevice_RequiresUUID(t *testing.T) {
	b := newTestBolt(t)
	err := b.SaveBLEDevice(model.BLEDevice{LongName: "no uuid"})
	if err == nil {
		t.Fatal("expected error for empty UUID")
	}
}

func TestSaveBLEDevice_PreservesFavoriteOnUpdate(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-1", LongName: "Alpha"})
	_ = b.SetBLEFavorite("uuid-1")

	// Re-save (update) — favorite must survive.
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-1", LongName: "Alpha Updated"})
	devs, _ := b.LoadBLEDevices()
	if len(devs) != 1 || !devs[0].Favorite {
		t.Fatalf("favorite clobbered: %v", devs)
	}
}

func TestLoadBLEDevices_FavoritesFirst(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-a", LongName: "A"})
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-b", LongName: "B"})
	_ = b.SetBLEFavorite("uuid-b")

	devs, err := b.LoadBLEDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 || devs[0].UUID != "uuid-b" {
		t.Fatalf("favorite not first: %v", devs)
	}
}

func TestLoadBLEDevices_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	devs, err := b.LoadBLEDevices()
	if err != nil {
		t.Fatal(err)
	}
	if devs != nil {
		t.Fatalf("expected nil, got %v", devs)
	}
}

func TestLookupBLEDevice_ByUUID(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-x", LongName: "Xray"})
	d, err := b.LookupBLEDevice("uuid-x")
	if err != nil || d == nil || d.UUID != "uuid-x" {
		t.Fatalf("lookup by uuid: %v %v", d, err)
	}
}

func TestLookupBLEDevice_ByLongName(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-y", LongName: "Yankee"})
	d, err := b.LookupBLEDevice("yankee")
	if err != nil || d == nil || d.UUID != "uuid-y" {
		t.Fatalf("lookup by longname: %v %v", d, err)
	}
}

func TestLookupBLEDevice_ByShortName(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-z", LongName: "Zulu", ShortName: "ZULU"})
	d, err := b.LookupBLEDevice("ZULU")
	if err != nil || d == nil || d.UUID != "uuid-z" {
		t.Fatalf("lookup by shortname: %v %v", d, err)
	}
}

func TestLookupBLEDevice_NotFound(t *testing.T) {
	b := newTestBolt(t)
	d, err := b.LookupBLEDevice("does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if d != nil {
		t.Fatalf("expected nil, got %v", d)
	}
}

func TestSetBLEFavorite_SingleFavorite(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-1", LongName: "One"})
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-2", LongName: "Two"})

	_ = b.SetBLEFavorite("uuid-1")

	devs, _ := b.LoadBLEDevices()
	favCount := 0
	for _, d := range devs {
		if d.Favorite {
			favCount++
			if d.UUID != "uuid-1" {
				t.Fatalf("wrong device favored: %q", d.UUID)
			}
		}
	}
	if favCount != 1 {
		t.Fatalf("want exactly 1 favorite, got %d", favCount)
	}
}

func TestSetBLEFavorite_ClearAll(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-1", LongName: "One"})
	_ = b.SetBLEFavorite("uuid-1")
	_ = b.SetBLEFavorite("") // clear

	devs, _ := b.LoadBLEDevices()
	for _, d := range devs {
		if d.Favorite {
			t.Fatalf("expected no favorites, got %q", d.UUID)
		}
	}
}

func TestSetBLEFavorite_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	if err := b.SetBLEFavorite("x"); err != nil {
		t.Fatal(err)
	}
}

func TestForgetBLEDevice_RemovesDevice(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "uuid-del", LongName: "Gone"})
	if err := b.ForgetBLEDevice("uuid-del"); err != nil {
		t.Fatalf("ForgetBLEDevice: %v", err)
	}
	devs, _ := b.LoadBLEDevices()
	if len(devs) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(devs))
	}
}

func TestForgetBLEDevice_Idempotent(t *testing.T) {
	b := newTestBolt(t)
	// Forgetting a non-existent device must not error.
	if err := b.ForgetBLEDevice("no-such-uuid"); err != nil {
		t.Fatal(err)
	}
}

func TestForgetBLEDevice_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	if err := b.ForgetBLEDevice("x"); err != nil {
		t.Fatal(err)
	}
}

func TestSaveBLEDevice_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	if err := b.SaveBLEDevice(model.BLEDevice{UUID: "x"}); err != nil {
		t.Fatal(err)
	}
}

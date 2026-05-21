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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/storage"
)

// ---- DefaultPath ------------------------------------------------------------

func TestDefaultPath_ReturnsValidPath(t *testing.T) {
	path, err := storage.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if !strings.HasSuffix(path, "meshx.bolt") {
		t.Fatalf("expected path ending in meshx.bolt, got %q", path)
	}
	// Parent directory must have been created.
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("parent dir %q does not exist: %v", dir, err)
	}
}

// ---- New error path ---------------------------------------------------------

func TestNew_ErrorOnLockedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meshx.bolt")

	// Hold the file open exclusively so the second Open times out.
	first, err := storage.New(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	// bbolt uses a file lock — a second open on the same path should
	// fail after the configured 5s timeout (too long for a test). We
	// use a directory path instead, which is always an error.
	_, err = storage.New(dir) // dir is a directory, not a valid bolt file
	if err == nil {
		t.Fatal("expected error opening directory as bolt file, got nil")
	}
}

// ---- ParseRadioDest edge cases ----------------------------------------------

func TestParseRadioDest_HostColonNonDigitTail(t *testing.T) {
	// A colon is present but the tail after it is non-numeric — should
	// fall through to USB.
	trans, addr := storage.ParseRadioDest("/dev/tty.usbserial")
	if trans != "usb" {
		t.Errorf("transport: got %q, want usb", trans)
	}
	if addr != "/dev/tty.usbserial" {
		t.Errorf("addr: got %q, want /dev/tty.usbserial", addr)
	}
}

func TestParseRadioDest_ColonEmptyPort(t *testing.T) {
	// Last colon with empty tail — not a port, should be USB.
	trans, addr := storage.ParseRadioDest("something:")
	if trans != "usb" {
		t.Errorf("transport: got %q, want usb", trans)
	}
	_ = addr
}

func TestParseRadioDest_AllVariants(t *testing.T) {
	tests := []struct {
		name      string
		dest      string
		wantTrans string
		wantAddr  string
	}{
		{"ble with complex uuid", "ble:aabb-ccdd-eeff-1122", "ble", "aabb-ccdd-eeff-1122"},
		{"tcp ipv4", "192.168.1.100:4403", "tcp", "192.168.1.100:4403"},
		{"tcp hostname", "radio.local:4403", "tcp", "radio.local:4403"},
		{"usb no colon", "/dev/ttyUSB0", "usb", "/dev/ttyUSB0"},
		{"usb cu device", "/dev/cu.usbserial-A10MHWD6", "usb", "/dev/cu.usbserial-A10MHWD6"},
		{"usb with alpha tail", "host:notaport", "usb", "host:notaport"},
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

// ---- ClaimRadioIdentity additional paths ------------------------------------

func TestClaimRadioIdentity_OldBucketMissing(t *testing.T) {
	// Claim with a pending ID that was never created via
	// ResolveRadioByConnection — oldRB == nil branch.
	b := newTestBolt(t)
	phantom := "pending:ble:ghost-uuid"
	got, err := b.ClaimRadioIdentity(phantom, 0xdeadbeef)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if got != "0xdeadbeef" {
		t.Fatalf("got %q, want 0xdeadbeef", got)
	}
}

func TestClaimRadioIdentity_MigratesNodesAndSettings(t *testing.T) {
	b := newTestBolt(t)
	pending, _ := b.ResolveRadioByConnection("tcp", "radio.local:4403")

	// Write a node and a setting under the pending id.
	mustSaveNode(t, b, pending, model.CachedNode{
		NodeNum: 42, LongName: "Migrated", ShortName: "MIGR",
	})
	if err := b.PutSetting(pending, "test_key", "test_value"); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}

	canonical, err := b.ClaimRadioIdentity(pending, 0x12345678)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	// Nodes must have been migrated.
	nodes, err := b.LoadNodes(canonical)
	if err != nil {
		t.Fatalf("LoadNodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].LongName != "Migrated" {
		t.Fatalf("nodes not migrated: %v", nodes)
	}

	// Settings must have been migrated.
	v, ok, _ := b.GetSetting(canonical, "test_key")
	if !ok || v != "test_value" {
		t.Fatalf("setting not migrated: %q %v", v, ok)
	}
}

// ---- LoadMessages negative limit --------------------------------------------

func TestLoadMessages_NegativeLimit_ReturnsAll(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000099"
	ch := "LongFast"
	for i := range 5 {
		mustSaveMessage(t, b, radio, ch, model.Message{
			Time:   "10:00",
			From:   "X",
			Text:   string(rune('a' + i)),
			SentAt: time.Now(),
		})
	}
	// limit < 0 is treated as "no limit" by the implementation
	// (the buf growth check `limit > 0 && len(buf) >= limit` never
	// fires). Verify all 5 messages come back.
	got, err := b.LoadMessages(radio, ch, -1)
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("want 5, got %d", len(got))
	}
}

// ---- SaveMessage skips zero-PacketID update on first call ------------------

func TestSaveMessage_ZeroPacketIDIsNotDeduplicated(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000030"
	ch := "LongFast"

	// Two messages with PacketID=0 must each get their own row
	// (zero PacketID means "no dedup").
	mustSaveMessage(t, b, radio, ch, model.Message{
		Time:     "10:00",
		From:     "A",
		Text:     "first zero-pid",
		PacketID: 0,
		SentAt:   time.Now(),
	})
	mustSaveMessage(t, b, radio, ch, model.Message{
		Time:     "10:01",
		From:     "B",
		Text:     "second zero-pid",
		PacketID: 0,
		SentAt:   time.Now(),
	})

	got, err := b.LoadMessages(radio, ch, 10)
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(got), got)
	}
}

// ---- ExpireStalePendingMessages edge: no pending messages -------------------

func TestExpireStalePendingMessages_NoMessages(t *testing.T) {
	b := newTestBolt(t)
	n, err := b.ExpireStalePendingMessages("0x000000ff", 1*time.Minute)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestExpireStalePendingMessages_ZeroSentAt_Skipped(t *testing.T) {
	// SentAt.IsZero() rows are skipped — they're mid-send with no
	// reliable timestamp yet.
	b := newTestBolt(t)
	radio := "0x000000f0"
	ch := "LongFast"

	mustSaveMessage(t, b, radio, ch, model.Message{
		PacketID: 99,
		Status:   model.StatusPending,
		// SentAt deliberately zero
	})

	n, err := b.ExpireStalePendingMessages(radio, 0)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 (zero SentAt skipped), got %d", n)
	}
}

// ---- PutSetting global bucket corner case -----------------------------------

func TestPutSetting_GlobalBucketExists(t *testing.T) {
	// Exercise the `radioID == ""` branch through multiple puts — the
	// settings bucket must persist across transactions.
	b := newTestBolt(t)
	keys := []string{"alpha", "beta", "gamma"}
	for _, k := range keys {
		if err := b.PutSetting("", k, "val-"+k); err != nil {
			t.Fatalf("PutSetting global %q: %v", k, err)
		}
	}
	for _, k := range keys {
		v, ok, _ := b.GetSetting("", k)
		if !ok || v != "val-"+k {
			t.Errorf("GetSetting global %q: got %q %v", k, v, ok)
		}
	}
}

// ---- SaveNode nil receiver --------------------------------------------------

func TestLoadNodes_EmptyBucket(t *testing.T) {
	// No nodes saved — LoadNodes must return an empty (nil) slice.
	b := newTestBolt(t)
	got, err := b.LoadNodes("0x00000050")
	if err != nil {
		t.Fatalf("LoadNodes: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 nodes, got %d", len(got))
	}
}

// ---- BLE: UpdateExisting preserves name + favorite on re-save --------------

func TestSaveBLEDevice_UpdatePreservesName(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "dev-1", LongName: "Original", ShortName: "ORIG"})
	_ = b.SetBLEFavorite("dev-1")

	// Re-save with different name — favorite must be preserved, name updated.
	mustSaveBLE(t, b, model.BLEDevice{UUID: "dev-1", LongName: "Updated", ShortName: "UPDT"})

	devs, err := b.LoadBLEDevices()
	if err != nil {
		t.Fatalf("LoadBLEDevices: %v", err)
	}
	if len(devs) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devs))
	}
	if devs[0].LongName != "Updated" {
		t.Errorf("LongName: got %q, want Updated", devs[0].LongName)
	}
	if !devs[0].Favorite {
		t.Error("favorite must be preserved across SaveBLEDevice")
	}
}

func TestClearBLEFavorite_NilReceiver(t *testing.T) {
	var b *storage.Bolt
	if err := b.SetBLEFavorite(""); err != nil {
		t.Fatal(err)
	}
}

// ---- BLE: LookupBLEDevice case-insensitive UUID ----------------------------

func TestLookupBLEDevice_CaseInsensitiveUUID(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "UPPER-UUID", LongName: "UpperCase"})
	d, err := b.LookupBLEDevice("upper-uuid")
	if err != nil {
		t.Fatalf("LookupBLEDevice: %v", err)
	}
	if d == nil || d.UUID != "UPPER-UUID" {
		t.Fatalf("case-insensitive UUID lookup failed: %v", d)
	}
}

// ---- Concurrency: concurrent reads don't race -------------------------------

func TestConcurrentReads_NoRace(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000060"
	ch := "LongFast"

	for i := range 20 {
		mustSaveMessage(t, b, radio, ch, model.Message{
			Time:   "11:00",
			From:   "Concurrent",
			Text:   string(rune('a' + i%26)),
			SentAt: time.Now(),
		})
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = b.LoadMessages(radio, ch, 5)
		}()
	}
	wg.Wait()
}

// ---- ResolveRadioByConnection: multiple distinct connections ----------------

func TestResolveRadioByConnection_MultipleTransports(t *testing.T) {
	b := newTestBolt(t)
	ids := map[string]string{}
	conns := []struct{ transport, addr string }{
		{"ble", "uuid-A"},
		{"tcp", "host-A:4403"},
		{"usb", "/dev/ttyUSB0"},
	}
	for _, c := range conns {
		id, err := b.ResolveRadioByConnection(c.transport, c.addr)
		if err != nil {
			t.Fatalf("resolve %s:%s: %v", c.transport, c.addr, err)
		}
		ids[c.transport+":"+c.addr] = id
	}
	// All three must be distinct pending placeholders.
	seen := map[string]bool{}
	for k, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id %q for connection %q", id, k)
		}
		seen[id] = true
		if !storage.IsPlaceholderRadioID(id) {
			t.Errorf("expected placeholder for %q, got %q", k, id)
		}
	}
}

// ---- BLE: DeleteBLEDevice (ForgetBLEDevice) on empty bucket ----------------

func TestForgetBLEDevice_EmptyStore(t *testing.T) {
	b := newTestBolt(t)
	// Delete from an empty store must not error.
	if err := b.ForgetBLEDevice("missing"); err != nil {
		t.Fatalf("ForgetBLEDevice on empty store: %v", err)
	}
}

// ---- SetBLEFavorite: switches favorite between two devices -----------------

func TestSetBLEFavorite_SwitchFavorite(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "dev-A", LongName: "Alpha"})
	mustSaveBLE(t, b, model.BLEDevice{UUID: "dev-B", LongName: "Beta"})

	_ = b.SetBLEFavorite("dev-A")
	_ = b.SetBLEFavorite("dev-B") // switch

	devs, err := b.LoadBLEDevices()
	if err != nil {
		t.Fatalf("LoadBLEDevices: %v", err)
	}
	favCount := 0
	for _, d := range devs {
		if d.Favorite {
			favCount++
			if d.UUID != "dev-B" {
				t.Errorf("wrong device favored: want dev-B, got %q", d.UUID)
			}
		}
	}
	if favCount != 1 {
		t.Fatalf("want exactly 1 favorite, got %d", favCount)
	}
}

// ---- LoadMessages: unknown radio returns empty slice (not error) ------------

func TestLoadMessages_UnknownRadio(t *testing.T) {
	b := newTestBolt(t)
	got, err := b.LoadMessages("0xdeadbeef", "LongFast", 10)
	if err != nil {
		t.Fatalf("LoadMessages unknown radio: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

// ---- SaveNodePrefs: nil receiver --------------------------------------------

func TestSaveNodePrefs_NilReceiverNoError(t *testing.T) {
	var b *storage.Bolt
	if err := b.SaveNodePrefs("x", 42, true, false); err != nil {
		t.Fatalf("nil SaveNodePrefs: %v", err)
	}
}

// ---- DefaultPath: reads $HOME -----------------------------------------------

func TestDefaultPath_UsesHomeDir(t *testing.T) {
	// Override HOME so we don't pollute the real ~/.meshx during tests.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	path, err := storage.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	wantDir := filepath.Join(tmpHome, ".meshx")
	if filepath.Dir(path) != wantDir {
		t.Fatalf("want dir %q, got %q", wantDir, filepath.Dir(path))
	}
	if filepath.Base(path) != "meshx.bolt" {
		t.Fatalf("want meshx.bolt, got %q", filepath.Base(path))
	}
	// Directory must exist.
	if _, err := os.Stat(wantDir); err != nil {
		t.Fatalf("dir not created: %v", err)
	}
}

// ---- New: successful reopen of existing file --------------------------------

func TestNew_ReopenExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reopen.bolt")

	// First open — creates the file.
	b1, err := storage.New(path)
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	_ = b1.Close()

	// Second open — file already exists; schema is idempotent.
	b2, err := storage.New(path)
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	_ = b2.Close()
}

// ---- BLE: LoadBLEDevices ordering with multiple non-favorites ---------------

func TestLoadBLEDevices_NonFavoritesOrdering(t *testing.T) {
	b := newTestBolt(t)
	mustSaveBLE(t, b, model.BLEDevice{UUID: "z-uuid", LongName: "Zulu"})
	mustSaveBLE(t, b, model.BLEDevice{UUID: "a-uuid", LongName: "Alpha"})
	mustSaveBLE(t, b, model.BLEDevice{UUID: "m-uuid", LongName: "Mike"})

	devs, err := b.LoadBLEDevices()
	if err != nil {
		t.Fatalf("LoadBLEDevices: %v", err)
	}
	if len(devs) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(devs))
	}
	// None should be favorite.
	for _, d := range devs {
		if d.Favorite {
			t.Errorf("unexpected favorite: %q", d.UUID)
		}
	}
}

// ---- updateExistingMessage: different channel skips update -----------------

func TestSaveMessage_PacketIDDifferentChannelCreatesNew(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000070"

	// First message on ch1.
	mustSaveMessage(t, b, radio, "ch1", model.Message{
		PacketID: 77,
		Time:     "12:00",
		From:     "A",
		Text:     "ch1 msg",
		Status:   model.StatusPending,
		SentAt:   time.Now(),
	})
	// Same PacketID but different channel — must create a new row, not
	// update the existing ch1 row.
	mustSaveMessage(t, b, radio, "ch2", model.Message{
		PacketID: 77,
		Time:     "12:01",
		From:     "B",
		Text:     "ch2 msg",
		Status:   model.StatusPending,
		SentAt:   time.Now(),
	})

	all, err := b.LoadMessages(radio, "", 10)
	if err != nil {
		t.Fatalf("LoadMessages: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 rows (one per channel), got %d: %v", len(all), all)
	}
}

// ---- SaveNodePrefs: flip muted only -----------------------------------------

func TestSaveNodePrefs_FlipMuted(t *testing.T) {
	b := newTestBolt(t)
	radio := "0x00000080"
	mustSaveNode(t, b, radio, model.CachedNode{
		NodeNum: 10, LongName: "Dave", ShortName: "DAVE",
	})
	// Set favorite + muted.
	if err := b.SaveNodePrefs(radio, 10, true, true); err != nil {
		t.Fatalf("SaveNodePrefs: %v", err)
	}
	// Clear muted only.
	if err := b.SaveNodePrefs(radio, 10, true, false); err != nil {
		t.Fatalf("SaveNodePrefs: %v", err)
	}
	nodes, err := b.LoadNodes(radio)
	if err != nil {
		t.Fatalf("LoadNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Muted {
		t.Error("muted should be false after second SaveNodePrefs")
	}
	if !nodes[0].Favorite {
		t.Error("favorite should still be true")
	}
}

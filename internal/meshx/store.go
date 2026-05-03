// Copyright (c) 2026 John Dewey

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

package meshx

import (
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// Store is the storage surface the meshx package consumes — the
// concrete implementation lives in internal/meshx/storage as
// *storage.Sqlite, cast to this interface at construction in
// newModel. Defined here (where it's consumed) per the osapi-io
// pattern: each consumer declares only the methods it actually calls,
// so a future daemon package can declare its own (likely larger)
// interface without bloating the TUI's view of storage.
//
// Methods correspond 1:1 with *storage.Sqlite's exported methods.
// The compile-time check `var _ Store = (*storage.Sqlite)(nil)` in
// app.go's RunRadio path catches any drift the moment a method is
// added or renamed.
type Store interface {
	// identity
	ResolveRadioByConnection(transport, addr string) (string, error)
	ClaimRadioIdentity(oldID string, myNodeNum uint32) (string, error)

	// messages
	SaveMessage(radioID, channel string, msg mdl.Message) error
	LoadMessages(radioID, channel string, limit int) ([]mdl.Message, error)
	ExpireStalePendingMessages(radioID string, ttl time.Duration) (int, error)

	// nodes
	SaveNode(radioID string, n mdl.CachedNode) error
	LoadNodes(radioID string) ([]mdl.CachedNode, error)
	SaveNodePrefs(radioID string, nodeNum uint32, favorite, muted bool) error

	// settings
	GetSetting(radioID, key string) (string, bool)
	PutSetting(radioID, key, value string) error

	// ble
	SaveBLEDevice(d mdl.BLEDevice) error
	LoadBLEDevices() ([]mdl.BLEDevice, error)
	LookupBLEDevice(needle string) (*mdl.BLEDevice, error)
	SetBLEFavorite(uuid string) error
	ForgetBLEDevice(uuid string) error

	// diagnostics
	ConsumeBootNotes() []string

	// lifecycle
	Close() error
}

// messageItemToModel projects the meshx renderer's row envelope down
// to the persistence/wire shape the storage layer accepts. Render-
// only fields (acks, group, style, expireAt, pinned) drop on the
// floor — they regenerate from live state on every launch.
func messageItemToModel(m messageItem) mdl.Message {
	return mdl.Message{
		Time:      m.time,
		From:      m.from,
		Text:      m.text,
		Mine:      m.mine,
		Bang:      m.bang,
		Status:    mdl.MessageStatus(m.status),
		Hops:      m.hops,
		SNR:       m.snr,
		PacketID:  m.packetID,
		ReplyID:   m.replyID,
		FromNum:   m.fromNum,
		SentAt:    m.sentAt,
		Corrupted: m.corrupted,
	}
}

// messageItemFromModel inflates a persisted Message into a renderer
// row envelope. Render-only fields stay at their zero values; the
// model layer fills them in (or doesn't — chat rows don't pin or
// fade).
//
// Text gets sanitized on read so historic rows written before
// sanitizeMessageText landed pick up the ⚠ marker + dim styling on
// next launch without any migration. The corruption flag is
// recomputed from msg.text every time, which means a future
// sanitizer change automatically re-evaluates the row.
func messageItemFromModel(m mdl.Message) messageItem {
	text, corrupted := sanitizeMessageText(m.Text)
	return messageItem{
		time:      m.Time,
		from:      m.From,
		text:      text,
		mine:      m.Mine,
		bang:      m.Bang,
		status:    messageStatus(m.Status),
		hops:      m.Hops,
		snr:       m.SNR,
		packetID:  m.PacketID,
		replyID:   m.ReplyID,
		fromNum:   m.FromNum,
		sentAt:    m.SentAt,
		corrupted: corrupted,
	}
}

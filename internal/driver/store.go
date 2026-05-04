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

package driver

import (
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// Store is the storage surface the Driver consumes — the concrete
// implementation lives in internal/meshx/storage as *storage.Sqlite,
// cast to this interface at construction in cmd/. Defined here
// (where it's consumed) per the osapi-io pattern: each consumer
// declares only the methods it actually calls, so a future remote
// client (MR-5) can declare a smaller surface without bloating the
// Driver's view of storage.
//
// Methods are the subset of *storage.Sqlite's API the Driver and
// its callers (TUI Update arms, daemon sink) actually use. BLE
// pairing storage isn't part of this surface — it's CLI / HTTP
// admin territory and the driver-side seam should not pull it in.
// See server.Store / cmd's BLE deps for that.
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

	// diagnostics
	ConsumeBootNotes() []string

	// lifecycle
	Close() error
}

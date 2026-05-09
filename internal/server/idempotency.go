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

package server

import (
	"sync"
	"time"
)

// idempotency.go implements the dedupe cache for POST /messages
// retries. Agents that send a request, drop the response (network
// blip, host crash), and retry should not double-broadcast on the
// radio — RF airtime is finite. The Idempotency-Key request header
// names the logical send; the cache holds the original result for
// the TTL window so the retry returns the same packet_id without a
// second dispatch.
//
// Pure RAM — daemon restart drops the cache. That's the right
// tradeoff: idempotency is a "retry within seconds" concern, and
// the most expensive thing a stale-after-restart retry does is
// allocate a second packet_id for what's logically the same send,
// which is the pre-cache behavior.

// idempotencyCacheTTL is how long a Send result stays cached for
// retry-dedupe. 60s mirrors common HTTP idempotency conventions and
// covers retries across a short network blip; longer windows risk
// stale results blocking legit re-uses of the same key.
const idempotencyCacheTTL = 60 * time.Second

// idempotencyCacheMax bounds total cache entries across all radios.
// Lazily enforced — when an insert finds the cache at the cap, we
// sweep expired entries first; if we're still over, we evict the
// oldest entry by expiresAt. Prevents unbounded growth from a
// misbehaving client that mints fresh keys without retry intent.
const idempotencyCacheMax = 4096

// idempotencyEntry is one cached Send result.
type idempotencyEntry struct {
	result    SendMessageResult
	expiresAt time.Time
}

// idempotencyCache is a TTL'd map keyed by "<radio_id>|<key>". Safe
// for concurrent use across HTTP handlers.
type idempotencyCache struct {
	mu      sync.Mutex
	entries map[string]idempotencyEntry
}

func newIdempotencyCache() *idempotencyCache {
	return &idempotencyCache{entries: make(map[string]idempotencyEntry)}
}

// Get returns a cached result if one exists for (radioID, key) and
// hasn't expired. Sweeps the entry on a lazy expire.
func (c *idempotencyCache) Get(radioID, key string) (SendMessageResult, bool) {
	if key == "" {
		return SendMessageResult{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	composite := radioID + "|" + key
	entry, ok := c.entries[composite]
	if !ok {
		return SendMessageResult{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, composite)
		return SendMessageResult{}, false
	}
	return entry.result, true
}

// Put records a Send result under (radioID, key). No-op when key is
// empty. Caps the cache at idempotencyCacheMax — sweeps expired
// entries first, evicts the oldest survivor if still over.
func (c *idempotencyCache) Put(radioID, key string, result SendMessageResult) {
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= idempotencyCacheMax {
		c.sweepExpiredLocked()
	}
	if len(c.entries) >= idempotencyCacheMax {
		c.evictOldestLocked()
	}
	c.entries[radioID+"|"+key] = idempotencyEntry{
		result:    result,
		expiresAt: time.Now().Add(idempotencyCacheTTL),
	}
}

// sweepExpiredLocked drops every entry whose TTL has lapsed. Caller
// must hold c.mu.
func (c *idempotencyCache) sweepExpiredLocked() {
	now := time.Now()
	for k, e := range c.entries {
		if now.After(e.expiresAt) {
			delete(c.entries, k)
		}
	}
}

// evictOldestLocked removes the entry with the smallest expiresAt.
// Caller must hold c.mu. Tied entries break arbitrarily — order
// among same-expiresAt entries doesn't affect correctness, only
// freshness of which surviving key answers a follow-up retry.
func (c *idempotencyCache) evictOldestLocked() {
	var oldestKey string
	var oldestAt time.Time
	first := true
	for k, e := range c.entries {
		if first || e.expiresAt.Before(oldestAt) {
			oldestKey = k
			oldestAt = e.expiresAt
			first = false
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

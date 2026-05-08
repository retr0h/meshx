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

package session

import (
	"strconv"
	"strings"
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// HydrationResult reports observable effects of HydrateFromStore so
// the caller can surface counters in its log surface (TUI emits
// notices, daemon emits slog lines).
type HydrationResult struct {
	// MessagesLoaded is the number of historic Message rows pulled
	// from the store and seeded into State.Messages.
	MessagesLoaded int
	// NodesLoaded is the number of CachedNode rows projected into
	// State.Nodes from the store's NodeDB cache.
	NodesLoaded int
	// GhostsCreated counts peers we'd never resolved a User for
	// that we still saw in message history — synthesized into
	// State.Nodes with the firmware-default callsign so /whois /
	// /ping / /rs can target them by id pre-handshake.
	GhostsCreated int
	// LastHeardBackfilled counts existing nodes whose LastHeardAt
	// got bumped from the freshest message-history timestamp,
	// rescuing peers whose NodeInfo beacon is older than their
	// most-recent text packet.
	LastHeardBackfilled int
	// StalePendingExpired is the count of outbound rows the
	// store flipped from StatusPending → StatusFail because they'd
	// been stuck past the TTL — the user sees them as ✗ and `R`
	// resends.
	StalePendingExpired int
	// BootNotes are diagnostic strings the store accumulated during
	// open / migration that the caller should surface.
	BootNotes []string
}

// Sanitizer scrubs a message's Text and reports whether any
// substitution / drop happened. The TUI passes its own
// sanitizeMessageText (UTF-8 + control-byte hardening for the
// terminal-layout invariants); the daemon leaves it nil and stores
// the bytes the radio actually sent.
type Sanitizer func(text string) (cleaned string, corrupted bool)

// RadioResolver looks up the canonical radio_id for a (transport,
// addr) pair. Backed by *storage.Sqlite.ResolveRadioByConnection.
// Pulled out as a function type so this package doesn't import
// storage.
type RadioResolver func(transport, addr string) (string, error)

// DestParser splits a connection target ("usb:/dev/cu...",
// "tcp:host:4403", "ble:<uuid>") into (transport, addr) so
// RadioResolver can consume it. Backed by storage.ParseRadioDest;
// caller supplies it for the same reason as RadioResolver.
type DestParser func(dest string) (transport, addr string)

// HydrationOptions tunes the behavior of HydrateFromStore. Zero
// values for the numeric fields fall back to sensible defaults
// (500 messages, 5-minute pending TTL); nil callbacks skip the
// corresponding step.
type HydrationOptions struct {
	// Dest is the transport-prefixed connection target used to look
	// up the canonical radio_id from the store. Empty Dest skips
	// identity resolution (caller has populated State.RadioID some
	// other way).
	Dest string
	// MessageLimit caps LoadMessages. Zero → 500.
	MessageLimit int
	// PendingTTL is the cutoff for ExpireStalePendingMessages.
	// Zero → 5 minutes.
	PendingTTL time.Duration
	// SanitizeText is an optional Sanitizer applied to every loaded
	// message's Text on read. See Sanitizer for who supplies what.
	SanitizeText Sanitizer
	// ResolveRadioByConnection is the dest → radio_id lookup.
	ResolveRadioByConnection RadioResolver
	// ParseRadioDest splits Dest for ResolveRadioByConnection.
	ParseRadioDest DestParser
}

// HydrateFromStore replays the cached identity, NodeDB, and message
// history from s.store into s.State so the consumer can render a
// non-empty session immediately on launch — same shape every consumer
// (local TUI on first paint, daemon on `meshx server start`, future
// remote-as-server resyncs) needs.
//
// Five steps run in order, each gated on whether the corresponding
// store call succeeded; partial-load is preferable to empty-state
// when one step trips:
//
//  1. Resolve canonical radio_id from the connection target. Falls
//     back to whatever's already on State.RadioID (a "pending:"
//     placeholder, typically) if the lookup misses. Pre-populates
//     State.MyNodeNum from the radio_id hex so callsigns render
//     before MyInfo arrives.
//  2. Load NodeDB cache into State.Nodes (state from the cached
//     muted flag, all the rest from mdl.NodeItemFromCached).
//  3. Sweep stale-pending messages so old StatusPending rows flip
//     to StatusFail and the user sees them as ✗ + R-resendable.
//  4. Load message history; sanitize each Text via opts.SanitizeText
//     if provided; ghost-create any sender we haven't resolved yet.
//  5. Backfill LastHeardAt on existing nodes from message timestamps
//     so a peer with a stale NodeInfo beacon still reads "now"
//     when their freshest text is fresh.
//
// Returns counters for the caller to surface and the boot notes the
// store accumulated during migration / open.
func (s *Session) HydrateFromStore(opts HydrationOptions) HydrationResult {
	var res HydrationResult
	if s.store == nil {
		return res
	}
	if opts.MessageLimit <= 0 {
		opts.MessageLimit = 500
	}
	if opts.PendingTTL <= 0 {
		opts.PendingTTL = 5 * time.Minute
	}

	// Step 1 — identity. Caller passes the dest + parser/resolver
	// so this package doesn't need to import the storage helpers
	// that own the format.
	if opts.Dest != "" && opts.ResolveRadioByConnection != nil && opts.ParseRadioDest != nil {
		transport, addr := opts.ParseRadioDest(opts.Dest)
		if rid, err := opts.ResolveRadioByConnection(transport, addr); err == nil {
			s.State.RadioID = rid
			if n, err := strconv.ParseUint(strings.TrimPrefix(rid, "0x"), 16, 32); err == nil {
				s.State.MyNodeNum = uint32(n)
			}
		}
	}

	// Step 2 — NodeDB cache. Loaded BEFORE message history so
	// ghost-peer creation in step 4 can skip already-known peers.
	if cached, err := s.store.LoadNodes(s.State.RadioID); err == nil {
		for _, n := range cached {
			state := mdl.StateOffline
			if n.Muted {
				state = mdl.StateMuted
			}
			s.State.NodesByNum[n.NodeNum] = len(s.State.Nodes)
			s.State.Nodes = append(s.State.Nodes, mdl.NodeItemFromCached(n, state))
			res.NodesLoaded++
		}
	}

	// Step 3 — stale-pending sweep. Done BEFORE history replay so
	// LoadMessages reflects the freshly-flipped status.
	if expired, err := s.store.ExpireStalePendingMessages(s.State.RadioID, opts.PendingTTL); err == nil {
		res.StalePendingExpired = expired
	}

	// Step 4 — message history + ghost-peer replay.
	if pastModels, err := s.store.LoadMessages(s.State.RadioID, "", opts.MessageLimit); err == nil {
		baseIdx := len(s.State.Messages)
		past := make([]mdl.MessageItem, 0, len(pastModels))
		for _, mm := range pastModels {
			if opts.SanitizeText != nil {
				mm.Text, mm.Corrupted = opts.SanitizeText(mm.Text)
			}
			past = append(past, mdl.MessageItem{Message: mm})
		}
		s.State.Messages = append(s.State.Messages, past...)
		res.MessagesLoaded = len(past)
		// Seed MessagesByPacketID so the live ApplyText path can
		// dedupe RAM-queue replays. PacketID==0 entries (system
		// rows) skip — they're TUI-local and never persist anyway,
		// but the guard is cheap.
		for i, msg := range past {
			if msg.PacketID == 0 {
				continue
			}
			s.State.MessagesByPacketID[msg.PacketID] = baseIdx + i
		}
		// Ghost-peer replay — synthesize firmware-default callsigns
		// for senders not in NodesByNum so /cqr / /whois / /ping can
		// target them by id pre-NodeInfo.
		for _, msg := range past {
			if msg.FromNum == 0 {
				continue
			}
			if _, ok := s.State.NodesByNum[msg.FromNum]; ok {
				continue
			}
			long, short := mdl.DefaultCallsign(msg.FromNum)
			s.State.Nodes = append(s.State.Nodes, mdl.NodeItem{
				Callsign:   long,
				ShortName:  short,
				NodeNum:    msg.FromNum,
				Unresolved: true,
				State:      mdl.StateOffline,
				LastHeard:  msg.Time,
				LastSNR:    msg.SNR,
				LastHops:   msg.Hops,
			})
			s.State.NodesByNum[msg.FromNum] = len(s.State.Nodes) - 1
			res.GhostsCreated++
		}
	}

	// Step 5 — last-heard backfill from message history.
	touched := map[uint32]struct{}{}
	for _, past := range s.State.Messages {
		if past.FromNum == 0 || past.SentAt.IsZero() {
			continue
		}
		idx, ok := s.State.NodesByNum[past.FromNum]
		if !ok {
			continue
		}
		if past.SentAt.After(s.State.Nodes[idx].LastHeardAt) {
			s.State.Nodes[idx].LastHeardAt = past.SentAt
			s.State.Nodes[idx].LastHops = past.Hops
			if past.SNR != "" {
				s.State.Nodes[idx].LastSNR = past.SNR
			}
			touched[past.FromNum] = struct{}{}
		}
	}
	res.LastHeardBackfilled = len(touched)

	res.BootNotes = s.store.ConsumeBootNotes()
	return res
}

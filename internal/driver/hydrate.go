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

// HydrationOptions tunes the behavior of HydrateFromStore.
type HydrationOptions struct {
	// Dest is the transport-prefixed connection target ("usb:/dev/cu...",
	// "tcp:host:4403", "ble:<uuid>") used to look up the canonical
	// radio_id from the store. Empty Dest skips identity resolution
	// (caller has already populated State.RadioID some other way).
	Dest string
	// MessageLimit caps LoadMessages. Zero falls back to 500 — the
	// same default the TUI's history pane has used since launch.
	MessageLimit int
	// PendingTTL is the cutoff ExpireStalePendingMessages applies.
	// Zero falls back to 5 minutes.
	PendingTTL time.Duration
	// SanitizeText is an optional pass run on every loaded message's
	// Text. Returns (cleaned, corrupted). Used by the TUI to scrub
	// invalid UTF-8 / control bytes before they wreck the layout
	// invariants. Daemon callers leave it nil — they store and
	// re-emit raw bytes; sanitization is a TUI render concern.
	SanitizeText func(s string) (string, bool)
	// ResolveRadioByConnection is the dest → radio_id lookup. Always
	// populated by callers with the underlying *storage.Sqlite.
	// Pulled out as a function so this package doesn't import
	// storage. Nil = skip identity resolution.
	ResolveRadioByConnection func(transport, addr string) (string, error)
	// ParseRadioDest splits the dest into (transport, addr) for
	// ResolveRadioByConnection. Pulled out for the same reason.
	ParseRadioDest func(dest string) (transport, addr string)
}

// HydrateFromStore replays the cached identity, NodeDB, and message
// history from d.Store into d.State so the consumer can render a
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
func (d *Driver) HydrateFromStore(opts HydrationOptions) HydrationResult {
	var res HydrationResult
	if d == nil || d.Store == nil {
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
			d.State.RadioID = rid
			if n, err := strconv.ParseUint(strings.TrimPrefix(rid, "0x"), 16, 32); err == nil {
				d.State.MyNodeNum = uint32(n)
			}
		}
	}

	// Step 2 — NodeDB cache. Loaded BEFORE message history so
	// ghost-peer creation in step 4 can skip already-known peers.
	if cached, err := d.Store.LoadNodes(d.State.RadioID); err == nil {
		for _, n := range cached {
			state := mdl.StateOffline
			if n.Muted {
				state = mdl.StateMuted
			}
			d.State.NodesByNum[n.NodeNum] = len(d.State.Nodes)
			d.State.Nodes = append(d.State.Nodes, mdl.NodeItemFromCached(n, state))
			res.NodesLoaded++
		}
	}

	// Step 3 — stale-pending sweep. Done BEFORE history replay so
	// LoadMessages reflects the freshly-flipped status.
	if expired, err := d.Store.ExpireStalePendingMessages(d.State.RadioID, opts.PendingTTL); err == nil {
		res.StalePendingExpired = expired
	}

	// Step 4 — message history + ghost-peer replay.
	if pastModels, err := d.Store.LoadMessages(d.State.RadioID, "", opts.MessageLimit); err == nil {
		baseIdx := len(d.State.Messages)
		past := make([]mdl.MessageItem, 0, len(pastModels))
		for _, mm := range pastModels {
			if opts.SanitizeText != nil {
				mm.Text, mm.Corrupted = opts.SanitizeText(mm.Text)
			}
			past = append(past, mdl.MessageItem{Message: mm})
		}
		d.State.Messages = append(d.State.Messages, past...)
		res.MessagesLoaded = len(past)
		// Seed MessagesByPacketID so the live ApplyText path can
		// dedupe RAM-queue replays. PacketID==0 entries (system
		// rows) skip — they're TUI-local and never persist anyway,
		// but the guard is cheap.
		for i, msg := range past {
			if msg.PacketID == 0 {
				continue
			}
			d.State.MessagesByPacketID[msg.PacketID] = baseIdx + i
		}
		// Ghost-peer replay — synthesize firmware-default callsigns
		// for senders not in NodesByNum so /cqr / /whois / /ping can
		// target them by id pre-NodeInfo.
		for _, msg := range past {
			if msg.FromNum == 0 {
				continue
			}
			if _, ok := d.State.NodesByNum[msg.FromNum]; ok {
				continue
			}
			long, short := mdl.DefaultCallsign(msg.FromNum)
			d.State.Nodes = append(d.State.Nodes, mdl.NodeItem{
				Callsign:   long,
				ShortName:  short,
				NodeNum:    msg.FromNum,
				Unresolved: true,
				State:      mdl.StateOffline,
				LastHeard:  msg.Time,
				LastSNR:    msg.SNR,
				LastHops:   msg.Hops,
			})
			d.State.NodesByNum[msg.FromNum] = len(d.State.Nodes) - 1
			res.GhostsCreated++
		}
	}

	// Step 5 — last-heard backfill from message history.
	touched := map[uint32]struct{}{}
	for _, past := range d.State.Messages {
		if past.FromNum == 0 || past.SentAt.IsZero() {
			continue
		}
		idx, ok := d.State.NodesByNum[past.FromNum]
		if !ok {
			continue
		}
		if past.SentAt.After(d.State.Nodes[idx].LastHeardAt) {
			d.State.Nodes[idx].LastHeardAt = past.SentAt
			d.State.Nodes[idx].LastHops = past.Hops
			if past.SNR != "" {
				d.State.Nodes[idx].LastSNR = past.SNR
			}
			touched[past.FromNum] = struct{}{}
		}
	}
	res.LastHeardBackfilled = len(touched)

	res.BootNotes = d.Store.ConsumeBootNotes()
	return res
}

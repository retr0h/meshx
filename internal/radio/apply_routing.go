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

package radio

// Routing-reply handling — flips outbound message rows to ack/fail
// on a request_id match, aggregates per-peer Ackers for ack roll-up,
// publishes a message_status SSE event so consumers stop polling.

import (
	"sort"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// ApplyRoutingResult reports whether a Routing reply matched one of
// our outbound rows and what status it landed on, so the TUI can
// surface "ack received" / "delivery failed" flashes.
type ApplyRoutingResult struct {
	Matched   bool
	Index     int
	OK        bool
	ErrorName string
}

// ApplyRouting flips the matching outbound message row's status —
// NONE → ack, anything else → fail. Persists the flip so the row
// stays correct across restarts (without this, expireStalePending
// would re-mark a delivered row "fail" on next launch).
//
// On a successful (OK) reply, also aggregates per-peer acks into
// MessageItem.Acks — each Routing reply with the same RequestID
// adds the sending peer to Ackers (deduped by NodeNum, so a peer
// re-acking via a second path doesn't double-count). The local
// radio's own ack-of-send (FromNum == MyNodeNum) is excluded; only
// genuine mesh peer echoes contribute.
//
// Foreign Routing replies (request_id matches no row of ours) drop
// silently. Ping-correlation lives in the TUI's reactRouting; this
// path only handles the message-status flip + ack roll-up.
func (s *Session) ApplyRouting(msg mdl.Routing) ApplyRoutingResult {
	defer s.PublishRouting(msg)
	if msg.RequestID == 0 {
		return ApplyRoutingResult{}
	}
	for i := range s.State.Messages {
		if s.State.Messages[i].PacketID != msg.RequestID || !s.State.Messages[i].Mine {
			continue
		}
		row := &s.State.Messages[i]
		if msg.OK {
			row.Status = mdl.StatusAck
			s.recordAck(row, msg)
		} else {
			row.Status = mdl.StatusFail
		}
		if s.store != nil {
			s.storeError(s.store.SaveMessage(
				s.State.RadioID,
				s.State.CurrentChannel,
				row.Message,
			))
		}
		// Surface the terminal flip as its own SSE event so consumers
		// don't have to diff /messages to detect ack/fail. Ackers
		// snapshot reflects the row's per-peer echoes at flip time;
		// later Routing replies for the same packet refresh the row's
		// Ackers but don't re-publish (the row has already terminated).
		ackersCopy := append([]mdl.Acker(nil), row.Ackers...)
		s.PublishMessageStatus(mdl.MessageStatusUpdate{
			PacketID: row.PacketID,
			Status:   row.Status,
			Ackers:   ackersCopy,
			At:       msg.At,
		})
		return ApplyRoutingResult{
			Matched:   true,
			Index:     i,
			OK:        msg.OK,
			ErrorName: msg.ErrorName,
		}
	}
	return ApplyRoutingResult{}
}

// recordAck folds a successful Routing reply into the row's per-
// peer Ackers slice. Skips the local-radio ack (FromNum == 0 or
// MyNodeNum) — that's "I queued/sent it," not "a peer echoed it."
// Dedups by NodeNum so a peer reaching us via two paths counts
// once at the shorter hop count. Slice stays sorted by Hops then
// NodeNum so consumers can render directly without re-sorting.
func (s *Session) recordAck(row *mdl.MessageItem, msg mdl.Routing) {
	if msg.FromNum == 0 || msg.FromNum == s.State.MyNodeNum {
		return
	}
	for i, a := range row.Ackers {
		if a.NodeNum != msg.FromNum {
			continue
		}
		if a.Hops <= msg.Hops {
			// Already heard from this peer at an equal-or-shorter
			// path — keep the shorter hop count, no shape change.
			return
		}
		row.Ackers[i].Hops = msg.Hops
		row.Ackers[i].At = msg.At
		// Refresh callsign in case the peer's NodeInfo arrived
		// between the first and second ack.
		if call := s.callsignForAck(msg.FromNum); call != "" {
			row.Ackers[i].Callsign = call
		}
		sortAckers(row.Ackers)
		return
	}
	row.Ackers = append(row.Ackers, mdl.Acker{
		NodeNum:  msg.FromNum,
		Callsign: s.callsignForAck(msg.FromNum),
		Hops:     msg.Hops,
		At:       msg.At,
	})
	sortAckers(row.Ackers)
}

// sortAckers orders by Hops ascending (closer peers first), tied
// broken by NodeNum for deterministic output.
func sortAckers(ackers []mdl.Acker) {
	sort.Slice(ackers, func(i, j int) bool {
		if ackers[i].Hops != ackers[j].Hops {
			return ackers[i].Hops < ackers[j].Hops
		}
		return ackers[i].NodeNum < ackers[j].NodeNum
	})
}

// callsignForAck resolves an ack sender's NodeNum to its display
// callsign. Falls back to the canonical "!<8-hex>" placeholder
// when the peer isn't in the NodeDB yet — better than dropping the
// ack just because their NodeInfo hasn't arrived.
func (s *Session) callsignForAck(num uint32) string {
	if idx, ok := s.State.NodesByNum[num]; ok && idx < len(s.State.Nodes) {
		if n := s.State.Nodes[idx]; n.Callsign != "" {
			return n.Callsign
		}
	}
	long, _ := mdl.DefaultCallsign(num)
	return long
}

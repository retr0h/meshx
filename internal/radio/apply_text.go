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

// TEXT_MESSAGE_APP inbound handling — broadcast / DM dispatch,
// sender NodeDB upsert, packet-ID dedupe, channel-unread bump,
// persistence. The DM-vs-broadcast split also picks the SSE event
// shape (dm_received vs text) so consumers can subscribe to whichever
// firehose they care about.

import (
	"time"

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// ApplyTextResult tells the TUI whether to advance selectedMsg
// (caller observed wasAtTail before calling) and whether the
// message is from a peer (so the TUI can ring the bell).
type ApplyTextResult struct {
	Index    int  // index in State.Messages where the row landed (-1 if dedupe-skipped)
	Skipped  bool // true when an existing PacketID was upgraded in place
	FromMine bool
}

// ApplyText handles an inbound TEXT_MESSAGE_APP packet. Updates the
// sender's NodeDB telemetry (lastSNR/RSSI/hops, ghost-creates if the
// peer hasn't sent NodeInfo yet), dedupes against a packet-ID replay,
// appends or refreshes the message row, bumps unread on non-active
// channels, and persists if a Store is wired. Sanitization of the
// text body is the caller's concern (lives in TUI today; daemon
// passes pre-sanitized text or ignores cleanup). The `alert` flag
// rides alongside `corrupted` — sender embedded a BEL (0x07), the
// Meshtastic external_notification trigger; renderer surfaces 🔔.
func (s *Session) ApplyText(
	ev mdl.Text,
	sanitizedText string,
	corrupted, alert bool,
) ApplyTextResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Inbound DMs addressed to MyNodeNum fire dm_received; channel
	// broadcasts (and pre-handshake packets where MyNodeNum=0) fire
	// text. Mutually exclusive — agents subscribe to whichever they
	// care about without filtering the firehose.
	if s.State.MyNodeNum != 0 && ev.ToNum == s.State.MyNodeNum {
		defer s.PublishDMReceived(ev)
	} else {
		defer s.PublishText(ev)
	}
	body := ev.Body
	defaultLong, _ := mdl.DefaultCallsign(body.FromNum)
	from := defaultLong
	if idx, ok := s.State.NodesByNum[body.FromNum]; ok {
		from = s.State.Nodes[idx].Callsign
		s.State.Nodes[idx].LastHeardAt = time.Now()
		s.State.Nodes[idx].HeardRank = 0
		if body.SNR != "" {
			s.State.Nodes[idx].LastSNR = body.SNR
		}
		if ev.RSSI != "" {
			s.State.Nodes[idx].LastRSSI = ev.RSSI
		}
		if body.Hops > 0 {
			s.State.Nodes[idx].LastHops = body.Hops
		}
	} else if body.FromNum != 0 {
		long, short := mdl.DefaultCallsign(body.FromNum)
		s.State.Nodes = append(s.State.Nodes, mdl.NodeItem{
			Callsign:    long,
			ShortName:   short,
			NodeNum:     body.FromNum,
			Unresolved:  true,
			LastHeardAt: time.Now(),
			LastSNR:     body.SNR,
			LastRSSI:    ev.RSSI,
			LastHops:    body.Hops,
		})
		s.State.NodesByNum[body.FromNum] = len(s.State.Nodes) - 1
		from = long
	}
	mine := body.FromNum == s.State.MyNodeNum
	item := mdl.MessageItem{Message: mdl.Message{
		Time:      body.Time,
		From:      from,
		Mine:      mine,
		Text:      sanitizedText,
		Corrupted: corrupted,
		Alert:     alert,
		Status:    mdl.StatusAck,
		Hops:      body.Hops,
		SNR:       body.SNR,
		PacketID:  body.PacketID,
		ReplyID:   body.ReplyID,
		FromNum:   body.FromNum,
		ToNum:     ev.ToNum,
		SentAt:    body.SentAt,
	}}
	channelName := s.State.CurrentChannel
	if ev.Channel < len(s.State.Channels) {
		channelName = s.State.Channels[ev.Channel].Name
	}
	if body.PacketID != 0 {
		if existing, ok := s.State.MessagesByPacketID[body.PacketID]; ok &&
			existing >= 0 && existing < len(s.State.Messages) {
			prev := &s.State.Messages[existing]
			prev.Hops = body.Hops
			prev.SNR = body.SNR
			if prev.Status == mdl.StatusPending {
				prev.Status = mdl.StatusAck
			}
			if s.store != nil {
				s.storeError(s.store.SaveMessage(s.State.RadioID, channelName, prev.Message))
			}
			return ApplyTextResult{Index: existing, Skipped: true, FromMine: mine}
		}
	}
	s.State.Messages = append(s.State.Messages, item)
	idx := len(s.State.Messages) - 1
	if body.PacketID != 0 {
		s.State.MessagesByPacketID[body.PacketID] = idx
	}
	if s.store != nil {
		s.storeError(s.store.SaveMessage(s.State.RadioID, channelName, item.Message))
	}
	if ev.Channel < len(s.State.Channels) &&
		s.State.Channels[ev.Channel].Name != s.State.CurrentChannel && !mine {
		s.State.Channels[ev.Channel].Unread++
	}
	return ApplyTextResult{Index: idx, FromMine: mine}
}

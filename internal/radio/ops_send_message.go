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

import mdl "github.com/retr0h/meshx/internal/meshx/model"

// Text-send op — single source of truth for the Send + RecordOutbound
// pair every locally-originated chat / DM / ham-bang message goes
// through. HTTP handler (handlers_send_message.go), TUI sendDM /
// sendPlainReply / sendBangReply, and (future) MCP all call this so
// the pump dispatch + outbound-row append + persist + publish happen
// in lockstep.
//
// Idempotency-Key dedupe stays in the HTTP handler because it's tied
// to HTTP semantics (the header is the only place the cache key
// arrives). The TUI doesn't retry sends; an MCP server that wants its
// own dedupe can wrap this method.

// SendMessageRequest is the inbound shape — mirrors mdl.SendText plus
// the Bang field that distinguishes ham-bang command output from
// plain chat (Bang stays in State.Messages but isn't on the wire; the
// firmware sees identical TEXT_MESSAGE_APP packets either way).
type SendMessageRequest struct {
	Channel int    // target channel slot (0..7)
	Text    string // message body
	ReplyID uint32 // PacketID this reply threads under; 0 = no reply
	ToNum   uint32 // 0 = broadcast on Channel; peer node_num = DM
	Bang    string // "/cq" / "/73" / … for ham-bang variants; "" for plain chat
}

// SendMessageResult echoes the dispatched packet so callers can
// correlate ack/fail events and update their local UI state.
type SendMessageResult struct {
	PacketID uint32 // MeshPacket.id the pump allocated; 0 if pump rejected
	OK       bool   // false when the outbound buffer was full or no radio attached
	Index    int    // index in State.Messages where the outbound row landed
}

// SendMessage dispatches a text packet and appends the matching
// outbound row in one shot. Send returns the allocated PacketID, which
// is then passed to RecordOutbound so the row's MessagesByPacketID
// index lines up with the later Routing receipt — without this two-
// step lockstep, the Routing handler couldn't flip pending → ack on
// the right row.
//
// Even when Send returns ok=false (demo mode / no pump) the row is
// still appended — the user typed it, it belongs in their chat log
// as a pending entry, and expireStalePending flips it to Fail when
// no ack arrives.
func (s *Session) SendMessage(req SendMessageRequest) SendMessageResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	pid, ok := s.Send(mdl.SendText{
		Channel: req.Channel,
		Text:    req.Text,
		ReplyID: req.ReplyID,
		ToNum:   req.ToNum,
	})
	res := s.recordOutbound(RecordOutboundOptions{
		Channel:  req.Channel,
		Text:     req.Text,
		Bang:     req.Bang,
		ReplyID:  req.ReplyID,
		PacketID: pid,
		ToNum:    req.ToNum,
	})
	return SendMessageResult{
		PacketID: pid,
		OK:       ok,
		Index:    res.Index,
	}
}

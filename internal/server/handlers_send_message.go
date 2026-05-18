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

package server

import (
	"context"

	"github.com/retr0h/meshx/internal/radio"
)

// POST /radios/{radio_id}/messages — dispatches an outbound text
// packet. Idempotency-Key dedupe sits in front of Send so a network
// blip + client retry doesn't double-broadcast on RF.

// SendMessageRequest is the inbound POST body for sending text.
// ToNum=0 sends a broadcast on the named channel — every peer
// listening on that channel slot decodes it. ToNum=peer.NodeNum
// sends a unicast DM to that peer; the channel still selects the
// PSK keyset the firmware uses to encrypt the packet, so DMs
// remain scoped to peers who share that channel's key.
type SendMessageRequest struct {
	Channel int    `json:"channel"            doc:"target channel slot index (0..7); the current channel's slot is the default"`
	Text    string `json:"text"               doc:"message body"                                                                                                                                                                              minLength:"1"`
	ReplyID uint32 `json:"reply_id,omitempty" doc:"PacketID this message replies to"                                                                                                                                                                        format:"int64" minimum:"0"`
	ToNum   uint32 `json:"to_num,omitempty"   doc:"recipient NodeNum for a DM (peer-addressed unicast); 0 = broadcast on the channel. Look up the numeric NodeNum via GET /radios/{radio_id}/nodes — callsigns are not resolved server-side."               format:"int64" minimum:"0"`
}

// SendMessageResult echoes the allocated PacketID so clients can
// correlate with ack / fail events on the SSE stream. (Named
// "Result" not "Response" so the OpenAPI schema name doesn't collide
// with oapi-codegen's auto-generated <OpId>Response wrapper.)
type SendMessageResult struct {
	PacketID uint32 `json:"packet_id" doc:"MeshPacket.id allocated by the radio (zero if pump rejected the send)"  format:"int64" minimum:"0"`
	OK       bool   `json:"ok"        doc:"false when the pump's outbound buffer was full or no radio is attached"`
}

type sendMessageInput struct {
	RadioID        string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
	IdempotencyKey string `                doc:"opaque request key (typically a UUID) for retry dedupe; identical key on the same radio within 60s returns the original result without re-dispatching to the radio" header:"Idempotency-Key"`
	Body           SendMessageRequest
}

type sendMessageOutput struct {
	Body SendMessageResult
}

func (s *Server) handleSendMessage(
	_ context.Context,
	in *sendMessageInput,
) (*sendMessageOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	// Idempotency-Key dedupe — return the original result for retries
	// of the same logical send within the TTL window so a network
	// blip + client retry doesn't double-broadcast on RF. Key is
	// per-radio so independent radios don't share a key namespace.
	// Stays in front of SendMessage because the Idempotency-Key header
	// is an HTTP-specific concept; the TUI doesn't retry, and an MCP
	// server that wants its own dedupe layer can wrap SendMessage.
	if cached, ok := s.idempotency.Get(in.RadioID, in.IdempotencyKey); ok {
		out := &sendMessageOutput{}
		out.Body = cached
		return out, nil
	}
	res := d.SendMessage(radio.SendMessageRequest{
		Channel: in.Body.Channel,
		Text:    in.Body.Text,
		ReplyID: in.Body.ReplyID,
		ToNum:   in.Body.ToNum,
	})
	out := &sendMessageOutput{}
	out.Body = SendMessageResult{PacketID: res.PacketID, OK: res.OK}
	s.idempotency.Put(in.RadioID, in.IdempotencyKey, out.Body)
	return out, nil
}

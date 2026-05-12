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

package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

// send_message — the canonical write op an agent uses to push text
// onto the mesh. Mirrors POST /radios/{id}/messages.

func (s *Server) registerMessageTools() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "send_message",
		Description: "Send a text message via a radio. to_num=0 (default) broadcasts on the channel; setting to_num to a peer's NodeNum (from list_nodes) sends a DM. The radio echoes a Routing reply which surfaces as an ack/fail event on the daemon's SSE stream — call list_messages to confirm delivery.",
	}, s.toolSendMessage)
}

type sendMessageArgs struct {
	RadioID        string `json:"radio_id"                  jsonschema:"canonical radio identifier from list_radios"`
	Text           string `json:"text"                      jsonschema:"message body (1..n bytes)"`
	Channel        int    `json:"channel,omitempty"         jsonschema:"target channel slot index (0..7); 0 = primary"`
	ToNum          int64  `json:"to_num,omitempty"          jsonschema:"recipient NodeNum for a DM; 0 = broadcast on the channel"`
	ReplyID        int64  `json:"reply_id,omitempty"        jsonschema:"PacketID this message threads under; 0 = standalone"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"opaque retry-dedupe key (typically a UUID); identical key on the same radio within 60s returns the original result without re-broadcasting"`
}

func (s *Server) toolSendMessage(
	ctx context.Context,
	_ *mcpsdk.CallToolRequest,
	args sendMessageArgs,
) (*mcpsdk.CallToolResult, any, error) {
	if args.Text == "" {
		return nil, nil, fmt.Errorf("send_message: text is required")
	}
	body := gen.SendMessageJSONRequestBody{
		Channel: int64(args.Channel),
		Text:    args.Text,
	}
	if args.ToNum != 0 {
		v := args.ToNum
		body.ToNum = &v
	}
	if args.ReplyID != 0 {
		v := args.ReplyID
		body.ReplyId = &v
	}
	var params *gen.SendMessageParams
	if args.IdempotencyKey != "" {
		params = &gen.SendMessageParams{IdempotencyKey: &args.IdempotencyKey}
	}
	resp, err := s.client.SendMessageWithResponse(ctx, args.RadioID, params, body)
	if err != nil {
		return nil, nil, fmt.Errorf("send_message: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, nil, fmt.Errorf("send_message: daemon returned %s", resp.Status())
	}
	return textResult(jsonOrErr(resp.JSON200)), nil, nil
}

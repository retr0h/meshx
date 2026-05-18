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

package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

// sendOpts captures the per-call shape for `meshx client send`. Held
// in a struct so runClientSend stays testable without dragging cobra
// flag plumbing into the function signature.
type sendOpts struct {
	Text           string
	Channel        int64
	ToNum          int64
	ReplyID        int64
	IdempotencyKey string
}

var (
	sendText           string
	sendChannel        int64
	sendToNum          int64
	sendReplyID        int64
	sendIdempotencyKey string
)

var clientSendCmd = &cobra.Command{
	Use:   "send <radio_id>",
	Short: "Send a one-shot text message via the daemon",
	Long: `POSTs /radios/{radio_id}/messages. The daemon dispatches the
TEXT_MESSAGE_APP packet to the radio and records the outbound row
just like the TUI would; --text is required, the other flags shape
the packet.

  meshx client send 0xabcdef01 --text "hi mesh"
  meshx client send 0xabcdef01 --text "hi peer" --to-num 3221225505      # DM
  meshx client send 0xabcdef01 --text "reply!" --reply-id 12345          # threaded
  meshx client send 0xabcdef01 --text "in #ham" --channel 1              # named slot`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		cfg, err := resolveClientConfig()
		if err != nil {
			return err
		}
		if sendText == "" {
			return fmt.Errorf("client send: --text is required")
		}
		radioID := args[0]
		logger.With(slog.String("subsystem", "client.send")).
			Debug(
				"running",
				slog.String("server", cfg.ServerURL),
				slog.String("radio_id", radioID),
				slog.Int64("channel", sendChannel),
				slog.Int64("to_num", sendToNum),
				slog.Int64("reply_id", sendReplyID),
			)
		c, err := newSDKClient(cfg)
		if err != nil {
			return err
		}
		return runClientSend(
			context.Background(),
			c,
			os.Stdout,
			radioID,
			sendOpts{
				Text:           sendText,
				Channel:        sendChannel,
				ToNum:          sendToNum,
				ReplyID:        sendReplyID,
				IdempotencyKey: sendIdempotencyKey,
			},
		)
	},
}

func init() {
	clientSendCmd.Flags().StringVarP(&sendText, "text", "t", "", "message body (required)")
	clientSendCmd.Flags().Int64VarP(&sendChannel, "channel", "c", 0, "target channel slot (0..7)")
	clientSendCmd.Flags().
		Int64Var(&sendToNum, "to-num", 0, "recipient NodeNum for a DM; 0 = broadcast")
	clientSendCmd.Flags().
		Int64Var(&sendReplyID, "reply-id", 0, "PacketID this message threads under; 0 = no reply")
	clientSendCmd.Flags().
		StringVar(&sendIdempotencyKey, "idempotency-key", "", "opaque key for retry dedupe (typically a UUID)")
}

func runClientSend(
	ctx context.Context,
	c *gen.ClientWithResponses,
	w io.Writer,
	radioID string,
	opts sendOpts,
) error {
	body := gen.SendMessageJSONRequestBody{
		Channel: opts.Channel,
		Text:    opts.Text,
	}
	if opts.ToNum != 0 {
		v := opts.ToNum
		body.ToNum = &v
	}
	if opts.ReplyID != 0 {
		v := opts.ReplyID
		body.ReplyId = &v
	}
	var params *gen.SendMessageParams
	if opts.IdempotencyKey != "" {
		params = &gen.SendMessageParams{IdempotencyKey: &opts.IdempotencyKey}
	}
	resp, err := c.SendMessageWithResponse(ctx, radioID, params, body)
	if err != nil {
		return fmt.Errorf("client send: %w", err)
	}
	if resp.JSON200 == nil {
		return fmt.Errorf("client send: daemon returned %s", resp.Status())
	}
	if resp.JSON200.Ok {
		_, _ = fmt.Fprintf(w, "sent packet_id=0x%x\n", resp.JSON200.PacketId)
	} else {
		_, _ = fmt.Fprintln(w, "queued offline (pump rejected; row stored as pending)")
	}
	return nil
}

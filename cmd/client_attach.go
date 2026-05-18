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

var clientAttachCmd = &cobra.Command{
	Use:   "attach <dest>",
	Short: "Attach a radio to the daemon at runtime",
	Long: `POSTs /radios/attach with the transport target (ble:<uuid>,
/dev/cu.usb…, host:port). The daemon dials the transport, runs the
Meshtastic handshake, and registers the radio — no restart needed.

  meshx client attach ble:48d917af-8a1f-e43e-4735-af3e1c8e35bc
  meshx client attach /dev/cu.usbmodem2101
  meshx client attach 192.168.1.50:4403`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		cfg, err := resolveClientConfig()
		if err != nil {
			return err
		}
		dest := args[0]
		logger.With(slog.String("subsystem", "client.attach")).
			Debug("running", slog.String("dest", dest))
		c, err := newSDKClient(cfg)
		if err != nil {
			return err
		}
		return runClientAttach(context.Background(), c, os.Stdout, dest)
	},
}

func runClientAttach(
	ctx context.Context,
	c *gen.ClientWithResponses,
	w io.Writer,
	dest string,
) error {
	resp, err := c.AttachRadioWithResponse(ctx, gen.AttachRadioJSONRequestBody{
		Dest: dest,
	})
	if err != nil {
		return fmt.Errorf("client attach: %w", err)
	}
	if resp.JSON200 == nil {
		return fmt.Errorf("client attach: daemon returned %s", resp.Status())
	}
	_, _ = fmt.Fprintf(w, "attached: %s\n", resp.JSON200.RadioId)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(
		w,
		"the radio is dialing — run `meshx client status` to see when it connects.",
	)
	_, _ = fmt.Fprintln(w, "once connected, `meshx client connect` opens the TUI.")
	return nil
}

var clientDetachCmd = &cobra.Command{
	Use:   "detach <radio_id>",
	Short: "Detach a radio from the daemon",
	Long: `DELETEs /radios/{radio_id}. Tears down the pump + transport and
removes the radio from the registry. The daemon stays up.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		cfg, err := resolveClientConfig()
		if err != nil {
			return err
		}
		radioID := args[0]
		logger.With(slog.String("subsystem", "client.detach")).
			Debug("running", slog.String("radio_id", radioID))
		c, err := newSDKClient(cfg)
		if err != nil {
			return err
		}
		return runClientDetach(context.Background(), c, os.Stdout, radioID)
	},
}

func runClientDetach(
	ctx context.Context,
	c *gen.ClientWithResponses,
	w io.Writer,
	radioID string,
) error {
	resp, err := c.DetachRadioWithResponse(ctx, radioID)
	if err != nil {
		return fmt.Errorf("client detach: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("client detach: daemon returned %s", resp.Status())
	}
	_, _ = fmt.Fprintf(w, "detached %s\n", radioID)
	return nil
}

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

var clientPairCmd = &cobra.Command{
	Use:   "pair <uuid>",
	Short: "Pair a BLE radio via the daemon",
	Long: `POSTs /transports/ble/devices with the provided UUID. The daemon
initiates the OS-level Bluetooth pairing dance (its host has to be
the one trusting the device; the client only triggers it) and
persists the result. Pass a uuid discovered by ` + "`meshx client scan ble`" + `.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		cfg, err := resolveClientConfig()
		if err != nil {
			return err
		}
		uuid := args[0]
		logger.With(slog.String("subsystem", "client.pair")).
			Debug(
				"running",
				slog.String("server", cfg.ServerURL),
				slog.String("uuid", uuid),
			)
		c, err := newSDKClient(cfg)
		if err != nil {
			return err
		}
		return runClientPair(context.Background(), c, os.Stdout, uuid)
	},
}

// runClientPair is the testable core.
func runClientPair(
	ctx context.Context,
	c *gen.ClientWithResponses,
	w io.Writer,
	uuid string,
) error {
	resp, err := c.PairBleWithResponse(ctx, gen.PairBleJSONRequestBody{Uuid: uuid})
	if err != nil {
		return fmt.Errorf("client pair: %w", err)
	}
	if resp.JSON200 == nil {
		return fmt.Errorf(
			"client pair: daemon returned %s",
			resp.Status(),
		)
	}
	d := resp.JSON200
	_, _ = fmt.Fprintf(w, "paired %s\n", d.Uuid)
	if name := deref(d.LongName); name != "" {
		_, _ = fmt.Fprintf(w, "  name:     %s\n", name)
	}
	if tag := deref(d.ShortName); tag != "" {
		_, _ = fmt.Fprintf(w, "  tag:      %s\n", tag)
	}
	if hw := deref(d.HwModel); hw != "" {
		_, _ = fmt.Fprintf(w, "  hardware: %s\n", hw)
	}
	if d.Favorite {
		_, _ = fmt.Fprintln(w, "  favorite: yes")
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "next steps:")
	_, _ = fmt.Fprintf(
		w,
		"  - `meshx server start --radio ble:%s` to attach this radio to the daemon\n",
		d.Uuid,
	)
	_, _ = fmt.Fprintln(w, "  - `meshx client status` to confirm it's registered")
	_, _ = fmt.Fprintln(w, "  - `meshx client connect` to open the TUI against it")
	return nil
}

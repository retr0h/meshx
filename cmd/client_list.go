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

package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

var clientListCmd = &cobra.Command{
	Use:   "list",
	Short: "List paired BLE devices known to the daemon",
	Long: `GETs /transports/ble/devices. Prints (UUID, NAME, HW, FAVORITE)
per saved device. The favorite is the one bare ` + "`meshx`" + ` falls
through to when no transport arg is given.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := resolveClientConfig()
		if err != nil {
			return err
		}
		logger.With(slog.String("subsystem", "client.list")).
			Debug("running", slog.String("server", cfg.ServerURL))
		c, err := newSDKClient(cfg)
		if err != nil {
			return err
		}
		return runClientList(context.Background(), c, os.Stdout)
	},
}

func runClientList(
	ctx context.Context,
	c *gen.ClientWithResponses,
	w io.Writer,
) error {
	resp, err := c.ListBleDevicesWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("client list: %w", err)
	}
	if resp.JSON200 == nil {
		return fmt.Errorf("client list: daemon returned %s", resp.Status())
	}
	devices := []gen.BLEDeviceView{}
	if resp.JSON200.Devices != nil {
		devices = *resp.JSON200.Devices
	}
	if len(devices) == 0 {
		_, _ = fmt.Fprintln(w, "no paired BLE devices.")
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "  → `meshx client scan ble` to discover nearby radios")
		_, _ = fmt.Fprintln(w, "  → `meshx client pair <uuid>` to save one")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "UUID\tNAME\tHW\tFAVORITE")
	for _, d := range devices {
		fav := "—"
		if d.Favorite {
			fav = "★"
		}
		_, _ = fmt.Fprintf(
			tw, "%s\t%s\t%s\t%s\n",
			d.Uuid, orDash(deref(d.LongName)), orDash(deref(d.HwModel)), fav,
		)
	}
	return tw.Flush()
}

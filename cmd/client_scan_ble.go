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
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

// clientScanBLEDuration mirrors the BLE scan default the daemon
// applies when the request's timeout_ms is omitted. Kept here so the
// "scanning N ms" line printed before the request matches what the
// daemon will use — if the user wants a different window they can
// pass --timeout.
const clientScanBLEDuration = 10000

var clientScanBLECmd = &cobra.Command{
	Use:   "ble",
	Short: "Scan for Meshtastic radios via the daemon's BLE adapter",
	Long: `POSTs /transports/ble/scan with the configured timeout and prints
the responding peripherals. The uuid shown here is what
` + "`meshx client pair`" + ` accepts.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := resolveClientConfig()
		if err != nil {
			return err
		}
		logger.With(slog.String("subsystem", "client.scan.ble")).
			Debug(
				"running",
				slog.String("server", cfg.ServerURL),
				slog.Int("timeout_ms", clientScanBLEDuration),
			)
		c, err := newSDKClient(cfg)
		if err != nil {
			return err
		}
		return runClientScanBLE(context.Background(), c, os.Stdout)
	},
}

// runClientScanBLE is the testable core — driven by a wired SDK
// client + an output writer.
func runClientScanBLE(
	ctx context.Context,
	c *gen.ClientWithResponses,
	w io.Writer,
) error {
	ms := int64(clientScanBLEDuration)
	resp, err := c.ScanBleWithResponse(ctx, gen.ScanBleJSONRequestBody{
		TimeoutMs: &ms,
	})
	if err != nil {
		return fmt.Errorf("client scan ble: %w", err)
	}
	if resp.JSON200 == nil {
		return fmt.Errorf(
			"client scan ble: daemon returned %s",
			resp.Status(),
		)
	}
	devices := []gen.BLESighting{}
	if resp.JSON200.Devices != nil {
		devices = *resp.JSON200.Devices
	}
	if len(devices) == 0 {
		_, _ = fmt.Fprintln(w, "no Meshtastic radios responded.")
		return nil
	}
	sort.SliceStable(devices, func(i, j int) bool {
		if devices[i].Rssi != devices[j].Rssi {
			return devices[i].Rssi > devices[j].Rssi
		}
		return devices[i].LocalName < devices[j].LocalName
	})
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "UUID\tNAME\tRSSI")
	for _, d := range devices {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d dBm\n", d.Uuid, orDash(d.LocalName), d.Rssi)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  → `meshx client pair <uuid>` to save one of these")
	return nil
}

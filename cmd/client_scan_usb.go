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
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/sdk/gen"
)

// clientScanUSBDuration mirrors the daemon's per-port USB identify
// timeout default. Matches `meshx usb scan`'s 1500ms.
const clientScanUSBDuration = 1500

var clientScanUSBCmd = &cobra.Command{
	Use:   "usb",
	Short: "Identify Meshtastic radios via the daemon's USB-serial adapter",
	Long: `POSTs /transports/usb/scan. The daemon walks every candidate
USB-serial port, sends a non-destructive Meshtastic handshake, and
returns whether each port responded. The port shown for a responding
radio is what ` + "`meshx server start --radio`" + ` accepts.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := resolveClientConfig()
		if err != nil {
			return err
		}
		logger.With(slog.String("subsystem", "client.scan.usb")).
			Debug(
				"running",
				slog.String("server", cfg.ServerURL),
				slog.Int("timeout_ms", clientScanUSBDuration),
			)
		c, err := newSDKClient(cfg)
		if err != nil {
			return err
		}
		return runClientScanUSB(context.Background(), c, os.Stdout)
	},
}

// runClientScanUSB is the testable core.
func runClientScanUSB(
	ctx context.Context,
	c *gen.ClientWithResponses,
	w io.Writer,
) error {
	ms := int64(clientScanUSBDuration)
	resp, err := c.ScanUsbWithResponse(ctx, gen.ScanUsbJSONRequestBody{
		TimeoutMs: &ms,
	})
	if err != nil {
		return fmt.Errorf("client scan usb: %w", err)
	}
	if resp.JSON200 == nil {
		return fmt.Errorf(
			"client scan usb: daemon returned %s",
			resp.Status(),
		)
	}
	devices := []gen.USBSighting{}
	if resp.JSON200.Devices != nil {
		devices = *resp.JSON200.Devices
	}
	if len(devices) == 0 {
		_, _ = fmt.Fprintln(w, "no USB-serial devices found.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PORT\tMESHTASTIC\tNAME\tHW")
	for _, d := range devices {
		marker := "—"
		name := "—"
		hw := "—"
		if d.IsMeshtastic {
			marker = "✓"
			name = deref(d.LongName)
			if name == "" {
				name = deref(d.ShortName)
			}
			if name == "" && d.NodeNum != nil {
				name = fmt.Sprintf("0x%x", *d.NodeNum)
			}
			hw = deref(d.HwModel)
		} else if r := deref(d.Reason); r != "" {
			name = r
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", d.Port, marker, name, hw)
	}
	return tw.Flush()
}

// deref returns the pointed-to string or "" if the pointer is nil.
// The generated SDK uses *string for optional fields; this keeps the
// scan output formatter terse.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

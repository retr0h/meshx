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
	"log/slog"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var usbScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Identify Meshtastic radios on USB-serial",
	Long: `Walks every candidate USB-serial port, sends a non-destructive
Meshtastic handshake, and prints whether each port responded.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger.With(slog.String("subsystem", "usb.scan")).
			Debug("running", slog.Int("timeout_ms", 1500))
		// Scan doesn't touch the pairing store — storeless Manager.
		mgr := newTransportsManager(nil)
		hits, err := mgr.ScanUSB(context.Background(), 1500)
		if err != nil {
			return err
		}
		if len(hits) == 0 {
			fmt.Println("no USB-serial devices found.")
			fmt.Println()
			fmt.Println("troubleshooting:")
			fmt.Println("  - plug in your radio with a DATA USB cable (not charge-only)")
			fmt.Println("  - verify the radio is powered on")
			fmt.Println("  - check `ls /dev/cu.*` (macOS) or `ls /dev/ttyUSB*` (Linux)")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "PORT\tMESHTASTIC\tNAME\tHW")
		for _, h := range hits {
			marker := "—"
			name := "—"
			hw := "—"
			if h.IsMeshtastic {
				marker = "✓"
				name = h.LongName
				if name == "" {
					name = h.ShortName
				}
				if name == "" {
					name = fmt.Sprintf("0x%x", h.NodeNum)
				}
				hw = h.HWModel
			} else if h.Reason != "" {
				name = h.Reason
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", h.Port, marker, name, hw)
		}
		return tw.Flush()
	},
}

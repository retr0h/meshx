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
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// usbCmd is the parent for every USB-serial operation: scanning,
// identifying, and opening a TUI session against a specific device.
// Every subcommand here goes through the in-process server — same
// code path the daemon's HTTP routes use. The CLI is just a local
// HTTP-less consumer of the same surface; nothing here knows about
// the transport package directly.
var usbCmd = &cobra.Command{
	Use:   "usb",
	Short: "USB-serial Meshtastic transport",
	Long: `Commands for discovering, identifying, and connecting to
Meshtastic radios over a USB-serial cable (the default transport
for a desk-mounted or data-cable-tethered radio).`,
}

var usbScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Identify Meshtastic radios on USB-serial",
	Long: `Walks every candidate USB-serial port, sends a non-destructive
Meshtastic handshake, and prints whether each port responded — same
output a remote client would get from POST /transports/usb/scan.`,
	RunE: func(c *cobra.Command, _ []string) error {
		log := logger.With(slog.String("subsystem", "usb.scan"))
		log.Debug("running", slog.Int("timeout_ms", 1500))
		var srv usbOps = localServer(c)
		hits, err := srv.ScanUSB(c.Context(), 1500)
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
		_ = tw.Flush()
		return nil
	},
}

// usbConnectCmd opens the TUI against a specific serial device.
// No arg = auto-detect through the in-process server, one arg =
// explicit device path.
var usbConnectCmd = &cobra.Command{
	Use:   "connect [device]",
	Short: "Open the TUI over USB serial",
	Long: `Connect to a Meshtastic radio over USB serial and open the TUI.

  meshx usb connect                    # auto-detect the only radio on USB
  meshx usb connect /dev/cu.usbserial… # explicit device path (macOS)
  meshx usb connect /dev/ttyUSB0       # explicit device path (Linux)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		dest := ""
		if len(args) == 1 {
			dest = args[0]
		}
		log := logger.With(slog.String("subsystem", "usb.connect"))
		log.Debug("running", slog.String("device", dest))
		if dest == "" {
			var srv usbOps = localServer(c)
			auto, err := srv.AutoDetectUSB(c.Context(), 1500)
			if err != nil {
				return fmt.Errorf(
					"usb auto-detect: %w — try `meshx usb scan` to see candidates",
					err,
				)
			}
			dest = auto
			log.Debug("auto-detected", slog.String("device", dest))
		}
		return runRadio(dest)
	},
}

func init() {
	usbCmd.AddCommand(usbScanCmd)
	usbCmd.AddCommand(usbConnectCmd)
	usbCmd.AddCommand(probeCmd)
	rootCmd.AddCommand(usbCmd)
}

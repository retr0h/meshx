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
	"time"

	"github.com/retr0h/meshx/internal/meshx/transport"
	"github.com/spf13/cobra"
)

// usbCmd is the parent for every USB-serial operation: scanning,
// identifying, and opening a TUI session against a specific device.
// Mirrors `ble` and `tcp` so the CLI tree reads consistently —
// transport family at the top, verb underneath.
var usbCmd = &cobra.Command{
	Use:   "usb",
	Short: "USB-serial Meshtastic transport",
	Long: `Commands for discovering, identifying, and connecting to
Meshtastic radios over a USB-serial cable (the default transport
for a desk-mounted or data-cable-tethered radio).`,
}

// usbConnectCmd opens the TUI against a specific serial device.
// No arg = auto-detect (same heuristic bare `meshx` uses), one arg
// = explicit device path.
var usbConnectCmd = &cobra.Command{
	Use:   "connect [device]",
	Short: "Open the TUI over USB serial",
	Long: `Connect to a Meshtastic radio over USB serial and open the TUI.

  meshx usb connect                    # auto-detect the only radio on USB
  meshx usb connect /dev/cu.usbserial… # explicit device path (macOS)
  meshx usb connect /dev/ttyUSB0       # explicit device path (Linux)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		dest := ""
		if len(args) == 1 {
			dest = args[0]
		}
		log := logger.With(slog.String("subsystem", "usb.connect"))
		log.Debug("running", slog.String("device", dest))
		if dest == "" {
			auto, err := transport.AutoDetectMeshtastic(1500 * time.Millisecond)
			if err != nil {
				return fmt.Errorf(
					"usb auto-detect: %w — try `meshx usb probe` to see candidates",
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
	usbCmd.AddCommand(usbConnectCmd)
	usbCmd.AddCommand(probeCmd)
	rootCmd.AddCommand(usbCmd)
}

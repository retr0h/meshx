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

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/tui"
)

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
			// AutoDetect doesn't need the sqlite store — construct a
			// storeless Manager.
			mgr := newTransportsManager(nil)
			auto, err := mgr.AutoDetectUSB(context.Background(), 1500)
			if err != nil {
				return fmt.Errorf(
					"usb auto-detect: %w — try `meshx usb scan` to see candidates",
					err,
				)
			}
			dest = auto
			log.Debug("auto-detected", slog.String("device", dest))
		}
		return tui.RunRadio(dest)
	},
}

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

import "github.com/spf13/cobra"

// clientScanCmd is the parent that groups `scan ble` and `scan usb`.
// Mirrors `meshx ble scan` / `meshx usb scan` but routes through the
// daemon's HTTP API so the daemon's adapter is the one doing the
// scanning — clients can't reach the hardware directly while a
// daemon owns it.
var clientScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Discover radios via the daemon (ble | usb)",
	Long: `Asks the daemon to scan for nearby radios using its own BLE or
USB adapter. The daemon does the work; the client just prints the
results. Use the discovered UUID/port with the appropriate next
command (pair for BLE, server-start with --radio for USB).

  meshx client scan ble    # POST /transports/ble/scan
  meshx client scan usb    # POST /transports/usb/scan`,
}

func init() {
	clientScanCmd.AddCommand(clientScanBLECmd)
	clientScanCmd.AddCommand(clientScanUSBCmd)
}

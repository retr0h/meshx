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

import "github.com/spf13/cobra"

// usbCmd is the parent for every USB-serial one-shot operation:
// scanning, identifying, and opening a TUI session against a
// specific device. Mirrors `ble` so the CLI tree reads consistently.
//
// These subcommands are CLI-local — they directly enumerate and
// probe USB-serial ports through cliUSBScanner. No daemon is
// required, no HTTP is involved. The daemon's /transports/usb/*
// routes exist for remote admin (a future web UI inspecting USB
// state on a headless box).
//
// Each subcommand lives in its own usb_<verb>.go file; this file is
// just the parent + the init wiring. usb_probe.go is the diagnostic
// deep-dump tool sibling.
var usbCmd = &cobra.Command{
	Use:   "usb",
	Short: "USB-serial Meshtastic transport",
	Long: `Commands for discovering, identifying, and connecting to
Meshtastic radios over a USB-serial cable (the default transport
for a desk-mounted or data-cable-tethered radio).`,
}

func init() {
	usbCmd.AddCommand(usbScanCmd)
	usbCmd.AddCommand(usbConnectCmd)
	usbCmd.AddCommand(probeCmd)
	rootCmd.AddCommand(usbCmd)
}

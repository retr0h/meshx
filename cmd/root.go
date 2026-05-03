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

// Package cmd contains the meshx cobra command tree.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/meshx/transport"
	"github.com/retr0h/meshx/internal/server"
	"github.com/retr0h/meshx/internal/tui"
)

// banner is the ASCII-art header printed above the long help text.
// Block style matches the BitchX-style splash variants in
// internal/tui/components_splash.go so the CLI visual identity is
// consistent with the TUI launch banner.
const banner = `
█▀▄▀█ █▀▀ █▀▀ █░░█ ▀▄▀
█░▀░█ █▀▀ ▀▀█ █▀▀█ █▀█
▀░░░▀ ▀▀▀ ▀▀▀ ▀░░▀ ▀░▀
`

var rootCmd = &cobra.Command{
	Use:   "meshx",
	Short: "Glitched-out terminal Meshtastic messenger",
	Long: banner + `
meshx is an irssi-style terminal Meshtastic messenger — an
irssi/BitchX/mutt-inspired chat client for your LoRa radio with a
vintage BBS aesthetic.

Usage patterns:

  meshx                          # auto-connect: USB if plugged in,
                                 # else saved Bluetooth favorite
  meshx usb connect [dev]        # open TUI over USB serial
  meshx ble connect <uuid|name>  # open TUI over Bluetooth (paired device)
  meshx serve start              # run the headless HTTP+SSE daemon

Transport-specific commands:

  meshx usb probe                # list USB candidates
  meshx ble scan                 # scan for nearby Bluetooth radios
  meshx ble pair <uuid>          # save a device for future connects
  meshx ble list                 # show saved Bluetooth devices
  meshx ble fav  <uuid|name>     # auto-connect target for bare meshx
  meshx ble forget <uuid|name>
  meshx ble disconnect           # clear the auto-connect favorite`,
	RunE: func(c *cobra.Command, _ []string) error {
		// USB auto-detect first so a plugged-in radio always wins —
		// short timeout keeps bare `meshx` snappy when nothing's
		// connected.
		if dev, err := transport.AutoDetectMeshtastic(1500 * time.Millisecond); err == nil {
			return tui.RunRadio(dev)
		}

		// BLE fallback through the in-process server — same code path
		// the daemon's /transports endpoints would follow.
		srv := localServer(c)
		target, _, err := srv.ResolveBLEAutoConnect()
		if err != nil {
			if errors.Is(err, server.ErrNoTransport) {
				return fmt.Errorf(
					"%s\n  → `meshx usb probe` to list USB candidates\n  → `meshx ble scan` to discover nearby radios",
					err,
				)
			}
			return err
		}
		uuid, ok := strings.CutPrefix(target, "ble:")
		if !ok {
			return fmt.Errorf("unexpected target shape: %q", target)
		}
		return tui.RunRadio("ble:" + uuid)
	},
}

// Execute runs the root command; invoked by main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

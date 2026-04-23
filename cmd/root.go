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
	"fmt"
	"os"
	"strings"

	"github.com/retr0h/meshx/internal/meshx"
	"github.com/spf13/cobra"
)

// rootCmd handles bare `meshx` by running the auto-connect
// resolution chain (USB first, saved Bluetooth as fallback). The
// typed subcommands (usb / tcp / ble / demo) live in their own
// files and attach to this root via init().
var rootCmd = &cobra.Command{
	Use:   "meshx",
	Short: "Glitched-out terminal Meshtastic messenger",
	Long: `meshx is an irssi-style terminal Meshtastic messenger — an
irssi/BitchX/mutt-inspired chat client for your LoRa radio with a
vintage BBS aesthetic.

Usage patterns:

  meshx                        # auto-connect: USB if plugged in,
                               # else saved Bluetooth favorite
  meshx usb connect [dev]      # open TUI over USB serial
  meshx tcp connect host[:p]   # open TUI over TCP
  meshx ble connect <uuid|name># open TUI over Bluetooth (paired device)
  meshx demo                   # canned-fixture UI, no radio needed

Transport-specific commands:

  meshx usb probe              # list USB candidates
  meshx ble scan               # scan for nearby Bluetooth radios
  meshx ble pair <uuid>        # save a device for future connects
  meshx ble list               # show saved Bluetooth devices
  meshx ble fav  <uuid|name>   # auto-connect target for bare meshx
  meshx ble forget <uuid|name>
  meshx ble disconnect         # clear the auto-connect favorite`,
	RunE: func(_ *cobra.Command, _ []string) error {
		target, err := meshx.AutoConnectTarget()
		if err != nil {
			return err
		}
		if rest, ok := strings.CutPrefix(target, "ble:"); ok {
			// Bluetooth fallback — delegate to the BLE session
			// launcher. Until the transport lands, this returns a
			// "not yet implemented" error that's much nicer than
			// cobra dumping the help text.
			return meshx.RunBLE(rest)
		}
		return meshx.RunRadio(target)
	},
}

// Execute runs the root command; invoked by main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

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

	"github.com/spf13/cobra"
)

// banner is the ASCII-art header printed before cobra's auto-generated
// help text. Block style matches the BitchX-style splash variants in
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
	Long: `meshx is an irssi-style terminal Meshtastic messenger — an
irssi/BitchX/mutt-inspired chat client for your LoRa radio with a
vintage BBS aesthetic.

Pick a transport explicitly:

  meshx usb connect [dev]        # open the TUI over USB serial
  meshx ble connect <uuid|name>  # open the TUI over Bluetooth (paired)
  meshx serve start              # run the headless HTTP+SSE daemon`,
	RunE: func(c *cobra.Command, _ []string) error {
		_, _ = fmt.Fprint(c.OutOrStdout(), banner)
		return c.Help()
	},
}

// Execute runs the root command; invoked by main. SilenceUsage
// drops the help-text dump on runtime failures (auto-connect with
// no radios, bind:port in use, …) where it's just noise. Cobra
// already prints "Error: <err>" on its own.
func Execute() {
	rootCmd.SilenceUsage = true
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

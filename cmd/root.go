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
	"time"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/meshx"
	"github.com/retr0h/meshx/internal/meshx/transport"
)

var (
	demoFlag bool
	portFlag string
)

var rootCmd = &cobra.Command{
	Use:   "meshx",
	Short: "Glitched-out terminal Meshtastic messenger",
	Long: `meshx is an irssi-style terminal Meshtastic messenger — an irssi/BitchX/mutt-
inspired chat client for your LoRa radio with a vintage BBS aesthetic.

Default behavior: auto-detect a connected Meshtastic radio on USB and
launch the live UI. Use "meshx probe" to see what devices are
available, or --port <path> to pick one explicitly.

Pass --demo to run the UI with canned data — no radio required.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if demoFlag {
			return meshx.RunDemo()
		}
		dest := portFlag
		if dest == "" {
			auto, err := transport.AutoDetectMeshtastic(1500 * time.Millisecond)
			if err != nil {
				return err
			}
			dest = auto
		}
		return meshx.RunRadio(dest)
	},
}

// Execute runs the root command; invoked by main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(
		&demoFlag, "demo", false,
		"Run the UI with canned data — no radio required.",
	)
	rootCmd.PersistentFlags().StringVar(
		&portFlag, "port", "",
		"Serial device path (e.g. /dev/cu.usbmodem2101) or TCP host[:port]. Auto-detects when omitted.",
	)
}

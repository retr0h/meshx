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

	"github.com/retr0h/meshx/internal/meshx"
)

var demoFlag bool

var rootCmd = &cobra.Command{
	Use:   "meshx",
	Short: "Glitched-out terminal Meshtastic messenger",
	Long: `meshx is a terminal Meshtastic messenger with a vintage BBS aesthetic —
three-pane mutt-style layout, vim keybindings, ham-radio shortcuts, and
a ░▒▓█ glitch palette borrowed from tlock and grind.

Pass --demo to boot the static layout preview with canned channels,
messages, and nodes. No radio required. This is the default until the
Meshtastic transport layer lands.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if demoFlag {
			return meshx.RunDemo()
		}
		// Until the Meshtastic transport layer lands, there is no
		// non-demo mode to run. Point the user at --demo.
		return fmt.Errorf("no radio transport wired up yet — try: meshx --demo")
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
		"Show the static demo layout (canned channels, messages, nodes). No radio required.",
	)
}

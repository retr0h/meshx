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
	"github.com/retr0h/meshx/internal/meshx"
	"github.com/spf13/cobra"
)

// demoCmd launches the TUI with the canned demo fixture — same UI
// and feature surface as a live radio session, no hardware needed.
// Replaces the old `--demo` top-level flag so `meshx demo` sits
// alongside `meshx usb connect` / `meshx tcp connect` / `meshx ble
// connect` as peer verbs under the same tree.
var demoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Run the UI with canned data — no radio required",
	Long: `Launch the meshX TUI populated from a hand-curated Demo
fixture. Every feature renders exactly as it does in live mode; use
this to kick the tires on the UI without a radio, or to capture
screenshots for docs.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return meshx.RunDemo()
	},
}

func init() {
	rootCmd.AddCommand(demoCmd)
}

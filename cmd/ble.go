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

// bleCmd groups every Bluetooth LE one-shot operation. Pair once via
// `ble pair <uuid>`, then switch between saved radios with
// `meshx ble connect <name>`. The favorite flag (`ble fav`) picks
// which saved device bare-launch flows fall through to.
//
// These subcommands are CLI-local — they directly interrogate the
// host's Bluetooth adapter (via cliBLEScanner / cliBLEPairer) and the
// SQLite store (via cliOpenBLEStore). No daemon is required, no HTTP
// is involved. The daemon's /transports/ble/* routes exist for
// remote admin (a future web UI inspecting Bluetooth state on a
// headless box) and reuse the same transport / storage packages
// behind their own consumer interfaces.
//
// Each subcommand lives in its own ble_<verb>.go file; this file is
// just the parent + the init wiring.
var bleCmd = &cobra.Command{
	Use:   "ble",
	Short: "Bluetooth LE Meshtastic transport",
	Long: `Commands for discovering, pairing, and connecting to
Meshtastic radios over Bluetooth LE. Pair a device once; its uuid
is saved to the local SQLite store at ~/.meshx/meshx.db.`,
}

// orDash renders empty strings as an em-dash so tabwriter-aligned
// tables don't have ragged blank cells.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func init() {
	bleCmd.AddCommand(bleScanCmd)
	bleCmd.AddCommand(blePairCmd)
	bleCmd.AddCommand(bleListCmd)
	bleCmd.AddCommand(bleForgetCmd)
	bleCmd.AddCommand(bleConnectCmd)
	bleCmd.AddCommand(bleDisconnectCmd)
	bleCmd.AddCommand(bleFavCmd)
	rootCmd.AddCommand(bleCmd)
}

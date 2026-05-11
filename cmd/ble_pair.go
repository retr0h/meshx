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
)

var blePairCmd = &cobra.Command{
	Use:   "pair <uuid>",
	Short: "Pair with a Meshtastic radio over BLE",
	Long: `Initiates OS-level Bluetooth pairing with the device at <uuid>
and persists it to the local pairing table.

macOS handles the 6-digit PIN through the system pairing dialog;
Linux goes through the BlueZ agent.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		uuid := args[0]
		logger.With(slog.String("subsystem", "ble.pair")).
			Debug("running", slog.String("uuid", uuid))
		fmt.Printf("pairing %s …\n", uuid)
		fmt.Println("  if your OS pops a Bluetooth pair prompt, enter the PIN shown on the radio.")
		mgr, closeFn, err := cliTransports()
		if err != nil {
			return fmt.Errorf("open store: %w", err)
		}
		defer closeFn()
		view, err := mgr.PairBLE(context.Background(), uuid)
		if err != nil {
			return err
		}
		fmt.Printf("paired with %s, saved to ~/.meshx/meshx.db\n", view.UUID)
		fmt.Println()
		fmt.Println("next steps:")
		fmt.Printf("  - `meshx ble connect %s` to open the TUI\n", view.UUID)
		fmt.Printf("  - `meshx ble fav %s` to make bare `meshx` auto-connect here\n", view.UUID)
		return nil
	},
}

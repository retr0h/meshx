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
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
)

var bleForgetCmd = &cobra.Command{
	Use:   "forget <uuid|name>",
	Short: "Remove a saved Bluetooth device",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		logger.With(slog.String("subsystem", "ble.forget")).
			Debug("running", slog.String("target", args[0]))
		store, err := cliOpenBLEStore()
		if err != nil {
			return fmt.Errorf("open store: %w", err)
		}
		defer func() { _ = store.Close() }()
		d, err := store.LookupBLEDevice(args[0])
		if err != nil {
			return fmt.Errorf("lookup: %w", err)
		}
		if d == nil {
			return fmt.Errorf("no saved device matches %q (run `meshx ble list`)", args[0])
		}
		if err := store.ForgetBLEDevice(d.UUID); err != nil {
			return fmt.Errorf("forget: %w", err)
		}
		fmt.Printf("forgot %s\n", args[0])
		return nil
	},
}

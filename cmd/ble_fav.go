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

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
)

var bleFavCmd = &cobra.Command{
	Use:   "fav <uuid|name>",
	Short: "Mark a saved device as the bare-launch favorite",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		logger.With(slog.String("subsystem", "ble.fav")).
			Debug("running", slog.String("target", args[0]))
		mgr, closeFn, err := cliTransports()
		if err != nil {
			return fmt.Errorf("open store: %w", err)
		}
		defer closeFn()
		view, err := mgr.SetBLEFavorite(context.Background(), args[0])
		if err != nil {
			return err
		}
		fmt.Printf("★ %s is now the auto-connect favorite\n", view.UUID)
		return nil
	},
}

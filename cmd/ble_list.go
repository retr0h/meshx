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
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var bleListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show saved Bluetooth devices",
	RunE: func(_ *cobra.Command, _ []string) error {
		logger.With(slog.String("subsystem", "ble.list")).Debug("running")
		mgr, closeFn, err := cliTransports()
		if err != nil {
			return fmt.Errorf("open store: %w", err)
		}
		defer closeFn()
		devs, err := mgr.ListBLEDevices(context.Background())
		if err != nil {
			return fmt.Errorf("load: %w", err)
		}
		if len(devs) == 0 {
			fmt.Println("no saved Bluetooth devices.")
			fmt.Println()
			fmt.Println("  → run `meshx ble scan` to discover nearby radios,")
			fmt.Println("    then `meshx ble pair <uuid>` to save one.")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "   UUID\tLONGNAME\tSHORTNAME\tHW")
		for _, d := range devs {
			star := "  "
			if d.Favorite {
				star = " ★"
			}
			_, _ = fmt.Fprintf(
				tw, "%s %s\t%s\t%s\t%s\n",
				star, d.UUID, orDash(d.LongName), orDash(d.ShortName), orDash(d.HWModel),
			)
		}
		return tw.Flush()
	},
}

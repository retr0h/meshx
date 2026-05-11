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
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var bleScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan for nearby Meshtastic radios over BLE",
	Long: "Runs a 10-second BLE scan and prints every peripheral that\n" +
		"advertises the Meshtastic service uuid. The uuid shown here\n" +
		"is what `meshx ble pair` accepts.",
	RunE: func(_ *cobra.Command, _ []string) error {
		logger.With(slog.String("subsystem", "ble.scan")).
			Debug("running", slog.Int("timeout_ms", 10000))
		// Scan doesn't touch the pairing store — construct a Manager
		// without sqlite so a missing meshx.db (fresh install, no
		// home dir) doesn't block discovery.
		mgr := newTransportsManager(nil)
		hits, err := mgr.ScanBLE(context.Background(), 10000)
		if err != nil {
			return err
		}
		if len(hits) == 0 {
			fmt.Println("no Meshtastic radios responded.")
			fmt.Println()
			fmt.Println("troubleshooting:")
			fmt.Println("  - confirm Bluetooth is on for both the host and the radio")
			fmt.Println("  - the radio must have BLE enabled in its config (default)")
			fmt.Println("  - on macOS, grant the first-time Bluetooth permission prompt")
			return nil
		}
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].RSSI != hits[j].RSSI {
				return hits[i].RSSI > hits[j].RSSI
			}
			return hits[i].LocalName < hits[j].LocalName
		})
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "UUID\tNAME\tRSSI")
		for _, h := range hits {
			name := h.LocalName
			if name == "" {
				name = "—"
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%d dBm\n", h.UUID, name, h.RSSI)
		}
		_ = tw.Flush()
		fmt.Println()
		fmt.Println("  → `meshx ble pair <uuid>` to save one of these")
		return nil
	},
}

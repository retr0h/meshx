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
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/server"
	"github.com/retr0h/meshx/internal/tui"
)

// bleCmd groups every Bluetooth LE operation. Pair once via
// `ble pair <uuid>`, then switch between saved radios with
// `meshx ble connect <name>`. The favorite flag (`ble fav`) picks
// which saved device bare `meshx` falls through to when no USB
// radio is connected.
//
// Every subcommand here goes through the in-process server — same
// code path the daemon's HTTP routes use. The CLI is just a local
// HTTP-less consumer of the same surface; nothing here knows about
// storage or transport directly.
var bleCmd = &cobra.Command{
	Use:   "ble",
	Short: "Bluetooth LE Meshtastic transport",
	Long: `Commands for discovering, pairing, and connecting to
Meshtastic radios over Bluetooth LE. Pair a device once; its uuid
is saved to the local SQLite store and reachable through the daemon
API for both this CLI and remote clients.`,
}

// localServer constructs an in-process server with the daemon's
// dependencies. The HTTP listener is never started — cmd just calls
// handler methods directly so CLI ops execute the same code path
// the HTTP daemon does.
func localServer(cmd *cobra.Command) *server.Server {
	store, scanner, pairer := serveDeps(cmd)
	return server.New(server.Config{
		Radios:  server.NewRegistry(),
		Store:   store,
		Scanner: scanner,
		Pairer:  pairer,
	})
}

var bleScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan for nearby Meshtastic radios over BLE",
	Long: "Runs a 10-second BLE scan and prints every peripheral that\n" +
		"advertises the Meshtastic service uuid. The uuid shown here\n" +
		"is what `meshx ble pair` accepts.",
	RunE: func(c *cobra.Command, _ []string) error {
		srv := localServer(c)
		hits, err := srv.ScanBLE(c.Context(), 10000)
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

var blePairCmd = &cobra.Command{
	Use:   "pair <uuid>",
	Short: "Pair with a Meshtastic radio over BLE",
	Long: `Initiates OS-level Bluetooth pairing with the device at <uuid>
and persists it to the local pairing table.

macOS handles the 6-digit PIN through the system pairing dialog;
Linux goes through the BlueZ agent.`,
	Args: cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		srv := localServer(c)
		uuid := args[0]
		fmt.Printf("pairing %s …\n", uuid)
		fmt.Println("  if your OS pops a Bluetooth pair prompt, enter the PIN shown on the radio.")
		if err := srv.PairBLE(c.Context(), uuid); err != nil {
			return err
		}
		fmt.Printf("paired with %s, saved to ~/.meshx/meshx.db\n", uuid)
		fmt.Println()
		fmt.Println("next steps:")
		fmt.Printf("  - `meshx ble connect %s` to open the TUI\n", uuid)
		fmt.Printf("  - `meshx ble fav %s` to make bare `meshx` auto-connect here\n", uuid)
		return nil
	},
}

var bleListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show saved Bluetooth devices",
	RunE: func(c *cobra.Command, _ []string) error {
		srv := localServer(c)
		devs, err := srv.ListBLEDevices(c.Context())
		if err != nil {
			return err
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
			_, _ = fmt.Fprintf(tw, "%s %s\t%s\t%s\t%s\n",
				star, d.UUID, orDash(d.LongName), orDash(d.ShortName), orDash(d.HWModel),
			)
		}
		return tw.Flush()
	},
}

var bleForgetCmd = &cobra.Command{
	Use:   "forget <uuid|name>",
	Short: "Remove a saved Bluetooth device",
	Args:  cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		srv := localServer(c)
		if err := srv.ForgetBLEDevice(c.Context(), args[0]); err != nil {
			return err
		}
		fmt.Printf("forgot %s\n", args[0])
		return nil
	},
}

var bleConnectCmd = &cobra.Command{
	Use:   "connect <uuid|name>",
	Short: "Open the TUI over Bluetooth",
	Args:  cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		srv := localServer(c)
		uuid, err := srv.ResolveBLE(c.Context(), args[0])
		if err != nil {
			return err
		}
		return tui.RunRadio("ble:" + uuid)
	},
}

var bleDisconnectCmd = &cobra.Command{
	Use:   "disconnect",
	Short: "Clear the auto-connect favorite",
	RunE: func(c *cobra.Command, _ []string) error {
		srv := localServer(c)
		return srv.ClearBLEFavorite(c.Context())
	},
}

var bleFavCmd = &cobra.Command{
	Use:   "fav <uuid|name>",
	Short: "Mark a saved device as the bare-launch favorite",
	Args:  cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		srv := localServer(c)
		view, err := srv.SetBLEFavoriteByName(c.Context(), args[0])
		if err != nil {
			return err
		}
		fmt.Printf("★ %s is now the auto-connect favorite\n", view.UUID)
		return nil
	},
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// silence unused import linter — context is consumed via cobra.Cmd.Context.
var _ = context.Background

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

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
	"os"
	"text/tabwriter"

	"github.com/retr0h/meshx/internal/meshx"
	"github.com/spf13/cobra"
)

// bleCmd groups every Bluetooth LE operation. Pair once via
// `ble pair <uuid>`, then switch between saved radios with
// `meshx ble connect <name>`. The favorite flag (`ble fav`) picks
// which saved device bare `meshx` falls through to when no USB
// radio is connected.
//
// The actual BLE transport is defined in
// internal/meshx/transport/ble.go. Commands in this file shell out
// to meshx.BLE* helpers that thread through that transport.
var bleCmd = &cobra.Command{
	Use:   "ble",
	Short: "Bluetooth LE Meshtastic transport",
	Long: `Commands for discovering, pairing, and connecting to
Meshtastic radios over Bluetooth LE. Pair a device once; its uuid
and friendly names are saved to the local SQLite store so future
sessions can reconnect by uuid, longname, or shortname.`,
}

// bleScanCmd performs a timed BLE scan and prints every radio that
// advertises the Meshtastic service UUID. Hardware-only; requires
// an active Bluetooth adapter on the host.
var bleScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan for nearby Meshtastic radios over BLE",
	Long: "Runs a 10-second BLE scan and prints every peripheral that\n" +
		"advertises the Meshtastic service uuid. Column output is:\n\n" +
		"  UUID                                  Name            RSSI\n\n" +
		"The uuid shown here is what `meshx ble pair` accepts.",
	RunE: func(_ *cobra.Command, _ []string) error {
		return meshx.BLEScan(os.Stdout)
	},
}

// blePairCmd walks the user through OS-level Bluetooth pairing for
// a specific device and persists it to the ble_devices table.
var blePairCmd = &cobra.Command{
	Use:   "pair <uuid>",
	Short: "Pair with a Meshtastic radio over BLE",
	Long: `Initiates OS-level Bluetooth pairing with the device at <uuid>.

macOS: the system pairing dialog handles the 6-digit PIN that the
radio displays on its OLED. Accept it in the dialog; meshx will
record the device on success.

Linux: pairing goes through the BlueZ agent at /org/bluez/agent.
You'll be prompted on stdin for the PIN shown on the radio.

On success, the device is saved to ~/.meshx/meshx.db and shows up
in ` + "`meshx ble list`" + ` for future connects.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return meshx.BLEPair(os.Stdout, os.Stdin, args[0])
	},
}

// bleListCmd prints the saved Bluetooth devices. Columns fit on a
// typical 120-col terminal; the first row of output is highlighted
// with `★` when a favorite is set.
var bleListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show saved Bluetooth devices",
	Long: `Prints every device previously paired with ` + "`meshx ble pair`" + `.
The favorite (if any) is marked with a leading ★. That device is
what bare ` + "`meshx`" + ` falls through to when no USB radio is plugged in.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		devs, err := meshx.BLEListDevices()
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

// bleForgetCmd removes a paired device from persistence. Accepts
// uuid OR friendly name (longname / shortname). Does NOT unpair
// at the OS level — macOS doesn't expose that, and on Linux the
// user can run `bluetoothctl remove <mac>` separately if they
// want to scrub it from BlueZ.
var bleForgetCmd = &cobra.Command{
	Use:   "forget <uuid|name>",
	Short: "Remove a saved Bluetooth device",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return meshx.BLEForget(os.Stdout, args[0])
	},
}

// bleConnectCmd opens the TUI against a saved Bluetooth device.
// `meshx ble connect <name>` is the everyday "launch meshx pointed
// at my radio" command when Bluetooth is the transport.
var bleConnectCmd = &cobra.Command{
	Use:   "connect <uuid|name>",
	Short: "Open the TUI over Bluetooth",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return meshx.RunBLE(args[0])
	},
}

// bleDisconnectCmd is a state-only command — it doesn't close an
// active session (there isn't one outside of the TUI), it just
// clears the favorite flag so bare `meshx` stops auto-connecting
// to the previously-marked device.
var bleDisconnectCmd = &cobra.Command{
	Use:   "disconnect",
	Short: "Clear the auto-connect favorite",
	Long: `Removes the ★ favorite flag from whichever saved device
currently holds it. After this, bare ` + "`meshx`" + ` will stop falling
through to Bluetooth when no USB radio is connected, unless there's
exactly one saved device in which case it keeps working.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return meshx.BLESetFavorite("")
	},
}

// bleFavCmd marks a saved device as THE auto-connect target for
// bare `meshx`. Only one device at a time holds the flag;
// re-setting moves it. Pairs with `ble disconnect` to clear.
var bleFavCmd = &cobra.Command{
	Use:   "fav <uuid|name>",
	Short: "Mark a saved device as the bare-launch favorite",
	Long: `With multiple saved Bluetooth radios, bare ` + "`meshx`" + ` needs
a tiebreaker when no USB radio is plugged in. ` + "`meshx ble fav`" + ` sets
that tiebreaker. The marked device appears with a leading ★ in
` + "`meshx ble list`" + `.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return meshx.BLEMarkFavorite(os.Stdout, args[0])
	},
}

// orDash returns "—" instead of an empty string so the tabwriter
// output reads clean (no trailing blank columns). Callers using
// this pipe every optional field through it.
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

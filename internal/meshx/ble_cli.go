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

package meshx

import (
	"database/sql"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/retr0h/meshx/internal/meshx/transport"
	"tinygo.org/x/bluetooth"
)

// meshtasticServiceUUID is the Meshtastic BLE GATT service UUID.
// Every Meshtastic radio advertises this service; `meshx ble scan`
// filters to peripherals that include it so we don't surface every
// random Bluetooth device in the area.
const meshtasticServiceUUID = "6ba1b218-15a8-461f-9fa8-5dcae273eafd"

// BLEDeviceView is the public projection of a saved Bluetooth
// device for CLI rendering (tabwriter output in `meshx ble list`).
// Kept separate from the internal bleDevice struct so the package's
// cross-cutting types don't leak into the cmd layer.
type BLEDeviceView struct {
	UUID      string
	LongName  string
	ShortName string
	HWModel   string
	Favorite  bool
}

// openSharedStorage opens the same sqlite the live-radio TUI uses,
// running migrations if needed. Returns nil db (no error) when
// $HOME resolution fails so the CLI degrades to "nothing saved"
// rather than dying before it can print a helpful error. Callers
// are responsible for closing the db when done.
func openSharedStorage() (*sql.DB, error) {
	path, err := defaultStoragePath()
	if err != nil {
		return nil, fmt.Errorf("storage path: %w", err)
	}
	db, _, err := openStorage(path)
	if err != nil {
		return nil, fmt.Errorf("open storage: %w", err)
	}
	return db, nil
}

// BLEListDevices reads saved Bluetooth devices from sqlite and
// returns the public view slice. Empty when nothing is paired yet.
func BLEListDevices() ([]BLEDeviceView, error) {
	db, err := openSharedStorage()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	raw, err := loadBLEDevices(db)
	if err != nil {
		return nil, err
	}
	out := make([]BLEDeviceView, 0, len(raw))
	for _, d := range raw {
		out = append(out, BLEDeviceView(d))
	}
	return out, nil
}

// BLEForget removes a saved device and prints a confirmation line.
// Accepts uuid or friendly name — resolves via lookupBLEDevice.
// Unknown names print a hint rather than erroring silently.
func BLEForget(out io.Writer, target string) error {
	db, err := openSharedStorage()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	d, err := lookupBLEDevice(db, target)
	if err != nil {
		return err
	}
	if d == nil {
		return fmt.Errorf("no saved device matches %q (run `meshx ble list`)", target)
	}
	if err := forgetBLEDevice(db, d.UUID); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "forgot %s (%s)\n", d.DisplayName(), d.UUID)
	return nil
}

// BLESetFavorite clears the favorite flag (empty uuid) or sets it
// on a specific device. Intended for the `ble disconnect` flow
// where the user is clearing auto-connect without naming a device.
func BLESetFavorite(uuid string) error {
	db, err := openSharedStorage()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return setBLEFavorite(db, uuid)
}

// BLEMarkFavorite resolves a name-or-uuid target to a saved device
// and sets it as the favorite, printing a confirmation line. This
// is the pair the CLI `ble fav` command uses — `BLESetFavorite` on
// its own only takes a uuid.
func BLEMarkFavorite(out io.Writer, target string) error {
	db, err := openSharedStorage()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	d, err := lookupBLEDevice(db, target)
	if err != nil {
		return err
	}
	if d == nil {
		return fmt.Errorf("no saved device matches %q (run `meshx ble list`)", target)
	}
	if err := setBLEFavorite(db, d.UUID); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "★ %s (%s) is now the auto-connect favorite\n", d.DisplayName(), d.UUID)
	return nil
}

// BLEScan runs a 10-second Bluetooth scan and prints every
// peripheral that advertises the Meshtastic service UUID in a
// tabwriter-aligned table. The UUID column is what `meshx ble
// pair` accepts. Requires the host Bluetooth adapter to be on;
// permission prompts (macOS) fire on first invocation.
func BLEScan(out io.Writer) error {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return fmt.Errorf("enable bluetooth adapter: %w — is Bluetooth on?", err)
	}

	// Collect unique peripherals keyed by address so repeated
	// advertisement packets don't duplicate rows. Locked because
	// the scan callback fires from the adapter's goroutine.
	type hit struct {
		address   string
		localName string
		rssi      int16
		seen      time.Time
	}
	var (
		mu   sync.Mutex
		hits = map[string]*hit{}
	)

	_, _ = fmt.Fprintln(out, "scanning for Meshtastic radios over BLE (10s)…")

	wantUUID, err := bluetooth.ParseUUID(meshtasticServiceUUID)
	if err != nil {
		return fmt.Errorf("parse service uuid: %w", err)
	}

	// Kick off the scan in a goroutine so we can stop it after the
	// timeout. The callback filters for our service uuid; devices
	// that don't advertise it aren't interesting to us.
	scanDone := make(chan error, 1)
	go func() {
		scanDone <- adapter.Scan(func(_ *bluetooth.Adapter, res bluetooth.ScanResult) {
			if !slices.Contains(res.ServiceUUIDs(), wantUUID) {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			addr := res.Address.String()
			h, ok := hits[addr]
			if !ok {
				h = &hit{address: addr}
				hits[addr] = h
			}
			h.localName = res.LocalName()
			h.rssi = res.RSSI
			h.seen = time.Now()
		})
	}()

	select {
	case err := <-scanDone:
		if err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}
	case <-time.After(10 * time.Second):
		if err := adapter.StopScan(); err != nil {
			return fmt.Errorf("stop scan: %w", err)
		}
		// Drain the scan goroutine so we don't leak it.
		<-scanDone
	}

	mu.Lock()
	defer mu.Unlock()
	if len(hits) == 0 {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "no Meshtastic radios responded.")
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "troubleshooting:")
		_, _ = fmt.Fprintln(out, "  - confirm Bluetooth is on for both the host and the radio")
		_, _ = fmt.Fprintln(
			out,
			"  - the radio must have BLE enabled in its config (Meshtastic default)",
		)
		_, _ = fmt.Fprintln(out, "  - on macOS, grant the first-time Bluetooth permission prompt")
		return nil
	}

	// Stable sort by RSSI descending (strongest signal first) so
	// the user's own radio — usually the closest — lands at the
	// top of the table. Alphabetical ties broken by name.
	ordered := make([]*hit, 0, len(hits))
	for _, h := range hits {
		ordered = append(ordered, h)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].rssi != ordered[j].rssi {
			return ordered[i].rssi > ordered[j].rssi
		}
		return ordered[i].localName < ordered[j].localName
	})

	_, _ = fmt.Fprintln(out)
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "UUID\tNAME\tRSSI")
	for _, h := range ordered {
		name := h.localName
		if name == "" {
			name = "—"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d dBm\n", h.address, name, h.rssi)
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "  → `meshx ble pair <uuid>` to save one of these")
	return nil
}

// BLEPair registers a Bluetooth device in the local ble_devices
// table so it shows up in `meshx ble list` and can be targeted by
// `meshx ble connect`. The OS-level pairing / bonding dialog still
// has to be accepted separately (the platform handles it on first
// Connect — macOS pops a system prompt, Linux goes through BlueZ's
// agent) before the TUI can actually exchange data.
//
// Metadata (longname / shortname / hardware) is left empty and
// filled in lazily: the first time the user runs `meshx ble
// connect <uuid>` successfully, the live session's upsertNode
// populates those fields from the radio's MyNodeInfo stream.
func BLEPair(out io.Writer, _ io.Reader, uuid string) error {
	db, err := openSharedStorage()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if err := saveBLEDevice(db, bleDevice{UUID: uuid}); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "saved %s to ~/.meshx/meshx.db\n", uuid)
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "next steps:")
	_, _ = fmt.Fprintln(out, "  - accept the OS Bluetooth pairing prompt if one hasn't fired yet")
	_, _ = fmt.Fprintln(out, "  - `meshx ble connect "+uuid+"` to open the TUI")
	_, _ = fmt.Fprintln(
		out,
		"  - `meshx ble fav "+uuid+"` to make bare `meshx` auto-connect here",
	)
	return nil
}

// RunBLE opens the TUI against a saved Bluetooth device. Resolves
// the name-or-uuid target from the ble_devices table, then hands
// off to RunRadio with the "ble:<uuid>" prefix so the shared
// transport.Dial dispatcher routes it through DialBLE. The pump
// doesn't need to know it's a radio over Bluetooth versus USB —
// the transport/Client abstraction keeps it oblivious.
func RunBLE(target string) error {
	db, err := openSharedStorage()
	if err != nil {
		return err
	}
	d, err := lookupBLEDevice(db, target)
	if err != nil {
		_ = db.Close()
		return err
	}
	// Close the setup-path handle before handing off to the TUI —
	// RunRadio opens its own storage handle for the live session,
	// and holding two fights the SQLite WAL lock on some systems.
	_ = db.Close()
	if d == nil {
		return fmt.Errorf(
			"no saved device matches %q — run `meshx ble list` to see what's paired",
			target,
		)
	}
	return RunRadio("ble:" + d.UUID)
}

// AutoConnectTarget resolves the bare-`meshx` fallback chain:
//  1. Exactly one USB Meshtastic radio → its device path.
//  2. No USB + exactly one saved BLE device → "ble:<uuid>".
//  3. No USB + multiple BLE + favorite set → "ble:<favorite-uuid>".
//  4. Everything else → error with a hint.
//
// The returned string is either a serial device path (handled by
// transport.Dial) or "ble:<uuid>" prefixed to flag Bluetooth
// dispatch in the root command. Keeping the prefix here rather
// than threading multiple transports into cmd/root.go means the
// root file stays a thin dispatcher.
func AutoConnectTarget() (string, error) {
	// 1. USB auto-detect — short timeout so bare `meshx` feels snappy
	//    when no radio is plugged in. Auto-detect returns an error
	//    when zero or more than one radio is present.
	if dev, err := transport.AutoDetectMeshtastic(1500 * time.Millisecond); err == nil {
		return dev, nil
	}

	// 2+3. BLE fallback — read saved devices and apply the
	//    resolution chain.
	db, err := openSharedStorage()
	if err != nil {
		return "", errNoTransport("storage: " + err.Error())
	}
	defer func() { _ = db.Close() }()
	devs, err := loadBLEDevices(db)
	if err != nil {
		return "", errNoTransport("ble list: " + err.Error())
	}
	if len(devs) == 0 {
		return "", errNoTransport(
			"no USB radio plugged in and no saved Bluetooth devices.\n" +
				"  → `meshx usb probe` to list USB candidates\n" +
				"  → `meshx ble scan` to discover nearby Bluetooth radios",
		)
	}
	if len(devs) == 1 {
		return "ble:" + devs[0].UUID, nil
	}
	for _, d := range devs {
		if d.Favorite {
			return "ble:" + d.UUID, nil
		}
	}
	names := make([]string, 0, len(devs))
	for _, d := range devs {
		names = append(names, d.DisplayName())
	}
	return "", errNoTransport(
		"multiple saved Bluetooth devices and no favorite set:\n" +
			"  - " + strings.Join(names, "\n  - ") +
			"\n  → `meshx ble fav <name>` to pick one for auto-connect\n" +
			"  → or `meshx ble connect <name>` to open an explicit session",
	)
}

// errNoTransport wraps a user-facing message in a clean error so
// cobra's RunE prints it without the "Error: " prefix eating
// formatting newlines.
func errNoTransport(msg string) error {
	return fmt.Errorf("%s", msg)
}

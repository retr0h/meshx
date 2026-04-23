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

package transport

import (
	"context"
	"fmt"
	"time"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"
	"tinygo.org/x/bluetooth"
)

// Meshtastic BLE GATT layout. These UUIDs are defined in the
// Meshtastic firmware's BluetoothPhoneAPI and never change across
// firmware versions, so hard-coding is fine (and the alternative —
// reading them from a config — doesn't help because the firmware
// itself has them baked in).
const (
	MeshtasticServiceUUID   = "6ba1b218-15a8-461f-9fa8-5dcae273eafd"
	MeshtasticFromRadioUUID = "2c55e69e-4993-11ed-b878-0242ac120002" // read: one FromRadio envelope per read
	MeshtasticToRadioUUID   = "f75c76d2-129e-4dad-a1dd-7866124401e7" // write: one ToRadio envelope per write
	MeshtasticFromNumUUID   = "ed9da18c-a800-4f66-a670-aa7547e34453" // notify: "new data available, drain FromRadio"
)

// bleScanTimeout is how long DialBLE waits in scan before declaring
// the target device unreachable. Long enough that a nearby radio
// has plenty of time to advertise (default advertising interval is
// ~200ms), short enough that a dead device fails fast.
const bleScanTimeout = 8 * time.Second

// bleReadBuf is the FromRadio scratch buffer. Meshtastic's recommended
// MTU is 517 bytes; the BLE spec caps a single read at the negotiated
// MTU minus 1 (the ATT response header). 512 leaves a little slack
// and matches what the Android client uses.
const bleReadBuf = 512

// DialBLE connects to a Meshtastic radio over Bluetooth LE by
// address/uuid. On macOS the address is a CBPeripheral UUID; on
// Linux it's a MAC. Either way the string must match what
// `meshx ble scan` printed for the target device — we scan briefly
// to rediscover it, then connect, discover the service, and pin
// the three characteristics (fromRadio, toRadio, fromNum) for the
// Run loop.
func DialBLE(addr string) (Client, error) {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return nil, fmt.Errorf("enable bluetooth adapter: %w — is Bluetooth on?", err)
	}

	// Scan to rediscover the peripheral. macOS's CoreBluetooth
	// requires a live scan result before Connect; we can't just
	// pass a saved UUID string. 8s is generous — typical Meshtastic
	// radios advertise every ~200ms.
	found, err := bleScanFor(adapter, addr, bleScanTimeout)
	if err != nil {
		return nil, err
	}

	device, err := adapter.Connect(found.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}

	// Discover the Meshtastic service. Passing the service UUID
	// filters out everything else the device happens to advertise
	// (battery level, device info) so the subsequent characteristic
	// discovery is fast.
	svcUUID, err := bluetooth.ParseUUID(MeshtasticServiceUUID)
	if err != nil {
		_ = device.Disconnect()
		return nil, fmt.Errorf("parse service uuid: %w", err)
	}
	services, err := device.DiscoverServices([]bluetooth.UUID{svcUUID})
	if err != nil {
		_ = device.Disconnect()
		return nil, fmt.Errorf("discover service: %w", err)
	}
	if len(services) == 0 {
		_ = device.Disconnect()
		return nil, fmt.Errorf(
			"device %s does not advertise the Meshtastic service — wrong device, "+
				"or the radio has Bluetooth disabled in its config",
			addr,
		)
	}
	svc := services[0]

	fromRadioUUID, _ := bluetooth.ParseUUID(MeshtasticFromRadioUUID)
	toRadioUUID, _ := bluetooth.ParseUUID(MeshtasticToRadioUUID)
	fromNumUUID, _ := bluetooth.ParseUUID(MeshtasticFromNumUUID)

	// Discover ALL characteristics on the service rather than a
	// filtered list. Two reasons: (1) on tinygo/CoreBluetooth the
	// filtered form is all-or-nothing — if any UUID in the filter
	// isn't present the whole call errors, which obscures which
	// one is missing; (2) some Meshtastic firmwares gate a subset
	// of characteristics behind pairing so the first post-pair
	// connect may still be in a transient state. Enumerate the
	// full set, match by UUID, and emit a specific error naming
	// the one that's absent.
	chars, err := svc.DiscoverCharacteristics(nil)
	if err != nil {
		_ = device.Disconnect()
		return nil, fmt.Errorf("discover characteristics: %w", err)
	}

	c := &bleClient{
		device: device,
		addr:   addr,
		notify: make(chan struct{}, 1),
	}
	var haveFromRadio, haveToRadio, haveFromNum bool
	for _, ch := range chars {
		switch ch.UUID() {
		case fromRadioUUID:
			c.fromRadio = ch
			haveFromRadio = true
		case toRadioUUID:
			c.toRadio = ch
			haveToRadio = true
		case fromNumUUID:
			c.fromNum = ch
			haveFromNum = true
		}
	}
	if !haveFromRadio || !haveToRadio || !haveFromNum {
		_ = device.Disconnect()
		var missing []string
		if !haveFromRadio {
			missing = append(missing, "fromRadio")
		}
		if !haveToRadio {
			missing = append(missing, "toRadio")
		}
		if !haveFromNum {
			missing = append(missing, "fromNum")
		}
		seen := make([]string, 0, len(chars))
		for _, ch := range chars {
			seen = append(seen, ch.UUID().String())
		}
		return nil, fmt.Errorf(
			"device %s advertises the Meshtastic service but is missing characteristics: %v\n"+
				"  characteristics found: %v\n"+
				"  (if you just paired, try disconnecting + reconnecting — some firmwares\n"+
				"   only expose the full set once the bond is established)",
			addr, missing, seen,
		)
	}

	return c, nil
}

// bleScanFor runs a filtered scan for `addr` and returns the
// matching ScanResult. Stops the scan and returns as soon as the
// target is found; returns a timeout error if it never appears.
// Kept package-internal because the CLI scan in meshx.BLEScan uses
// a different (unfiltered, table-printing) scan flow.
func bleScanFor(
	adapter *bluetooth.Adapter,
	addr string,
	timeout time.Duration,
) (bluetooth.ScanResult, error) {
	var (
		found   bluetooth.ScanResult
		foundCh = make(chan bluetooth.ScanResult, 1)
	)
	// Scan callback posts to foundCh exactly once. subsequent
	// matches (duplicate adv packets while we're stopping) drop on
	// the floor via the default in the select.
	scanErrCh := make(chan error, 1)
	go func() {
		scanErrCh <- adapter.Scan(func(a *bluetooth.Adapter, res bluetooth.ScanResult) {
			if res.Address.String() != addr {
				return
			}
			select {
			case foundCh <- res:
				_ = a.StopScan()
			default:
			}
		})
	}()
	select {
	case res := <-foundCh:
		found = res
		// Drain the scan goroutine.
		<-scanErrCh
		return found, nil
	case <-time.After(timeout):
		_ = adapter.StopScan()
		<-scanErrCh
		return bluetooth.ScanResult{}, fmt.Errorf(
			"ble: device %s not found within %s — is it powered on and in range?",
			addr, timeout,
		)
	}
}

// bleClient is the BLE transport's implementation of Client.
// Wraps a connected bluetooth.Device + the three Meshtastic
// characteristics. `notify` is a coalescing signal channel — the
// fromNum callback posts to it when new data is available, the Run
// loop drains fromRadio in response. Capacity 1 so consecutive
// notifies while we're already draining collapse into one extra
// iteration.
type bleClient struct {
	device    bluetooth.Device
	fromRadio bluetooth.DeviceCharacteristic
	toRadio   bluetooth.DeviceCharacteristic
	fromNum   bluetooth.DeviceCharacteristic
	addr      string

	notify chan struct{}
}

// Close disconnects the peripheral. Safe to call even after Run
// returns an error — tinygo/bluetooth's Disconnect is idempotent.
func (c *bleClient) Close() error {
	// Best-effort: cancel any in-flight notify subscription. The
	// library doesn't return an error from EnableNotifications(nil)
	// but on some platforms the call is a no-op; either way we
	// still want to Disconnect below.
	_ = c.fromNum.EnableNotifications(nil)
	if err := c.device.Disconnect(); err != nil {
		return fmt.Errorf("ble disconnect %s: %w", c.addr, err)
	}
	return nil
}

// Run subscribes to fromNum notifications, drains the initial
// fromRadio queue (the radio may have packets buffered from before
// we connected), then loops on:
//
//   - ctx.Done → exit.
//   - notify signal → drain fromRadio until empty.
//   - in channel → marshal and write to toRadio.
//
// This matches Meshtastic's documented BLE protocol: the radio
// signals "new data available" via the fromNum notification; the
// client reads fromRadio repeatedly until it returns an empty
// payload, meaning the queue is drained.
func (c *bleClient) Run(
	ctx context.Context,
	out chan<- *pb.FromRadio,
	in <-chan *pb.ToRadio,
) error {
	// Drain fromRadio BEFORE subscribing to fromNum. Meshtastic's
	// BLE stack uses the first read as a liveness signal — the
	// radio only starts pumping notifications after it's seen us
	// read at least once. Subscribing first can hang indefinitely
	// on some firmwares (macOS reports it as an EnableNotifications
	// timeout), because the descriptor-write ack never comes.
	// Matches the Meshtastic Python / Android reference clients'
	// ordering.
	if err := c.drainFromRadio(ctx, out); err != nil {
		return err
	}

	// Subscribe to fromNum as an OPTIMISATION. Even if this
	// silently fails (known macOS 26 / tinygo interaction where the
	// descriptor write ACK never comes), the polling ticker below
	// keeps the stream flowing. That matches how the Meshtastic
	// Python client works — it polls regardless and treats notify
	// as a nice-to-have wakeup. We surface the error but don't
	// bail on it.
	_ = c.fromNum.EnableNotifications(func(_ []byte) {
		// Coalesce: if notify is already queued, this notification
		// is redundant — the Run loop will drain everything on the
		// next iteration anyway. Non-blocking send keeps the BLE
		// stack goroutine fast and prevents back-pressure from the
		// radio pacing itself against our processing speed.
		select {
		case c.notify <- struct{}{}:
		default:
		}
	})

	// Polling ticker — backstop for platforms where fromNum
	// notifications are unreliable. 500ms is frequent enough that
	// the UI feels live but slow enough that we're not hammering
	// the radio's BLE stack.
	poll := time.NewTicker(500 * time.Millisecond)
	defer poll.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-c.notify:
			if err := c.drainFromRadio(ctx, out); err != nil {
				return err
			}

		case <-poll.C:
			// Polling drain — catches data that notify missed.
			// Most ticks will be no-ops (zero-byte reads return
			// nil immediately from drainFromRadio).
			if err := c.drainFromRadio(ctx, out); err != nil {
				return err
			}

		case msg := <-in:
			data, err := proto.Marshal(msg)
			if err != nil {
				return fmt.Errorf("marshal ToRadio: %w", err)
			}
			// Write WITH response — Meshtastic's reference clients
			// (python, android) all use response-required writes to
			// toRadio. Some firmware versions silently drop write-
			// without-response, which looks to the client like the
			// write succeeded but the radio never acts on it. The
			// extra round-trip cost is negligible at chat rates.
			if _, err := c.toRadio.Write(data); err != nil {
				return fmt.Errorf("ble write toRadio: %w", err)
			}
			// Post-write drain — the radio's response to our
			// handshake / message is sitting in fromRadio right
			// now. Draining immediately gets it into the pump
			// without waiting for the next poll tick or a notify.
			if err := c.drainFromRadio(ctx, out); err != nil {
				return err
			}
		}
	}
}

// drainFromRadio loops Read() on the fromRadio characteristic,
// unmarshals each envelope, and posts it to `out`. Stops when the
// read returns zero bytes — the Meshtastic protocol's sentinel for
// "queue empty". Respects ctx.Done between reads so a shutdown
// doesn't block inside the loop when a radio is chatty.
func (c *bleClient) drainFromRadio(ctx context.Context, out chan<- *pb.FromRadio) error {
	buf := make([]byte, bleReadBuf)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := c.fromRadio.Read(buf)
		if err != nil {
			return fmt.Errorf("ble read fromRadio: %w", err)
		}
		if n == 0 {
			return nil
		}
		msg := &pb.FromRadio{}
		if err := proto.Unmarshal(buf[:n], msg); err != nil {
			return fmt.Errorf("unmarshal FromRadio: %w", err)
		}
		select {
		case out <- msg:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

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
	"strings"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"go.bug.st/serial"
)

// DialSerial opens the named USB-serial device at 115200 8N1 — the
// Meshtastic firmware's canonical serial config. Default timeout is
// short so a stalled read is visible quickly.
func DialSerial(dev string) (Client, error) {
	port, err := serial.Open(dev, &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		return nil, fmt.Errorf("open serial %s: %w", dev, err)
	}
	return &serialClient{port: port, dev: dev}, nil
}

// ListSerialPorts returns the available USB-serial device names on
// this host, filtered to the ones that commonly host a Meshtastic
// radio. On darwin every USB-serial device shows up TWICE as both
// /dev/cu.* and /dev/tty.* — same physical device, different open
// semantics (tty.* blocks until carrier-detect, cu.* doesn't). We
// always want cu.* for a TUI, so we drop the tty.* siblings here.
func ListSerialPorts() ([]string, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range ports {
		low := strings.ToLower(p)
		// On darwin, skip the /dev/tty.* sibling of every /dev/cu.*
		// device — opening tty.* blocks waiting for DCD.
		if strings.HasPrefix(p, "/dev/tty.") {
			continue
		}
		// Keep candidates that look like a USB serial device.
		if strings.Contains(low, "usbserial") ||
			strings.Contains(low, "slab") ||
			strings.Contains(low, "wchusb") ||
			strings.Contains(low, "usbmodem") ||
			strings.HasPrefix(low, "/dev/ttyusb") ||
			strings.HasPrefix(low, "/dev/ttyacm") ||
			strings.HasPrefix(low, "com") {
			out = append(out, p)
		}
	}
	return out, nil
}

// AutoDetectSerialPort returns the first likely Meshtastic serial
// port, or an error listing candidates when the pick is ambiguous.
// The error text always suggests the exact --port flag the caller
// should paste to proceed.
func AutoDetectSerialPort() (string, error) {
	ports, err := ListSerialPorts()
	if err != nil {
		return "", err
	}
	switch len(ports) {
	case 0:
		return "", fmt.Errorf(
			"no USB-serial device found\n\n" +
				"  Troubleshooting:\n" +
				"   - Plug in your Meshtastic radio with a DATA USB cable (not charge-only)\n" +
				"   - Verify the radio is powered on (LED / screen active)\n" +
				"   - On macOS, check `ls /dev/cu.*` to see what port it enumerated as\n" +
				"   - Some radios need a driver:\n" +
				"       CH340/CH341: https://www.wch-ic.com/downloads/CH34XSER_MAC_ZIP.html\n" +
				"       CP210x:      https://www.silabs.com/developers/usb-to-uart-bridge-vcp-drivers",
		)
	case 1:
		return ports[0], nil
	default:
		// Multiple candidates — the common cause on darwin was the
		// /dev/tty.* sibling which we now filter out, so landing here
		// means the user has more than one radio or another USB-serial
		// device connected. Pick the first and suggest explicit --port
		// if that's wrong.
		return "", fmt.Errorf(
			"multiple USB-serial devices found, specify --port <path>:\n  - %s\n  "+
				"the first is usually correct; try that one, or pick the one matching your radio",
			strings.Join(ports, "\n  - "),
		)
	}
}

type serialClient struct {
	port serial.Port
	dev  string
}

func (c *serialClient) Close() error {
	return c.port.Close()
}

func (c *serialClient) Run(
	ctx context.Context,
	out chan<- *pb.FromRadio,
	in <-chan *pb.ToRadio,
) error {
	return runStream(ctx, c.port, out, in)
}

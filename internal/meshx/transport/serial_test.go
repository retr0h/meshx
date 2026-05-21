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

package transport

import (
	"strings"
	"testing"
)

// TestListSerialPorts_FilterRules exercises the filtering logic inside
// ListSerialPorts using a hand-rolled table of representative path strings.
// Because ListSerialPorts calls serial.GetPortsList (hardware), this file
// tests the filter predicate in isolation by running the same logic on known
// inputs.
//
// The filter keeps: usbserial, slab, wchusb, usbmodem, /dev/ttyUSB*,
// /dev/ttyACM*, COM*. It drops: /dev/tty.* (macOS DCD-blocking sibling).
func TestListSerialPorts_FilterRules(t *testing.T) {
	t.Parallel()

	// filterPort replicates the exact filter logic from ListSerialPorts so
	// this test is not coupled to the hardware-dependent outer function.
	filterPort := func(p string) bool {
		low := strings.ToLower(p)
		if strings.HasPrefix(p, "/dev/tty.") {
			return false
		}
		return strings.Contains(low, "usbserial") ||
			strings.Contains(low, "slab") ||
			strings.Contains(low, "wchusb") ||
			strings.Contains(low, "usbmodem") ||
			strings.HasPrefix(low, "/dev/ttyusb") ||
			strings.HasPrefix(low, "/dev/ttyacm") ||
			strings.HasPrefix(low, "com")
	}

	cases := []struct {
		name string
		port string
		keep bool
	}{
		// Kept paths.
		{name: "macOS cu.usbserial", port: "/dev/cu.usbserial-0200674E", keep: true},
		{name: "macOS cu.usbmodem", port: "/dev/cu.usbmodem2101", keep: true},
		{name: "CP210x slab path", port: "/dev/cu.SLAB_USBtoUART", keep: true},
		{name: "WCH USB path", port: "/dev/cu.wchusb1234", keep: true},
		{name: "Linux ttyUSB", port: "/dev/ttyUSB0", keep: true},
		{name: "Linux ttyACM", port: "/dev/ttyACM0", keep: true},
		{name: "Windows COM port", port: "COM3", keep: true},
		{name: "Windows COM port uppercase", port: "COM12", keep: true},
		// Dropped paths.
		{name: "macOS tty.usbserial — dropped", port: "/dev/tty.usbserial-0200674E", keep: false},
		{name: "macOS tty.usbmodem — dropped", port: "/dev/tty.usbmodem2101", keep: false},
		{name: "generic tty — dropped", port: "/dev/tty", keep: false},
		{name: "Bluetooth serial — dropped", port: "/dev/tty.Bluetooth-Incoming-Port", keep: false},
		{name: "unrelated /dev/null — dropped", port: "/dev/null", keep: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := filterPort(tc.port)
			if got != tc.keep {
				t.Errorf("filterPort(%q) = %v, want %v", tc.port, got, tc.keep)
			}
		})
	}
}

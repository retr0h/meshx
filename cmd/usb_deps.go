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
	"time"

	"github.com/retr0h/meshx/internal/meshx/transport"
)

// usbScanner is the narrow USB-management surface the usb subcommands
// require. Identify probes every candidate serial port; AutoDetect
// returns the single Meshtastic-responding port (or a useful
// multi-line error when zero or multiple are found).
type usbScanner interface {
	Identify(timeoutMS int) ([]transport.DeviceInfo, error)
	AutoDetect(timeoutMS int) (string, error)
}

// transportUSBScanner satisfies usbScanner by delegating to the
// transport package.
type transportUSBScanner struct{}

func (transportUSBScanner) Identify(timeoutMS int) ([]transport.DeviceInfo, error) {
	return transport.IdentifyAllSerial(time.Duration(timeoutMS) * time.Millisecond)
}

func (transportUSBScanner) AutoDetect(timeoutMS int) (string, error) {
	return transport.AutoDetectMeshtastic(time.Duration(timeoutMS) * time.Millisecond)
}

// cliUSBScanner is the package-level wiring; tests can swap.
var cliUSBScanner usbScanner = transportUSBScanner{}

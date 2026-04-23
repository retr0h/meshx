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
	"io"
	"math/rand"
	"strings"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"
)

// Client is the wire-level interface any transport must satisfy. It's
// a bidirectional stream of Meshtastic protobuf envelopes.
type Client interface {
	io.Closer
	// Run pumps FromRadio frames to the `out` channel and reads
	// ToRadio envelopes from `in`. Blocks until ctx is cancelled or
	// the connection fails. Returns the first error encountered.
	Run(ctx context.Context, out chan<- *pb.FromRadio, in <-chan *pb.ToRadio) error
}

// Dial opens a transport to the given destination. Dest is:
//   - "ble:<uuid-or-mac>" — Bluetooth LE peripheral
//   - A serial device path, e.g. "/dev/cu.usbserial-0200674E"
//   - A TCP host:port, e.g. "192.168.1.42" (port defaults to 4403)
//     or "meshtasticd.local:4403"
//
// The heuristic: "ble:" prefix → BLE, "/dev/" or "COM" prefix →
// serial, everything else → TCP. Callers can force the transport
// with the typed constructors (DialSerial, DialTCP, DialBLE).
func Dial(dest string) (Client, error) {
	switch {
	case strings.HasPrefix(dest, "ble:"):
		return DialBLE(strings.TrimPrefix(dest, "ble:"))
	case strings.HasPrefix(dest, "/dev/"), strings.HasPrefix(dest, "COM"):
		return DialSerial(dest)
	default:
		return DialTCP(dest)
	}
}

// SendWantConfig emits the standard handshake ToRadio envelope that
// prompts the radio to dump its current config — MyNodeInfo, NodeInfo
// entries for every known peer, ChannelConfig per channel, DeviceConfig
// values, then a ConfigComplete marker carrying the request's nonce.
//
// Meshtastic convention: send a random (non-zero) nonce, then look for
// FromRadio_ConfigCompleteId matching it in the stream.
func SendWantConfig(in chan<- *pb.ToRadio) uint32 {
	nonce := rand.Uint32()
	if nonce == 0 {
		nonce = 1
	}
	in <- &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_WantConfigId{WantConfigId: nonce},
	}
	return nonce
}

// MarshalToRadio is a thin wrapper so callsites don't have to import
// the protobuf runtime directly.
func MarshalToRadio(m *pb.ToRadio) ([]byte, error) {
	buf, err := proto.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal ToRadio: %w", err)
	}
	return buf, nil
}

// UnmarshalFromRadio decodes a framed payload into a FromRadio envelope.
func UnmarshalFromRadio(payload []byte) (*pb.FromRadio, error) {
	out := &pb.FromRadio{}
	if err := proto.Unmarshal(payload, out); err != nil {
		return nil, fmt.Errorf("unmarshal FromRadio: %w", err)
	}
	return out, nil
}

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

// Package transport handles the raw wire protocol to a Meshtastic
// radio — serial or TCP — and exposes a stream of decoded FromRadio
// envelopes plus a channel for ToRadio outbound messages.
//
// Meshtastic's framing is identical for serial and TCP: every packet
// is a 4-byte header followed by a protobuf payload.
//
//	byte 0:  0x94         (start1)
//	byte 1:  0xc3         (start2)
//	byte 2:  payload size high byte
//	byte 3:  payload size low byte
//	bytes 4..4+N:         protobuf-serialized FromRadio or ToRadio
//
// Max payload size is 512 bytes (maxToFromRadioSize in upstream). We
// drop frames whose header doesn't match 0x94 0xc3 — the radio
// occasionally boots/reboots and emits garbage or debug text over
// serial before the stream stabilizes.
package transport

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	start1         byte = 0x94
	start2         byte = 0xc3
	headerLen           = 4
	maxPayloadSize      = 512
)

// ErrBadStart indicates that the next byte on the wire was not the
// expected start1 marker. Readers typically discard one byte and
// retry.
var ErrBadStart = errors.New("transport: bad frame start byte")

// ReadFrame reads one complete Meshtastic frame from r, resyncing if
// it lands mid-stream on a junk byte. Returns the raw protobuf
// payload (header stripped). Blocks until a full frame is read or an
// I/O error occurs.
//
// The resync logic tolerates boot-up debug prints, garbled restarts,
// and stale bytes in the serial buffer — read() one byte at a time
// until we see 0x94 0xc3, then the header + payload.
func ReadFrame(r io.Reader) ([]byte, error) {
	one := make([]byte, 1)
	// Resync: slide forward until we see start1 + start2.
	var prev byte
	for {
		if _, err := io.ReadFull(r, one); err != nil {
			return nil, err
		}
		if prev == start1 && one[0] == start2 {
			break
		}
		prev = one[0]
	}

	// Read the 2-byte size.
	sizeBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, sizeBuf); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint16(sizeBuf)
	if int(size) > maxPayloadSize {
		return nil, fmt.Errorf("transport: oversize frame (%d > %d)", size, maxPayloadSize)
	}
	if size == 0 {
		return []byte{}, nil
	}

	// Read the protobuf payload.
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// WriteFrame encodes a protobuf payload in the Meshtastic wire
// framing and writes it to w.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > maxPayloadSize {
		return fmt.Errorf("transport: payload too large (%d > %d)", len(payload), maxPayloadSize)
	}
	header := []byte{start1, start2, 0, 0}
	binary.BigEndian.PutUint16(header[2:], uint16(len(payload)))
	if _, err := w.Write(header); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return nil
}

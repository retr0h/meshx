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
	"errors"
	"fmt"
	"io"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
)

// runStream is the shared reader+writer loop used by both the serial
// and TCP Client implementations. It fans out:
//
//	reader goroutine: rw → framing.ReadFrame → proto.Unmarshal → out
//	writer:           in → proto.Marshal → framing.WriteFrame → rw
//
// Returns when ctx is cancelled, either side closes, or an I/O error
// occurs. Errors from read and write are both surfaced; the first
// wins (errCh is buffered to 2 so the loser doesn't block).
//
// CONCURRENCY CONTRACT: when runStream returns, the writer goroutine
// has exited but the reader MAY still be parked inside ReadFrame —
// I/O reads on serial ports and TCP conns can't be context-cancelled,
// only unblocked by Close. The caller MUST invoke Client.Close() (which
// closes the underlying io.ReadWriter) after Run returns; that pulls
// the rug out from under ReadFrame, the reader observes io.EOF, and
// exits. pump.run.runSession follows this pattern: every successful
// runSession is paired with a p.client.Close() at pump.go:410, so
// the reader always exits within one frame-read of session shutdown.
// Without that Close, the reader goroutine leaks.
func runStream(
	ctx context.Context,
	rw io.ReadWriter,
	out chan<- *pb.FromRadio,
	in <-chan *pb.ToRadio,
) error {
	errCh := make(chan error, 2)

	// Reader: pull frames until EOF or ctx cancellation.
	go func() {
		for {
			payload, err := ReadFrame(rw)
			if err != nil {
				if errors.Is(err, io.EOF) || ctx.Err() != nil {
					errCh <- nil
					return
				}
				errCh <- fmt.Errorf("read: %w", err)
				return
			}
			msg, err := UnmarshalFromRadio(payload)
			if err != nil {
				// Decode errors are survivable — log via the channel
				// consumer ideally, but for now just drop the frame
				// and keep going.
				continue
			}
			select {
			case out <- msg:
			case <-ctx.Done():
				errCh <- nil
				return
			}
		}
	}()

	// Writer: drain the in channel, frame, and write to the device.
	go func() {
		for {
			select {
			case <-ctx.Done():
				errCh <- nil
				return
			case msg, ok := <-in:
				if !ok {
					errCh <- nil
					return
				}
				payload, err := MarshalToRadio(msg)
				if err != nil {
					errCh <- err
					return
				}
				if err := WriteFrame(rw, payload); err != nil {
					errCh <- fmt.Errorf("write: %w", err)
					return
				}
			}
		}
	}()

	// Return on the first error (or nil) from either goroutine.
	return <-errCh
}

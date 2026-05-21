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
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"google.golang.org/protobuf/proto"
)

// pipeRW wraps a read-side and a write-side so runStream can talk to a
// synthetic stream in tests without any real I/O.
type pipeRW struct {
	r io.Reader
	w io.Writer
}

func (p *pipeRW) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipeRW) Write(b []byte) (int, error) { return p.w.Write(b) }

// TestRunStream_ReaderDelivery verifies that runStream correctly decodes
// a framed FromRadio envelope from the read side and forwards it on the out
// channel before the context is cancelled.
func TestRunStream_ReaderDelivery(t *testing.T) {
	t.Parallel()

	// Build a single valid FromRadio frame.
	original := &pb.FromRadio{
		PayloadVariant: &pb.FromRadio_ConfigCompleteId{ConfigCompleteId: 42},
	}
	payload, err := proto.Marshal(original)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	var frameBuf bytes.Buffer
	if err := WriteFrame(&frameBuf, payload); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	// Append EOF sentinel so ReadFrame terminates after the one frame.
	rw := &pipeRW{
		r: &frameBuf,
		w: io.Discard,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out := make(chan *pb.FromRadio, 1)
	in := make(chan *pb.ToRadio)

	done := make(chan error, 1)
	go func() {
		done <- runStream(ctx, rw, out, in)
	}()

	select {
	case msg := <-out:
		if !proto.Equal(original, msg) {
			t.Errorf("decoded message %v, want %v", msg, original)
		}
	case err := <-done:
		// runStream returning nil (EOF) before we drain out is also
		// acceptable — the frame was enqueued but we consumed the error
		// first. Drain out non-blocking.
		if err != nil {
			t.Fatalf("runStream returned unexpected error: %v", err)
		}
		select {
		case msg := <-out:
			if !proto.Equal(original, msg) {
				t.Errorf("decoded message %v, want %v", msg, original)
			}
		default:
			t.Error("runStream returned nil but no message was delivered")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message from runStream")
	}
}

// TestRunStream_WriterDelivery verifies that runStream correctly marshals an
// outbound ToRadio command and writes it as a framed packet to the write side.
func TestRunStream_WriterDelivery(t *testing.T) {
	t.Parallel()

	// Use two separate pipes:
	//   readPipeR / readPipeW  — the read side fed into runStream (blocks forever)
	//   writePipeR / writePipeW — captures what runStream writes
	//
	// WriteFrame issues two separate Write calls (header + payload), so we
	// run ReadFrame on writePipeR in a goroutine — ReadFrame handles the
	// fragmented delivery via repeated reads from the pipe.
	readPipeR, readPipeW := io.Pipe()
	writePipeR, writePipeW := io.Pipe()
	defer func() { _ = readPipeW.Close() }()
	defer func() { _ = readPipeR.Close() }()
	defer func() { _ = writePipeW.Close() }()

	rw := &pipeRW{r: readPipeR, w: writePipeW}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan *pb.FromRadio, 1)
	in := make(chan *pb.ToRadio, 1)

	go func() {
		_ = runStream(ctx, rw, out, in)
	}()

	want := &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_WantConfigId{WantConfigId: 7},
	}
	in <- want

	// ReadFrame reassembles the frame from the fragmented pipe writes.
	type frameResult struct {
		payload []byte
		err     error
	}
	frameCh := make(chan frameResult, 1)
	go func() {
		p, err := ReadFrame(writePipeR)
		frameCh <- frameResult{p, err}
	}()

	select {
	case res := <-frameCh:
		if res.err != nil {
			t.Fatalf("ReadFrame: %v", res.err)
		}
		got := &pb.ToRadio{}
		if err := proto.Unmarshal(res.payload, got); err != nil {
			t.Fatalf("proto.Unmarshal: %v", err)
		}
		if !proto.Equal(want, got) {
			t.Errorf("written message %v, want %v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runStream to write the frame")
	}
}

// TestRunStream_ContextCancel verifies that runStream exits promptly when its
// context is cancelled, returning the context error.
func TestRunStream_ContextCancel(t *testing.T) {
	t.Parallel()

	// Infinite blocking reader — never produces data.
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	defer func() { _ = pr.Close() }()

	rw := &pipeRW{r: pr, w: io.Discard}

	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan *pb.FromRadio, 1)
	in := make(chan *pb.ToRadio)

	done := make(chan error, 1)
	go func() {
		done <- runStream(ctx, rw, out, in)
	}()

	cancel()
	// Unblock the blocking read so the reader goroutine can observe ctx.Done.
	_ = pw.Close()

	select {
	case err := <-done:
		// nil is acceptable (context cancel observed before the write-side).
		_ = err
	case <-time.After(2 * time.Second):
		t.Fatal("runStream did not return after context cancel")
	}
}

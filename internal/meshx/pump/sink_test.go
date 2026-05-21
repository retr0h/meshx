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

package pump

import (
	"testing"
	"time"
)

type recordingBus struct {
	events []any
	ch     chan any
}

func newRecordingBus(bufSize int) *recordingBus {
	return &recordingBus{ch: make(chan any, bufSize)}
}

func (b *recordingBus) Publish(event any) {
	b.events = append(b.events, event)
	select {
	case b.ch <- event:
	default:
	}
}

func TestBusSink_PublishesToBus(t *testing.T) {
	rb := newRecordingBus(4)
	s := &BusSink{Bus: rb}

	s.Send("hello")

	select {
	case got := <-rb.ch:
		if got != "hello" {
			t.Fatalf("bus received %v, want %q", got, "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus publish")
	}
}

func TestBusSink_NilBus_DoesNotPanic(_ *testing.T) {
	s := &BusSink{Bus: nil}
	s.Send("safe")
}

func TestBusSink_MultipleEvents_OrderPreserved(t *testing.T) {
	rb := newRecordingBus(8)
	s := &BusSink{Bus: rb}

	events := []any{"a", "b", "c"}
	for _, e := range events {
		s.Send(e)
	}

	if len(rb.events) != len(events) {
		t.Fatalf("bus events len: want %d, got %d", len(events), len(rb.events))
	}
	for i, want := range events {
		if rb.events[i] != want {
			t.Fatalf("bus events[%d]: want %v, got %v", i, want, rb.events[i])
		}
	}
}

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

// EventBus is the narrow event-bus surface BusSink requires — a
// single Publish method. Declared here (at the consumer seam) per
// the osapi-io pattern so *bus.Bus satisfies it structurally.
type EventBus interface {
	Publish(event any)
}

// BusSink publishes every pump event to the Bus. All consumers
// (TUI, future CLI tail, etc.) subscribe to the Bus to receive
// events. The pump stays decoupled — it only knows about Sink.
type BusSink struct {
	Bus EventBus
}

// Send publishes msg to the Bus. Nil-safe.
func (s *BusSink) Send(msg any) {
	if s.Bus != nil {
		s.Bus.Publish(msg)
	}
}

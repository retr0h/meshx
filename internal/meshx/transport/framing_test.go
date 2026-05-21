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
	"errors"
	"io"
	"strings"
	"testing"
)

// TestWriteFrame verifies that WriteFrame encodes the Meshtastic 4-byte
// header correctly and rejects payloads that exceed maxPayloadSize.
func TestWriteFrame(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		payload   []byte
		wantErr   bool
		wantBytes []byte // full expected wire output when no error
	}{
		{
			name:    "empty payload — header only",
			payload: []byte{},
			wantBytes: []byte{
				0x94, 0xc3, // start1 + start2
				0x00, 0x00, // size = 0
			},
		},
		{
			name:    "single byte payload",
			payload: []byte{0xFF},
			wantBytes: []byte{
				0x94, 0xc3,
				0x00, 0x01,
				0xFF,
			},
		},
		{
			name:    "multi-byte payload — size big-endian",
			payload: []byte{0x01, 0x02, 0x03},
			wantBytes: []byte{
				0x94, 0xc3,
				0x00, 0x03,
				0x01, 0x02, 0x03,
			},
		},
		{
			name:    "payload at exact maxPayloadSize boundary (512 bytes)",
			payload: bytes.Repeat([]byte{0xAB}, maxPayloadSize),
			wantBytes: func() []byte {
				hdr := make([]byte, 4, 4+maxPayloadSize)
				copy(hdr, []byte{0x94, 0xc3, 0x02, 0x00})
				return append(hdr, bytes.Repeat([]byte{0xAB}, maxPayloadSize)...)
			}(),
		},
		{
			name:    "payload one byte over max — error",
			payload: bytes.Repeat([]byte{0x00}, maxPayloadSize+1),
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			err := WriteFrame(&buf, tc.payload)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(buf.Bytes(), tc.wantBytes) {
				t.Errorf("wire bytes = %v, want %v", buf.Bytes(), tc.wantBytes)
			}
		})
	}
}

// TestReadFrame verifies that ReadFrame decodes frames written by WriteFrame
// and that it resyncs past junk bytes before the start marker.
func TestReadFrame(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   func() []byte // wire bytes fed to the reader
		want    []byte        // expected payload (nil == error expected)
		wantErr bool
	}{
		{
			name: "clean frame — empty payload",
			input: func() []byte {
				var buf bytes.Buffer
				_ = WriteFrame(&buf, []byte{})
				return buf.Bytes()
			},
			want: []byte{},
		},
		{
			name: "clean frame — single byte payload",
			input: func() []byte {
				var buf bytes.Buffer
				_ = WriteFrame(&buf, []byte{0xDE})
				return buf.Bytes()
			},
			want: []byte{0xDE},
		},
		{
			name: "clean frame — arbitrary payload",
			input: func() []byte {
				var buf bytes.Buffer
				_ = WriteFrame(&buf, []byte{0x01, 0x02, 0x03, 0x04})
				return buf.Bytes()
			},
			want: []byte{0x01, 0x02, 0x03, 0x04},
		},
		{
			name: "junk prefix — resync finds valid frame",
			input: func() []byte {
				var buf bytes.Buffer
				// Write garbage that doesn't contain the start sequence.
				buf.Write([]byte{0x00, 0x01, 0x02, 0xFF})
				// Then a valid frame.
				_ = WriteFrame(&buf, []byte{0xAA, 0xBB})
				return buf.Bytes()
			},
			want: []byte{0xAA, 0xBB},
		},
		{
			name: "round-trip 512-byte max payload",
			input: func() []byte {
				payload := bytes.Repeat([]byte{0x5A}, maxPayloadSize)
				var buf bytes.Buffer
				_ = WriteFrame(&buf, payload)
				return buf.Bytes()
			},
			want: bytes.Repeat([]byte{0x5A}, maxPayloadSize),
		},
		{
			name: "oversize frame header — error",
			input: func() []byte {
				// Manually craft a header claiming 513 bytes.
				return []byte{0x94, 0xc3, 0x02, 0x01}
			},
			wantErr: true,
		},
		{
			name: "EOF before complete header — error",
			input: func() []byte {
				// Only start bytes, no size.
				return []byte{0x94, 0xc3}
			},
			wantErr: true,
		},
		{
			name: "EOF before complete payload — error",
			input: func() []byte {
				// Header claims 4 bytes but stream ends after 2.
				return []byte{0x94, 0xc3, 0x00, 0x04, 0xAA, 0xBB}
			},
			wantErr: true,
		},
		{
			name: "empty reader — EOF propagated",
			input: func() []byte {
				return []byte{}
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := bytes.NewReader(tc.input())
			got, err := ReadFrame(r)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("payload = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestWriteFrame_WriterError verifies that WriteFrame surfaces errors from
// the underlying writer for both the header and the payload write calls.
func TestWriteFrame_WriterError(t *testing.T) {
	t.Parallel()

	t.Run("header write fails", func(t *testing.T) {
		t.Parallel()
		err := WriteFrame(&errWriter{failAfter: 0}, []byte{0x01})
		if err == nil {
			t.Fatal("expected error from failing writer, got nil")
		}
	})

	t.Run("payload write fails after header succeeds", func(t *testing.T) {
		t.Parallel()
		// failAfter=1 lets the header write succeed, then fails on payload.
		err := WriteFrame(&errWriter{failAfter: 1}, []byte{0x01, 0x02})
		if err == nil {
			t.Fatal("expected error from failing writer, got nil")
		}
	})
}

// errWriter fails after failAfter successful Write calls.
type errWriter struct {
	calls     int
	failAfter int
}

func (w *errWriter) Write(p []byte) (int, error) {
	if w.calls >= w.failAfter {
		return 0, errors.New("write: simulated failure")
	}
	w.calls++
	return len(p), nil
}

// TestReadFrame_ResyncEdgeCases exercises boundary conditions in the
// resync loop: a stream that is entirely the start1 byte, and a stream
// where start1 appears but is not followed by start2.
func TestReadFrame_ResyncEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("stream of start1 bytes only — EOF", func(t *testing.T) {
		t.Parallel()
		// Six 0x94 bytes — resync loop reads until EOF, never sees start2.
		r := bytes.NewReader(bytes.Repeat([]byte{start1}, 6))
		_, err := ReadFrame(r)
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			// Some io.ReadFull implementations wrap EOF as ErrUnexpectedEOF.
			t.Errorf("expected EOF-family error, got %v", err)
		}
	})

	t.Run("start1 not followed by start2 — resync continues", func(t *testing.T) {
		t.Parallel()
		// 0x94 0x00 0x94 0x00 … eventually EOF — never syncs.
		var b []byte
		for i := 0; i < 10; i++ {
			b = append(b, start1, 0x00)
		}
		r := bytes.NewReader(b)
		_, err := ReadFrame(r)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

// TestReadWriteFrame_RoundTrip is a property-style table test that
// confirms WriteFrame followed by ReadFrame returns the original payload
// unchanged for a range of interesting payload sizes and contents.
func TestReadWriteFrame_RoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload []byte
	}{
		{"nil payload treated as empty", nil},
		{"1 byte", []byte{0x42}},
		{"all-zeros 10 bytes", make([]byte, 10)},
		{"all-ones 10 bytes", bytes.Repeat([]byte{0xFF}, 10)},
		{"contains start sequence mid-payload", []byte{0x94, 0xc3, 0x00, 0x01}},
		{"max size", bytes.Repeat([]byte{0x7E}, maxPayloadSize)},
		{"protobuf-like varint prefix", []byte{0x08, 0x96, 0x01}},
		{"printable ASCII", []byte(strings.Repeat("hello", 10))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var wire bytes.Buffer
			if err := WriteFrame(&wire, tc.payload); err != nil {
				t.Fatalf("WriteFrame error: %v", err)
			}
			got, err := ReadFrame(&wire)
			if err != nil {
				t.Fatalf("ReadFrame error: %v", err)
			}
			// nil and empty slice are equivalent over the wire.
			if len(tc.payload) == 0 && len(got) == 0 {
				return
			}
			if !bytes.Equal(got, tc.payload) {
				t.Errorf("round-trip mismatch: got %v, want %v", got, tc.payload)
			}
		})
	}
}

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

// SSE parser tests for `meshx client tail`. The interesting logic is
// the line-loop that buffers `event:` / `data:` / `id:` per frame and
// emits one JSON line on the blank-line frame boundary — easy to
// regress, so each row exercises a different SSE framing nuance.

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseStub returns an httptest.Server that emits the supplied
// pre-formatted SSE stream verbatim and then closes. Used to drive
// the tail consumer without standing up the real daemon's
// /events handler.
func sseStub(t *testing.T, stream string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, stream)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestRunClientTail — the SSE framing parser must emit one JSON
// envelope per blank-line-delimited frame, preserve the `id` line
// when present, and ignore comment / unknown prefix lines.
func TestRunClientTail(t *testing.T) {
	cases := []struct {
		name        string
		stream      string
		wantLines   []string
		wantOmitted []string
	}{
		{
			name: "one-event-with-id-emits-envelope-with-id",
			stream: "id: 42\n" +
				"event: text\n" +
				"data: {\"foo\":\"bar\"}\n" +
				"\n",
			wantLines: []string{
				`{"id":"42","kind":"text","data":{"foo":"bar"}}`,
			},
		},
		{
			name: "event-without-id-omits-id-key",
			stream: "event: text\n" +
				"data: {\"foo\":1}\n" +
				"\n",
			wantLines: []string{
				`{"kind":"text","data":{"foo":1}}`,
			},
			wantOmitted: []string{`"id":`},
		},
		{
			name: "two-events-back-to-back-each-emits-one-envelope",
			stream: "event: text\n" +
				"data: {\"a\":1}\n" +
				"\n" +
				"event: routing\n" +
				"data: {\"b\":2}\n" +
				"\n",
			wantLines: []string{
				`{"kind":"text","data":{"a":1}}`,
				`{"kind":"routing","data":{"b":2}}`,
			},
		},
		{
			name: "comment-and-unknown-prefix-lines-are-ignored",
			stream: ": keepalive\n" +
				"retry: 5000\n" +
				"event: text\n" +
				"data: {\"x\":\"y\"}\n" +
				"\n",
			wantLines: []string{
				`{"kind":"text","data":{"x":"y"}}`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := sseStub(t, tc.stream)
			cfg := clientConfig{ServerURL: srv.URL}
			var buf bytes.Buffer
			// The stub closes after the stream, which lands as
			// io.EOF — runClientTail treats that as a clean exit.
			if err := runClientTail(context.Background(), cfg, &buf, "0xabcdef01", ""); err != nil {
				t.Fatalf("runClientTail: %v", err)
			}
			got := buf.String()
			for _, want := range tc.wantLines {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\n--- got ---\n%s", want, got)
				}
			}
			for _, omit := range tc.wantOmitted {
				if strings.Contains(got, omit) {
					t.Errorf("output should not contain %q\n--- got ---\n%s", omit, got)
				}
			}
		})
	}
}

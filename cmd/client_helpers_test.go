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

// Shared fixtures + harness builders for the cmd/ client tests. Per
// the file-pair rule, this is the one *_test.go in cmd/ without a
// production counterpart — it holds the cross-cutting test
// infrastructure each per-command test file consumes.

import (
	"net/http/httptest"
	"testing"

	"github.com/retr0h/meshx/internal/radio"
	"github.com/retr0h/meshx/internal/sdk/gen"
	"github.com/retr0h/meshx/internal/server"
)

// clientHarness builds a real *server.Server with the given attached
// radios, an httptest.Server in front of it, and returns a wired SDK
// client pointed at that server. Empty radios slice produces an
// empty registry — useful for the "no radios attached" branch.
func clientHarness(
	t *testing.T,
	radios ...*radio.Session,
) *gen.ClientWithResponses {
	t.Helper()
	s := server.New(server.Config{Radios: server.NewRegistry()})
	for _, sess := range radios {
		s.Drivers().Add(sess.State.RadioID, sess)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c, err := gen.NewClientWithResponses(srv.URL)
	if err != nil {
		t.Fatalf("build SDK client: %v", err)
	}
	return c
}

// fakeRadio is a *radio.Session pre-seeded with the canonical-shape
// fields the /radios endpoints project. Test rows mutate one of
// these and hand it to clientHarness. MyNodeNum sits above int32 max
// on purpose so the test exercises the wide-uint32 wire path —
// format:"int64" on the spec field means oapi-codegen produces Go
// int64; before #90 it narrowed to int32 and 0xdeadbeef tripped the
// JSON decoder.
func fakeRadio(id, dest string) *radio.Session {
	sess := radio.New(nil, nil, nil)
	sess.State.RadioID = id
	sess.State.ConnectDest = dest
	sess.State.MyNodeNum = 0xdeadbeef
	sess.State.Connected = true
	return sess
}

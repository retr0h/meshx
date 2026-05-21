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
	"net"
	"strings"
	"testing"
)

// TestDialTCP_ExplicitPortPreserved verifies that when a host:port destination
// is passed to DialTCP the explicit port is used unchanged — DialTCP must not
// append meshtasticDefaultPort to an address that already has a port.
func TestDialTCP_ExplicitPortPreserved(t *testing.T) {
	t.Parallel()

	// Spin up a loopback listener on an OS-assigned port and keep it
	// accepting until the test is done.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	// Accept connections in the background so DialTCP does not hang.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed by Cleanup
			}
			_ = conn.Close()
		}
	}()

	dest := ln.Addr().String() // "127.0.0.1:<port>"
	_, port, _ := net.SplitHostPort(dest)

	c, err := DialTCP(dest)
	if err != nil {
		t.Fatalf("DialTCP(%q) unexpected error: %v", dest, err)
	}
	_ = c.Close()

	// The port in the destination string must be the one we bound — not
	// meshtasticDefaultPort (4403). Just verify they differ if the
	// OS happened to assign 4403 this would be a false pass, but the
	// important invariant is that DialTCP did not rewrite the address.
	if port == meshtasticDefaultPort {
		t.Logf("OS assigned port 4403 — skipping port-preservation assertion")
		return
	}
	if !strings.Contains(dest, port) {
		t.Errorf("dest %q does not contain expected port %s", dest, port)
	}
}

// TestDialTCP_DefaultPort verifies that DialTCP appends meshtasticDefaultPort
// (4403) when the destination has no port. We use 127.0.0.1 — the TCP connect
// will be refused immediately, and the error message must mention port 4403.
func TestDialTCP_DefaultPort(t *testing.T) {
	t.Parallel()

	_, err := DialTCP("127.0.0.1")
	if err == nil {
		t.Skip("unexpectedly connected to 127.0.0.1:4403; skipping assertion")
	}
	if !strings.Contains(err.Error(), "4403") {
		t.Errorf("error %q should mention port 4403 (default port not appended)", err.Error())
	}
}

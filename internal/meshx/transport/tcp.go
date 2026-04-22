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
	"net"
	"strings"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
)

// meshtasticDefaultPort is the TCP port the firmware opens on WiFi
// radios and the `meshtasticd` Linux daemon.
const meshtasticDefaultPort = "4403"

// DialTCP connects to a Meshtastic radio over TCP. Destination may
// be "host", "host:port", or "ip". When no port is present,
// meshtasticDefaultPort (4403) is assumed.
func DialTCP(dest string) (Client, error) {
	if !strings.Contains(dest, ":") {
		dest = net.JoinHostPort(dest, meshtasticDefaultPort)
	}
	conn, err := net.Dial("tcp", dest)
	if err != nil {
		return nil, fmt.Errorf("dial tcp %s: %w", dest, err)
	}
	return &tcpClient{conn: conn, dest: dest}, nil
}

type tcpClient struct {
	conn net.Conn
	dest string
}

func (c *tcpClient) Close() error {
	return c.conn.Close()
}

func (c *tcpClient) Run(ctx context.Context, out chan<- *pb.FromRadio, in <-chan *pb.ToRadio) error {
	return runStream(ctx, c.conn, out, in)
}

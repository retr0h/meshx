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

import (
	"context"

	"github.com/spf13/cobra"
)

// daemonRunner is the narrow consumer-seam interface this cobra
// command depends on, declared per the osapi-io pattern. Both
// *server.Server and any future variant (an in-memory fake for
// tests, a unix-socket-only flavor, …) satisfy this surface — Go's
// structural typing means we don't `implements` anywhere; the
// compiler verifies.
type daemonRunner interface {
	Run(ctx context.Context, addr string) error
}

// serverCmd is the parent for every daemon operation.
// Subcommands: start.
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run the meshx HTTP+SSE daemon (headless)",
	Long: `Commands for running the meshx daemon — exposes channels, nodes,
messages, and live events over HTTP+SSE.

  meshx server start                       # bind 127.0.0.1:4404, no radio attached
  meshx server start --bind :4404          # listen on all interfaces
  meshx server start --radio /dev/cu.usb…  # attach a radio over USB`,
}

func init() {
	rootCmd.AddCommand(serverCmd)
}

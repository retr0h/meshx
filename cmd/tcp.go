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
	"github.com/retr0h/meshx/internal/tui"
	"github.com/spf13/cobra"
)

// tcpCmd is the parent for every TCP operation against a
// meshtasticd instance or a WiFi-connected radio exposing the
// native API on port 4403. Sibling of `usb` and `ble`.
var tcpCmd = &cobra.Command{
	Use:   "tcp",
	Short: "TCP Meshtastic transport",
	Long: `Commands for connecting to a Meshtastic radio or meshtasticd
instance over TCP on the native Meshtastic port (4403 by default).`,
}

// tcpConnectCmd opens the TUI against a remote Meshtastic endpoint.
// Required arg is the host or host:port string.
var tcpConnectCmd = &cobra.Command{
	Use:   "connect <host[:port]>",
	Short: "Open the TUI over TCP",
	Long: `Connect to a Meshtastic endpoint over TCP and open the TUI.

  meshx tcp connect meshtasticd.local         # port defaults to 4403
  meshx tcp connect 192.168.1.42:4403         # explicit port`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return tui.RunRadio(args[0])
	},
}

func init() {
	tcpCmd.AddCommand(tcpConnectCmd)
	rootCmd.AddCommand(tcpCmd)
}

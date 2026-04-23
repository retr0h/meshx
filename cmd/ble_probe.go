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
	"fmt"
	"os"
	"time"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"github.com/retr0h/meshx/internal/meshx/transport"
	"github.com/spf13/cobra"
)

// bleProbeTimeout is how long `meshx ble probe` listens for
// FromRadio packets before summarizing. 15s is enough for a
// healthy Meshtastic radio to finish dumping its full config
// stream (MyNodeInfo → NodeInfos → Channels → Config →
// ConfigComplete), which is what the full handshake is
// supposed to produce.
const bleProbeTimeout = 15 * time.Second

// bleProbeCmd is the diagnostic sibling of `meshx ble connect`.
// It runs the same DialBLE + Run path the TUI uses but dumps
// every FromRadio envelope to stderr instead of feeding it into
// Bubble Tea. Used to isolate transport failures ("packets
// aren't flowing") from integration failures ("packets are
// flowing but the UI doesn't reflect them").
var bleProbeCmd = &cobra.Command{
	Use:   "probe <uuid>",
	Short: "Diagnostic probe — dump raw FromRadio packets for 15s",
	Long: `Connect to a Bluetooth Meshtastic radio, send the standard
WantConfigId handshake, and print every FromRadio packet received
for 15 seconds. No TUI, no pump integration — just the raw stream.

Use this to debug a connect that opens the TUI but shows no live
data: if probe sees packets, the transport is fine and the bug is
in the pump / model wiring; if probe sees nothing, the issue is
in DialBLE / bleClient.Run itself.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return bleProbe(args[0])
	},
}

// bleProbe is the RunE body split out so the cobra Command stays
// declarative. All output goes to stderr so piping stdout for
// structured capture still works cleanly.
func bleProbe(addr string) error {
	fmt.Fprintf(os.Stderr, "ble probe %s\n", addr)
	fmt.Fprintln(os.Stderr, "  step 1: DialBLE — scan + connect + discover characteristics")

	client, err := transport.DialBLE(addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = client.Close() }()

	fmt.Fprintln(os.Stderr, "  step 2: connected; running Run loop in background")

	// Channels with modest buffers — the probe is chatty but also
	// slow (stderr prints + protobuf String()) so a little slack
	// prevents the transport goroutine from blocking on a full
	// out channel mid-drain.
	out := make(chan *pb.FromRadio, 64)
	in := make(chan *pb.ToRadio, 8)

	ctx, cancel := context.WithTimeout(context.Background(), bleProbeTimeout)
	defer cancel()

	runDone := make(chan error, 1)
	go func() {
		runDone <- client.Run(ctx, out, in)
	}()

	// Kick off the standard Meshtastic handshake — without this
	// the radio has nothing to send and fromRadio reads return
	// empty immediately, making it look like the stream is dead
	// when really we just never asked for anything.
	fmt.Fprintln(os.Stderr, "  step 3: sending WantConfigId handshake…")
	nonce := transport.SendWantConfig(in)
	fmt.Fprintf(os.Stderr, "    → nonce = 0x%08x\n", nonce)

	fmt.Fprintf(os.Stderr, "  step 4: listening for FromRadio packets (%s)…\n", bleProbeTimeout)
	fmt.Fprintln(os.Stderr)

	counts := map[string]int{}
	total := 0
	sawConfigComplete := false

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "  step 5: 15s elapsed — summarizing")
			summarizeProbe(total, counts, sawConfigComplete)
			// Let the Run goroutine clean up before we return.
			<-runDone
			return nil

		case err := <-runDone:
			fmt.Fprintln(os.Stderr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Run returned early: %v\n", err)
				summarizeProbe(total, counts, sawConfigComplete)
				return fmt.Errorf("probe Run: %w", err)
			}
			fmt.Fprintln(os.Stderr, "  Run returned cleanly (unexpected before timeout)")
			summarizeProbe(total, counts, sawConfigComplete)
			return nil

		case msg := <-out:
			total++
			kind := fromRadioKind(msg)
			counts[kind]++
			fmt.Fprintf(os.Stderr, "  [%03d] %s\n", total, kind)
			if msg.GetConfigCompleteId() == nonce {
				sawConfigComplete = true
			}
		}
	}
}

// fromRadioKind returns a short human-readable tag for the packet
// variant so the probe output reads as a stream of event types
// rather than a wall of protobuf. Matches the case names from
// pb.FromRadio's oneof family.
func fromRadioKind(msg *pb.FromRadio) string {
	if msg == nil || msg.PayloadVariant == nil {
		return "<nil>"
	}
	switch v := msg.PayloadVariant.(type) {
	case *pb.FromRadio_Packet:
		port := "?"
		if v.Packet != nil && v.Packet.GetDecoded() != nil {
			port = v.Packet.GetDecoded().Portnum.String()
		}
		return fmt.Sprintf("MeshPacket port=%s", port)
	case *pb.FromRadio_MyInfo:
		return fmt.Sprintf("MyNodeInfo my_node_num=0x%x", v.MyInfo.GetMyNodeNum())
	case *pb.FromRadio_NodeInfo:
		return fmt.Sprintf("NodeInfo num=0x%x user=%q",
			v.NodeInfo.GetNum(),
			v.NodeInfo.GetUser().GetLongName(),
		)
	case *pb.FromRadio_Config:
		return "Config"
	case *pb.FromRadio_LogRecord:
		return "LogRecord"
	case *pb.FromRadio_ConfigCompleteId:
		return fmt.Sprintf("ConfigCompleteId=0x%08x", v.ConfigCompleteId)
	case *pb.FromRadio_Rebooted:
		return "Rebooted"
	case *pb.FromRadio_ModuleConfig:
		return "ModuleConfig"
	case *pb.FromRadio_Channel:
		return fmt.Sprintf("Channel index=%d", v.Channel.GetIndex())
	case *pb.FromRadio_Metadata:
		return "DeviceMetadata"
	default:
		return fmt.Sprintf("unknown variant %T", v)
	}
}

// summarizeProbe prints a closing tally — total packets, breakdown
// by kind, and whether the ConfigComplete sentinel (proof that the
// radio fully responded to our handshake) arrived. This is the
// quick diagnosis the user pastes back when reporting "BLE
// doesn't work."
func summarizeProbe(total int, counts map[string]int, sawCC bool) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "─── probe summary ───")
	fmt.Fprintf(os.Stderr, "  total packets: %d\n", total)
	fmt.Fprintln(os.Stderr, "  by kind:")
	if len(counts) == 0 {
		fmt.Fprintln(os.Stderr, "    (none)")
	}
	for k, v := range counts {
		fmt.Fprintf(os.Stderr, "    %-40s %d\n", k, v)
	}
	fmt.Fprintln(os.Stderr)
	if sawCC {
		fmt.Fprintln(os.Stderr, "  ✓ ConfigComplete nonce seen — full handshake succeeded.")
		fmt.Fprintln(os.Stderr, "    If the TUI still shows no data, the bug is in the")
		fmt.Fprintln(os.Stderr, "    pump/model wiring, not the BLE transport.")
	} else if total > 0 {
		fmt.Fprintln(os.Stderr, "  ⚠ Got some packets but no ConfigComplete — partial handshake.")
		fmt.Fprintln(os.Stderr, "    Radio may be slow, or toRadio writes may not be landing.")
	} else {
		fmt.Fprintln(os.Stderr, "  ✗ Zero packets received.")
		fmt.Fprintln(os.Stderr, "    Either fromNum notifications aren't subscribing,")
		fmt.Fprintln(os.Stderr, "    or the WantConfigId write never reached the radio.")
		fmt.Fprintln(os.Stderr, "    Try: disconnect in macOS Bluetooth settings, then re-pair,")
		fmt.Fprintln(os.Stderr, "    then re-run this probe.")
	}
}

func init() {
	bleCmd.AddCommand(bleProbeCmd)
}

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

package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
	"github.com/spf13/cobra"

	"github.com/retr0h/meshx/internal/meshx/transport"
)

var (
	probePort    string
	probeTimeout time.Duration
	probeDump    bool
	probeIDTO    time.Duration
)

// probeCmd — diagnostic. Two modes:
//
//  1. Default (no --port): scans all candidate serial ports, probes each
//     for Meshtastic, prints a human-readable table. Tells the user
//     EXACTLY which port is their radio and what to pass as --port.
//
//  2. With --port: opens that specific device, runs the full config
//     handshake, dumps every FromRadio packet for --timeout. Used when
//     debugging framing or protocol issues against a known device.
var probeCmd = &cobra.Command{
	Use:   "probe",
	Short: "Find and identify Meshtastic radios on USB",
	Long: `Scans USB-serial devices and identifies which (if any) are Meshtastic
radios. Prints a labelled table so you know exactly which port to
pass as --port to the main meshx command.

With --port <path>, runs the full config handshake against that
device and dumps every FromRadio packet for the --timeout duration
— a deep diagnostic for framing / protocol debugging.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		logger.With(slog.String("subsystem", "usb.probe")).Debug(
			"running",
			slog.String("port", probePort),
			slog.Duration("timeout", probeTimeout),
			slog.Duration("id_timeout", probeIDTO),
			slog.Bool("dump", probeDump),
		)
		if probePort != "" {
			return probeDeepDump(probePort)
		}
		return probeScanAndIdentify()
	},
}

func probeScanAndIdentify() error {
	fmt.Fprintln(os.Stderr, "scanning USB-serial ports for Meshtastic radios…")
	infos, err := transport.IdentifyAllSerial(probeIDTO)
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		fmt.Println("no USB-serial devices found")
		fmt.Println()
		fmt.Println("troubleshooting:")
		fmt.Println("  - plug in your radio with a DATA USB cable (not charge-only)")
		fmt.Println("  - verify the radio is powered on")
		fmt.Println("  - check `ls /dev/cu.*` (macOS) or `ls /dev/ttyUSB*` (Linux)")
		return nil
	}

	var meshtastic, other []transport.DeviceInfo
	for _, d := range infos {
		if d.IsMeshtastic {
			meshtastic = append(meshtastic, d)
		} else {
			other = append(other, d)
		}
	}

	if len(meshtastic) > 0 {
		fmt.Printf("Meshtastic radio(s) found (%d):\n", len(meshtastic))
		for _, d := range meshtastic {
			fmt.Println(d.String())
		}
		fmt.Println()
		if len(meshtastic) == 1 {
			fmt.Printf("  → to start meshx against this radio:\n")
			fmt.Printf("      meshx                         # auto-detects this device\n")
			fmt.Printf("      meshx --port %s   # explicit\n", meshtastic[0].Port)
		} else {
			fmt.Println("  → multiple radios found. Start meshx with one of:")
			for _, d := range meshtastic {
				fmt.Printf("      meshx --port %s\n", d.Port)
			}
		}
	} else {
		fmt.Println("no Meshtastic radios responded on any serial port.")
		fmt.Println()
		fmt.Println("ports checked:")
		for _, d := range infos {
			fmt.Println(d.String())
		}
		fmt.Println()
		fmt.Println("troubleshooting:")
		fmt.Println("  - the device might be a non-Meshtastic serial adapter")
		fmt.Println("  - power-cycle the radio")
		fmt.Println("  - try a known-good DATA USB cable")
		fmt.Println("  - confirm firmware is installed on the radio")
	}

	if len(other) > 0 && len(meshtastic) > 0 {
		fmt.Println()
		fmt.Println("other USB-serial devices (not Meshtastic):")
		for _, d := range other {
			fmt.Println(d.String())
		}
	}
	return nil
}

func probeDeepDump(dest string) error {
	fmt.Fprintf(os.Stderr, "probe: connecting to %s\n", dest)

	client, err := transport.Dial(dest)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	in := make(chan *pb.ToRadio, 4)
	out := make(chan *pb.FromRadio, 16)

	runErr := make(chan error, 1)
	go func() { runErr <- client.Run(ctx, out, in) }()

	nonce := transport.SendWantConfig(in)
	fmt.Fprintf(os.Stderr, "probe: handshake sent (nonce=0x%x)\n", nonce)

	n := 0
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(
				os.Stderr,
				"probe: timeout after %s, %d packets received\n",
				probeTimeout,
				n,
			)
			return nil
		case err := <-runErr:
			if err != nil {
				return fmt.Errorf("transport: %w", err)
			}
			return nil
		case msg := <-out:
			n++
			if probeDump {
				printFromRadio(n, msg)
			}
			if cc := msg.GetConfigCompleteId(); cc == nonce {
				if !probeDump {
					fmt.Fprintf(
						os.Stderr,
						"probe: handshake complete (%d packets). Re-run with --dump to see each packet.\n",
						n,
					)
				} else {
					fmt.Fprintf(os.Stderr, "probe: handshake complete (%d packets)\n", n)
				}
				return nil
			}
		}
	}
}

// printFromRadio emits a one-line summary per packet — enough to
// verify we're talking to the radio without dumping full protobuf.
func printFromRadio(seq int, msg *pb.FromRadio) {
	switch v := msg.GetPayloadVariant().(type) {
	case *pb.FromRadio_MyInfo:
		fmt.Printf("[%3d] MyNodeInfo     num=0x%x  reboots=%d  min_app=%d\n",
			seq, v.MyInfo.GetMyNodeNum(), v.MyInfo.GetRebootCount(), v.MyInfo.GetMinAppVersion())
	case *pb.FromRadio_NodeInfo:
		u := v.NodeInfo.GetUser()
		fmt.Printf("[%3d] NodeInfo       num=0x%x  long=%q short=%q hw=%v\n",
			seq, v.NodeInfo.GetNum(), u.GetLongName(), u.GetShortName(), u.GetHwModel())
	case *pb.FromRadio_Channel:
		c := v.Channel.GetSettings()
		fmt.Printf("[%3d] Channel        index=%d  name=%q role=%v\n",
			seq, v.Channel.GetIndex(), c.GetName(), v.Channel.GetRole())
	case *pb.FromRadio_Config:
		fmt.Printf("[%3d] Config         %T\n", seq, v.Config.GetPayloadVariant())
	case *pb.FromRadio_ModuleConfig:
		fmt.Printf("[%3d] ModuleConfig   %T\n", seq, v.ModuleConfig.GetPayloadVariant())
	case *pb.FromRadio_Packet:
		p := v.Packet
		fmt.Printf("[%3d] MeshPacket     from=0x%x to=0x%x port=%v hops=%d snr=%v rssi=%v\n",
			seq, p.GetFrom(), p.GetTo(), p.GetDecoded().GetPortnum(),
			int(p.GetHopStart())-int(p.GetHopLimit()), p.GetRxSnr(), p.GetRxRssi())
	case *pb.FromRadio_ConfigCompleteId:
		fmt.Printf("[%3d] ConfigComplete nonce=0x%x\n", seq, v.ConfigCompleteId)
	default:
		fmt.Printf("[%3d] %T\n", seq, v)
	}
}

func init() {
	probeCmd.Flags().StringVar(
		&probePort, "port", "",
		"Serial device path or TCP host[:port]. When omitted, probe scans + identifies all candidate USB-serial devices.",
	)
	probeCmd.Flags().DurationVar(
		&probeTimeout, "timeout", 5*time.Second,
		"Deep-dump timeout (only used with --port). Default 5s covers a handshake.",
	)
	probeCmd.Flags().DurationVar(
		&probeIDTO, "id-timeout", 1500*time.Millisecond,
		"Per-port identification timeout during scan. Default 1.5s.",
	)
	probeCmd.Flags().BoolVar(
		&probeDump, "dump", false,
		"With --port, dump each FromRadio packet (default: show summary on completion).",
	)
}

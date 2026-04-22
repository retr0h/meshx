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
	"errors"
	"fmt"
	"strings"
	"time"

	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
)

// DeviceInfo summarizes one candidate serial port after an
// identification probe: whether it responded to a Meshtastic
// handshake, and (if so) what node metadata came back.
type DeviceInfo struct {
	Port         string // /dev/cu.usbmodem2101 etc.
	IsMeshtastic bool
	Err          error // why identification failed (only when !IsMeshtastic)

	// Populated when IsMeshtastic is true:
	NodeNum   uint32
	ShortName string
	LongName  string
	HWModel   string // pretty form, e.g. "HELTEC_V3" or "hw 99" for unknowns
}

// String renders the device in a one-line human-readable form, used
// by `meshx probe` output.
func (d DeviceInfo) String() string {
	if !d.IsMeshtastic {
		if d.Err != nil {
			return fmt.Sprintf("  %-40s  not Meshtastic — %v", d.Port, d.Err)
		}
		return fmt.Sprintf("  %-40s  not Meshtastic", d.Port)
	}
	name := d.LongName
	if name == "" {
		name = d.ShortName
	}
	if name == "" {
		name = fmt.Sprintf("node 0x%x", d.NodeNum)
	}
	return fmt.Sprintf("  %-40s  ✓ Meshtastic — %s (%s)", d.Port, name, d.HWModel)
}

// IdentifyAllSerial walks every candidate USB serial port and probes
// each one for Meshtastic. The probe is non-destructive: it opens the
// port, sends a WantConfigId handshake, waits up to timeout for a
// valid FromRadio frame, then closes.
//
// Returns the full list in the order ListSerialPorts() yielded them.
// Callers pick the IsMeshtastic==true entries; the rest carry an err
// explaining why identification failed (wrong baud, bound socket,
// non-Meshtastic protocol, timeout).
func IdentifyAllSerial(timeout time.Duration) ([]DeviceInfo, error) {
	ports, err := ListSerialPorts()
	if err != nil {
		return nil, err
	}
	infos := make([]DeviceInfo, len(ports))
	for i, p := range ports {
		infos[i] = identifyOne(p, timeout)
	}
	return infos, nil
}

// AutoDetectMeshtastic probes every serial candidate and returns the
// single device that responded as a Meshtastic radio. Errors when
// zero or more than one is found — with a message that names the
// exact device(s) discovered so the user can copy/paste.
func AutoDetectMeshtastic(timeout time.Duration) (string, error) {
	infos, err := IdentifyAllSerial(timeout)
	if err != nil {
		return "", err
	}
	var hits []DeviceInfo
	for _, d := range infos {
		if d.IsMeshtastic {
			hits = append(hits, d)
		}
	}
	switch len(hits) {
	case 0:
		if len(infos) == 0 {
			return "", fmt.Errorf(
				"no USB-serial device found\n\n" +
					"  - Plug in your Meshtastic radio with a DATA USB cable (not charge-only)\n" +
					"  - Verify the radio is powered on (LED / screen active)\n" +
					"  - Some radios need a driver:\n" +
					"      CH340/CH341: https://www.wch-ic.com/downloads/CH34XSER_MAC_ZIP.html\n" +
					"      CP210x:      https://www.silabs.com/developers/usb-to-uart-bridge-vcp-drivers",
			)
		}
		lines := []string{"no Meshtastic radio responded on any serial port:"}
		for _, d := range infos {
			lines = append(lines, d.String())
		}
		lines = append(lines,
			"",
			"  Try: power-cycle the radio, use a known-good DATA USB cable, or pass --port <path>.",
		)
		return "", errors.New(strings.Join(lines, "\n"))
	case 1:
		return hits[0].Port, nil
	default:
		lines := []string{"multiple Meshtastic radios found; pass --port <path> to pick one:"}
		for _, d := range hits {
			lines = append(lines, d.String())
		}
		return "", errors.New(strings.Join(lines, "\n"))
	}
}

// identifyOne opens a single port, asks it to identify, and closes.
// Always returns a populated DeviceInfo — even on failure, with Err
// set so the caller can render a useful "why not" line.
func identifyOne(port string, timeout time.Duration) DeviceInfo {
	info := DeviceInfo{Port: port}

	client, err := DialSerial(port)
	if err != nil {
		info.Err = err
		return info
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	in := make(chan *pb.ToRadio, 2)
	out := make(chan *pb.FromRadio, 8)
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx, out, in) }()

	// Fire the handshake — a well-formed ToRadio envelope. Non-
	// Meshtastic devices ignore it or echo junk; they won't decode
	// into a valid FromRadio frame within the timeout.
	_ = SendWantConfig(in)

	for {
		select {
		case <-ctx.Done():
			if info.NodeNum == 0 {
				info.Err = fmt.Errorf("no Meshtastic response within %s", timeout)
			}
			return info
		case msg := <-out:
			if msg == nil {
				continue
			}
			// Any recognizable variant counts as Meshtastic.
			switch v := msg.GetPayloadVariant().(type) {
			case *pb.FromRadio_MyInfo:
				info.IsMeshtastic = true
				info.NodeNum = v.MyInfo.GetMyNodeNum()
			case *pb.FromRadio_NodeInfo:
				if v.NodeInfo.GetNum() == info.NodeNum || info.NodeNum == 0 {
					info.IsMeshtastic = true
					u := v.NodeInfo.GetUser()
					if info.NodeNum == 0 {
						info.NodeNum = v.NodeInfo.GetNum()
					}
					info.ShortName = u.GetShortName()
					info.LongName = u.GetLongName()
					info.HWModel = hwModelName(int(u.GetHwModel()))
				}
			case *pb.FromRadio_ConfigCompleteId:
				// Seen the whole config dump — we have enough.
				return info
			}
		}
	}
}

// HwModelName is the public version of hwModelName, exported for the
// meshx package's pump which translates NodeInfo telemetry.
func HwModelName(n int) string { return hwModelName(n) }

// hwModelName maps the HardwareModel enum integer to a pretty name.
// Meshtastic adds new hardware variants faster than vendored protobuf
// bindings can keep up, so we fall back to "hw %d" for unknowns — the
// user will at least see a stable integer they can look up.
func hwModelName(n int) string {
	// Known values from the current vendored proto. New ones fall
	// through to the numeric form below.
	known := map[int]string{
		0:   "UNSET",
		1:   "TLORA_V2",
		2:   "TLORA_V1",
		3:   "TLORA_V2_1_1P6",
		4:   "TBEAM",
		5:   "HELTEC_V2_0",
		6:   "TBEAM_V0P7",
		7:   "T_ECHO",
		8:   "TLORA_V1_1P3",
		9:   "RAK4631",
		10:  "HELTEC_V2_1",
		11:  "HELTEC_V1",
		12:  "LILYGO_TBEAM_S3_CORE",
		13:  "RAK11200",
		14:  "NANO_G1",
		15:  "TLORA_V2_1_1P8",
		16:  "TLORA_T3_S3",
		17:  "NANO_G1_EXPLORER",
		18:  "NANO_G2_ULTRA",
		19:  "LORA_TYPE",
		20:  "WIPHONE",
		21:  "WIO_WM1110",
		22:  "RAK2560",
		23:  "HELTEC_HRU_3601",
		25:  "STATION_G1",
		26:  "RAK11310",
		27:  "SENSELORA_RP2040",
		28:  "SENSELORA_S3",
		29:  "CANARYONE",
		30:  "RP2040_LORA",
		31:  "STATION_G2",
		32:  "LORA_RELAY_V1",
		33:  "NRF52840DK",
		34:  "PPR",
		35:  "GENIEBLOCKS",
		36:  "NRF52_UNKNOWN",
		37:  "PORTDUINO",
		38:  "ANDROID_SIM",
		39:  "DIY_V1",
		40:  "NRF52840_PCA10059",
		41:  "DR_DEV",
		42:  "M5STACK",
		43:  "HELTEC_V3",
		44:  "HELTEC_WSL_V3",
		45:  "BETAFPV_2400_TX",
		46:  "BETAFPV_900_NANO_TX",
		47:  "RPI_PICO",
		48:  "HELTEC_WIRELESS_TRACKER",
		49:  "HELTEC_WIRELESS_PAPER",
		50:  "T_DECK",
		51:  "T_WATCH_S3",
		52:  "PICOMPUTER_S3",
		53:  "HELTEC_HT62",
		54:  "EBYTE_ESP32_S3",
		55:  "ESP32_S3_PICO",
		56:  "CHATTER_2",
		57:  "HELTEC_WIRELESS_PAPER_V1_0",
		58:  "HELTEC_WIRELESS_TRACKER_V1_0",
		59:  "UNPHONE",
		60:  "TD_LORAC",
		61:  "CDEBYTE_EORA_S3",
		62:  "TWC_MESH_V4",
		63:  "NRF52_PROMICRO_DIY",
		64:  "RADIOMASTER_900_BANDIT_NANO",
		65:  "HELTEC_CAPSULE_SENSOR_V3",
		66:  "HELTEC_VISION_MASTER_T190",
		67:  "HELTEC_VISION_MASTER_E213",
		68:  "HELTEC_VISION_MASTER_E290",
		69:  "HELTEC_MESH_NODE_T114",
		70:  "SENSECAP_INDICATOR",
		71:  "TRACKER_T1000_E",
		72:  "RAK3172",
		73:  "WIO_E5",
		74:  "RADIOMASTER_900_BANDIT",
		75:  "ME25LS01_4Y10TD",
		76:  "RP2040_FEATHER_RFM95",
		77:  "M5STACK_COREBASIC",
		78:  "M5STACK_CORE2",
		79:  "RPI_PICO2",
		80:  "M5STACK_CORES3",
		81:  "SEEED_XIAO_S3",
		82:  "MS24SF1",
		83:  "TLORA_C6",
		84:  "WISMESH_TAP",
		85:  "ROUTASTIC",
		86:  "MESH_TAB",
		87:  "MESHLINK",
		88:  "XIAO_NRF52_KIT",
		89:  "THINKNODE_M1",
		90:  "THINKNODE_M2",
		91:  "T_ETH_ELITE",
		92:  "HELTEC_SENSOR_HUB",
		93:  "RESERVED_FRIED_CHICKEN",
		94:  "HELTEC_MESH_POCKET",
		95:  "SEEED_SOLAR_NODE",
		96:  "NOMADSTAR_METEOR_PRO",
		97:  "CROWPANEL",
		99:  "HELTEC_V3_E",
		101: "PRIVATE_HW",
	}
	if name, ok := known[n]; ok {
		return name
	}
	return fmt.Sprintf("hw %d", n)
}

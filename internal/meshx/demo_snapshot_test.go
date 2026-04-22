package meshx

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSnapshotView prints the full View() string to test output so we
// can eyeball the actual render without spawning a TTY.
func TestSnapshotView(_ *testing.T) {
	m := initialModel()
	m.mode = modeInput
	m.input.Focus()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = updated.(model)
	fmt.Println("──── snapshot (demo) ────")
	fmt.Println(m.View())
	fmt.Println("──── end ────")
}

// TestSnapshotLive simulates the state the model would be in after a
// successful radio handshake, so the live-mode top bar is fully
// populated — every segment shows a real value.
func TestSnapshotLive(_ *testing.T) {
	m := initialRadioModel("/dev/cu.usbmodem2101")
	m.mode = modeInput
	m.input.Focus()
	m.connected = true
	m.myNodeNum = 0x103d20cd
	m.radioFirmware = "2.7.15.567b8ea"
	m.radioRole = "CLIENT"
	m.radioRegion = "US"
	m.radioModemPreset = "LONG_FAST"
	m.radioTxPower = 30
	m.hasTelemetry = true
	m.batteryLevel = 87
	m.batteryVoltage = 3.94
	m.channelUtil = 4.2
	m.myGrid = "CN85ow"
	m.currentChannel = "#default"
	m.channels = []channelItem{{name: "#default"}}
	// Minimal NodeDB so myCallsign / myNode resolve to something live.
	m.nodes = []nodeItem{{
		callsign: "retr0h",
		hwModel:  "HELTEC_V3_E",
		firmware: "2.7.15.567b8ea",
		state:    "online",
	}}
	m.nodesByNum[m.myNodeNum] = 0

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 220, Height: 20})
	m = updated.(model)
	fmt.Println("──── snapshot (live, fully populated) ────")
	fmt.Println(m.View())
	fmt.Println("──── end ────")
}

package meshx

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestVisualAuditDump renders a live-mode model with specific
// messages + peers that exercise every user-reported bug, then
// prints the stripped View() output to stderr so the dev can
// inspect the actual ANSI layout without running the TUI.
func TestVisualAuditDump(t *testing.T) {
	if os.Getenv("MESHX_AUDIT") == "" {
		t.Skip("set MESHX_AUDIT=1 to dump visual audit")
	}
	m := newModel(nil, "/dev/null")
	defer func() {
		if m.db != nil {
			_ = m.db.Close()
		}
	}()
	m.w = 180
	m.h = 50
	m.myLatitude = 34.0
	m.myLongitude = -118.0
	m.myNodeNum = 0x103d20cd
	m.connected = true

	// Seed peers.
	for _, p := range []struct {
		num      uint32
		callsign string
		lat, lon float64
	}{
		{0x7fb49f66, "retr0h-A", 34.01, -118.0},
		{0x7aad1435, "retr0h-B", 34.02, -118.0},
		{0xda5ebc78, "Gleep", 34.01, -117.99},
		{0xb29fac1c, "ATAK 8ca7", 34.0, -117.98},
		{0x85c5edab, "PAS1", 33.99, -118.0},
		{0xabc, "mmca solar test", 0, 0},
		{0xdead01, "CyberdyneSystems", 0, 0},
		{0x43adacc, "node 0x43adacc", 0, 0},
		{0xbeef, "bubbingtenny2k", 0, 0},
		{0x82680694, "node 0x82680694", 0, 0},
	} {
		m.nodes = append(m.nodes, nodeItem{
			callsign:    p.callsign,
			nodeNum:     p.num,
			lastHeardAt: time.Now(),
		})
		m.nodesByNum[p.num] = len(m.nodes) - 1
		if p.lat != 0 {
			m.peerPositions[p.num] = peerPosition{
				latitude:  p.lat,
				longitude: p.lon,
				at:        time.Now(),
			}
		}
	}
	m.nodes = append(m.nodes, nodeItem{
		callsign:    "retr0h",
		nodeNum:     m.myNodeNum,
		lastHeardAt: time.Now(),
	})
	m.nodesByNum[m.myNodeNum] = len(m.nodes) - 1

	// Seed messages — the multi-line canary + hop-column-drift
	// rows the user's screenshots flagged.
	m.messages = []messageItem{
		{
			time: "19:20", from: "mmca solar test", fromNum: 0xabc,
			text:   "End of Day Report:\nMax Power: 1375.2576 mW at Pot setting: 133, Voltage: 6.0160 V, Current: 228.6000 mA\nWas the battery fully charged during the day? Yes",
			status: "ack", hops: 4, snr: "-3.5",
		},
		{
			time: "19:24", from: "node 0x82680694", fromNum: 0x82680694,
			text: "?", status: "ack", hops: 6, snr: "9.5",
		},
		{
			time: "19:26", from: "CyberdyneSystems", fromNum: 0xdead01,
			text: "🔔 Alert Bell Character!", status: "ack", hops: 0, snr: "-12.2",
		},
		{
			time: "19:44", from: "node 0x43adacc", fromNum: 0x43adacc,
			text: "Hello", status: "ack", hops: 6, snr: "-11.8",
		},
		{
			time: "21:10", from: "bubbingtenny2k", fromNum: 0xbeef,
			text: "understandable", status: "ack", hops: 4, snr: "-0.5",
		},
	}
	m.selectedMsg = len(m.messages) - 1
	m.currentChannel = "#default"
	m.channels = []channelItem{{name: "#default"}}

	// ── Messages pane ─────────────────────────────────────────
	fmt.Fprintln(os.Stderr, "=== MESSAGES PANE ===")
	fmt.Fprintln(os.Stderr, stripANSI(m.renderMessagesPane(m.w, m.h-5)))

	// ── /radar ────────────────────────────────────────────────
	fmt.Fprintln(os.Stderr, "\n=== /radar ===")
	fmt.Fprintln(os.Stderr, stripANSI(m.renderRadarPane(m.w, m.h-5)))

	// ── /nearby ───────────────────────────────────────────────
	fmt.Fprintln(os.Stderr, "\n=== /nearby ===")
	m.overlay = overlayNearby
	m.focused = paneNodes
	fmt.Fprintln(os.Stderr, stripANSI(m.renderNearbyPane(m.w, m.h-5)))

	// ── /nodes ────────────────────────────────────────────────
	fmt.Fprintln(os.Stderr, "\n=== /nodes ===")
	m.overlay = overlayNodes
	fmt.Fprintln(os.Stderr, stripANSI(m.renderNodesPane(m.w, m.h-5)))

	// Check that ANSI is actually present in the unstripped
	// /radar output — if paneStyle strips it, user sees no color
	// regardless of what we do.
	rawRadar := m.renderRadarPane(m.w, m.h-5)
	escapes := strings.Count(rawRadar, "\x1b[")
	t.Logf("radar raw output: %d ANSI escapes, %d bytes", escapes, len(rawRadar))
	if escapes < 20 {
		t.Errorf("radar output has only %d ANSI escapes — coloring isn't reaching render", escapes)
	}
}

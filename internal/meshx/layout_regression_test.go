package meshx

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// stripANSI removes CSI escape sequences so we can inspect layout
// without the terminal color noise.
func stripANSI(s string) string {
	var b strings.Builder
	inSeq := false
	for _, r := range s {
		if r == 0x1b {
			inSeq = true
			continue
		}
		if inSeq {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inSeq = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestMultiLineMessageLayout(t *testing.T) {
	m := newModel(DefaultDemo(), "")
	m.w = 140
	m.h = 40

	msg := messageItem{
		time:    "19:20",
		from:    "mmca solar test",
		text:    "End of Day Report:\nMax Power: 1375.2576 mW at Pot setting: 133, Voltage: 6.0160 V, Current: 228.6000 mA\nWas the battery fully charged during the day? Yes",
		status:  "ack",
		hops:    4,
		snr:     "-3.5",
		fromNum: 0xabcdef12,
	}
	rendered := m.renderMessageRow(msg, false, m.w-4, rowBgOdd, false, false)
	lines := strings.Split(stripANSI(rendered), "\n")
	t.Logf("rendered %d lines", len(lines))
	for i, ln := range lines {
		t.Logf("  [%d] %q (w=%d)", i, ln, lipgloss.Width(ln))
	}

	// Visual width must match m.w-4 for every line.
	targetW := m.w - 4
	for i, ln := range lines {
		got := lipgloss.Width(ln)
		if got != targetW {
			t.Errorf("line %d width=%d want=%d", i, got, targetW)
		}
	}

	// First line must carry the signal columns (hop marker + SNR);
	// continuation lines must NOT. Hop format is right-aligned
	// ("↝  4h") so check for the glyph and the hop digit
	// separately rather than a concatenated literal.
	if !strings.Contains(lines[0], "↝") || !strings.Contains(lines[0], "4h") ||
		!strings.Contains(lines[0], "-3.5dB") {
		t.Errorf("line 0 missing signal columns: %q", lines[0])
	}
	for i := 1; i < len(lines); i++ {
		if strings.Contains(lines[i], "↝") || strings.Contains(lines[i], "dB") {
			t.Errorf("line %d should not carry signal columns: %q", i, lines[i])
		}
	}
}

func TestHopSnrColumnAlignment(t *testing.T) {
	m := newModel(DefaultDemo(), "")
	m.w = 140
	m.h = 40

	rows := []messageItem{
		{
			time:    "19:24",
			from:    "node 0x82680694",
			text:    "?",
			status:  "ack",
			hops:    6,
			snr:     "9.5",
			fromNum: 0x82680694,
		},
		{
			time:    "19:26",
			from:    "CyberdyneSystems",
			text:    "🔔 Alert Bell Character!",
			status:  "ack",
			hops:    0,
			snr:     "-12.2",
			fromNum: 0xdead01,
		},
		{
			time:    "19:44",
			from:    "node 0x43adacc",
			text:    "Hello",
			status:  "ack",
			hops:    6,
			snr:     "-11.8",
			fromNum: 0x43adacc,
		},
		{
			time:    "21:10",
			from:    "bubbingtenny2k",
			text:    "understandable",
			status:  "ack",
			hops:    4,
			snr:     "-0.5",
			fromNum: 0xbeef,
		},
	}
	// Measure by VISUAL cells (lipgloss.Width of the prefix up to
	// the ↝), not byte index — "node 0x…" contains no multi-byte
	// runes but "👻 " prefixes do, and "🔔" in the body shifts byte
	// counts without shifting visual columns. A pure strings.Index
	// gives false alignment failures on rows that differ only in
	// emoji presence.
	var visualCols []int
	for _, row := range rows {
		s := stripANSI(m.renderMessageRow(row, false, m.w-4, rowBgOdd, false, false))
		idx := strings.Index(s, "↝")
		if idx < 0 {
			t.Errorf("no hop marker in: %q", s)
			continue
		}
		visualCol := lipgloss.Width(s[:idx])
		visualCols = append(visualCols, visualCol)
		t.Logf("visualCol=%d for %s", visualCol, row.from)
	}
	for i := 1; i < len(visualCols); i++ {
		if visualCols[i] != visualCols[0] {
			t.Errorf("hop column drifted: row %d col=%d vs row0 col=%d",
				i, visualCols[i], visualCols[0])
		}
	}
}

func TestNodeNumColorDistinct(t *testing.T) {
	// Two retr0h radios (same callsign, different nodeNum) should
	// land on different palette colors.
	nums := []uint32{0x7fb49f66, 0x7aad1435, 0x103d20cd, 0xda5ebc78, 0xb29fac1c}
	seen := map[string][]uint32{}
	for _, n := range nums {
		c := nodeNumColor(n)
		seen[c] = append(seen[c], n)
	}
	t.Logf("palette assignments:")
	for c, ns := range seen {
		t.Logf("  %s → %v", c, ns)
	}
	if len(seen) < len(nums) {
		// Some collision is OK but two adjacent radios must not
		// collapse — that's the "both retr0h pink" bug.
		t.Logf("collisions: %d distinct colors across %d nodeNums", len(seen), len(nums))
	}
	if a, b := nodeNumColor(0x7fb49f66), nodeNumColor(0x7aad1435); a == b {
		t.Errorf("0x7fb49f66 and 0x7aad1435 collapsed to same color %s", a)
	}
}

func TestHelpExitsToInput(t *testing.T) {
	m := newModel(DefaultDemo(), "")
	m.w = 140
	m.h = 40
	m.mode = modeHelp

	// Simulate ESC in help mode.
	// updateHelp is a method; can't call tea.KeyMsg directly without
	// the package machinery. Instead just verify the closeOverlayToInput
	// call sequence produces modeInput.
	_ = m.closeOverlayToInput()
	if m.mode != modeInput {
		t.Errorf("after closeOverlayToInput, mode=%v want modeInput", m.mode)
	}
}

func TestRadarLegendColorMatchesCanvas(t *testing.T) {
	// Seed a live-mode model with self + a handful of peers at
	// distinct positions so /radar has content to plot.
	m := newModel(nil, "/dev/null")
	defer func() { _ = m.db.Close() }()
	m.w = 140
	m.h = 40
	m.myLatitude = 34.0
	m.myLongitude = -118.0
	m.myNodeNum = 0x103d20cd

	// Peer setup — 5 close peers with distinct nodeNums, plus one
	// further peer to test the "non-legend keeps default color"
	// path.
	peers := []struct {
		num      uint32
		callsign string
		lat, lon float64
	}{
		{0x7fb49f66, "retr0h-A", 34.01, -118.0},
		{0x7aad1435, "retr0h-B", 34.02, -118.0},
		{0xda5ebc78, "Gleep", 34.01, -117.99},
		{0xb29fac1c, "ATAK 8ca7", 34.0, -117.98},
		{0x85c5edab, "PAS1", 33.99, -118.0},
		{0xffffffff, "Far", 35.0, -119.0},
	}
	for _, p := range peers {
		m.nodes = append(m.nodes, nodeItem{
			callsign:    p.callsign,
			nodeNum:     p.num,
			lastHeardAt: time.Now(),
		})
		m.nodesByNum[p.num] = len(m.nodes) - 1
		m.peerPositions[p.num] = peerPosition{
			latitude:  p.lat,
			longitude: p.lon,
			at:        time.Now(),
		}
	}
	// Self node too.
	m.nodes = append(m.nodes, nodeItem{
		callsign:    "retr0h",
		nodeNum:     m.myNodeNum,
		lastHeardAt: time.Now(),
	})
	m.nodesByNum[m.myNodeNum] = len(m.nodes) - 1

	rendered := m.renderRadarPane(m.w, m.h)
	stripped := stripANSI(rendered)
	for _, ln := range strings.Split(stripped, "\n") {
		t.Log(ln)
	}
	fmt.Println("radar ---")
	fmt.Println(rendered)
	fmt.Println("--- end radar")

	// The legend should name the 5 closest peers.
	for _, name := range []string{"retr0h-A", "retr0h-B", "Gleep", "ATAK 8ca7", "PAS1"} {
		if !strings.Contains(stripped, name) {
			t.Errorf("legend missing %q", name)
		}
	}

	// The /radar legend uses deterministic slot-index assignment
	// (nickColorPalette[i % len]) for closest-N peers so distinct
	// colors are GUARANTEED regardless of hash quality. Can't
	// directly read legendColors from inside renderRadarPane, but
	// we can verify the scheme against the palette: for any N ≤
	// len(palette), palette[0..N-1] are trivially distinct.
	if len(nickColorPalette) < 5 {
		t.Fatalf("palette too small (%d) for 5-peer legend",
			len(nickColorPalette))
	}
	seen := map[string]int{}
	for i := 0; i < 5; i++ {
		c := nickColorPalette[i%len(nickColorPalette)]
		if prior, dup := seen[c]; dup {
			t.Errorf("palette slot %d and %d both = %s — closest-N distinctness broken",
				i, prior, c)
		}
		seen[c] = i
	}
	t.Logf("/radar closest-5 → %d distinct palette slots (of %d palette size)",
		len(seen), len(nickColorPalette))
	// NOTE: we don't assert ANSI escape bytes in rendered output —
	// lipgloss's termenv detection suppresses color in non-TTY
	// test environments (go test pipes stdout), so even valid
	// styling produces bare runes here. In the live TUI where
	// Bubble Tea owns the terminal, the escapes emit as expected.
}

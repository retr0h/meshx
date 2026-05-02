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

package meshx

// ui_map.go — the /nearby and /radar overlay renderers.
//
// Both surfaces need (bearing, distance) from our position to every
// peer with a known fix. They share a `peerPlot` collection helper
// so the math runs once per render; /nearby sorts by distance and
// formats a bar chart, /radar projects onto a polar grid. Geometry
// helpers (haversineKm, bearingDeg, compassAbbr) live in geo.go.

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// peerPlot is one row's worth of (who, where) for the geo overlays.
// distanceKm and bearing are recomputed from live lat/lon rather
// than cached on the nodeItem so moving QTH (/qth) picks up the
// new self-position on the very next render.
type peerPlot struct {
	node     *nodeItem
	distKm   float64
	bearing  float64 // 0-360°; 0 = north, 90 = east.
	directRF bool    // lastHops <= 1 — we can hear them without a relay.
}

// sortPlotsByDistance orders a plot slice closest-first. Shared by
// /nearby, /radar's legend, and selectedNodeItem so "the peer at
// index N" means the same thing everywhere.
//
// Uses nodeNum as a deterministic tiebreaker on equal distances —
// without it, collocated peers (think two radios at the same
// tower) iterate in map order from m.peerPositions and can swap
// slots between renders, making j/k feel "jumpy" when the cursor
// lands on a row that just got reordered beneath it.
func sortPlotsByDistance(plots []peerPlot) {
	sort.Slice(plots, func(i, j int) bool {
		if plots[i].distKm != plots[j].distKm {
			return plots[i].distKm < plots[j].distKm
		}
		return plots[i].node.nodeNum < plots[j].node.nodeNum
	})
}

// nearbyRoster returns the ordered slice /nearby renders — self
// prepended at the top (distance 0) followed by every peer plot
// sorted by distance. Shared entrypoint so renderNearbyPane,
// selectedNodeItem (for whois/ping/etc from the cursor), and the
// bounds clamp in moveSelection all treat "the peer at index N"
// as the same peer.
//
// /radar deliberately does NOT use this; it plots self at the
// canvas centre instead and only consumes the peer list.
func (m model) nearbyRoster() []peerPlot {
	plots := m.collectPeerPlots()
	sortPlotsByDistance(plots)
	if self := m.selfPlot(); self != nil {
		// Prepend — zero distance sorts first naturally, but
		// explicit prepend avoids surprises if a peer ever reports
		// lat/lon identical to ours.
		plots = append([]peerPlot{*self}, plots...)
	}
	return plots
}

// selfPlot builds a peerPlot for our own radio — used by
// nearbyRoster to anchor self at the top of the list. Returns nil
// when MyNodeInfo hasn't arrived (no nodeItem for self yet).
func (m model) selfPlot() *peerPlot {
	if m.myNodeNum == 0 {
		return nil
	}
	idx, ok := m.nodesByNum[m.myNodeNum]
	if !ok || idx >= len(m.nodes) {
		return nil
	}
	return &peerPlot{
		node:     &m.nodes[idx],
		distKm:   0,
		bearing:  0,
		directRF: true,
	}
}

// collectPeerPlots walks m.nodes + m.peerPositions and returns a
// plot entry for every peer we have BOTH a position fix AND a
// nodeItem for. Skips self (m.myNodeNum) since the two peer-surface
// overlays each handle self explicitly — /nearby via
// nearbyRoster's prepend, /radar via the centered glyph. Order is
// unspecified — callers sort (usually via sortPlotsByDistance).
func (m model) collectPeerPlots() []peerPlot {
	plots := make([]peerPlot, 0, len(m.peerPositions))
	for num, pos := range m.peerPositions {
		if num == m.myNodeNum {
			continue
		}
		idx, ok := m.nodesByNum[num]
		if !ok || idx >= len(m.nodes) {
			continue
		}
		km := haversineKm(m.myLatitude, m.myLongitude, pos.latitude, pos.longitude)
		if km <= 0 {
			continue
		}
		n := &m.nodes[idx]
		plots = append(plots, peerPlot{
			node:     n,
			distKm:   km,
			bearing:  bearingDeg(m.myLatitude, m.myLongitude, pos.latitude, pos.longitude),
			directRF: n.lastHops <= 1,
		})
	}
	return plots
}

// renderNearbyPane — distance-sorted roster. Each row has a
// distance bar scaled to the farthest peer in the list, absolute
// km, compass bearing, and the 8-point cardinal abbreviation.
// Selection highlights the cursor via the shared wrapSelection
// pipeline; j/k walks step through peers in order.
func (m model) renderNearbyPane(width, height int) string {
	header := paneHeader("NEARBY", paneNodes, m.focused == paneNodes)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained)).Italic(true)

	// No self-fix — distances + bearings are meaningless without an
	// origin point. Explain the situation in-pane rather than
	// flashing-and-bailing at command time, which made /nearby look
	// broken when the user's radio hadn't broadcast a Position
	// packet yet (cold-boot, GPS off in firmware, indoor with no
	// sky view, etc.).
	if m.myLatitude == 0 && m.myLongitude == 0 {
		count := lipgloss.NewStyle().
			Foreground(lipgloss.Color(mhDrained)).
			Render("  (waiting for own GPS fix)")
		lines := make([]string, 0, 9)
		lines = append(lines,
			header+count,
			"",
			dim.Render("   no GPS fix on this radio yet — /nearby needs your own"),
			dim.Render("   position to compute distances + bearings to peers."),
			"",
			dim.Render("   options:"),
			dim.Render("     • wait for your radio to acquire a fix (outdoor + sky view)"),
			dim.Render("     • check position.* in your radio config (Meshtastic app/CLI)"),
			dim.Render("     • try /sync to force a NodeDB re-dump"),
		)
		return renderBorderedPane(
			strings.Join(lines, "\n"), width, height, paneNodes, m.focused == paneNodes,
		)
	}

	plots := m.nearbyRoster()

	peerCount := len(plots)
	if m.myNodeNum != 0 {
		peerCount-- // self doesn't count toward the "N with GPS fix" tally
	}
	count := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Render(fmt.Sprintf("  (%d with GPS fix — closest first)", peerCount))

	lines := make([]string, 0, 3+len(plots))
	lines = append(lines, header+count, "")

	if len(plots) == 0 {
		lines = append(lines,
			dim.Render("   no peers with a GPS fix yet — positions land as"),
			dim.Render("   Meshtastic Position packets arrive (periodic)."),
		)
		return renderBorderedPane(
			strings.Join(lines, "\n"), width, height, paneNodes, m.focused == paneNodes,
		)
	}

	// Scale the per-row bar to the farthest peer so the widest
	// bar fills. Cap the bar to 24 cells — anything wider and the
	// layout breaks on narrow terminals. Bar scaling uses the full
	// list (not the visible window) so distances stay comparable as
	// the user scrolls.
	maxKm := plots[len(plots)-1].distKm
	const barMax = 24

	// Pane height accounting:
	//   border (2) + Padding(1,1) → 4 lines reserved by paneStyle
	//   header + count             → 1 line
	//   blank separator            → 1 line
	// Anything left is the row budget for plot entries. Without this
	// budget, a list longer than the pane overflows lipgloss's Height
	// — the whole UI grows past m.h, the terminal scrolls everything
	// up, and the user is left looking at the bottom of the list with
	// the top (and the cursor on the "(you)" anchor row) scrolled off
	// the top of the screen. That's the "scrolls to the bottom" bug.
	rowsAvailable := height - 4 - 2
	if rowsAvailable < 1 {
		rowsAvailable = 1
	}
	// Pick a window of `rowsAvailable` plots that contains
	// m.selectedNd. Default to the head of the list (closest peers
	// first) so a freshly-opened /nearby starts at the top — the
	// usual case. Slide down only if the cursor moves out of view.
	startIdx := 0
	if len(plots) > rowsAvailable {
		if m.selectedNd >= rowsAvailable {
			startIdx = m.selectedNd - rowsAvailable + 1
		}
		if maxStart := len(plots) - rowsAvailable; startIdx > maxStart {
			startIdx = maxStart
		}
	}
	endIdx := startIdx + rowsAvailable
	if endIdx > len(plots) {
		endIdx = len(plots)
	}
	visible := plots[startIdx:endIdx]

	// Render styling — matches /nodes (renderUserCell) so callsigns
	// carry the same meaning across surfaces: sigil + color by
	// derived state, yellow for favorites, lavender for muted.
	// Bar colors stay meshGreen / mhDrained regardless of peer
	// state — they're a distance ruler, not a state indicator.
	//
	// Every span below carries an explicit Background() — without
	// it, lipgloss's per-span `\e[0m` trailing reset would break
	// the outer wrapSelection tint and the selection highlight
	// would only cover the ██ gutter. Swap rowBg → selectionRowBg
	// at the top of each row when selected so EVERY inner span
	// picks up the selection tint, matching how renderNoticeRow /
	// renderMessageRow solved the same issue.
	for i, p := range visible {
		actualIdx := i + startIdx
		isSel := actualIdx == m.selectedNd && m.focused == paneNodes
		rowBg := rowBgOdd
		if isSel {
			rowBg = selectionRowBg
		}
		bgCol := lipgloss.Color(rowBg)

		// Sigil + name color derived from the live state — same
		// switch /nodes uses so a peer reads consistently across
		// both overlays. Self is marked in magenta (the "me"
		// color reserved in palette.go); fav beats state; self
		// beats fav so the logged-in radio always stands out.
		state := p.node.currentState()
		sigil := " "
		sigilColor := mhDrained
		nameColor := mhFG
		switch state {
		case "online":
			sigil = "@"
			sigilColor = mhGreen
		case "muted":
			sigil = "⊘"
			sigilColor = mhLavender
			nameColor = mhLavender
		case "failed":
			sigil = "✗"
			sigilColor = mhPink
			nameColor = mhPink
		case "offline":
			sigil = "·"
			sigilColor = mhDrained
			nameColor = mhDrained
		}
		if p.node.fav {
			sigil = "+"
			sigilColor = mhYellow
			nameColor = mhYellow
		}
		// Self-marker — only the sigil picks up the magenta "me"
		// color. Name + distance columns stay on their normal
		// state-derived styling so the row reads the same as
		// every other peer; the purple `@` is the sole signal
		// that this is the logged-in radio.
		isSelf := m.myNodeNum != 0 && p.node.nodeNum == m.myNodeNum
		if isSelf {
			sigil = "@"
			sigilColor = mhMagenta
		}
		sigilStyled := lipgloss.NewStyle().
			Foreground(lipgloss.Color(sigilColor)).
			Background(bgCol).
			Bold(state == "online" || p.node.fav || isSelf).
			Render(sigil)
		nameStyled := lipgloss.NewStyle().
			Foreground(lipgloss.Color(nameColor)).
			Background(bgCol).
			Render(padOrTruncate(p.node.callsign, 22))
		barStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(meshGreen)).
			Background(bgCol)
		barDimStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(mhDrained)).
			Background(bgCol)
		dim := lipgloss.NewStyle().
			Foreground(lipgloss.Color(mhDrained)).
			Background(bgCol)
		spacer := lipgloss.NewStyle().Background(bgCol)

		// Self row: zero-distance "0.0 km · N 0°" is technically
		// accurate but reads like a rendering bug. Use "(you)" +
		// "— home QTH" + a dimmed-dots bar so the row declares
		// "this is anchor, not a datapoint." Colors stay dim
		// (not purple) — the magenta sigil already signals self.
		var distCol, bearingCol, barCol string
		if isSelf {
			distCol = dim.Render(padOrTruncate("(you)", 10))
			bearingCol = dim.Render("— home QTH")
			barCol = barDimStyle.Render(strings.Repeat("·", barMax))
		} else {
			filled := int(math.Round(p.distKm / maxKm * barMax))
			if filled < 1 {
				filled = 1
			}
			if filled > barMax {
				filled = barMax
			}
			barCol = barStyle.Render(strings.Repeat("▓", filled)) +
				barDimStyle.Render(strings.Repeat("░", barMax-filled))
			distCol = dim.Render(padOrTruncate(fmt.Sprintf("%6.1f km", p.distKm), 10))
			bearingCol = dim.Render(fmt.Sprintf("%s %3.0f°", compassAbbr(p.bearing), p.bearing))
		}

		row := peerRowLine(
			rowBg, sigilStyled, nameStyled, barCol, barMax,
			distCol, bearingCol,
			paneInnerWidth(width)-gutterWidth,
		)
		lines = append(lines, wrapSelection(row, isSel, false, paneInnerWidth(width), rowBg))
		_ = spacer
	}

	// Clamp selection after the slice-size changes (peers come and
	// go as positions arrive).
	if m.selectedNd >= len(plots) {
		// Don't mutate here — rendering is read-only; the next key
		// press will clamp via moveSelection. Just avoid visual
		// confusion by not flagging an out-of-range row as selected.
		_ = plots
	}

	return renderBorderedPane(
		strings.Join(lines, "\n"), width, height, paneNodes, m.focused == paneNodes,
	)
}

// renderRadarPane — polar scope with peers plotted by (bearing,
// distance). You sit at the centre; north is up, east is right.
// Ring scale is adaptive: the farthest peer sets the outer ring
// and we draw 4 concentric rings inside that.
//
// Glyph choice:
//   - '@' (pink, bold) = self, dead centre
//   - '●' (mesh-green)  = direct-RF peer (lastHops <= 1)
//   - '·' (cyan)        = multi-hop peer
//
// Density note: when two peers project onto the same cell, the
// highest-priority glyph wins (self > direct-RF > multi-hop). The
// legend at the bottom surfaces the closest few by name so a busy
// scope still reads as a directory of "who's close."
func (m model) renderRadarPane(width, height int) string {
	header := paneHeader("RADAR", paneNodes, m.focused == paneNodes)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained)).Italic(true)

	// No self-fix — there's no centre to plot peers around. Same
	// in-pane explainer /nearby uses for the same reason.
	if m.myLatitude == 0 && m.myLongitude == 0 {
		lines := []string{
			header, "",
			dim.Render("   no GPS fix on this radio yet — /radar needs your own"),
			dim.Render("   position to plot peers around you."),
			"",
			dim.Render("   options:"),
			dim.Render("     • wait for your radio to acquire a fix (outdoor + sky view)"),
			dim.Render("     • check position.* in your radio config (Meshtastic app/CLI)"),
			dim.Render("     • try /sync to force a NodeDB re-dump"),
		}
		return renderBorderedPane(
			strings.Join(lines, "\n"), width, height, paneNodes, m.focused == paneNodes,
		)
	}

	plots := m.collectPeerPlots()
	if len(plots) == 0 {
		lines := []string{
			header, "",
			dim.Render("   no peers with a GPS fix yet — waiting for"),
			dim.Render("   Meshtastic Position packets to land."),
		}
		return renderBorderedPane(
			strings.Join(lines, "\n"), width, height, paneNodes, m.focused == paneNodes,
		)
	}

	sortPlotsByDistance(plots)
	// Outer-ring scale = farthest peer, rounded up to a nice tick
	// so the legend reads in round numbers instead of "23.4 km".
	maxKm := niceRadarTick(plots[len(plots)-1].distKm)

	// Grid geometry. Use a square canvas in CHARACTER cells but
	// compensate for the terminal's 2:1 vertical:horizontal cell
	// aspect by widening the horizontal extent. 60x20 with aspect
	// compensation reads as a visually-square scope.
	innerW := width - 6
	if innerW > 80 {
		innerW = 80
	}
	rows := 20
	cols := innerW
	if cols < 40 {
		cols = 40
	}
	cx := cols / 2
	cy := rows / 2

	// Canvas: rows of runes. Pre-fill with ' ' for background.
	canvas := make([][]rune, rows)
	colors := make([][]string, rows)
	for r := 0; r < rows; r++ {
		canvas[r] = make([]rune, cols)
		colors[r] = make([]string, cols)
		for c := range canvas[r] {
			canvas[r][c] = ' '
		}
	}

	// Draw concentric rings at 25%, 50%, 75%, 100% of maxKm so the
	// scale is always readable regardless of maxKm. Compass ticks
	// at the four cardinal headings.
	ringFracs := []float64{0.25, 0.5, 0.75, 1.0}
	for _, frac := range ringFracs {
		drawRing(canvas, colors, cx, cy, float64(cols)/2*frac, float64(rows)/2*frac, mhDrained)
	}
	// N/E/S/W markers just inside the outer ring.
	canvas[1][cx] = 'N'
	colors[1][cx] = mhLavender
	canvas[rows-2][cx] = 'S'
	colors[rows-2][cx] = mhLavender
	canvas[cy][cols-2] = 'E'
	colors[cy][cols-2] = mhLavender
	canvas[cy][1] = 'W'
	colors[cy][1] = mhLavender

	// Plot peers. Higher-priority glyphs overwrite lower-priority
	// ones — direct-RF wins over multi-hop on collision. Colors
	// are meshGreen (direct) / mhCyan (multi-hop); no per-peer
	// hues because on a busy scope adjacent peers in the same
	// cluster kept overwriting each other's unique colors with
	// the generic green, defeating the identification purpose.
	for _, p := range plots {
		rad := p.bearing * math.Pi / 180
		scale := p.distKm / maxKm
		dx := math.Sin(rad) * scale * float64(cols) / 2
		dy := -math.Cos(rad) * scale * float64(rows) / 2
		x := cx + int(math.Round(dx))
		y := cy + int(math.Round(dy))
		if x < 0 || x >= cols || y < 0 || y >= rows {
			continue
		}
		glyph := '·'
		color := mhCyan
		if p.directRF {
			glyph = '●'
			color = meshGreen
		}
		existing := canvas[y][x]
		if existing == '●' && glyph == '·' {
			continue
		}
		canvas[y][x] = glyph
		colors[y][x] = color
	}
	// Self at centre, wins over everything. Magenta matches the
	// "me" color /nodes + /nearby use, so your own radio reads
	// the same across all three peer surfaces.
	canvas[cy][cx] = '@'
	colors[cy][cx] = mhMagenta

	// Radar canvas → multi-line styled string via the radarCanvas
	// Component. The Component owns the per-cell SGR + bold-on-anchor
	// rules and the Box-sized contract; this renderer just hands it
	// the buffer + colors and gets back a properly-padded block.
	canvasW := cols + 2 // 2-cell lead pad
	canvasBlock := radarCanvas{
		Canvas:  canvas,
		Colors:  colors,
		LeadPad: 2,
	}.Render(Box{Width: canvasW, Height: rows})

	// Legend — ring scale + top 5 closest by name with bearing.
	// Re-bind dim here without italic: the function-scope dim is
	// italicized for the no-fix explainer, but the legend reads
	// cleaner upright. (Shadows the outer dim deliberately.)
	dim = lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	keyDim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained)).Italic(true)
	legend := []string{
		"",
		fmt.Sprintf("  %s  outer ring = %.1f km  ·  %s direct-RF  %s multi-hop  %s me",
			keyDim.Render("scale:"),
			maxKm,
			lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen)).Bold(true).Render("●"),
			lipgloss.NewStyle().Foreground(lipgloss.Color(mhCyan)).Render("·"),
			lipgloss.NewStyle().Foreground(lipgloss.Color(mhMagenta)).Bold(true).Render("@"),
		),
	}
	n := len(plots)
	if n > 5 {
		n = 5
	}
	if n > 0 {
		legend = append(legend, dim.Render("  closest:"))
		for i := 0; i < n; i++ {
			p := plots[i]
			legend = append(legend,
				fmt.Sprintf("    %s  %s %3.0f°  %.1f km",
					padOrTruncate(p.node.callsign, 22),
					compassAbbr(p.bearing), p.bearing, p.distKm))
		}
	}

	bodyRows := strings.Split(canvasBlock, "\n")
	lines := make([]string, 0, 2+len(bodyRows)+len(legend))
	lines = append(lines, header, "")
	lines = append(lines, bodyRows...)
	lines = append(lines, legend...)
	return renderBorderedPane(
		strings.Join(lines, "\n"), width, height, paneNodes, m.focused == paneNodes,
	)
}

// drawRing paints an elliptical ring at the given center + radii
// onto the canvas using the '·' glyph in the supplied color.
// Existing glyphs are preserved (ring doesn't overwrite peers or
// the self marker — rings are drawn first, then overlaid).
func drawRing(canvas [][]rune, colors [][]string, cx, cy int, rx, ry float64, color string) {
	rows := len(canvas)
	if rows == 0 {
		return
	}
	cols := len(canvas[0])
	// Parametric circle at 60 sample points — enough to look
	// continuous at typical scope sizes without rendering cost.
	for i := 0; i < 60; i++ {
		theta := float64(i) * 2 * math.Pi / 60
		x := cx + int(math.Round(math.Sin(theta)*rx))
		y := cy + int(math.Round(-math.Cos(theta)*ry))
		if x < 0 || x >= cols || y < 0 || y >= rows {
			continue
		}
		if canvas[y][x] == ' ' {
			canvas[y][x] = '·'
			colors[y][x] = color
		}
	}
}

// niceRadarTick rounds a km distance up to the next "nice" number
// for the outer-ring scale. Keeps the legend readable — a peer
// 23.4km out shows as a 25km ring, not "23.4km". Ticks scale with
// magnitude: 1, 2, 5, 10, 25, 50, 100, 250, 500, 1000 km.
func niceRadarTick(km float64) float64 {
	ticks := []float64{1, 2, 5, 10, 25, 50, 100, 250, 500, 1000}
	for _, t := range ticks {
		if km <= t {
			return t
		}
	}
	return math.Ceil(km/1000) * 1000
}

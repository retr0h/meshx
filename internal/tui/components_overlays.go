// Copyright (c) 2026 John Dewey
//
// Overlay-row Components — leaf renderers for the /channels and
// /nodes (users) overlays. Same pattern as components_chat.go and
// components_notice.go: pre-compute the styled per-cell strings,
// stitch with Row{Cells:[]Cell{}} via the *Line helpers. Each row
// contracts to exactly contentW cells per ansiCells via the Cell
// width budgets, so a wide name, keycap-bodied callsign, or unread
// badge can never push the pane's right ║ frame out of column.
//
// This is the same architectural promise messageRow makes: every
// row in the View tree returns size-correct output regardless of
// what's inside it. Overlay rows previously composed strings with
// inline lipgloss.Render + manual padding; here the cell layer
// owns the geometry and the styling stays in-line per-cell.

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// paneHeaderCell renders the bold uppercase title that every
// overlay pane (CHANNELS, NODES, NEARBY, RADAR, #channel, HELP)
// emits as its first body row. Focused panes render in bright fg;
// unfocused in dim drained so the user sees at a glance which pane
// the cursor is bound to via Ctrl+W navigation.
func paneHeaderCell(text string, focused bool) string {
	s := lipgloss.NewStyle().Bold(true)
	if focused {
		s = s.Foreground(lipgloss.Color(mhFG))
	} else {
		s = s.Foreground(lipgloss.Color(mhDrained))
	}
	return s.Render(strings.ToUpper(text))
}

// paneCountSuffix renders a dim "(...)" suffix that sits next to
// a paneHeaderCell — used for "(304 msgs)" on the messages pane,
// "(#mesh: 4/12 · sort: heard)" on /nodes, etc. Always dim drained
// so it reads as metadata, not chrome.
func paneCountSuffix(text string) string {
	if text == "" {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Render(text)
}

// paneLegendLine renders a dim italic legend strip used by the
// /nodes overlay to explain the sigil glyphs ("legend:  @online
// +pinned ⊘muted ✗failed ·stale"). Italic so it reads as a passive
// reference, dim so it doesn't compete with the data rows.
func paneLegendLine(text string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Italic(true).
		Render(text)
}

// paneEmptyMessage renders an empty-pane placeholder block — the
// 3-6 line dim/italic explainers /nearby and /radar emit when the
// user's radio has no GPS fix or no peers have broadcast positions
// yet. Each line is dim italic so it reads as advisory; an empty
// string in lines emits a blank row.
func paneEmptyMessage(lines ...string) string {
	dim := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Italic(true)
	out := make([]string, len(lines))
	for i, l := range lines {
		if l == "" {
			out[i] = ""
		} else {
			out[i] = dim.Render(l)
		}
	}
	return strings.Join(out, "\n")
}

// tabCompletionFlashCell renders the tab-cycle feedback shown in
// the status flash row when the user pages through completion
// matches. Format: "N/M  match1 · match2 · match3" — the active
// counter + active match render in pink-bold, the denominator and
// inactive matches drop to dim drained, separators are dim lavender.
// Same maxheadroom "loud-number / quiet-chrome" rhythm the byte
// counter uses on the input bar.
func tabCompletionFlashCell(matches []matchItem, active int) string {
	pinkBold := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhPink)).
		Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	counter := pinkBold.Render(fmt.Sprintf("%d", active+1)) +
		dim.Render(fmt.Sprintf("/%d", len(matches)))
	sep := dim.Render("  ·  ")
	parts := make([]string, len(matches))
	for i, mi := range matches {
		if i == active {
			parts[i] = pinkBold.Render(mi.display)
		} else {
			parts[i] = dim.Render(mi.display)
		}
	}
	return counter + "  " + strings.Join(parts, sep)
}

// splashTaglineCell renders the BitchX-style tagline that hangs
// under the splash banner:
// `░▒▓█▓▒░ Meshtastic messenger  ·  by retr0h ░▒▓█▓▒░`.
// Sparks bracket the brand in mesh-green, product name in cyan,
// connector in dim drained, author credit in magenta. The callsign
// argument is unused today (kept on the signature so call sites
// don't churn) — the running-as info already lives in the top
// status bar and doesn't need to duplicate here.
func splashTaglineCell(_ string) string {
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color(mhCyan))
	magenta := lipgloss.NewStyle().Foreground(lipgloss.Color(mhMagenta))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	spark := lipgloss.NewStyle().
		Foreground(lipgloss.Color(meshGreen)).
		Render("░▒▓█▓▒░")
	return spark + " " +
		cyan.Render("Meshtastic") + dim.Render(" messenger  ·  by ") +
		magenta.Render("retr0h") + " " + spark
}

// gutterWidth is the left margin reserved for the selection
// indicator — 2-cell block + 1-cell gap. Matches tlock's double-cell
// "pixel" sizing so the selected row reads as chunky 8-bit, not a
// thin vertical line.
const gutterWidth = 3

// dimRow strips ANSI styling from a pre-rendered row and re-applies
// a single dim color, so non-matching rows fade into the background
// when a node filter is active. The matching rows stay fully colored
// — so the filtered set reads as a "highlighted path" through the
// feed that j/k navigates.
func dimRow(s string) string {
	// Drop existing SGR escapes.
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			end := i + 2
			for end < len(s) && s[end] != 'm' {
				end++
			}
			if end < len(s) {
				end++
			}
			i = end
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Faint(true).
		Render(b.String())
}

// wrapSelection applies the 8-bit block-highlight style to the whole
// row. Three mutually-exclusive states:
//
//   - selected:    thick mesh-green ██ gutter + drained bg tint (cursor)
//   - searchHit:   thin mesh-green │  gutter + dim-green bg tint (match)
//   - neither:     empty gutter so widths stay aligned
//
// Selection wins over a hit when both are true.
//
// Optional rowBg — when non-empty, the neutral (no marker) row also
// fills to full width with that bg color so the row reads as a solid
// rectangle instead of a ragged left-aligned fragment. Used by the
// message list view; left empty for channel / node rows.
func wrapSelection(content string, selected, searchHit bool, width int, rowBg ...string) string {
	if width <= gutterWidth {
		width = gutterWidth + 1
	}
	innerW := width - gutterWidth
	//
	neutralBg := ""
	if len(rowBg) > 0 {
		neutralBg = rowBg[0]
	}
	//
	// No marker — just keep the 3-col left pad for alignment. If a
	// rowBg was provided, force every line to the full inner width
	// with that bg so the tint covers the whole row (no drop-off
	// at the right).
	if !selected && !searchHit {
		pad := strings.Repeat(" ", gutterWidth)
		parts := strings.Split(content, "\n")
		for i, p := range parts {
			cur := ansiCells(p)
			line := p
			if cur > innerW {
				line = padCells(p, innerW)
			} else if cur < innerW && neutralBg != "" {
				tail := strings.Repeat(" ", innerW-cur)
				tail = lipgloss.NewStyle().
					Background(lipgloss.Color(neutralBg)).
					Render(tail)
				line = p + tail
			} else if cur < innerW {
				line = p + strings.Repeat(" ", innerW-cur)
			}
			parts[i] = pad + line
		}
		return strings.Join(parts, "\n")
	}
	//
	// Selection (cursor) style wins over search-hit when both apply.
	var gutter string
	var bg string
	if selected {
		gutter = lipgloss.NewStyle().
			Foreground(lipgloss.Color(meshGreen)).
			Bold(true).
			Render("██") + " "
		bg = selectionRowBg
	} else {
		gutter = lipgloss.NewStyle().
			Foreground(lipgloss.Color(meshGreen)).
			Bold(true).
			Render("│ ") + " "
		bg = searchHitRowBg // dim neon-green background for a subtle row pop
	}
	//
	// Bg tint covers the whole row — no more, no less. We pad to
	// exactly innerW cells via padCells (keycap-aware) BEFORE handing
	// to lipgloss, so the lipgloss style only paints; it does not
	// resize. Going through Width()+MaxWidth() instead lets lipgloss's
	// runewidth-based measurement undercount keycap emoji and overpad
	// the row by 1 cell, which lands the right ║ frame outside the
	// column on rows whose body contains a keycap.
	lineStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bg)).
		Foreground(lipgloss.Color(mhFG))
		//
	parts := strings.Split(content, "\n")
	for i, p := range parts {
		parts[i] = gutter + lineStyle.Render(padCells(p, innerW))
	}
	return strings.Join(parts, "\n")
}

// channelRowLine renders one /channels overlay row at exactly
// contentW cells. The row is a flex-body Component with the channel
// name + optional unread badge stitched as cells; pre-styled so the
// pane background extends through the whole row consistently with
// the message log's zebra fill.
//
//	[name (flex)] [unread badge (fixed)]
//
// `private` channels render in magenta; public in cyan; an unread
// count > 0 shows as a yellow `<count>` badge right of the name.
func channelRowLine(name string, private bool, unread int, contentW int) string {
	nameColor := mhCyan
	if private {
		nameColor = mhMagenta
	}
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(nameColor))
	badge := ""
	badgeW := 0
	if unread > 0 {
		txt := fmt.Sprintf(" %d", unread)
		badge = lipgloss.NewStyle().
			Foreground(lipgloss.Color(mhYellow)).
			Bold(true).
			Render(txt)
		badgeW = ansiCells(txt)
	}
	cells := []Cell{
		{Content: nameStyle.Render(name), Width: -1},
		{Content: badge, Width: badgeW},
	}
	return Row{Cells: cells}.Render(Box{Width: contentW, Height: 1})
}

// nodePresentation is the per-node styling tuple every BitchX-style
// peer surface (/nodes, /nearby, /radar) renders from. Centralizing
// the switch in one place means a node tagged "online" gets the
// same green `@` sigil + neutral name on every overlay; a "muted"
// peer reads as lavender `⊘` everywhere; etc.
//
// Priority: state default → fav promotes to yellow `+` → self
// promotes to magenta `@`. Selection bumps name to bold + the
// nodes-pane accent (magenta).
type nodePresentation struct {
	Sigil        string
	SigilColor   string
	NameColor    string
	BracketColor string
	Bold         bool
}

// nodePresentationFor computes the styling for one node tile from
// the node + flags. Lives here (not in ui.go) so every pane that
// renders a peer cell stays in lockstep — same sigil for "online"
// everywhere, no drift across overlays.
func nodePresentationFor(n nodeItem, isSelf, isSelected bool) nodePresentation {
	state := n.CurrentState()
	p := nodePresentation{
		Sigil:        " ",
		SigilColor:   mhDrained,
		NameColor:    mhFG,
		BracketColor: mhDrained,
	}
	switch state {
	case stateOnline:
		p.Sigil = "@"
		p.SigilColor = mhGreen
	case stateMuted:
		p.Sigil = "⊘"
		p.SigilColor = mhLavender
		p.NameColor = mhLavender
	case stateFailed:
		p.Sigil = "✗"
		p.SigilColor = mhPink
		p.NameColor = mhPink
	case stateOffline:
		p.Sigil = "·"
		p.SigilColor = mhDrained
		p.NameColor = mhDrained
	}
	if n.Fav {
		p.Sigil = "+"
		p.SigilColor = mhYellow
		p.NameColor = mhYellow
	}
	if isSelf {
		p.Sigil = "@"
		p.SigilColor = mhMagenta
	}
	if isSelected {
		p.NameColor = mhMagenta
		p.BracketColor = mhMagenta
		p.Bold = true
	}
	return p
}

// peerRowLine renders one /nearby row from the node + flags. The
// Component owns the styling switch (via nodePresentationFor) so
// callers pass raw flags instead of pre-styled strings — React
// "props in, styled output out".
//
//	"  " (2) + sigil (1) + " " (1) + name (22) + "  " (2) +
//	bar (barW) + "  " (2) + dist (10) + "  " (2) + "·" (1) +
//	"  " (2) + bearing (flex)
func peerRowLine(
	n nodeItem,
	isSelf, isSelected bool,
	rowBg string,
	bar string,
	barW int,
	dist string,
	bearing string,
	contentW int,
) string {
	pres := nodePresentationFor(n, isSelf, isSelected)
	bg := lipgloss.NewStyle().Background(lipgloss.Color(rowBg))
	state := n.CurrentState()
	sigil := lipgloss.NewStyle().
		Foreground(lipgloss.Color(pres.SigilColor)).
		Background(lipgloss.Color(rowBg)).
		Bold(state == stateOnline || n.Fav || isSelf).
		Render(pres.Sigil)
	name := lipgloss.NewStyle().
		Foreground(lipgloss.Color(pres.NameColor)).
		Background(lipgloss.Color(rowBg)).
		Render(padOrTruncate(n.Callsign, 22))
	dim := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Background(lipgloss.Color(rowBg))
	cells := []Cell{
		{Content: bg.Render("  "), Width: 2},
		{Content: sigil, Width: 1},
		{Content: bg.Render(" "), Width: 1},
		{Content: name, Width: 22},
		{Content: bg.Render("  "), Width: 2},
		{Content: bar, Width: barW},
		{Content: bg.Render("  "), Width: 2},
		{Content: dim.Render(dist), Width: 10},
		{Content: bg.Render("  "), Width: 2},
		{Content: dim.Render("·"), Width: 1},
		{Content: bg.Render("  "), Width: 2},
		{Content: dim.Render(bearing), Width: -1, PadStyle: bg},
	}
	return Row{Cells: cells, FillStyle: bg}.Render(Box{Width: contentW, Height: 1})
}

// searchHitRowBg is the deep-green tint applied to rows whose
// content matches the active /search filter — same hex used by
// /nodes (renderUserCell), /nearby, and the messages pane so a hit
// reads with one consistent visual signal across every list. Picked
// to be distinct from selectionRowBg (#2a4a5a) so a row that is BOTH
// selected AND a hit still picks one obvious state — selection wins.
const searchHitRowBg = "#0e2618"

// distanceBarCell renders the per-row distance bar used by /nearby:
// `filled` filled cells of `▓` in mesh-green plus `barMax-filled`
// empty cells of `░` in dim drained, all on rowBg so the bar stays
// painted through the zebra stripe. Returns a single styled string
// of exactly barMax cells per ansiCells.
//
//	▓▓▓▓▓▓▓▓▓░░░░░░░░░░░░░░░       (filled=9, barMax=24)
//
// Bar colors are fixed (mesh-green / drained) regardless of peer
// state — the bar is a distance ruler, not a state indicator; node
// state colors live in nodePresentationFor.
func distanceBarCell(filled, barMax int, rowBg string) string {
	bgCol := lipgloss.Color(rowBg)
	filledStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(meshGreen)).
		Background(bgCol)
	emptyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Background(bgCol)
	if filled < 0 {
		filled = 0
	}
	if filled > barMax {
		filled = barMax
	}
	return filledStyle.Render(strings.Repeat("▓", filled)) +
		emptyStyle.Render(strings.Repeat("░", barMax-filled))
}

// distanceBarUnknownCell renders the bar for the "(you)" / no-distance
// case — a flat row of `·` in dim drained so the row reads as "no
// distance to plot" rather than "zero distance" (which would imply
// you're directly on top of the antenna).
func distanceBarUnknownCell(barMax int, rowBg string) string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Background(lipgloss.Color(rowBg))
	return style.Render(strings.Repeat("·", barMax))
}

// earlierCountLine renders the "… N earlier" scrollback indicator
// shown above the messages pane when the user has scrolled back past
// the natural tail. Dim drained italic so it reads as advisory
// chrome, not data — same visual rhythm as paneEmptyMessage and the
// help overlay's scroll indicator.
func earlierCountLine(n int) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Italic(true).
		Render(fmt.Sprintf("   … %d earlier", n))
}

// helpSectionLine renders a section heading inside the /help
// overlay (e.g. "MODES", "GLOBAL", "WINDOW NAV") in cyan bold +
// underline, matching the irssi help-section convention.
func helpSectionLine(text string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhCyan)).
		Bold(true).
		Underline(true).
		Render(text)
}

// helpScrollIndicator renders the bottom-of-help scroll position
// hint: "line N/M   j/k scroll · d/u page · g/G top/bottom · q/Esc/?
// close" — or, when content fits without scrolling, a simpler
// "q / Esc / ? to close". Always dim drained so it reads as
// passive chrome, not active.
func helpScrollIndicator(scroll, total, visible int) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	if total <= visible {
		return dim.Render("q / Esc / ? to close")
	}
	return dim.Render(fmt.Sprintf(
		"line %d/%d   j/k scroll · d/u page · g/G top/bottom · q/Esc/? close",
		scroll+1, total,
	))
}

// helpKVLine renders one row of the /help overlay: a left-margin
// pad, the key (e.g. "Ctrl+W") in yellow, a column gutter, and the
// description text in default fg. The key column is fixed-width
// (keyW cells) so every kv row's description aligns at the same
// column; the description takes the flex slot and truncates with
// an ellipsis if the row is narrower than the description.
//
//	"  " (2) + key (keyW) + "  " (2) + description (flex)
func helpKVLine(key, desc string, keyW, contentW int) string {
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhYellow)).
		Bold(true)
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhFG))
	cells := []Cell{
		{Content: "  ", Width: 2},
		{Content: keyStyle.Render(padOrTruncate(key, keyW)), Width: keyW},
		{Content: "  ", Width: 2},
		{Content: descStyle.Render(desc), Width: -1},
	}
	return Row{Cells: cells}.Render(Box{Width: contentW, Height: 1})
}

// userCellLine renders one [ @callsign ] tile for the /nodes grid
// from the node + flags. Sigil/colors/bold all derive from
// nodePresentationFor so /nodes and /nearby always read the same
// for the same peer state. Display name combines shortname +
// callsign when the peer broadcasts a Meshtastic 4-char badge.
//
//	"[" (1) + " " (1) + sigil (1) + " " (1) + name (flex) + " " (1) + "]" (1)
func userCellLine(n nodeItem, isSelf, isSelected bool, cellW int) string {
	pres := nodePresentationFor(n, isSelf, isSelected)
	bracket := lipgloss.NewStyle().Foreground(lipgloss.Color(pres.BracketColor))
	if pres.Bold {
		bracket = bracket.Bold(true)
	}
	sigilStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(pres.SigilColor)).
		Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(pres.NameColor))
	if pres.Bold {
		nameStyle = nameStyle.Bold(true)
	}
	display := n.Callsign
	if n.ShortName != "" {
		display = n.ShortName + " " + n.Callsign
	}
	cells := []Cell{
		{Content: bracket.Render("["), Width: 1},
		{Content: " ", Width: 1},
		{Content: sigilStyle.Render(pres.Sigil), Width: 1},
		{Content: " ", Width: 1},
		{Content: nameStyle.Render(display), Width: -1},
		{Content: " ", Width: 1},
		{Content: bracket.Render("]"), Width: 1},
	}
	return Row{Cells: cells}.Render(Box{Width: cellW, Height: 1})
}

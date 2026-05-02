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

package meshx

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

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
	state := n.currentState()
	p := nodePresentation{
		Sigil:        " ",
		SigilColor:   mhDrained,
		NameColor:    mhFG,
		BracketColor: mhDrained,
	}
	switch state {
	case "online":
		p.Sigil = "@"
		p.SigilColor = mhGreen
	case "muted":
		p.Sigil = "⊘"
		p.SigilColor = mhLavender
		p.NameColor = mhLavender
	case "failed":
		p.Sigil = "✗"
		p.SigilColor = mhPink
		p.NameColor = mhPink
	case "offline":
		p.Sigil = "·"
		p.SigilColor = mhDrained
		p.NameColor = mhDrained
	}
	if n.fav {
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
	state := n.currentState()
	sigil := lipgloss.NewStyle().
		Foreground(lipgloss.Color(pres.SigilColor)).
		Background(lipgloss.Color(rowBg)).
		Bold(state == "online" || n.fav || isSelf).
		Render(pres.Sigil)
	name := lipgloss.NewStyle().
		Foreground(lipgloss.Color(pres.NameColor)).
		Background(lipgloss.Color(rowBg)).
		Render(padOrTruncate(n.callsign, 22))
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
	display := n.callsign
	if n.shortName != "" {
		display = n.shortName + " " + n.callsign
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

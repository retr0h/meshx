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

// userCellLine renders one [ @callsign ] tile for the /nodes grid
// at exactly cellW cells. Sigil + name color derive from node state
// + fav + self per the BitchX/IRC-users convention; brackets dim by
// default, magenta when selected. The trailing right-bracket is
// allocated as the final fixed-width cell so the tile is always
// exactly cellW cells regardless of name length — Row's flex slot
// handles the truncation/padding when the display name overflows.
//
// Cells:
//
//	"[" (1) + " " (1) + sigil (1) + " " (1) + name (flex) + " " (1) + "]" (1)
func userCellLine(
	sigil string,
	sigilColor string,
	name string,
	bracketColor string,
	nameColor string,
	bold bool,
	cellW int,
) string {
	bracket := lipgloss.NewStyle().Foreground(lipgloss.Color(bracketColor))
	if bold {
		bracket = bracket.Bold(true)
	}
	sigilStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(sigilColor)).
		Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(nameColor))
	if bold {
		nameStyle = nameStyle.Bold(true)
	}
	cells := []Cell{
		{Content: bracket.Render("["), Width: 1},
		{Content: " ", Width: 1},
		{Content: sigilStyle.Render(sigil), Width: 1},
		{Content: " ", Width: 1},
		{Content: nameStyle.Render(name), Width: -1},
		{Content: " ", Width: 1},
		{Content: bracket.Render("]"), Width: 1},
	}
	return Row{Cells: cells}.Render(Box{Width: cellW, Height: 1})
}

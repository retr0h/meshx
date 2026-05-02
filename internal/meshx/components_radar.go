// Copyright (c) 2026 John Dewey
//
// Radar canvas Component — the polar-scope view in /radar. Holds a
// 2D rune buffer + per-cell color buffer and renders it as a
// multi-line styled string that contracts to exactly Box.Width *
// Box.Height cells per ansiCells. This is the same Box contract
// every other Component obeys, so the radar pane composes into the
// View tree like any other surface — no special casing.

package meshx

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// radarCanvas is a 2D character grid Component. The renderer fills
// canvas[y][x] with a rune and colors[y][x] with a hex color (or
// "" for the default fg); Render emits each row as a styled string
// padded to Box.Width via Cell-level layout so cells align with
// the surrounding Bordered frame regardless of pane width.
//
// Bold is applied to the "anchor" glyphs ('@' for self, '●' for
// direct-RF peers) so they pop visually against the rings + ticks.
// Other glyphs render at normal weight in their assigned color.
//
// LeadPad is the leading "  " gutter the legacy renderer prepended
// — kept here as configurable so callers can drop it (e.g., when
// composing into a tighter inner layout).
type radarCanvas struct {
	Canvas  [][]rune
	Colors  [][]string
	LeadPad int
}

// Render emits Box.Height lines of Box.Width cells. The 2D buffer
// is rendered row by row with per-cell SGR styling; rows beyond the
// canvas height are blank (Box.Width spaces). Each row is padded to
// Box.Width via Cell-level layout to honor the Component contract.
func (r radarCanvas) Render(box Box) string {
	if box.Empty() {
		return ""
	}
	rows := len(r.Canvas)
	if rows > box.Height {
		rows = box.Height
	}
	out := make([]string, 0, box.Height)
	for y := 0; y < rows; y++ {
		var b strings.Builder
		if r.LeadPad > 0 {
			b.WriteString(strings.Repeat(" ", r.LeadPad))
		}
		row := r.Canvas[y]
		colors := r.Colors[y]
		for x := 0; x < len(row); x++ {
			ch := row[x]
			color := ""
			if x < len(colors) {
				color = colors[x]
			}
			if color == "" {
				b.WriteRune(ch)
				continue
			}
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
			if ch == '@' || ch == '●' {
				style = style.Bold(true)
			}
			b.WriteString(style.Render(string(ch)))
		}
		out = append(out, padCells(b.String(), box.Width))
	}
	for y := rows; y < box.Height; y++ {
		out = append(out, strings.Repeat(" ", box.Width))
	}
	return strings.Join(out, "\n")
}

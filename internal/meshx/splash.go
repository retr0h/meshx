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

import (
	"math/rand"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// BitchX pioneered the rotating-ANSI-logo splash — running the client
// gave you a different graffiti art banner each launch. We pay homage:
// meshx ships several block-art MESHX variants and picks one randomly
// on startup. Each plays in the maxheadroom palette (cyan / mesh-green
// / magenta / amber / pink) so launches feel alive and different.
//
// The art itself is composed from plain-text block characters — no
// embedded ANSI — and tinted at render time via lipgloss so the
// palette stays unified with the rest of the UI.

// splashVariant is one logo design — a set of raw text rows + a color
// chooser function that says which color each row should render in.
type splashVariant struct {
	name  string
	rows  []string
	color func(rowIdx int) string // returns a hex color for the given row
}

// allSplashVariants — hand-drawn block-art MESHX logos. More can be
// added freely; the launch-time pick is uniform-random across all.
var allSplashVariants = []splashVariant{
	{
		name: "shadow-bold",
		rows: []string{
			" ███╗   ███╗███████╗███████╗██╗  ██╗██╗  ██╗",
			" ████╗ ████║██╔════╝██╔════╝██║  ██║╚██╗██╔╝",
			" ██╔████╔██║█████╗  ███████╗███████║ ╚███╔╝ ",
			" ██║╚██╔╝██║██╔══╝  ╚════██║██╔══██║ ██╔██╗ ",
			" ██║ ╚═╝ ██║███████╗███████║██║  ██║██╔╝ ██╗",
			" ╚═╝     ╚═╝╚══════╝╚══════╝╚═╝  ╚═╝╚═╝  ╚═╝",
		},
		color: func(i int) string {
			// Cyan → mesh-green → magenta gradient down the rows.
			switch i {
			case 0, 1:
				return mhCyan
			case 2, 3:
				return meshGreen
			default:
				return mhMagenta
			}
		},
	},
	{
		name: "pixel-blocks",
		rows: []string{
			// Row 0's M was 6 cells (██████) while rows 1-4's M was
			// 7 cells (███ ███, ██ █ ██, ██   ██). The one-cell
			// delta rendered as a visible "bite" out of the top-
			// right corner of the M — widened row 0 to a solid
			// 7-cell slab so the letter's top edge aligns with the
			// body.
			" ███████ ██████ ██████ ██   ██ ██   ██",
			" ███ ███ ██     ██     ██   ██  ██ ██ ",
			" ██ █ ██ ██████ ██████ ███████   ███  ",
			" ██   ██ ██         ██ ██   ██  ██ ██ ",
			" ██   ██ ██████ ██████ ██   ██ ██   ██",
		},
		color: func(i int) string {
			// Amber top → pink bottom — hot graffiti fade.
			switch i {
			case 0:
				return mhYellow
			case 1:
				return mhOrange
			case 2:
				return mhPink
			case 3:
				return mhMagenta
			default:
				return "#c678dd"
			}
		},
	},
	{
		name: "heavy-shade",
		rows: []string{
			" ▓▓▓▓▓▓ ▓▓▓▓▓▓ ▓▓▓▓▓▓ ▓▓  ▓▓ ▓▓  ▓▓",
			" ▓▓░░▓▓ ▓▓░░░░ ▓▓░░░░ ▓▓░░▓▓ ░▓▓▓▓░",
			" ▓▓▒▒▓▓ ▓▓▒▒▒▒ ▓▓▓▓▓▓ ▓▓▓▓▓▓  ░▓▓░ ",
			" ▓▓▓▓▓▓ ▓▓░░░░ ░░░░▓▓ ▓▓░░▓▓ ░▓▓▓▓░",
			" ▓▓  ▓▓ ▓▓▓▓▓▓ ▓▓▓▓▓▓ ▓▓  ▓▓ ▓▓  ▓▓",
		},
		color: func(i int) string {
			// Mesh-green drive, cyan echo.
			if i%2 == 0 {
				return meshGreen
			}
			return mhCyan
		},
	},
	{
		name: "slab-classic",
		rows: []string{
			// M E S H X — every letter exactly 8 cells wide with
			// 1-cell gap. Row 0's M used to be 9 wide (two peaks
			// separated by a space) while rows 1-4 were 8 wide,
			// shifting every following letter by one column on the
			// top row. Collapsed the inner gap on row 0 so all five
			// rows align to the same column grid.
			" ███▄▄███ ████ █████ ██  ██ ██  ██",
			" ████████ █▄▄  █▄▄▄▄ ██  ██ ██▄▄██",
			" ██▀▀▀▀██ █    ▀▀▀▀█ ██████   ██  ",
			" ██    ██ ████ █████ ██  ██ ██▀▀██",
			" ▀▀    ▀▀ ▀▀▀▀ ▀▀▀▀▀ ▀▀  ▀▀ ▀▀  ▀▀",
		},
		color: func(i int) string {
			// Single color rotation — pick one hot color for the whole logo.
			palette := []string{mhCyan, meshGreen, mhMagenta, mhYellow, mhPink}
			return palette[i%len(palette)]
		},
	},
}

// pickSplash selects a random splash variant at launch time. Matches
// the BitchX "different banner every run" feel.
func pickSplash() splashVariant {
	return allSplashVariants[rand.Intn(len(allSplashVariants))]
}

// splashAsNotices builds the BitchX-on-connect greeting as a slice
// of noticeRow values — one group, `-!- ` chrome on every line,
// centered block-art logo with per-row color from the variant's
// gradient, cyan+magenta tagline underneath. Returned as raw rows
// (not appended) so the caller composes them into the log via
// m.noticeCard; that's the same single-entrypoint discipline every
// other `-!-` writer follows and keeps splash out of the "rogue
// m.messages = append" smell.
func splashAsNotices(v splashVariant, callsign string) []noticeRow {
	// Normalize row widths to the variant's widest row so every line
	// centers at the same column. Hand-drawn block-art tends to
	// drift a cell or two row-to-row (slab-classic had row 0 at 35
	// cells and rows 1-4 at 34 — enough to make the whole block
	// look tilted since our centering math uses per-row width).
	// Right-padding with spaces keeps the logo rectangular.
	maxW := 0
	for _, row := range v.rows {
		if w := lipgloss.Width(row); w > maxW {
			maxW = w
		}
	}
	normalizedRows := make([]string, len(v.rows))
	for i, row := range v.rows {
		w := lipgloss.Width(row)
		if w < maxW {
			normalizedRows[i] = row + strings.Repeat(" ", maxW-w)
		} else {
			normalizedRows[i] = row
		}
	}

	out := make([]noticeRow, 0, len(v.rows)+4)

	// Leading blank padding row — breathing room above the logo.
	out = append(out, noticeRow{text: "", style: noticeStyle{}})

	// Block-art rows: per-row variant color + centered so the logo
	// floats in the pane middle while the `-!-` prefix stays flush.
	for i, row := range normalizedRows {
		out = append(out, noticeRow{
			text: row,
			style: noticeStyle{
				fg:     v.color(i),
				bold:   true,
				center: true,
			},
		})
	}

	// Blank separator between logo and tagline.
	out = append(out, noticeRow{text: "", style: noticeStyle{}})

	// Tagline — composed from the splashTaglineCell() Component
	// which owns the gradient + per-token coloring (mesh-green
	// spark, cyan brand, dim punctuation, magenta handle).
	out = append(out, noticeRow{
		text:  splashTaglineCell(callsign),
		style: noticeStyle{center: true},
	})

	// Trailing blank padding row.
	out = append(out, noticeRow{text: "", style: noticeStyle{}})

	return out
}

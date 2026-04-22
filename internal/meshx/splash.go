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
			" ██████  ██████ ██████ ██   ██ ██   ██",
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
			" ███▄ ▄███ ████ █████ ██  ██ ██  ██",
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

// renderSplash composes the splash screen: colored logo centered in
// the viewport, tagline + credits underneath, "press any key" prompt.
func renderSplash(width, height int, v splashVariant) string {
	logoLines := make([]string, len(v.rows))
	for i, row := range v.rows {
		logoLines[i] = lipgloss.NewStyle().
			Foreground(lipgloss.Color(v.color(i))).
			Bold(true).
			Render(row)
	}
	logo := strings.Join(logoLines, "\n")

	// Decorative frame around the logo.
	spark := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen)).
		Render("░▒▓█▓▒░")
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	mesh := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen)).Bold(true)
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color(mhCyan))
	magenta := lipgloss.NewStyle().Foreground(lipgloss.Color(mhMagenta))

	tagline := cyan.Render("Meshtastic") + dim.Render(" messenger  ·  ") +
		magenta.Render("inspired by BitchX + irssi + mutt")

	credit := dim.Render("// by retr0h  ·  ") +
		mesh.Render(`//\`) +
		dim.Render("  ·  maxheadroom palette  ·  variant: ") +
		mesh.Render(v.name)

	prompt := dim.Render("press any key to continue  (auto-dismiss in 3s)")

	body := lipgloss.JoinVertical(
		lipgloss.Center,
		spark,
		"",
		logo,
		"",
		spark,
		"",
		tagline,
		credit,
		"",
		"",
		prompt,
	)

	return lipgloss.Place(
		width, height,
		lipgloss.Center, lipgloss.Center,
		body,
	)
}

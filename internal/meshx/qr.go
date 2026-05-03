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

// qr.go — ASCII renderer for QR codes. Used by /channel share to
// emit a phone-scannable share URL into the message log without any
// network or image dependency, and by /qrtest as a diagnostic so the
// renderer can be iterated on without minting real channels.
//
// We use the half-block trick — `▀` (U+2580 UPPER HALF BLOCK) lets
// each terminal cell carry TWO QR rows by setting fg = top-row color
// and bg = bottom-row color. The result has roughly 1:1 module aspect
// ratio in a typical terminal where a cell is taller than wide,
// which is what most phone QR scanners expect. Without this trick,
// each module would be one full cell and the QR would render as a
// tall-skinny rectangle that scanners often refuse.

import (
	"errors"
	"strings"

	"github.com/skip2/go-qrcode"
)

// qrQuietZone is the mandatory white border around a QR code per the
// ISO spec — 4 modules on every side. Scanners use the quiet zone to
// detect the code's edges. Skipping it (or going below 4) is the
// single most common reason a "perfect-looking" QR fails to scan.
const qrQuietZone = 4

// renderQRASCII encodes `data` as a QR code and returns it as a
// multi-line string suitable for systemBlock display. Uses the
// half-block trick (`▀` per cell carries two QR rows) so the rendered
// code stays roughly square in a terminal with cell aspect > 1.
//
// Recovery level Medium is the sweet spot for screen capture / phone
// camera reads — High wastes module budget on error correction we
// don't need (the QR isn't going to get scratched), Low can fail
// when the recipient's camera is at an angle or the screen has
// glare.
//
// Returns an error if the data exceeds Version 40's capacity (~2.9KB
// for binary at level M). A typical channel-share URL is ~80 bytes,
// well under.
func renderQRASCII(data string) (string, error) {
	if data == "" {
		return "", errors.New("empty qr payload")
	}
	q, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return "", err
	}
	// Disable the library's default border so we apply our own quiet
	// zone with the same half-block math as the body. The library's
	// border uses full cells, which would make the rendered output
	// asymmetric: 4-cell-tall border at top/bottom but 4-cell-wide on
	// the sides — the half-block compresses height 2x.
	q.DisableBorder = true
	grid := q.Bitmap()
	// q.Bitmap() returns the QR with NO quiet zone (because we
	// disabled the border). Pad it ourselves so top/bottom each get 4
	// false rows and left/right each get 4 false cols — same 4 modules
	// the spec calls for, and after the half-block compression the
	// vertical quiet zone reads as 2 cell-rows on each end (4 modules
	// × half-block compression = 2 cells), which is enough for the
	// scanner's edge detection while keeping the on-screen footprint
	// reasonable.
	padded := padQRGrid(grid, qrQuietZone)
	return halfBlockEncode(padded), nil
}

// padQRGrid returns a copy of `grid` with `pad` modules of false
// (white) on every side. Separated out so renderQRASCII reads top-to-
// bottom and so we can unit-test the padding math without invoking
// the QR encoder.
func padQRGrid(grid [][]bool, pad int) [][]bool {
	if len(grid) == 0 {
		return grid
	}
	w := len(grid[0])
	newW := w + 2*pad
	out := make([][]bool, 0, len(grid)+2*pad)
	// Top quiet zone — `pad` rows of all-false.
	for range pad {
		out = append(out, make([]bool, newW))
	}
	// Body rows with `pad` false cells on each side.
	for _, row := range grid {
		newRow := make([]bool, newW)
		copy(newRow[pad:], row)
		out = append(out, newRow)
	}
	// Bottom quiet zone.
	for range pad {
		out = append(out, make([]bool, newW))
	}
	return out
}

// halfBlockEncode collapses two QR rows into one terminal cell row
// using `▀` (U+2580 UPPER HALF BLOCK). For each cell:
//
//	top=on,  bot=on  → "█" (full block — both halves dark)
//	top=on,  bot=off → "▀" (upper half only)
//	top=off, bot=on  → "▄" (lower half only)
//	top=off, bot=off → " " (space — both halves clear)
//
// This avoids ANSI color escapes entirely — which means the QR
// survives copy/paste, screen capture, terminal recording (asciinema
// / screencast), AND any color-flipping (light vs dark terminal,
// inverted color scheme). Phone scanners care about contrast, not
// hue; black-on-default-background is the most reliable input.
//
// If the grid has an odd number of rows, the trailing row is rendered
// as upper-half only (bot=off) — equivalent to padding with a quiet
// row, which doesn't hurt scanability.
func halfBlockEncode(grid [][]bool) string {
	if len(grid) == 0 {
		return ""
	}
	var sb strings.Builder
	for y := 0; y < len(grid); y += 2 {
		topRow := grid[y]
		var botRow []bool
		if y+1 < len(grid) {
			botRow = grid[y+1]
		}
		for x := 0; x < len(topRow); x++ {
			top := topRow[x]
			bot := false
			if botRow != nil && x < len(botRow) {
				bot = botRow[x]
			}
			switch {
			case top && bot:
				sb.WriteString("█")
			case top:
				sb.WriteString("▀")
			case bot:
				sb.WriteString("▄")
			default:
				sb.WriteByte(' ')
			}
		}
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

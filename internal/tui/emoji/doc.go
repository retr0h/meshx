// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

// Package emoji holds the generated emoji-presentation lookup table
// the TUI's cell-width measurement consults to promote bare-emoji
// clusters whose East Asian Width is "Neutral" (and which the stock
// Go width libraries therefore measure as 1 cell) — see
// internal/tui/components_box.go::ansiCells.
//
// emoji-data.txt is checked in alongside the generator so refreshes
// are deterministic and offline-buildable. Mirrors the
// internal/sdk/gen/ layout (vendored source data + generator + .gen.go
// output) but renamed to reflect what the generated code IS, not how
// it was made — call sites read as emoji.IsEmojiPresentation(r).
//
// To refresh after a Unicode release:
//
//	curl -fsSL https://www.unicode.org/Public/<ver>/ucd/emoji/emoji-data.txt \
//	    -o internal/tui/emoji/emoji-data.txt
//	just go::generate
//	go test ./...
//	git commit -am "chore(tui): refresh emoji widths to Unicode <ver>"
package emoji

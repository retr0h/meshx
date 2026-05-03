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

package tui

// maxheadroom palette (80s neon) — matches ~/git/dotfiles nvim theme
// and the sibling retr0h projects (grind, tlock). Reused across every
// surface in meshx so the UI reads as one identity.
const (
	mhOrange   = "#ffb86c"
	mhCyan     = "#00d4ff"
	mhMagenta  = "#c678dd"
	mhGreen    = "#50fa7b"
	mhYellow   = "#e5c07b"
	mhPink     = "#ff6ec7" // hot pink — active channel tab, error pulses
	mhPinkDim  = "#7a3a60" // pink fade-off, for pulsed effects
	mhLavender = "#6272a4" // inactive tabs, muted / subdued states
	mhFG       = "#c0caf5" // default text foreground
	mhDrained  = "#3b4261" // dim gray — labels, separators

	// meshGreen — the Meshtastic brand mint. Used for the focused-pane
	// border, the //\ wordmark, input bar prompt, and any accent that
	// wants a "brand" identity distinct from the cooler mhGreen.
	meshGreen = "#67ea94"
)

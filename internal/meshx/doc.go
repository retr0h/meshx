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

// Package meshx implements the core of the meshx terminal Meshtastic
// messenger: the Bubble Tea model, the irssi-style input + scrollback
// nav + overlay modes, the ham-radio /commands, the BitchX-style
// rotating splash, the maxheadroom palette, tab completion, and the
// glitched-out rendering primitives.
//
// Project-level docs:
//   - ../../docs/keymap.md        — every keybinding and /command
//   - ../../docs/development.md   — setup, testing, conventions
//   - ../../docs/contributing.md  — PR workflow
//
// Inspired by irssi, BitchX, and mutt — modal input + scrollback nav
// come from vim/mutt, the slash-command dispatcher and ASCII-graffiti
// splash come from BitchX, the colored-nick plus stable status-bar
// layout comes from irssi.
package meshx

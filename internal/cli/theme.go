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

// Package cli holds CLI-only output helpers — never imported by the
// running TUI under internal/tui/, only by cmd/. Mirrors kvlt's
// theme system (same Theme struct, same role names, same lipgloss
// renderer plumbing) so all retr0h CLIs share one shape; meshx
// ships one theme today, more can be added behind MESHX_THEME later
// without touching callers.
package cli

import (
	"io"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme is a six-role palette covering every place meshx's CLI
// surface emits styled text. Roles are stable across themes so a
// theme swap is a pure recolor — no callers change.
//
//	Mute    labels, secondary metadata
//	Accent  primary highlight — paths, version strings, headlines
//	OK      success events
//	Err     errors at the CLI boundary
//	Info    cool-toned hints and pointers
//	Banner* the two banner lines — Top/Bot let themes decide whether
//	        the brand color sits above or below the implicit midline
//
// Each role is a lipgloss.Style. lipgloss handles NO_COLOR and TTY
// detection via termenv so we don't reinvent it here.
type Theme struct {
	Name      string
	Mute      lipgloss.Style
	Accent    lipgloss.Style
	OK        lipgloss.Style
	Err       lipgloss.Style
	Info      lipgloss.Style
	BannerTop lipgloss.Style
	BannerBot lipgloss.Style
}

// fg is shorthand for `lipgloss.NewStyle().Foreground(...)` so theme
// definitions stay scannable. Hex strings render as 24-bit truecolor
// when the terminal supports it; xterm-256 indexes are also accepted.
func fg(c string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c))
}

// faint uses lipgloss's dim attribute rather than a specific color
// so the muted role adapts to the user's terminal background and
// matches install.sh's `\033[0;2m` output exactly.
var faint = lipgloss.NewStyle().Faint(true)

// ThemeMaxheadroom is meshx's default — pulled from the maxheadroom
// palette also used by grind and tlock so the family reads as one.
// mhGreen (#50fa7b) is the primary accent — phosphor / CRT / mesh-
// radio vibe; supporting roles are drawn from the same palette
// (mhCyan for OK/info — multi-hop direct, mhPink for Err — alert).
// Truecolor (24-bit) so the install banner, the running TUI, and
// `--help` all paint with the exact same hue.
var ThemeMaxheadroom = Theme{
	Name:      "maxheadroom",
	Mute:      faint,
	Accent:    fg("#50fa7b"), // mhGreen
	OK:        fg("#50fa7b"), // mhGreen
	Err:       fg("#ff6ec7"), // mhPink (the in-app alert color)
	Info:      fg("#00d4ff"), // mhCyan
	BannerTop: faint,
	BannerBot: fg("#50fa7b"),
}

var themes = []*Theme{
	&ThemeMaxheadroom,
}

var active = &ThemeMaxheadroom

func init() {
	if t, ok := lookupTheme(os.Getenv("MESHX_THEME")); ok {
		active = t
	}
}

// SetTheme replaces the active theme. Returns false if name is
// unknown — callers can fall back to the default and warn.
func SetTheme(name string) bool {
	t, ok := lookupTheme(name)
	if !ok {
		return false
	}
	active = t
	return true
}

// ActiveTheme returns the currently-active theme.
func ActiveTheme() *Theme { return active }

// ThemeNames returns every registered theme name. The first element
// is the default; subsequent are alphabetical so listings are
// deterministic.
func ThemeNames() []string {
	out := make([]string, 0, len(themes))
	for _, t := range themes {
		out = append(out, t.Name)
	}
	if len(out) <= 1 {
		return out
	}
	first := out[0]
	rest := append([]string(nil), out[1:]...)
	sort.Strings(rest)
	return append([]string{first}, rest...)
}

func lookupTheme(name string) (*Theme, bool) {
	if name == "" {
		return nil, false
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, t := range themes {
		if strings.EqualFold(t.Name, want) {
			return t, true
		}
	}
	return nil, false
}

// rendererFor returns a lipgloss renderer bound to w so callers
// writing to non-stdout sinks (os.Stderr, a buffer in tests) get
// accurate NO_COLOR / TTY behavior.
func rendererFor(w io.Writer) *lipgloss.Renderer {
	if f, ok := w.(*os.File); ok {
		return lipgloss.NewRenderer(f)
	}
	return lipgloss.DefaultRenderer()
}

func render(w io.Writer, st lipgloss.Style, s string) string {
	return st.Renderer(rendererFor(w)).Render(s)
}

// Mute returns s rendered as secondary text per the active theme.
func Mute(w io.Writer, s string) string { return render(w, active.Mute, s) }

// Accent returns s rendered as the brand accent color.
func Accent(w io.Writer, s string) string { return render(w, active.Accent, s) }

// OK returns s in the success color.
func OK(w io.Writer, s string) string { return render(w, active.OK, s) }

// Err returns s in the error color.
func Err(w io.Writer, s string) string { return render(w, active.Err, s) }

// Info returns s in the cool-toned info/hint color.
func Info(w io.Writer, s string) string { return render(w, active.Info, s) }

// Banner returns the MESHX block-letter logo, themed via the active
// theme's BannerTop/BannerBot colors. Line-level coloring matches
// the install summary so curl|bash and `meshx --help` look the same.
// Two rows — Pagga-style — to stay consistent with kvlt/grind/tlock.
// (The "E" reads as a slightly squared "C" at 2 rows; a known Pagga
// limitation we accept for family consistency.)
func Banner(w io.Writer) string {
	const top = "█▀▄▀█ █▀▀ █▀ █░█ ▀▄▀"
	const bot = "█░▀░█ █▄▄ ▄█ █▀█ ▄▀▄"
	return render(w, active.BannerTop, top) + "\n" +
		render(w, active.BannerBot, bot) + "\n"
}

// Success renders a leading "✓" mark in the OK color followed by
// msg. Falls back to "[ok]" when lipgloss decides not to color
// (NO_COLOR / non-TTY).
func Success(w io.Writer, msg string) string {
	mark := OK(w, "✓")
	if !strings.ContainsRune(mark, 0x1b) {
		return "[ok] " + msg
	}
	return mark + " " + msg
}

// Failure mirrors Success for error one-liners.
func Failure(w io.Writer, msg string) string {
	mark := Err(w, "✗")
	if !strings.ContainsRune(mark, 0x1b) {
		return "[err] " + msg
	}
	return mark + " " + msg
}

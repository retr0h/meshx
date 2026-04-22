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
	"sort"
	"strings"
)

// tabState captures the in-progress cycling completion. First Tab
// computes matches + inserts match 0; subsequent Tabs cycle through
// the same set. Any non-Tab key clears the state.
type tabState struct {
	matches []string // candidate completions, sorted, all starting with stem
	cursor  int      // current index into matches
	stem    string   // original word fragment before first Tab
	start   int      // byte offset in input where the word begins
	end     int      // byte offset in input where the replacement ends now
}

// slashCommands is the canonical completion universe for /commands.
// Keep alphabetical — users see this in the "N matches: …" flash
// so order matters for predictability.
var slashCommands = []string{
	"73", "88", "channel", "channels", "clear", "config", "cq", "cqr",
	"exit", "grid", "help", "join", "k", "mesh", "msg", "names", "nodes",
	"part", "ping", "q", "qrm", "qrz", "qsb", "qsl", "qth", "quit", "r",
	"reply", "rs", "search", "sk", "sked", "tr", "trace", "traceroute",
	"users", "w", "whois", "wx",
}

// computeCompletions finds the word under/before the cursor, decides
// which completion universe applies based on its prefix, and returns
// the matches + the byte range in `text` that should be replaced.
func (m model) computeCompletions(text string, cursor int) (matches []string, start, end int) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}
	start, end = wordBounds(text, cursor)
	word := text[start:end]

	var universe []string
	switch {
	case strings.HasPrefix(word, "/"):
		// Command completion. Strip the `/` for matching, add it back
		// per candidate when returning.
		stripped := strings.ToLower(strings.TrimPrefix(word, "/"))
		for _, c := range slashCommands {
			if strings.HasPrefix(c, stripped) {
				matches = append(matches, "/"+c)
			}
		}
		return matches, start, end
	case strings.HasPrefix(word, "#") || strings.HasPrefix(word, "*"):
		// Channel completion — match both public (#foo) and private
		// (*secret*) channels by any prefix.
		for _, c := range m.channels {
			if strings.HasPrefix(strings.ToLower(c.name), strings.ToLower(word)) {
				matches = append(matches, c.name)
			}
		}
		return matches, start, end
	default:
		// Nick/callsign completion. Pull candidates from nodes.
		for _, n := range m.nodes {
			if strings.HasPrefix(strings.ToLower(n.callsign), strings.ToLower(word)) {
				universe = append(universe, n.callsign)
			}
		}
		// Also allow completing "me" so /reply me works.
		if strings.HasPrefix("me", strings.ToLower(word)) {
			universe = append(universe, "me")
		}
	}

	sort.Strings(universe)
	return universe, start, end
}

// wordBounds returns the byte offsets of the word containing (or
// ending at) the cursor. A "word" runs from the previous whitespace
// (exclusive) to the next whitespace (exclusive), so `/cq ` + cursor
// right after the space yields an empty word at the cursor position.
func wordBounds(s string, cur int) (start, end int) {
	start = cur
	for start > 0 && !isTokenSep(s[start-1]) {
		start--
	}
	end = cur
	for end < len(s) && !isTokenSep(s[end]) {
		end++
	}
	return start, end
}

func isTokenSep(b byte) bool {
	return b == ' ' || b == '\t'
}

// applyCompletion returns the new input text + cursor position after
// inserting `match` into `text` between `start` and `end`. When
// replacing at start-of-input AND the match is a callsign (not a
// /command, not a #channel), append `: ` instead of ` ` — classic
// irssi behavior for addressing someone mid-conversation.
func applyCompletion(text string, start, end int, match string) (out string, newCursor int) {
	suffix := " "
	if start == 0 && !strings.HasPrefix(match, "/") && !strings.HasPrefix(match, "#") &&
		!strings.HasPrefix(match, "*") {
		suffix = ": "
	}
	// If the char after `end` is already a space, don't double-add.
	if end < len(text) && text[end] == ' ' {
		suffix = ""
	}
	out = text[:start] + match + suffix + text[end:]
	newCursor = start + len(match) + len(suffix)
	return out, newCursor
}

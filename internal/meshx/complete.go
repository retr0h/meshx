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
	"fmt"
	"sort"
	"strings"
)

// looksLikeHexStem reports whether the user's partial word reads as
// the beginning of a Meshtastic node-num hex id — "0x" prefix, or
// bare hex digits of length >= 2 (avoids tripping on trivial words
// like "a" or "ef" that could mean anything). Used by the nick
// completer to decide when to walk nodesByNum for id matches.
func looksLikeHexStem(word string) bool {
	lw := strings.ToLower(word)
	if strings.HasPrefix(lw, "0x") {
		return true
	}
	if len(lw) < 2 {
		return false
	}
	for _, r := range lw {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

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
	"exit", "grid", "help", "join", "k", "mesh", "msg", "nodes",
	"part", "ping", "q", "qrm", "qrz", "qsb", "qsl", "qth", "quit", "r",
	"reply", "rs", "search", "sk", "sked", "tr", "trace", "traceroute",
	"w", "whois", "wx",
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
		// Nick/callsign completion. Pull candidates from nodes using
		// a two-pass match so typing a partial from anywhere in a
		// callsign lands a completion — peers like "node 0xd64b01be"
		// can be reached by typing "0x", "d64", or "Rural". Prefix
		// matches rank ahead of mid-string matches so the irssi
		// "nick-at-start" idiom still wins when it's what you meant.
		lw := strings.ToLower(word)
		var prefixHits, substrHits []string
		for _, n := range m.nodes {
			lc := strings.ToLower(n.callsign)
			switch {
			case strings.HasPrefix(lc, lw):
				prefixHits = append(prefixHits, n.callsign)
			case strings.Contains(lc, lw):
				substrHits = append(substrHits, n.callsign)
			}
		}
		// Hex-id completion — if the stem looks like a hex prefix
		// (e.g. "0x", "0xd6", "d64") walk nodesByNum and offer the
		// real callsign for any node whose num matches. This is how
		// you address the "node 0x…" placeholder peers by id.
		if looksLikeHexStem(word) {
			needle := strings.TrimPrefix(strings.ToLower(word), "0x")
			for num, idx := range m.nodesByNum {
				if idx >= len(m.nodes) {
					continue
				}
				hex := fmt.Sprintf("%x", num)
				if strings.Contains(hex, needle) {
					substrHits = append(substrHits, m.nodes[idx].callsign)
				}
			}
		}
		// Sort each tier separately, then concat prefix-first so an
		// exact/prefix match always ranks ahead of a mid-string match
		// when the user cycles through Tab hits.
		sort.Strings(prefixHits)
		sort.Strings(substrHits)
		universe = append(universe, prefixHits...)
		universe = append(universe, substrHits...)
		// Also allow completing "me" so /reply me works.
		if strings.HasPrefix("me", lw) {
			universe = append(universe, "me")
		}
	}

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

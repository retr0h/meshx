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
	"exit", "grid", "h", "help", "info", "join", "k", "mesh", "msg",
	"nodes", "part", "ping", "q", "qrm", "qrz", "qsb", "qsl", "qth",
	"quit", "r", "reply", "rs", "search", "sk", "sked", "sync",
	"tr", "trace", "traceroute", "w", "whois", "wx",
}

// callsignArgCommands is the set of /verbs whose first argument is a
// callsign (which can legitimately contain spaces — Meshtastic
// longnames like "0aac Base" or "North Redondo Beach Base"). When
// the user is completing inside one of these commands, we treat the
// ENTIRE rest-of-line as the single completion target rather than
// splitting on whitespace — otherwise cycling after "0aac Base "
// would match every node containing an empty string.
var callsignArgCommands = map[string]bool{
	"whois": true, "w": true,
	"cqr":  true,
	"rs":   true,
	"ping": true,
	"tr":   true, "trace": true, "traceroute": true,
	"msg":   true,
	"reply": true, "r": true,
	"qrm":  true,
	"qsb":  true,
	"k":    true,
	"sked": true,
	"73":   true,
	"qsl":  true,
	"sk":   true,
	"env":  true,
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

	// Command-aware arg boundary — if the line looks like
	// `/<callsign-arg-cmd> <anything>` and the cursor is past the
	// first space, treat the whole rest as one completion target so
	// multi-word callsigns ("0aac Base") round-trip cleanly.
	if cStart, ok := commandArgStart(text, cursor); ok {
		start = cStart
		end = len(text)
		word := text[start:end]
		universe := m.nickUniverse(word)
		return universe, start, end
	}

	start, end = wordBounds(text, cursor)
	word := text[start:end]

	// Only two completion universes: /command names and callsigns.
	// No channel completion, no "me" completion — any command that
	// takes an argument targets a user, and the /nodes overlay is
	// for browsing the mesh. Keeping the surface tight means Tab
	// never suggests something the user didn't reach for.
	if strings.HasPrefix(word, "/") {
		// Command completion. `/foo<Tab>` with no matches leaves the
		// input alone; a command that takes no argument just doesn't
		// auto-append anything after the verb.
		stripped := strings.ToLower(strings.TrimPrefix(word, "/"))
		for _, c := range slashCommands {
			if strings.HasPrefix(c, stripped) {
				matches = append(matches, "/"+c)
			}
		}
		return matches, start, end
	}
	return m.nickUniverse(word), start, end
}

// nickUniverse returns the ranked list of callsign completions for
// a given stem. Prefix matches sort first, substring matches
// second, with hex-id matches folded into substring.
//
// Empty / whitespace-only stems return no matches — Tab-cycling
// through every node in the mesh isn't useful (that's what the
// /nodes overlay is for). Requiring at least one non-space char
// before offering suggestions matches irssi / BitchX behavior
// too: Tab on empty input is a no-op, not a dump-the-world.
//
// Matching is strictly case-sensitive. Callsigns are normalized
// at ingest (normalizeCallsign collapses whitespace to `_`), so
// the user's stem and the stored callsign already share one
// canonical shape. `o` is `o`; `O` is `O`; no folding.
// The hex-id path still folds case because hex digits are
// conventionally case-insensitive (0xD64B01BE == 0xd64b01be).
func (m model) nickUniverse(word string) []string {
	stem := strings.TrimSpace(word)
	if stem == "" {
		return nil
	}
	var prefixHits, substrHits []string
	for _, n := range m.nodes {
		switch {
		case strings.HasPrefix(n.callsign, stem):
			prefixHits = append(prefixHits, n.callsign)
		case strings.Contains(n.callsign, stem):
			substrHits = append(substrHits, n.callsign)
		}
	}
	// Hex-id completion — if the stem looks like a hex prefix
	// ("0x", "0xd6", "d64") walk nodesByNum and offer the real
	// callsign for any node whose num matches. Addresses the
	// "node 0x…" placeholder peers by id. Hex is inherently
	// case-insensitive so this path keeps the lower-case fold.
	if looksLikeHexStem(stem) {
		needle := strings.TrimPrefix(strings.ToLower(stem), "0x")
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
	sort.Strings(prefixHits)
	sort.Strings(substrHits)
	out := make([]string, 0, len(prefixHits)+len(substrHits)+1)
	out = append(out, prefixHits...)
	out = append(out, substrHits...)
	return out
}

// commandArgStart checks whether the input line reads as
// `/<callsign-arg-cmd> ...` and the cursor is past the first
// space — i.e. we're typing inside the command's argument, not
// on the command name itself. When true, returns the byte offset
// where the arg begins (right after the first space); callers
// should then treat [start, len(text)) as one completion target
// so multi-word callsigns stay coherent across Tab cycles.
func commandArgStart(text string, cursor int) (int, bool) {
	if !strings.HasPrefix(text, "/") {
		return 0, false
	}
	space := strings.IndexByte(text, ' ')
	if space < 0 || cursor <= space {
		return 0, false
	}
	verb := strings.ToLower(text[1:space])
	if !callsignArgCommands[verb] {
		return 0, false
	}
	return space + 1, true
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

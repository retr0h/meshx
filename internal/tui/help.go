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

// helpEntry is one row in the per-command help table. usage is a
// single-line invocation spec (same shape as the flash-bar hints
// emitted when a command is called wrong). summary is one or two
// sentences describing what the command does and when you'd use it.
type helpEntry struct {
	usage   string
	summary string
}

// helpEntries backs `/help <verb>` — irssi / BitchX style per-command
// lookup. Entries are intentionally short; the full scrollable
// reference stays at `/help` alone. Every verb in slashCommands
// should appear here so the tab-completion surface matches the
// help surface.
var helpEntries = map[string]helpEntry{
	// Ham verbs.
	"cq": {
		usage:   "/cq [tail]",
		summary: "broadcast a CQ call with an optional custom tail; asks anyone on the mesh to respond",
	},
	"cqr": {
		usage:   "/cqr <call>",
		summary: "respond to someone's CQ with a real copy report (SNR / RSSI / hops); threads to their CQ packet",
	},
	"rs": {
		usage:   "/rs <call>",
		summary: "send a formatted signal report for <call> from their most-recently-heard packet",
	},
	"73": {
		usage:   "/73 [call]",
		summary: "cordial sign-off; broadcasts a general 73 or directs one at <call> if supplied",
	},
	"88": {
		usage:   "/88",
		summary: "love-and-kisses ham slang — broadcast only",
	},
	"qsl": {
		usage:   "/qsl [call]",
		summary: "acknowledge / confirm receipt; directed at <call> when supplied, otherwise broadcast",
	},
	"qth": {
		usage:   "/qth [text]",
		summary: "broadcast your location; empty sends your Maidenhead grid, otherwise sends the custom text",
	},
	"grid": {
		usage:   "/grid [locator]",
		summary: "just the Maidenhead grid locator — shorter / more data-friendly than /qth",
	},
	"qrz": {
		usage:   "/qrz",
		summary: "\"who is calling me?\" — broadcast a prompt for identification",
	},
	"qrm": {
		usage:   "/qrm <call>",
		summary: "report man-made interference on <call>'s signal",
	},
	"qsb": {
		usage:   "/qsb <call>",
		summary: "report that <call>'s signal is fading in and out",
	},
	"sk": {
		usage:   "/sk [call]",
		summary: "final sign-off — stronger than /73; directed at <call> when supplied",
	},
	"wx": {
		usage:   "/wx [conditions]",
		summary: "weather at your QTH; defaults to a placeholder when no conditions supplied",
	},
	"mesh": {
		usage:   "/mesh",
		summary: "live summary of the mesh you can hear — number of nodes online / muted / stale",
	},
	"k": {
		usage:   "/k <call>",
		summary: "\"over — go ahead\" — ragchew turn-taking; directs at <call>",
	},

	// Messaging.
	"msg": {
		usage:   "/msg <call> <text>",
		summary: "send a direct message to <call> — opens a @peer DM tab; subsequent typing keeps replying to that peer",
	},
	"query": {
		usage:   "/query <call>",
		summary: "open (or focus) a DM tab for <call> without sending — irssi convention",
	},
	"close": {
		usage:   "/close",
		summary: "close the active DM tab and return to the prior channel (alias: /unquery)",
	},
	"reply": {
		usage:   "/reply [call] [text]",
		summary: "reply to <call>; uses the highlighted sender when omitted in nav mode",
	},
	"r": {
		usage:   "/r [call] [text]",
		summary: "alias for /reply",
	},
	"ping": {
		usage:   "/ping <call>",
		summary: "RTT + signal check against <call>; output lands as a systemBlock in the message log",
	},
	"tr": {
		usage:   "/tr <call>",
		summary: "traceroute from us to <call> — shows the mesh hop path; the only way to debug \"why is this peer 4 hops out\"",
	},
	"whois": {
		usage:   "/whois <call>",
		summary: "dump node metadata for <call> — hw, fw, last-heard, signal, grid if published",
	},
	"w": {usage: "/w <call>", summary: "alias for /whois"},
	"pin": {
		usage:   "/pin",
		summary: "toggle pin on the last ephemeral notice — pauses its 60s TTL so it stays in the log; `⌜ … ⌟` corners mark it. Run again to resume the timer. Nav alternative: highlight the row and press `P`.",
	},

	// Overlays & utilities.
	"channels": {
		usage:   "/channels",
		summary: "open the channels overlay (j/k walks, Enter activates)",
	},
	"channel": {
		usage:   "/channel list | add <url> | new <name> | share <name> | del <name>",
		summary: "full channel lifecycle — `list` opens the overlay; `add` imports a meshtastic:// share URL; `new` mints a fresh AES256 channel with a random PSK; `share` renders the channel as an ASCII QR for in-person scanning (PSK never touches the network); `del` disables a slot. PRIMARY can't be deleted — use /config to rename. Aliases: del / delete / rm.",
	},
	"nodes": {
		usage:   "/nodes",
		summary: "open the nodes overlay — BitchX-style bracketed grid of every known peer",
	},
	"nearby": {
		usage:   "/nearby",
		summary: "distance-sorted roster of peers with a GPS fix — closest first, with a bar chart, bearing, and compass abbreviation. Requires your own GPS fix.",
	},
	"radar": {
		usage:   "/radar",
		summary: "polar scope centered on your QTH — peers plotted by bearing and distance; ● direct-RF, · multi-hop, @ self. Concentric rings scale to the farthest peer. Requires your own GPS fix.",
	},
	"join": {
		usage:   "/join <channel>",
		summary: "switch the active channel by name",
	},
	"search": {
		usage:   "/search <pattern>",
		summary: "search the message log — case-insensitive substring match against from + body. Matching rows highlight with a dim-green background; press n to step to the next hit, N for the previous. Esc clears the search. /search alone with no pattern is a usage hint; press / in nav mode for the live-filter prompt instead",
	},
	"config": {
		usage:   "/config",
		summary: "open the interactive radio config panel — j/k walks, Enter edits (toggles bools or opens an inline string editor for longname / shortname), Ctrl+S commits the diff to the radio in one shot, Esc on a dirty panel prompts y/n. Separate from /mute (which silences only the meshX terminal ding)",
	},
	"mute": {
		usage:   "/mute",
		summary: "toggle the meshX terminal ding (BEL on alert-flagged inbound text — messages whose sender embedded a 0x07 BEL; renderer also surfaces 🔔 on those rows). Persists across restarts. The radio's own buzzer is unaffected — for that, use /config → \"radio buzzer\"",
	},
	// /dingtest is intentionally NOT listed here. It's a hidden
	// diagnostic that fires the BEL verification path. Available
	// from the dispatcher; surfacing it in /help or completion
	// would leak debug surface to normal users.
	"me": {
		usage:   "/me <action>",
		summary: "IRC-style action — broadcasts \"* <action>\" on the current channel; receivers see it as your row with the leading \"* \" marker",
	},
	"version": {
		usage:   "/version",
		summary: "show meshX build identity (commit, build date) plus the connected radio's firmware version. Same data `meshx version` prints",
	},
	"ignore": {
		usage:   "/ignore <call>",
		summary: "hide chat messages from <call> in the messages pane (local-only filter, doesn't touch the wire). Use /unignore to drop the filter. Cleared on restart",
	},
	"unignore": {
		usage:   "/unignore [call]",
		summary: "remove <call> from the /ignore list. With no args, lists currently ignored callsigns",
	},
	"reboot": {
		usage:   "/reboot",
		summary: "send AdminMessage_RebootSeconds(5) to the radio — restarts in 5s. Useful when a module-config write needs a reboot or when the radio is wedged. meshx reconnects automatically",
	},
	"who": {
		usage:   "/who",
		summary: "alias for /nodes — IRC convention for \"show me the user list\"",
	},
	"whoami": {
		usage:   "/whoami",
		summary: "alias for /info — IRC convention for \"who am I?\"",
	},
	"list": {
		usage:   "/list",
		summary: "alias for /channels — IRC convention for \"show me the channels\"",
	},
	"lastlog": {
		usage:   "/lastlog [call|text]",
		summary: "with no args, jump to the most recent message (like vim G). With a callsign, jump to the last message FROM that peer; falls back to a body substring match if no sender hits",
	},
	"info": {
		usage:   "/info",
		summary: "dump meshX's current knowledge — self id, peer-count breakdown, and which peers are still unresolved \"node 0x…\" placeholders",
	},
	"nick": {
		usage:   "/nick <longname>",
		summary: "quick-access immediate-write of User.long_name via AdminMessage.SetOwner. No reboot. The canonical edit path for both longname and shortname (with draft + Ctrl+S) is /config — /nick stays as the fast inline rename muscle-memory expects",
	},
	"sync": {
		usage:   "/sync",
		summary: "ask the radio to re-dump its NodeDB via a fresh WantConfigId handshake; use to force-refresh unresolved peers",
	},
	"clear": {
		usage:   "/clear",
		summary: "clear local scrollback only — does not unsend anything, does not wipe the SQLite store",
	},
	"help": {
		usage:   "/help [verb]",
		summary: "open the full scrollable help overlay; pass a verb for a single-command summary",
	},
	"h": {usage: "/h [verb]", summary: "alias for /help"},
	"exit": {
		usage:   "/exit",
		summary: "quit the app cleanly; aliases: /quit, /q",
	},
	"quit": {usage: "/quit", summary: "alias for /exit"},
	"q":    {usage: "/q", summary: "alias for /exit"},
	"part": {
		usage:   "/part",
		summary: "leave the current channel (not yet wired — Meshtastic channels are radio-configured)",
	},
}

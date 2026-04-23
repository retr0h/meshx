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
	"sked": {
		usage:   "/sked <call>",
		summary: "propose a scheduled contact with <call> ~24h out",
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
		summary: "direct text at a node (still a channel broadcast on the wire — Meshtastic DMs are not wired yet)",
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
		summary: "traceroute from us to <call>; aliases: /traceroute, /trace",
	},
	"trace":      {usage: "/trace <call>", summary: "alias for /tr"},
	"traceroute": {usage: "/traceroute <call>", summary: "alias for /tr"},
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
		usage:   "/channel list | /channel add <url>",
		summary: "channel management; `list` opens the overlay, `add` imports a meshtastic:// URL",
	},
	"nodes": {
		usage:   "/nodes",
		summary: "open the nodes overlay — BitchX-style bracketed grid of every known peer",
	},
	"join": {
		usage:   "/join <channel>",
		summary: "switch the active channel by name",
	},
	"search": {
		usage:   "/search <pattern>",
		summary: "live-search the message log; aliases: /find",
	},
	"find": {usage: "/find <pattern>", summary: "alias for /search"},
	"config": {
		usage:   "/config",
		summary: "dump the radio + identity configuration as a systemBlock",
	},
	"info": {
		usage:   "/info",
		summary: "dump meshX's current knowledge — self id, peer-count breakdown, and which peers are still unresolved \"node 0x…\" placeholders",
	},
	"nick": {
		usage:   "/nick <longname>",
		summary: "set the radio's User.long_name (up to 36 bytes) via AdminMessage.SetOwner; no reboot required",
	},
	"callsign": {usage: "/callsign <name>", summary: "ham-idiomatic alias for /nick"},
	"tag": {
		usage:   "/tag <text-or-emoji>",
		summary: "set the radio's User.short_name (up to 4 bytes; usually one emoji) via AdminMessage.SetOwner",
	},
	"emoji": {
		usage:   "/emoji <x>",
		summary: "alias for /tag (most people set shortname to an emoji)",
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

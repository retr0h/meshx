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

// Package meshx rendering surface.
//
// ui.go holds everything View-side: the top-level View() dispatcher,
// every pane renderer (messages / channels / nodes), the status-bar
// family (top status, channel status, input row, top divider), the
// help overlay, plus the small styling primitives (paneStyle,
// paneHeader, nickColor, zebraBg, wrapSelection, padOrTruncate) that
// the renderers share. No state mutation lives here — all mutation
// happens in app.go (model + Update + message handlers), input.go
// (nav / mode transitions), or commands.go (slash dispatch).
package meshx

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m model) View() string {
	if m.w == 0 || m.h == 0 {
		return ""
	}

	status := m.renderStatusBar()
	divider := m.renderTopDivider()
	chanRow := m.renderChannelStatus()
	inputRow := m.renderInputRow()

	// Row budget:
	//   status(1) + top-divider(1) + body(bodyH) + chan-row(1) + input(1) + tail(1)
	// = 5 + bodyH
	bodyH := m.h - 5
	if bodyH < 8 {
		bodyH = 8
	}

	var body string
	switch m.mode {
	case modeHelp:
		body = m.renderHelpView(bodyH)
	default:
		body = m.renderIrssiBody(bodyH)
	}

	return status + "\n" + divider + "\n" + body + "\n" + chanRow + "\n" + inputRow
}

// renderIrssiBody — main log takes the whole width. When an overlay
// is active (channels / nodes), it replaces the log. ESC always
// closes the overlay and returns to the input bar.
func (m model) renderIrssiBody(height int) string {
	switch m.overlay {
	case overlayChannels:
		return m.renderChannelsPane(m.w, height)
	case overlayNodes:
		return m.renderNodesPane(m.w, height)
	default:
		return m.renderMessagesPane(m.w, height)
	}
}

// renderChannelStatus is the irssi lower status line — tmux-pane
// style: every channel is rendered in a stable position. The current
// channel gets a highlighted brackets + bold mesh-green, unread
// counts shown inline, others in quiet cyan. Cycling with Ctrl+N/P
// or Alt+1/2/3 only changes which one is highlighted — the list
// doesn't reshuffle, so muscle memory works.
func (m model) renderChannelStatus() string {
	label := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	// Active channel uses hot pink — stands out from the mesh-green
	// borders + callsign + status fields. Max-headroom palette already
	// has plenty of green/cyan; the pink gives the "currently typing here"
	// tab a signature color of its own.
	activeTab := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhPink)).
		Bold(true)
	activeBracket := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhPink)).
		Bold(true)
	other := lipgloss.NewStyle().Foreground(lipgloss.Color(mhLavender))
	otherIdx := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	unread := lipgloss.NewStyle().Foreground(lipgloss.Color(mhYellow)).Bold(true)
	alertStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mhOrange)).Bold(true)

	// Left side is intentionally empty — the top status bar already
	// owns the callsign under `//\ retr0h`. Rendering it again here
	// was a redundancy that pulled the eye to two separate green
	// elements saying the same thing. A bare space keeps the layout
	// aligned without carrying visual weight.
	left := ""

	var tabs []string
	for i, c := range m.channels {
		marker := ""
		if c.unread > 0 {
			if c.private {
				marker = " " + alertStyle.Render(fmt.Sprintf("(%d!)", c.unread))
			} else {
				marker = " " + unread.Render(fmt.Sprintf("(%d)", c.unread))
			}
		}
		idx := fmt.Sprintf("%d:", i+1)
		if c.name == m.currentChannel {
			tab := activeBracket.Render("[") +
				activeTab.Render(idx+c.name) + marker +
				activeBracket.Render("]")
			tabs = append(tabs, tab)
		} else {
			tab := " " + otherIdx.Render(idx) + other.Render(c.name) + marker + " "
			tabs = append(tabs, tab)
		}
	}
	mid := strings.Join(tabs, " ")

	modeTag := "INPUT"
	switch m.mode {
	case modeNav:
		modeTag = "NAV"
	case modeSearch:
		modeTag = "SEARCH"
	case modeHelp:
		modeTag = "HELP"
	}
	right := label.Render("[" + modeTag + "]")
	if m.flash != "" {
		// Flash color depends on kind — errors / hints in dim lavender,
		// successful actions in quiet green. Heuristic: anything that
		// starts with "unknown", "usage", or "use " is a hint, not a win.
		flashStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mhGreen))
		lower := strings.ToLower(m.flash)
		if strings.HasPrefix(lower, "unknown") ||
			strings.HasPrefix(lower, "usage") ||
			strings.HasPrefix(lower, "use ") ||
			strings.HasPrefix(lower, "no ") {
			flashStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained)).Italic(true)
		}
		right = flashStyle.Render(m.flash) + "  " + right
	}

	content := " " + left + "   " + mid
	pad := m.w - lipgloss.Width(content) - lipgloss.Width(right) - 2
	if pad < 1 {
		pad = 1
	}
	return content + strings.Repeat(" ", pad) + right + " "
}

// renderInputRow renders the always-on bottom input bar or the /
// search prompt. Includes a channel prefix in mesh-green so the user
// always knows where their typing will go.
func (m model) renderInputRow() string {
	amber := lipgloss.NewStyle().Foreground(lipgloss.Color(mhOrange)).Bold(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen)).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))

	if m.mode == modeSearch {
		return " " + amber.Render("/ ") + m.searchInput.View() +
			"  " + dim.Render("ESC cancel · Enter match")
	}
	if m.mode == modeNav {
		hint := dim.Render(
			"NAV · j/k · r reply · w whois · t trace · p ping · * star · ESC back to input · / search · ? help",
		)
		return " " + hint
	}
	// Input mode — default.
	// Input-bar channel prefix stays mesh-green — pink is reserved
	// for the highlighted active-channel tab in the status row above.
	prefix := green.Render("["+m.currentChannel+"] ") + amber.Render("› ")
	return " " + prefix + m.input.View()
}

// renderTopDivider draws a full-width double-line ruler across the
// screen under the status bar. Pure vintage BBS masthead divider.
func (m model) renderTopDivider() string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen))
	return style.Render(strings.Repeat("═", m.w))
}

// renderHelpView draws a full-pane help overlay listing every keybind
// and every `:` command, organized by category. Any key dismisses it.
func (m model) renderHelpView(height int) string {
	head := lipgloss.NewStyle().
		Foreground(lipgloss.Color(meshGreen)).
		Bold(true)
	sec := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhCyan)).
		Bold(true).
		Underline(true)
	key := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhYellow)).
		Bold(true)
	desc := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhFG))
	dim := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained))

	kv := func(k, d string) string {
		return "  " + key.Render(padOrTruncate(k, 14)) + "  " + desc.Render(d)
	}

	lines := []string{
		head.Render("S Q U E L C H   ·   H E L P"),
		dim.Render("j/k scroll · q/Esc/? close · irssi-style modal UI"),
		"",
		sec.Render("MODES"),
		kv("INPUT (default)", "cursor in the input bar — type a message or /command"),
		kv("NAV (Esc)", "cursor in the scrollback — single letters act on highlight"),
		kv("SEARCH (/ in nav)", "live-filter current pane. Enter commits, Esc cancels"),
		kv("HELP (?)", "this screen — Esc / q / ? to dismiss"),
		"",
		sec.Render("GLOBAL"),
		kv("Ctrl+X", "exit app (also Ctrl+C on empty input)"),
		kv("?", "open help"),
		kv("Enter", "send message / run /command / activate selection"),
		kv("Esc", "input → nav mode / nav → back to input / cancel modal"),
		kv("Tab", "complete /command, #channel, or nick (cycles)"),
		kv("Shift+Tab", "cycle completion backwards"),
		"",
		sec.Render("CHANNEL SWITCHING"),
		kv("Alt+1..4", "jump to channel by index"),
		kv("Ctrl+N / Ctrl+P", "cycle to next / prev channel"),
		kv("/join <name>", "switch to named channel"),
		kv("/channel list", "show all known channels"),
		"",
		sec.Render("WINDOW NAV"),
		kv("Ctrl+W k", "from input → jump up to the message log (nav mode)"),
		kv("Ctrl+W j", "from nav   → drop down to the input bar"),
		kv("Esc (input)", "same as Ctrl+W k — enter scrollback nav"),
		kv("Esc (nav)", "same as Ctrl+W j — return to input bar"),
		"",
		sec.Render("OVERLAYS"),
		kv("/channels", "open channels list — j/k walk, Enter opens"),
		kv("/nodes /users /names", "open users grid — h/j/k/l walk, Enter whois"),
		kv("/help or ?", "this help screen — j/k scroll, Esc closes"),
		"",
		sec.Render("NAV MODE (after Esc)"),
		kv("j / k", "walk selection down / up"),
		kv("gg / G", "top / bottom"),
		kv("Ctrl+D / Ctrl+U", "half-page down / up"),
		kv("/", "search within focused pane"),
		kv("n / N", "next / prev search hit"),
		kv("Enter", "detail view (hop, SNR, RSSI, hex id)"),
		kv("Esc / i / q", "back to input mode"),
		"",
		sec.Render("NAV-MODE QUICK-KEYS (on message/node selection)"),
		kv("r", "reply — prefills /reply <sender> into input"),
		kv("t", "traceroute selected sender"),
		kv("p", "ping selected sender"),
		kv("w", "whois selected sender"),
		kv("*", "pin / unpin selected node"),
		kv("m", "mute / unmute selected node"),
		kv("F", "filter messages to selected node's traffic"),
		kv("X", "clear active filter"),
		kv("s", "cycle node sort (heard → name → state)  (nodes drawer)"),
		"",
		sec.Render("HAM RADIO /COMMANDS"),
		kv("/cq [tail]", "broadcast CQ with optional custom tail"),
		kv("/cqr <call>", "respond to someone's CQ with a real copy report"),
		kv("/rs <call>", "send a signal report (real SNR/RSSI/hops)"),
		kv("/73 [call]", "sign-off (\"best regards\")"),
		kv("/88", "love-and-kisses ham slang"),
		kv("/qsl", "acknowledge / confirm receipt"),
		kv("/qth [grid]", "broadcast your location / grid square"),
		kv("/sked <call>", "propose a scheduled contact"),
		kv("/qrz", "\"who is calling me?\" — prompt for ID"),
		kv("/qrm <call>", "report interference on their signal"),
		kv("/qsb <call>", "report that their signal is fading"),
		kv("/sk", "final sign-off — stronger than /73"),
		kv("/wx [conditions]", "weather report at your QTH"),
		kv("/grid [locator]", "broadcast just your Maidenhead grid"),
		kv("/mesh", "summarize the mesh you can hear (meshtastic-specific)"),
		kv("/k <call>", "\"over — go ahead\" ragchew turn-taking"),
		"",
		sec.Render("MESSAGING /COMMANDS"),
		kv("/msg <call> <text>", "direct message to node"),
		kv("/reply [call] [text]", "reply (uses highlighted sender if omitted)"),
		kv("/r", "alias for /reply"),
		kv("/ping <call>", "RTT + signal check"),
		kv("/tr <call>", "traceroute — alias /traceroute"),
		kv("/whois <call>", "node metadata — alias /w"),
		"",
		sec.Render("CHANNEL / UTIL /COMMANDS"),
		kv("/join <channel>", "switch to named channel"),
		kv("/channel list", "list known channels"),
		kv("/config", "show radio + identity configuration"),
		kv("/clear", "clear local scrollback (does not unsend)"),
		kv("/help", "open this help"),
		kv("/q, /quit", "hint — use Ctrl+X to exit"),
		"",
		sec.Render("NOTES ON CHANNELS"),
		kv("", "Channels are configured on the RADIO, not in meshx."),
		kv("", "A channel = a name + a shared PSK (encryption key)."),
		kv("", "Create channels via the official Meshtastic app / CLI;"),
		kv("", "meshx imports them once the radio is configured."),
		kv("", "Future: /channel add <meshtastic://url> to import by URL,"),
		kv("", "/channel share <name> to emit a QR for another client."),
	}

	// Viewport: show only the window that fits in the frame. Reserve
	// rows for the border (2), padding (2), header (1), blank (1),
	// and scroll-indicator (1).
	visible := height - 7
	if visible < 5 {
		visible = 5
	}
	maxScroll := len(lines) - visible
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := m.helpScroll
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	end := scroll + visible
	if end > len(lines) {
		end = len(lines)
	}
	viewLines := append([]string(nil), lines[scroll:end]...)

	// Scroll indicator — shows position + vim-style hint.
	indicator := ""
	if len(lines) > visible {
		pos := fmt.Sprintf("line %d/%d", scroll+1, len(lines))
		hint := "j/k scroll · d/u page · g/G top/bottom · q/Esc/? close"
		indicator = dim.Render(pos + "   " + hint)
	} else {
		indicator = dim.Render("q / Esc / ? to close")
	}
	viewLines = append(viewLines, "", indicator)

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color(meshGreen)).
		Padding(1, 3).
		Width(m.w - 4).
		Height(height - 2).
		Render(strings.Join(viewLines, "\n"))
}

// statusSegment wraps a styled value in the `░▒▓ value ▓▒░` tmux /
// powerline gradient chrome. `content` is already styled (fg color +
// bold etc.); `chromeColor` tints the ░▒▓ bars themselves. Consecutive
// segments butt directly against each other for the classic
// stacked-gradient look: `░▒▓ call ▓▒░░▒▓ hw ▓▒░`.
func statusSegment(content, chromeColor string) string {
	chrome := lipgloss.NewStyle().Foreground(lipgloss.Color(chromeColor))
	return chrome.Render("░▒▓ ") + content + chrome.Render(" ▓▒░")
}

func (m model) renderStatusBar() string {
	call := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen)).Bold(true)
	label := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	val := lipgloss.NewStyle().Foreground(lipgloss.Color(mhCyan)).Bold(true)
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(mhYellow)).Bold(true)
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color(mhGreen)).Bold(true)
	pink := lipgloss.NewStyle().Foreground(lipgloss.Color(mhPink)).Bold(true)
	demoTag := lipgloss.NewStyle().Foreground(lipgloss.Color(mhOrange)).Bold(true)

	// Chrome (the ░▒▓ gradient bars) is always dim drained — the
	// segment's content carries the semantic color.
	chrome := mhDrained

	// Build the segment list. Live mode pulls from model state;
	// demo mode keeps the canned values so screenshots stay juicy.
	var segs []string

	// Segment 1: brand mark + callsign, both in mesh-green.
	// Brand segment: `//\ <shortname> <longname>` — shortname first
	// (Meshtastic's 4-char "badge" that fits on a radio OLED) then
	// the full callsign. Falls back to just `//\ <longname>` when no
	// shortname is known (demo, or before MyNodeInfo arrives).
	brand := call.Render(`//\`) + "  "
	if sn := m.myShortName(); sn != "" {
		brand += call.Render(sn) + " " + call.Render(m.myCallsign())
	} else {
		brand += call.Render(m.myCallsign())
	}
	segs = append(segs, statusSegment(brand, chrome))

	{
		// One render path for both demo and live mode — demo mode
		// pre-populates the same model fields below, so every
		// segment just reads model state. Fields show "—" when the
		// radio hasn't yet sent them (DeviceMetrics telemetry is
		// periodic — default every 30 min on a fresh radio).
		n := m.myNode()

		// Slim unicode icons used throughout the bar. Light-weight
		// powerline-style glyphs rather than emoji — render at the
		// same cell width as text, keep color theming clean.
		//
		//   ⌂  home / hardware
		//   ⚙  firmware (cog)
		//   ⌬  channel (radio ring)
		//   ⟐  tx power (diamond with dot)
		//   ⚡  battery
		//   ≈  channel utilization (airwaves)
		//   ⌖  role (crosshair)
		//   ⌘  region (command-like — regulatory code)
		//   ☖  grid square (shogi piece traditionally used for QTH)
		//   ⚭  peers (linked-rings)
		//   ●  online / connecting dot

		// Hardware + firmware.
		hw := "—"
		if n != nil && n.hwModel != "" {
			hw = n.hwModel
		}
		fw := shortFirmware(m.radioFirmware)
		segs = append(segs, statusSegment(
			label.Render("⌂ ")+val.Render(hw)+"  "+label.Render("⚙ ")+val.Render(fw),
			chrome,
		))

		// Channel + modem preset (e.g. "⌬ #default LongFast").
		chParts := []string{label.Render("⌬ ")}
		if m.currentChannel != "" {
			chParts = append(chParts, val.Render(m.currentChannel))
		} else {
			chParts = append(chParts, val.Render("—"))
		}
		if m.radioModemPreset != "" {
			chParts = append(chParts, " "+label.Render(m.radioModemPreset))
		}
		segs = append(segs, statusSegment(strings.Join(chParts, ""), chrome))

		// TX power in dBm.
		tx := "—"
		if m.radioTxPower != 0 {
			tx = fmt.Sprintf("%d dBm", m.radioTxPower)
		}
		segs = append(segs, statusSegment(label.Render("⟐ ")+warn.Render(tx), chrome))

		// Battery + voltage.
		batt := "—"
		if m.hasTelemetry {
			pct := "—"
			if m.batteryLevel > 0 {
				if m.batteryLevel > 100 {
					pct = "pwr"
				} else {
					pct = fmt.Sprintf("%d%%", m.batteryLevel)
				}
			}
			if m.batteryVoltage > 0 {
				batt = fmt.Sprintf("%.2fV %s", m.batteryVoltage, pct)
			} else {
				batt = pct
			}
		}
		segs = append(segs, statusSegment(label.Render("⚡ ")+val.Render(batt), chrome))

		// Channel utilization.
		util := "—"
		if m.hasTelemetry {
			util = fmt.Sprintf("%.1f%%", m.channelUtil)
		}
		segs = append(segs, statusSegment(label.Render("≈ ")+val.Render(util), chrome))

		// Role (CLIENT / ROUTER / REPEATER / TRACKER …).
		if m.radioRole != "" {
			segs = append(segs, statusSegment(label.Render("⌖ ")+val.Render(m.radioRole), chrome))
		}

		// Region (regulatory domain).
		if m.radioRegion != "" {
			segs = append(segs, statusSegment(label.Render("⌘ ")+val.Render(m.radioRegion), chrome))
		}

		// Grid square (Maidenhead).
		if m.myGrid != "" {
			segs = append(segs, statusSegment(label.Render("☖ ")+val.Render(m.myGrid), chrome))
		}

		// Peer count.
		segs = append(
			segs,
			statusSegment(label.Render("⚭ ")+val.Render(fmt.Sprintf("%d", len(m.nodes))), chrome),
		)
	}

	// Right-most segment: connection state.
	var state string
	switch {
	case m.isDemo():
		state = ok.Render("online") + "  " + demoTag.Render("[DEMO]")
	case m.connected:
		state = ok.Render("● online")
	default:
		state = pink.Render("● connecting")
	}
	segs = append(segs, statusSegment(state, chrome))

	content := strings.Join(segs, "")

	// Graceful narrow-terminal fallback: when the bar is too wide,
	// drop segments one at a time from the center outward until it
	// fits. Brand (first) and state (last) are preserved so the
	// user always knows who they are + whether they're connected.
	for lipgloss.Width(content) > m.w-2 && len(segs) > 2 {
		mid := len(segs) / 2
		if mid == 0 || mid == len(segs)-1 {
			break
		}
		segs = append(segs[:mid], segs[mid+1:]...)
		content = strings.Join(segs, "")
	}

	pad := m.w - lipgloss.Width(content) - 2
	if pad < 0 {
		pad = 0
	}
	return " " + content + strings.Repeat(" ", pad) + " "
}

// paneStyle returns a border-only styled pane container. Focused panes
// get a double-line box in mesh-green — classic BBS / Norton Commander
// / Turbo Pascal IDE look. Unfocused panes get a thin single-line box
// in dim lavender. Clean, consistent, mutt-y.
// paneAccentColor returns the signature color for each pane — used
// both for the focused border and the giant Ctrl+W pane-number.
//
//	channels = cyan   (#00d4ff — left pane, the "inbox list")
//	messages = mesh-green (center pane, brand color)
//	nodes    = magenta (#c678dd — right pane, "network roster")
func paneAccentColor(paneIdx int) string {
	switch paneIdx {
	case paneChannels:
		return mhCyan
	case paneNodes:
		return mhMagenta
	default:
		return meshGreen
	}
}

// nickColorPalette is the accent-color ring used to hash peer
// callsigns into distinct hues — irssi/weechat convention. Avoids
// mesh-green (brand), magenta (reserved for "me"), and any
// drained / lavender-adjacent tones that would blend with the
// quiet labels elsewhere. Bright-saturated hues only so the glitch
// Max Headroom read is loud and nicks pop off the log.
var nickColorPalette = []string{
	mhCyan,    // #00d4ff  neon cyan
	mhYellow,  // #e5c07b  warm amber
	mhOrange,  // #ffb86c  sunset
	mhPink,    // #ff6ec7  hot pink
	"#a78bfa", // electric violet
	"#7dd3fc", // sky blue
	"#facc15", // acid yellow
	"#f472b6", // bubblegum
}

// nickColor deterministically maps a callsign to one of the peer
// accent colors via FNV-1a plus a murmur3-style avalanche mix so
// the low bits carry enough entropy for a good modulo distribution.
// Raw FNV-1a on short near-duplicate strings ("node 0x…") clustered
// several peers into the same bucket; the avalanche step spreads
// them out. Same callsign → same color every time so the eye picks
// each peer out of the log by hue. Empty / system rows fall back
// to drained.
func nickColor(callsign string) string {
	if callsign == "" {
		return mhDrained
	}
	var sum uint32 = 2166136261
	for _, r := range callsign {
		sum ^= uint32(r)
		sum *= 16777619
	}
	sum ^= sum >> 16
	sum *= 0x85ebca6b
	sum ^= sum >> 13
	return nickColorPalette[int(sum)%len(nickColorPalette)]
}

// paneHeader renders a plain bold uppercase header. Focused panes
// already show their accent color in the double-lined border, so
// the header itself stays neutral fg (focused) or drained (not) —
// repeating the accent in the header was extra color noise that
// made the UI read as mesh-green everywhere at once. The border
// carries focus; the header carries the label.
func paneHeader(text string, paneIdx int, focused bool) string {
	_ = paneIdx
	s := lipgloss.NewStyle().Bold(true)
	if focused {
		s = s.Foreground(lipgloss.Color(mhFG))
	} else {
		s = s.Foreground(lipgloss.Color(mhDrained))
	}
	return s.Render(strings.ToUpper(text))
}

func paneStyle(width, height, paneIdx int, focused bool) lipgloss.Style {
	s := lipgloss.NewStyle().
		Width(width-2).
		Height(height-2).
		Padding(1, 1)
	if focused {
		return s.
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color(paneAccentColor(paneIdx)))
	}
	return s.
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(mhDrained))
}

func (m model) renderChannelsPane(width, height int) string {
	header := paneHeader("CHANNELS", paneChannels, m.focused == paneChannels)

	lines := make([]string, 0, 2+len(m.channels))
	lines = append(lines, header, "")
	for i, c := range m.channels {
		lines = append(
			lines,
			m.renderChannelRow(c, i == m.selectedCh && m.focused == paneChannels, width-4),
		)
	}
	return paneStyle(width, height, paneChannels, m.focused == paneChannels).
		Render(strings.Join(lines, "\n"))
}

func (m model) renderChannelRow(c channelItem, selected bool, inner int) string {
	name := c.name
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mhCyan))
	if c.private {
		nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(mhMagenta))
	}
	unread := ""
	if c.unread > 0 {
		unread = lipgloss.NewStyle().
			Foreground(lipgloss.Color(mhYellow)).
			Bold(true).
			Render(fmt.Sprintf(" %d", c.unread))
	}
	row := nameStyle.Render(name) + unread
	return wrapSelection(row, selected, m.isStringSearchHit(c.name), inner)
}

// sortedNodes returns a view of m.nodes with pinned (fav) nodes first,
// then sorted by the current sort mode. The returned slice is a copy
// so we don't mutate storage order.
func (m model) sortedNodes() []nodeItem {
	out := make([]nodeItem, len(m.nodes))
	copy(out, m.nodes)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].fav != out[j].fav {
			return out[i].fav // pinned first, always
		}
		switch m.nodeSort {
		case sortByName:
			return out[i].callsign < out[j].callsign
		case sortByState:
			return stateWeight(out[i].state) < stateWeight(out[j].state)
		default: // sortByLastHeard
			return out[i].heardRank < out[j].heardRank
		}
	})
	return out
}

// stateWeight orders node states: online < offline < muted < failed.
func stateWeight(s string) int {
	switch s {
	case "online":
		return 0
	case "offline":
		return 1
	case "muted":
		return 2
	case "failed":
		return 3
	default:
		return 4
	}
}

// renderNodesPane — BitchX/osiris-style "Users" grid. Nodes ARE the
// users on a Meshtastic network, so we treat them like IRC nicks: a
// bracketed grid of [ @name ] cells, color-coded by state, prefixed
// with IRC-style sigils (@ = online, + = pinned/fav, blank = normal,
// · = offline/stale, ✗ = failed, ⊘ = muted).
func (m model) renderNodesPane(width, height int) string {
	total := len(m.nodes)
	online := 0
	for _, n := range m.nodes {
		if n.state == "online" {
			online++
		}
	}

	header := paneHeader("NODES", paneNodes, m.focused == paneNodes)
	count := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Render(fmt.Sprintf("  (#mesh: %d/%d · sort: %s)", online, total, m.nodeSort.label()))
	legend := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Italic(true).
		Render("legend:  @online  +pinned  ⊘muted  ✗failed  ·stale")

	sorted := m.sortedNodes()

	// Grid layout — fixed-width cells, as many columns as fit.
	// Each cell: "[ @callsign    ] " → up to ~20 visible cells.
	inner := width - 4 // minus pane border + pane padding
	if inner < 18 {
		inner = 18
	}
	const cellPad = 1 // inter-cell gap
	cellW := 22
	if inner >= 100 {
		cellW = 24
	}
	if inner < 60 {
		cellW = 18
	}
	cols := (inner + cellPad) / (cellW + cellPad)
	if cols < 1 {
		cols = 1
	}

	var gridLines []string
	for row := 0; row*cols < len(sorted); row++ {
		var cells []string
		for c := 0; c < cols; c++ {
			idx := row*cols + c
			if idx >= len(sorted) {
				cells = append(cells, strings.Repeat(" ", cellW))
				continue
			}
			cells = append(
				cells,
				m.renderUserCell(sorted[idx], idx == m.selectedNd && m.focused == paneNodes, cellW),
			)
		}
		gridLines = append(gridLines, strings.Join(cells, strings.Repeat(" ", cellPad)))
	}

	lines := make([]string, 0, 4+len(gridLines))
	lines = append(lines, header+count, "", legend, "")
	lines = append(lines, gridLines...)

	return paneStyle(width, height, paneNodes, m.focused == paneNodes).
		Render(strings.Join(lines, "\n"))
}

// renderUserCell renders one [ @callsign ] bracketed cell in the
// BitchX-Users grid. Selected cells get the green highlight gutter
// (truncated into the cell).
func (m model) renderUserCell(n nodeItem, selected bool, cellW int) string {
	// IRC-style sigil choice:
	sigil := " "
	sigilColor := mhDrained
	switch n.state {
	case "online":
		sigil = "@"
		sigilColor = mhGreen
	case "muted":
		sigil = "⊘"
		sigilColor = mhLavender
	case "failed":
		sigil = "✗"
		sigilColor = mhPink
	case "offline":
		sigil = "·"
		sigilColor = mhDrained
	}
	if n.fav {
		sigil = "+"
		sigilColor = mhYellow
	}

	// Name stays neutral fg for online nodes — the green `@` sigil
	// alone carries the "alive" pulse (irssi / BitchX user-list
	// convention). Non-online states do tint the name since those
	// ARE worth surfacing at a glance.
	nameColor := mhFG
	switch n.state {
	case "offline":
		nameColor = mhDrained
	case "failed":
		nameColor = mhPink
	case "muted":
		nameColor = mhLavender
	}
	if n.fav {
		nameColor = mhYellow
	}
	// Selection highlight uses the nodes pane's own accent (magenta).
	// Every pane's selected item takes that pane's accent so the
	// focus indicator visually belongs to the pane it's in — avoids
	// mesh-green sprawl across brand / input / online-sigils / this.
	if selected {
		nameColor = mhMagenta
	}

	sigilStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(sigilColor)).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(nameColor))
	if selected {
		nameStyle = nameStyle.Bold(true)
	}
	bracketStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	if selected {
		bracketStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(mhMagenta)).Bold(true)
	}

	// Compute how many cells the name can occupy. cellW = "[ S name  ] "
	// where S = sigil. Fixed chrome: "[ " (2) + sigil (1) + " " (1) + " ]" (2) = 6.
	nameBudget := cellW - 6
	if nameBudget < 3 {
		nameBudget = 3
	}
	// When the node has a shortname (Meshtastic 4-char badge),
	// prefix it before the longname: "💀 retr0h". Disambiguates
	// rows that share a longname (e.g. two "retr0h" radios with
	// distinct shortnames) and matches the iPhone app's
	// "Longname (!hex)" treatment — short identifier first,
	// full identifier after. Falls back to longname alone when
	// the peer doesn't broadcast a shortname.
	display := n.callsign
	if n.shortName != "" {
		display = n.shortName + " " + n.callsign
	}
	name := padOrTruncate(display, nameBudget)

	cell := bracketStyle.Render("[") +
		" " + sigilStyle.Render(sigil) + " " +
		nameStyle.Render(name) +
		" " + bracketStyle.Render("]")

	// If a search query is active and this cell's callsign matches,
	// wrap in the same dim-green hit bg that rows use — gives users
	// a visible "these are the matches" marker in the grid.
	if m.isStringSearchHit(n.callsign) && !selected {
		cell = lipgloss.NewStyle().
			Background(lipgloss.Color("#0e2618")).
			Render(cell)
	}
	return cell
}

func (m model) renderMessagesPane(width, height int) string {
	chanName := m.currentChannel
	if chanName == "" {
		chanName = "#primary"
	}
	header := paneHeader(strings.ToUpper(chanName), paneMessages, m.focused == paneMessages)
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Render(fmt.Sprintf("  (%d msgs)", len(m.messages)))

	rowsFree := height - 6
	if rowsFree < 3 {
		rowsFree = 3
	}

	var lines []string
	lines = append(lines, header+hint, "")

	// irssi-style: always the dense one-row-per-message list.
	// Default anchors on the tail (show latest rows). If the user
	// has scrolled the selection above the natural tail via j/k or
	// Ctrl+F / Ctrl+U, drop the viewport back so the selected row
	// stays visible — otherwise nav feels broken because moving
	// selectedMsg doesn't seem to move anything on screen.
	startIdx := tailStartList(m.messages, rowsFree)
	if m.selectedMsg < startIdx {
		startIdx = m.selectedMsg - rowsFree/3
		if startIdx < 0 {
			startIdx = 0
		}
	}
	if startIdx > 0 {
		lines = append(lines,
			lipgloss.NewStyle().
				Foreground(lipgloss.Color(mhDrained)).
				Italic(true).
				Render(fmt.Sprintf("   … %d earlier", startIdx)))
	}
	selected := m.focused == paneMessages && m.mode == modeNav
	var lastGroup uint64
	var groupBg string
	for i := startIdx; i < len(m.messages); i++ {
		msg := m.messages[i]
		faded := m.nodeFilter != "" && !m.msgMatchesFilter(msg)

		// Group rows share one zebra stripe — every line in a /whois
		// or /config block gets the same bg so the block reads as one
		// card instead of alternating stripes.
		var bg string
		switch {
		case msg.group != 0 && msg.group == lastGroup:
			bg = groupBg
		case msg.group != 0:
			// System blocks (/whois, /config, /ping, /env, /info
			// cards) always use the lighter zebra tint so the block
			// reads consistently as one shaded card — never the
			// near-black rowBgEven that would make a block look
			// like terminal-bg depending on parity.
			bg = rowBgOdd
			lastGroup = msg.group
			groupBg = bg
		default:
			bg = zebraBg(i)
			lastGroup = 0
			groupBg = ""
		}

		// Highlight the whole group when any row in it is selected —
		// j/k lands the cursor on the header row, but visually the
		// entire block shows as the current selection.
		isSelected := i == m.selectedMsg && selected
		if !isSelected && msg.group != 0 && selected {
			if sel := m.messages[m.selectedMsg]; sel.group == msg.group {
				isSelected = true
			}
		}

		line := m.renderMessageRow(msg, isSelected, width-4, bg)
		if faded {
			line = dimRow(line)
		}
		lines = append(lines, line)
	}
	// BitchX / irssi gravity — pin the log to the BOTTOM of the
	// pane. When the message list doesn't fill the available rows,
	// pad the top with blank lines so content rises from the input
	// bar upward instead of hanging off the pane header. Once the
	// log outgrows rowsFree, tailStartList trims the head so we're
	// always showing the newest rows flush against the bottom of
	// the pane.
	//
	// Rows-used math: 2 header rows (header + blank separator), plus
	// one optional "… N earlier" row when startIdx > 0. Everything
	// else is message rows. rowsFree is the budget passed to
	// tailStartList so it's also the correct cap for what we're
	// bottom-aligning against.
	// BitchX / irssi gravity — pin the log to the bottom of the
	// pane. `rowsFree` IS the pane's total content budget
	// (includes header + separator + message rows), so pad to
	// that total. Previous attempt treated rowsFree as
	// message-only budget, which overflowed the paneStyle height
	// by 2 rows and pushed the top-bar off-screen.
	if pad := rowsFree - len(lines); pad > 0 {
		rebuilt := make([]string, 0, rowsFree)
		rebuilt = append(rebuilt, lines[:2]...) // preserve header + separator
		for i := 0; i < pad; i++ {
			rebuilt = append(rebuilt, "")
		}
		rebuilt = append(rebuilt, lines[2:]...)
		lines = rebuilt
	}
	return paneStyle(width, height, paneMessages, m.focused == paneMessages).
		Render(strings.Join(lines, "\n"))
}

// tailStartList is the row-budget calculator for list view — each
// message is 1 row, +1 if it has an acks sub-line. System messages
// are still 1 row.
func tailStartList(msgs []messageItem, rowsBudget int) int {
	rows := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		// System multi-line blocks carry embedded newlines; each
		// newline = one more visual row.
		cost := 1 + strings.Count(msgs[i].text, "\n")
		if msgs[i].acks != "" {
			cost++
		}
		if rows+cost > rowsBudget {
			return i + 1
		}
		rows += cost
	}
	return 0
}

// Zebra stripe bgs for message rows — dense mutt-style list where
// every message has a solid bg and adjacent messages alternate shade.
// No blank separators between — the color alternation IS the visual
// separator, which keeps the grid continuous from top to bottom of
// the pane and gives the whole feed a thick, woven feel.
// Two complementary mid-charcoal shades from the tokyo-night / max
// headroom family — never pure black. Both read as tinted gray, not
// void, so the zebra reads as soft alternation rather than harsh
// contrast with the pane bg.
const (
	rowBgEven = "#1a1b26" // cool tokyo-night base
	rowBgOdd  = "#24283b" // one step lighter + barely-purple undertone
)

// zebraBg returns the bg tint for the Nth message row in display order.
func zebraBg(i int) string {
	if i%2 == 0 {
		return rowBgEven
	}
	return rowBgOdd
}

// renderMessageRow is the mutt/BBS-style compact one-row message
// renderer. Chunky layout:
//
//	[▎] [F] [TIME ] [FROM............] [text...........]  [↝N] [SNR ] [ok]
//	  ^
//	  thin sender-colored accent + zebra-striped bg
//
// The entire row is rendered on a zebra bg so adjacent messages
// alternate shade — no blank separator needed; the color alternation
// is the visual separator. Continuous grid from top to bottom.
func (m model) renderMessageRow(msg messageItem, selected bool, inner int, rowBg string) string {
	// "splash" rows are decorative BitchX-style log banners — already
	// lipgloss-styled (colored block-art + tagline), rendered with
	// no `-!-` prefix, no timestamp, no sender column, no selection
	// highlight. Just the pre-styled text centered in the pane so
	// the splash sits quietly at the top of the scrollback until
	// real messages push it off-screen.
	if msg.status == "splash" {
		return lipgloss.PlaceHorizontal(inner, lipgloss.Center, msg.text)
	}
	tstamp := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Background(lipgloss.Color(rowBg))
	me := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhMagenta)).
		Background(lipgloss.Color(rowBg)).
		Bold(true)
	// Per-sender callsign color — irssi/weechat-style nick hash so
	// each peer picks a stable color from the accent palette. Turns
	// a cyan-wall log into something you can skim by hue; each
	// conversation reads as a distinct thread at a glance. Ghost
	// peers ("node 0x…" placeholders whose NodeInfo never arrived)
	// render drained instead so they don't compete visually with
	// resolved names. Hash the RESOLVED name (via displayFrom),
	// not the raw msg.from — otherwise a ghost that later resolved
	// still gets drained because its stored from-string still
	// starts with "node 0x".
	resolvedName := m.displayFrom(msg)
	peerColor := nickColor(resolvedName)
	if strings.HasPrefix(resolvedName, "node 0x") {
		peerColor = mhDrained
	}
	peer := lipgloss.NewStyle().
		Foreground(lipgloss.Color(peerColor)).
		Background(lipgloss.Color(rowBg)).
		Bold(true)
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(mhFG)).Background(lipgloss.Color(rowBg))
	ack := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhGreen)).
		Background(lipgloss.Color(rowBg)).
		Bold(true)
	fail := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhPink)).
		Background(lipgloss.Color(rowBg)).
		Bold(true)
	bang := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhYellow)).
		Background(lipgloss.Color(rowBg)).
		Bold(true)
	sys := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhLavender)).
		Background(lipgloss.Color(rowBg)).
		Italic(true)
	hop := lipgloss.NewStyle().
		Foreground(lipgloss.Color(meshGreen)).
		Background(lipgloss.Color(rowBg))

	contentW := inner - gutterWidth
	if contentW < 40 {
		contentW = 40
	}

	// ── Thin 1-cell sender-color accent on the left edge — a ▎ tick,
	//    not a solid block. Quiet enough to not clash, bright enough
	//    to show "who's talking" at a glance. Default maps the accent
	//    to the same hash bucket the peer name renders in, so the
	//    tick and the callsign pick the same color.
	senderAccentColor := nickColor(resolvedName)
	if strings.HasPrefix(resolvedName, "node 0x") {
		senderAccentColor = mhDrained
	}
	if msg.mine {
		senderAccentColor = mhMagenta
	}
	if msg.bang != "" {
		senderAccentColor = mhYellow
	}
	if msg.status == "fail" {
		senderAccentColor = mhPink
	}
	if msg.status == "system" {
		senderAccentColor = mhLavender
	}
	accent := lipgloss.NewStyle().
		Foreground(lipgloss.Color(senderAccentColor)).
		Background(lipgloss.Color(rowBg)).
		Bold(true).
		Render("▎") + lipgloss.NewStyle().Background(lipgloss.Color(rowBg)).Render(" ")

	// System messages — single-line. Multi-line blocks are emitted
	// as multiple messageItems sharing a `group` ID; the pane loop
	// binds them visually by reusing the same zebra bg.
	if msg.status == "system" {
		timeCol := "   " + msg.time + "  "
		// Continuation lines in a block hide the timestamp so only the
		// header row carries it — makes the block read as one card.
		if msg.group != 0 && !strings.HasPrefix(msg.text, "-!- whois") &&
			!strings.HasPrefix(msg.text, "-!- config") &&
			!strings.HasPrefix(msg.text, "-!- env") &&
			!strings.HasPrefix(msg.text, "-!- ping") &&
			!strings.HasPrefix(msg.text, "-!- traceroute") {
			// Not the header row — blank out time, keep accent.
			// Width must match header's "   HH:MM  " (10 cells) so the
			// `-!-` prefix column lines up between header and body.
			timeCol = "          "
		}
		// Ghost-glyph special case — systemBlock lines whose body
		// contains "👻 " render the glyph in warn-orange bold while
		// the prose stays in the regular sys (lavender italic)
		// style. Embedding ANSI directly in the text doesn't work
		// because sys.Render wraps the whole string; any mid-string
		// reset bleeds the rest of the line into default color.
		// Splitting lets each half render with its own style,
		// concatenated on the same zebra bg.
		body := msg.text
		prefixIdx := strings.Index(body, "👻 ")
		if prefixIdx >= 0 {
			// Muted ghost glyph — drained fg, no bold. The point is
			// to say "this is a placeholder-warning line", not shout
			// for attention. Let the prose carry the message and
			// the glyph just tag it semantically.
			ghostStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(mhDrained)).
				Background(lipgloss.Color(rowBg))
			pre := body[:prefixIdx]
			post := body[prefixIdx+len("👻 "):]
			line := accent + tstamp.Render(timeCol) +
				sys.Render(pre) +
				ghostStyle.Render("👻") +
				sys.Render(" "+post)
			return wrapSelection(line, selected, m.isMsgSearchHit(msg), inner, rowBg)
		}
		line := accent + tstamp.Render(timeCol) + sys.Render(msg.text)
		return wrapSelection(line, selected, m.isMsgSearchHit(msg), inner, rowBg)
	}

	// Flag column (1 col + space).
	flag := " "
	flagStyle := tstamp
	switch {
	case msg.status == "fail":
		flag = "✗"
		flagStyle = fail
	case msg.bang != "":
		flag = "*"
		flagStyle = bang
	case msg.mine:
		flag = "›"
		flagStyle = me
	}

	// From column — 16 visible cells, truncated w/ ellipsis.
	// Resolve via displayFrom so "node 0x…" placeholders flip to
	// real callsigns as NodeInfo arrives mid-session. Ghost peers
	// (no NodeInfo ever received) get a 👻 prefix as a quick visual
	// marker alongside the drained color — doubly obvious which
	// senders are placeholders vs resolved. Own messages use our
	// actual callsign (not the irssi "me" placeholder) so the
	// brand in the top bar and the sender column tell the same
	// story about identity — the magenta peer style still flags
	// "this one's yours" at a glance.
	fromRaw := m.displayFrom(msg)
	if msg.mine {
		fromRaw = m.myCallsign()
	} else if strings.HasPrefix(fromRaw, "node 0x") {
		fromRaw = "👻 " + fromRaw
	}
	// 30-cell from column — Meshtastic longnames cap at 36 bytes per
	// the firmware; 30 display cells covers the large majority of
	// real callsigns ("AmputiLayag_MeshNodeQTHlab", "SGV_Shredder__
	// Base", "Gleep - socalme.sh") without ellipsis while still
	// leaving the message column the dominant share of the row on
	// typical terminal widths.
	const fromW = 30
	senderStyle := peer
	if msg.mine {
		senderStyle = me
	}
	fromPadded := padOrTruncate(fromRaw, fromW)

	// Right-side metadata column: hops / SNR / ack — always in the
	// same column positions so rows line up visually as a grid.
	// Hops cell is always rendered for incoming packets so absence
	// never has to be inferred — direct (0 hop) reads "↝ dx",
	// multi-hop reads "↝Nh". Our own outgoing messages (mine) skip
	// the cell since "hops from me to mesh" isn't a thing we know
	// until the packet echoes back.
	hopCol := "      "
	switch {
	case msg.mine:
		// blank — our outbound packet, no RX hops to report.
	case msg.hops > 0:
		hopCol = fmt.Sprintf("↝%dh   ", msg.hops)
	default:
		hopCol = "↝ dx   "
	}
	snrCol := "        "
	if msg.snr != "" {
		snrCol = fmt.Sprintf("%sdB  ", msg.snr)
	}
	statusCol := "  "
	switch msg.status {
	case "ack":
		statusCol = "✓ "
	case "fail":
		statusCol = "✗ "
	}
	right := hop.Render(hopCol) + hop.Render(snrCol) + func() string {
		switch msg.status {
		case "ack":
			return ack.Render(statusCol)
		case "fail":
			return fail.Render(statusCol)
		default:
			return tstamp.Render(statusCol)
		}
	}()

	// Text column — flexible middle. Compute its width from what's left.
	//   flag(2) + time(7) + from(18) + gap(1) + right(len) = fixed
	// Fixed left-side columns, in visible cells:
	//   accent "▎ "       = 2
	//   flag + space       = 2
	//   time "HH:MM  "    = 7
	//   fromPadded         = fromW
	//   twoSpace gap       = 2
	const leftFixed = 2 + 2 + 7 + fromW + 2
	rightW := lipgloss.Width(right)
	textW := contentW - leftFixed - rightW
	if textW < 10 {
		textW = 10
	}

	// The on-wire content is msg.text only — msg.bang is purely a
	// local marker identifying which /command emitted the message.
	// We render the text as-is, so what you see matches what went
	// out (a clean ham-style payload like "73" or "QTH: CN85", not
	// meshx-internal "!grid CN85" chrome).
	txtClamped := padOrTruncate(msg.text, textW)
	styledTxt := text.Render(txtClamped)
	_ = bang // kept in scope for any future command-styling use

	// Build the right-hand segment with a 2-space gap — both on the
	// tinted bg so the row reads as a single uninterrupted rectangle.
	gapStyle := lipgloss.NewStyle().Background(lipgloss.Color(rowBg))
	twoSpace := gapStyle.Render("  ")

	left := accent +
		flagStyle.Render(flag+" ") +
		tstamp.Render(msg.time+"  ") +
		senderStyle.Render(fromPadded) +
		twoSpace +
		styledTxt

	row := left + right
	// Append acks subline if present — indented past the accent+flag+time+from cols.
	if msg.acks != "" {
		indent := gapStyle.Render(strings.Repeat(" ", 2+2+7+fromW+2))
		row += "\n" + indent + sys.Render(msg.acks)
	}

	// Threading — when this message carries a reply_id pointing at a
	// parent we have in the log, render a dim one-line quoted
	// reference ABOVE the row. Indented under the timestamp column so
	// it reads as "this line is context for the row below". Format:
	//   ┌ <from> <time>  "<text, truncated>"
	// Matches how mutt / modern chat clients show reply context.
	if msg.replyID != 0 {
		if parent := m.findMessageByPacketID(msg.replyID); parent != nil {
			// Indent the quote to sit under the reply's from-column
			// start — 2 (accent) + 2 (flag) + 7 (time) = 11 cells —
			// so the ┌ hook reads as "context for the row below"
			// rather than floating out on the left margin.
			quoteIndent := gapStyle.Render(strings.Repeat(" ", 2+2+7))
			quoteW := inner - (2 + 2 + 7) - gutterWidth
			if quoteW < 20 {
				quoteW = 20
			}
			parentFrom := m.displayFrom(*parent)
			if parentFrom == "" {
				parentFrom = "—"
			}
			// Hot-pink hook — matches the /active channel bracket
			// style so "threading" reads as another active-state
			// signal, distinct from the drained labels elsewhere.
			hookStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(mhPink)).
				Background(lipgloss.Color(rowBg)).
				Bold(true)
			quoteBody := fmt.Sprintf("%s %s  %q",
				parentFrom, parent.time, truncateRunes(parent.text, 60))
			quoteLine := quoteIndent + hookStyle.Render("┌ ") +
				tstamp.Render(padOrTruncate(quoteBody, quoteW-2))
			row = quoteLine + "\n" + row
		}
	}

	return wrapSelection(row, selected, m.isMsgSearchHit(msg), inner, rowBg)
}

// findMessageByPacketID returns a pointer to the m.messages entry
// whose packetID matches, or nil. Used by the renderer to resolve
// reply_id → parent message for threaded quote rendering.
func (m model) findMessageByPacketID(id uint32) *messageItem {
	if id == 0 {
		return nil
	}
	for i := range m.messages {
		if m.messages[i].packetID == id {
			return &m.messages[i]
		}
	}
	return nil
}

// displayFrom returns the callsign to render for a message, preferring
// the CURRENT NodeDB entry over the `from` snapshot taken at ingest.
// This is what backfills "node 0xdeadbeef" → real callsign once the
// corresponding NodeInfo arrives — without it, the ingest-time
// fallback is baked into the row forever. Falls back to msg.from
// when the node isn't in nodesByNum (demo seeds with no fromNum,
// or peers we never learned about).
func (m model) displayFrom(msg messageItem) string {
	if msg.fromNum == 0 {
		return msg.from
	}
	if idx, ok := m.nodesByNum[msg.fromNum]; ok && idx < len(m.nodes) {
		if cs := m.nodes[idx].callsign; cs != "" {
			return cs
		}
	}
	return msg.from
}

// truncateRunes clamps s to at most n display runes, appending …
// when the source was longer. Used for parent-message quote lines
// so a long reply target doesn't blow out the width budget.
func truncateRunes(s string, n int) string {
	count := 0
	for i := range s {
		count++
		if count > n {
			return s[:i] + "…"
		}
	}
	return s
}

// dimRow strips ANSI styling from a pre-rendered row and re-applies
// a single dim color, so non-matching rows fade into the background
// when a node filter is active. The matching rows stay fully colored
// — so the filtered set reads as a "highlighted path" through the
// feed that j/k navigates.
func dimRow(s string) string {
	// Drop existing SGR escapes.
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			end := i + 2
			for end < len(s) && s[end] != 'm' {
				end++
			}
			if end < len(s) {
				end++
			}
			i = end
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Faint(true).
		Render(b.String())
}

// padOrTruncate forces a string to exactly width w display cells:
// right-pads with spaces if short, truncates with an ellipsis if long.
// Uses lipgloss.Width for emoji-aware measurement.
func padOrTruncate(s string, w int) string {
	cur := lipgloss.Width(s)
	if cur == w {
		return s
	}
	if cur < w {
		return s + strings.Repeat(" ", w-cur)
	}
	// Too long — keep w-1 cells + ellipsis.
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > w-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	for used < w-1 {
		b.WriteRune(' ')
		used++
	}
	b.WriteRune('…')
	return b.String()
}

// gutterWidth is the left margin reserved for the selection indicator —
// 2-cell block + 1-cell gap. Matches tlock's double-cell "pixel" sizing
// so the selected row reads as chunky 8-bit, not a thin vertical line.
const gutterWidth = 3

// wrapSelection applies the 8-bit block-highlight style to the whole
// row. Three mutually-exclusive states:
//
//   - selected:    thick mesh-green ██ gutter + drained bg tint (cursor)
//   - searchHit:   thin mesh-green │  gutter + dim-green bg tint (match)
//   - neither:     empty gutter so widths stay aligned
//
// Selection wins over a hit when both are true.
//
// Optional rowBg — when non-empty, the neutral (no marker) row also
// fills to full width with that bg color so the row reads as a solid
// rectangle instead of a ragged left-aligned fragment. Used by the
// message list view; left empty for channel / node rows.
func wrapSelection(content string, selected, searchHit bool, width int, rowBg ...string) string {
	if width <= gutterWidth {
		width = gutterWidth + 1
	}
	innerW := width - gutterWidth

	neutralBg := ""
	if len(rowBg) > 0 {
		neutralBg = rowBg[0]
	}

	// No marker — just keep the 3-col left pad for alignment. If a rowBg
	// was provided, force every line to the full inner width with that
	// bg so the tint covers the whole row (no drop-off at the right).
	if !selected && !searchHit {
		pad := strings.Repeat(" ", gutterWidth)
		parts := strings.Split(content, "\n")
		for i, p := range parts {
			truncated := truncateLine(p, innerW)
			if neutralBg != "" {
				truncated = lipgloss.NewStyle().
					Background(lipgloss.Color(neutralBg)).
					Width(innerW).
					MaxWidth(innerW).
					Inline(true).
					Render(truncated)
			}
			parts[i] = pad + truncated
		}
		return strings.Join(parts, "\n")
	}

	// Selection (cursor) style wins over search-hit when both apply.
	var gutter string
	var bg string
	if selected {
		gutter = lipgloss.NewStyle().
			Foreground(lipgloss.Color(meshGreen)).
			Bold(true).
			Render("██") + " "
		bg = mhDrained
	} else {
		gutter = lipgloss.NewStyle().
			Foreground(lipgloss.Color(meshGreen)).
			Bold(true).
			Render("│ ") + " "
		bg = "#0e2618" // dim neon-green background for a subtle row pop
	}

	// Width + MaxWidth clamp to exactly innerW so the bg tint covers the
	// whole row — no more, no less. Prevents terminal auto-wrap from
	// punching holes in the highlight block.
	lineStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bg)).
		Foreground(lipgloss.Color(mhFG)).
		Width(innerW).
		MaxWidth(innerW).
		Inline(true)

	parts := strings.Split(content, "\n")
	for i, p := range parts {
		parts[i] = gutter + lineStyle.Render(truncateLine(p, innerW))
	}
	return strings.Join(parts, "\n")
}

// isMsgSearchHit is true when search mode has a committed query and
// the message (from or text) contains it (case-insensitive).
func (m model) isMsgSearchHit(msg messageItem) bool {
	if m.searchQuery == "" {
		return false
	}
	return strings.Contains(strings.ToLower(msg.from+" "+msg.text), m.searchQuery)
}

// isStringSearchHit is the plain-string variant — used for channels
// and node callsigns.
func (m model) isStringSearchHit(s string) bool {
	if m.searchQuery == "" {
		return false
	}
	return strings.Contains(strings.ToLower(s), m.searchQuery)
}

// truncateLine cuts a pre-styled string to a visible-cell width. Drops
// ANSI escape sequences from the visible count so styled content
// doesn't get prematurely clipped.
func truncateLine(s string, maxW int) string {
	if lipgloss.Width(s) <= maxW {
		return s
	}
	// Walk the string honoring CSI escape sequences (ESC [ … m).
	var out strings.Builder
	var visible int
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			// copy through the 'm' terminator
			end := i + 2
			for end < len(s) && s[end] != 'm' {
				end++
			}
			if end < len(s) {
				end++
			}
			out.WriteString(s[i:end])
			i = end
			continue
		}
		r := []rune(s[i:])
		if len(r) == 0 {
			break
		}
		rw := lipgloss.Width(string(r[0]))
		if visible+rw > maxW-1 {
			out.WriteRune('…')
			// NOTE: deliberately no \x1b[0m here — we rely on the
			// caller's outer lipgloss style to reset at the end. An
			// embedded reset would kill the zebra/selected bg tint
			// for any padding rendered after the truncation point.
			return out.String()
		}
		out.WriteRune(r[0])
		visible += rw
		i += len(string(r[0]))
	}
	return out.String()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func resolveJump(to, n int) int {
	if to < 0 {
		return n - 1
	}
	return clamp(to, 0, n-1)
}

func toggleFlash(on bool, whenOn, whenOff string) string {
	if on {
		return whenOn
	}
	return whenOff
}

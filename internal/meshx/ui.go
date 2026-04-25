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
	"time"

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
// is active (channels / nodes / nearby / radar), it replaces the
// log. ESC always closes the overlay and returns to the input bar.
func (m model) renderIrssiBody(height int) string {
	switch m.overlay {
	case overlayChannels:
		return m.renderChannelsPane(m.w, height)
	case overlayNodes:
		return m.renderNodesPane(m.w, height)
	case overlayNearby:
		return m.renderNearbyPane(m.w, height)
	case overlayRadar:
		return m.renderRadarPane(m.w, height)
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
	var right string
	if m.mode == modeInput {
		// Byte counter lives in the mode badge while composing — the
		// status row is fixed-width and flush-right, so the counter
		// never gets pushed off-screen by long input text. Ramps
		// through five colors as the composition approaches the wire
		// cap so pressure is visible before you hit it.
		//
		// Counts BODY bytes only via wirePayloadBytes — the verb + any
		// target arg is meshx chrome that doesn't cost budget.
		// `/reply bubbingtenny2k ` reads 0/228 until the user starts
		// typing the actual reply body.
		n := wirePayloadBytes(m.input.Value())
		pct := float64(n) / float64(meshtasticMaxTextBytes)
		counterTxt := fmt.Sprintf("%d/%d", n, meshtasticMaxTextBytes)
		counterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
		switch {
		case n >= meshtasticMaxTextBytes:
			counterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(mhPink)).Bold(true)
		case pct >= 0.9:
			counterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(mhOrange)).Bold(true)
		case pct >= 0.75:
			counterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(mhYellow)).Bold(true)
		case pct >= 0.5:
			counterStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(mhFG))
		}
		right = counterStyle.Render(counterTxt) + " " +
			label.Render("["+modeTag+"]")
	} else {
		right = label.Render("[" + modeTag + "]")
	}
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
			"NAV · j/k · r reply · R resend · w whois · t trace · p ping · P pin · * star · ESC back to input · / search · ? help",
		)
		return " " + hint
	}
	// Input mode — default.
	// Input-bar channel prefix stays mesh-green — pink is reserved
	// for the highlighted active-channel tab in the status row above.
	// The byte counter lives in the [INPUT] badge on the top status
	// row so it stays visible regardless of composition length;
	// nothing here on the right.
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
		kv("/nearby", "distance-sorted peer roster (closest first; requires own GPS fix)"),
		kv("/radar", "polar scope — peers plotted by bearing + distance around you"),
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
		kv("R", "resend — retransmit a failed (✗) outbound row"),
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

// paneInnerWidth returns the content-area width inner renderers
// should target given a `width` argument from View(). One place to
// change the math instead of hunting down `width-4` literals.
func paneInnerWidth(width int) int {
	return width - 4
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
			m.renderChannelRow(
				c,
				i == m.selectedCh && m.focused == paneChannels,
				paneInnerWidth(width),
			),
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
	for i := range m.nodes {
		if m.nodes[i].currentState() == "online" {
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
	state := n.currentState()
	switch state {
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
	switch state {
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
	// Self-marker — the logged-in radio's sigil picks up the
	// magenta "me" color reserved in palette.go so users can
	// spot their own tile at a glance. Name keeps its normal
	// state-derived color so the tile reads like every other
	// row visually; the purple `@` is the sole signal that this
	// is you.
	if m.myNodeNum != 0 && n.nodeNum == m.myNodeNum {
		sigil = "@"
		sigilColor = mhMagenta
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
	naturalStart := tailStartList(m.messages, rowsFree)
	startIdx := naturalStart
	scrollback := m.selectedMsg < naturalStart
	if scrollback {
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
		case msg.status == "system" || msg.status == "notice":
			// Standalone `-!-` rows (storage notices, single-line
			// system messages) also pin to rowBgOdd so they read
			// the same shade as grouped system blocks above and
			// below them — mixing zebra parity into the system
			// stream made storage lines flicker between shades.
			bg = rowBgOdd
			lastGroup = 0
			groupBg = ""
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

		// Pin-corner boundaries — `⌜` goes on the first row of a
		// pinned group, `⌟` on the last. For singleton pinned rows
		// (group == 0) both are true so the row reads as self-
		// bracketed. Computing here rather than inside renderNoticeRow
		// keeps the renderer oblivious to message-list indices.
		pinFirst := false
		pinLast := false
		if msg.pinned {
			pinFirst = msg.group == 0 || i == 0 || m.messages[i-1].group != msg.group
			pinLast = msg.group == 0 || i+1 >= len(m.messages) || m.messages[i+1].group != msg.group
		}
		line := m.renderMessageRow(msg, isSelected, paneInnerWidth(width), bg, pinFirst, pinLast)
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
	// that total.
	//
	// Reply-threaded rows render as TWO visual lines (quote-line +
	// row, joined with "\n" inside one string). tailStartList +
	// naive `len(lines)` padding both treat each slice element as
	// one row, which under-counts the real row usage and makes the
	// pane overflow — that overflow bubbles through paneStyle's
	// Height limit, pushes the whole view up, and shears the top
	// status bar off the terminal top. Count visual rows instead
	// (1 + strings.Count(line, "\n")) for both the trim-to-fit and
	// the pad-up-to-fit passes.
	visualRows := func(ls []string) int {
		n := 0
		for _, l := range ls {
			n += 1 + strings.Count(l, "\n")
		}
		return n
	}
	// Overflow trim — different ends depending on which way the
	// viewport is anchored. When tail-pinned (the default, no
	// scrollback active) we drop the OLDEST rows so the newest stay
	// flush with the input bar. When the user has scrolled above
	// the tail with j/k / PgUp, the freshly-grown slice runs from
	// `startIdx` past the cursor toward the (now off-screen) tail;
	// trimming oldest there would yank the slice straight back to
	// the tail and visually undo the scroll. Trim from the END in
	// that case so the cursor + earlier-context rows stay on
	// screen and the (irrelevant) newer rows are what gets clipped.
	if scrollback {
		for visualRows(lines) > rowsFree && len(lines) > 2 {
			lines = lines[:len(lines)-1]
		}
	} else {
		for visualRows(lines) > rowsFree && len(lines) > 2 {
			lines = append(lines[:2:2], lines[3:]...)
		}
	}
	if pad := rowsFree - visualRows(lines); pad > 0 {
		rebuilt := make([]string, 0, len(lines)+pad)
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
	// selectionRowBg is the background for the currently-selected
	// row in nav mode. Deeper + more saturated than the zebra
	// shades so the cursor reads unambiguously across the whole
	// row width — the ██ gutter alone wasn't enough once the row
	// got crowded with timestamps, callsigns, and signal columns.
	// Chosen to be distinct from the searchHit bg (#0e2618) so a
	// selected-AND-hit row still picks one obvious state.
	selectionRowBg = "#2a4a5a"
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
// renderNoticeRow renders a status="notice" messageItem — a
// pre-styled colored info line using the exact same frame every
// other system row wears: 3-col wrapSelection gutter, lavender ▎
// accent, drained `   HH:MM  ` timestamp column. The only thing
// that differs from status="system" is the body: notice rows
// pass msg.text through verbatim (caller supplies lipgloss-styled
// text) instead of wrapping it with sys.Render's italic lavender.
// Result: a line that looks identical to /storage and /whois
// entries except the words themselves are colored. Used by the
// BitchX splash greeter and any future "say something in color
// without losing the chrome" moment.
func (m model) renderNoticeRow(
	msg messageItem,
	selected bool,
	inner int,
	rowBg string,
	pinFirst, pinLast bool,
) string {
	// When this row is the nav cursor, override the zebra bg with
	// the selection tint BEFORE any of the inner spans render. The
	// notice row pipeline bakes rowBg into every styled component
	// (accent, timestamp, prefix, body), and a later wrapSelection
	// Background() wouldn't override those nested ANSI codes — so
	// without this override the cursor wouldn't read on -!- rows
	// at all, only on plain chat rows where wrapSelection's bg
	// isn't fighting pre-baked spans.
	if selected {
		rowBg = selectionRowBg
	}

	// Default style — zero-value noticeStyle is the canonical
	// lavender italic system line. Letting msg.style be nil falls
	// back to this so callers without a style (legacy entry points
	// that haven't migrated to m.notice yet) still render sanely.
	style := noticeStyle{}
	if msg.style != nil {
		style = *msg.style
	}

	// Fade — lerp every fg on this row toward rowBg during the last
	// noticeFadeWindow before expiry. Frozen at 0 while the user is
	// in modeNav so a mid-scroll read doesn't dim under the cursor.
	// Pinned rows also get alpha=0 (computed inside noticeFadeAlpha).
	fade := 0.0
	if m.mode != modeNav {
		fade = noticeFadeAlpha(msg, time.Now())
	}
	lav := lerpHex(mhLavender, rowBg, fade)
	drn := lerpHex(mhDrained, rowBg, fade)
	bodyFg := style.fg
	if bodyFg == "" {
		bodyFg = mhLavender
	}
	bodyFg = lerpHex(bodyFg, rowBg, fade)

	accent := lipgloss.NewStyle().
		Foreground(lipgloss.Color(lav)).
		Background(lipgloss.Color(rowBg)).
		Bold(true).
		Render("▎") + lipgloss.NewStyle().Background(lipgloss.Color(rowBg)).Render(" ")
	tstamp := lipgloss.NewStyle().
		Foreground(lipgloss.Color(drn)).
		Background(lipgloss.Color(rowBg))
	// sys — the standard `-!-` chrome style. Lavender italic over
	// rowBg, identical to what systemLine has worn since day one.
	// Used for the prefix + center-pad so the left edge of every
	// notice row reads cohesive regardless of body color.
	sys := lipgloss.NewStyle().
		Foreground(lipgloss.Color(lav)).
		Background(lipgloss.Color(rowBg)).
		Italic(true)

	timeCol := "   " + msg.time + "  "
	if msg.time == "" {
		timeCol = "          "
	}
	// Pinned first-of-group corner — replace the leading space of
	// timeCol with `⌜`. Same 10-cell width, so alignment down the
	// pane is preserved. Rendered in meshGreen without fade so the
	// pin affordance stays at full brightness even if the row is
	// mid-fade (which shouldn't happen since pin pauses expiry, but
	// defensive).
	tsRendered := tstamp.Render(timeCol)
	if pinFirst && len(timeCol) > 0 {
		corner := lipgloss.NewStyle().
			Foreground(lipgloss.Color(meshGreen)).
			Background(lipgloss.Color(rowBg)).
			Bold(true).
			Render("⌜")
		tsRendered = corner + tstamp.Render(timeCol[1:])
	}

	// Fast path — default styling, no centering. One sys.Render
	// over the whole msg.text produces a single uninterrupted ANSI
	// span, which the terminal paints as one clean lavender-italic
	// band on rowBg. Every storage / whois / identified line lands
	// here.
	if style.fg == "" && !style.center && !style.bold {
		line := accent + tsRendered + sys.Render(msg.text)
		line = appendPinTail(line, inner, rowBg, pinLast)
		return wrapSelection(line, selected, false, inner, rowBg)
	}

	// Styled path — the body takes a custom fg / bold / center.
	// Split the literal "-!- " prefix off so it stays flush-left in
	// the standard sys style; only the content after it receives
	// the override styling. Keeping the prefix uniform across every
	// notice row is what makes the splash banner visually stack
	// with regular `-!-` lines.
	const prefix = "-!- "
	bodyContent := strings.TrimPrefix(msg.text, prefix)

	bodyStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(rowBg)).
		Foreground(lipgloss.Color(bodyFg))
	if style.fg == "" {
		bodyStyle = bodyStyle.Italic(true)
	}
	if style.bold {
		bodyStyle = bodyStyle.Bold(true)
	}
	styledBody := bodyStyle.Render(bodyContent)

	// center — pad leading sys-styled spaces so the body floats at
	// (inner - bw) / 2 in the pane. Fixed 19-cell prefix = gutter 3
	// + accent 2 + timeCol 10 + "-!- " 4.
	var pad string
	if style.center {
		const prefixCells = 19
		bw := lipgloss.Width(bodyContent)
		padLen := (inner-bw)/2 - prefixCells
		if padLen > 0 {
			pad = sys.Render(strings.Repeat(" ", padLen))
		}
	}

	line := accent + tsRendered + sys.Render(prefix) + pad + styledBody
	line = appendPinTail(line, inner, rowBg, pinLast)
	return wrapSelection(line, selected, false, inner, rowBg)
}

// appendPinTail pads `line` with rowBg-colored spaces and terminates
// with a meshGreen `⌟` at the rightmost content column when pinLast
// is true. Sized to `inner - gutterWidth - 1` so wrapSelection's
// Width() pass leaves the corner flush against the `║` pane frame.
// No-op when pinLast is false.
func appendPinTail(line string, inner int, rowBg string, pinLast bool) string {
	if !pinLast {
		return line
	}
	innerW := inner - gutterWidth
	curW := lipgloss.Width(line)
	padN := innerW - curW - 1
	if padN < 0 {
		padN = 0
	}
	fill := lipgloss.NewStyle().
		Background(lipgloss.Color(rowBg)).
		Render(strings.Repeat(" ", padN))
	corner := lipgloss.NewStyle().
		Foreground(lipgloss.Color(meshGreen)).
		Background(lipgloss.Color(rowBg)).
		Bold(true).
		Render("⌟")
	return line + fill + corner
}

func (m model) renderMessageRow(
	msg messageItem,
	selected bool,
	inner int,
	rowBg string,
	pinFirst, pinLast bool,
) string {
	// "notice" rows are pre-styled colored info lines — the splash
	// greeter on launch, future connection banners, anything that
	// wants to say something in color without getting flattened by
	// the default system-row lavender wrap. See renderNoticeRow.
	if msg.status == "notice" || msg.status == "system" {
		return m.renderNoticeRow(msg, selected, inner, rowBg, pinFirst, pinLast)
	}
	// Same selection-bg override as renderNoticeRow: every styled
	// span below bakes rowBg into its ANSI escape, so wrapSelection's
	// outer Background() can't win against the nested codes. Swap
	// rowBg for the selection tint at the TOP of the render so every
	// downstream span picks it up natively.
	if selected {
		rowBg = selectionRowBg
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
	shortName := ""
	if msg.mine {
		fromRaw = m.myCallsign()
		shortName = m.myShortName()
	} else {
		if idx, ok := m.nodesByNum[msg.fromNum]; ok && idx >= 0 && idx < len(m.nodes) {
			shortName = m.nodes[idx].shortName
		}
		if strings.HasPrefix(fromRaw, "node 0x") {
			fromRaw = "👻 " + fromRaw
		}
	}
	// Avatar-style shortname prefix — "[SHORT] longname". Matches the
	// official Meshtastic app convention of leading each message with
	// a shortname badge so the community practice of addressing peers
	// by short_name in the body ("70F8 your hop count keeps going up")
	// is resolvable at a glance. Skip the brackets entirely when we
	// don't have a shortname yet (ghost peer pre-NodeInfo, or our own
	// radio before /tag has been run) so the column never carries
	// empty [    ] chrome fighting the longname for space.
	if shortName != "" {
		fromRaw = "[" + shortName + "] " + fromRaw
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
	// Right-aligned metric columns so the numbers always end at
	// the same column regardless of digit count or sign. "↝ 6h"
	// and "↝12h" both end at the same "h" position; "9.5dB" and
	// "-12.2dB" both end at the same "B" position. Left-padding
	// the value rather than right-padding is the key — humans
	// read numbers from the right edge, so aligning units is
	// what actually lets you compare rows at a glance.
	const hopColW = 7 // "↝%3dh" = 5 cells value + 2 trailing gap
	const snrColW = 8 // "%6sdB" = 8 cells total
	var hopCol string
	switch {
	case msg.mine:
		hopCol = strings.Repeat(" ", hopColW)
	case msg.hops > 0:
		// "↝%3dh  " — 3-digit hop field keeps 1/10/100 flush.
		hopCol = fmt.Sprintf("↝%3dh  ", msg.hops)
	default:
		// "↝  dx  " — "dx" (direct, 0 hops) lives in the same
		// 3-cell value region as hop numbers above.
		hopCol = "↝  dx  "
	}
	snrCol := strings.Repeat(" ", snrColW)
	if msg.snr != "" {
		// "%6s" right-aligns the SNR value in 6 cells, then
		// "dB" adds 2 → 8 total. "9.5" → "   9.5dB", "-12.2" →
		// " -12.2dB", "10.2" → "  10.2dB" — the "B" lands at the
		// same column every time.
		snrCol = fmt.Sprintf("%6sdB", msg.snr)
	}
	// statusCol — per-message delivery state co-located with the row.
	//   "…"  pending: sent, awaiting Routing reply from the radio
	//   "✓"  ack:     radio acknowledged delivery
	//   "✗"  fail:    Routing returned a non-NONE error reason
	//   " "  empty:   inbound message (no delivery state to show)
	// Persistent across scrollback + unique per row so multiple
	// in-flight messages each carry their own indicator.
	// One-cell status glyph. Gap to the SNR column is assembled
	// separately below so columns stay independent — treating
	// the gap as statusCol's problem mixes chrome into content.
	statusCol := " "
	switch msg.status {
	case "pending":
		statusCol = "…"
	case "ack":
		statusCol = "✓"
	case "fail":
		statusCol = "✗"
	}
	// Status-column gap — 1 cell of breathing room between SNR
	// and the state glyph so "-0.5dB✓" doesn't collide. The gap
	// is its own span on the same rowBg tint so it blends, not
	// part of statusCol (column chrome stays separate from
	// column content).
	statusGap := lipgloss.NewStyle().Background(lipgloss.Color(rowBg)).Render(" ")
	right := hop.Render(hopCol) + hop.Render(snrCol) + statusGap + func() string {
		switch msg.status {
		case "pending":
			return tstamp.Render(statusCol) // dim drained — in flight
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
	//
	// Multi-line messages (radios can embed \n — e.g. "End of Day
	// Report:\nMax Power: …") lay out with the signal columns on
	// the FIRST visual line only. Continuation lines hang under
	// the text column so the right edge stays aligned with every
	// other row in the log. Collapsing to one line would hide
	// content; padding every continuation line to carry metrics
	// looks weird (~4h floating three lines below the callsign).
	bodyLines := strings.Split(msg.text, "\n")
	if len(bodyLines) == 0 {
		bodyLines = []string{""}
	}
	firstStyled := text.Render(padOrTruncate(bodyLines[0], textW))
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
		firstStyled

	row := left + right

	// Continuation lines (line 2+) hang under the text column.
	// Carry the same sender-color ▎ accent as the first line so the
	// color bar reads as one tall strip spanning the whole message
	// group — easier to scan "where does this message end" when a
	// solar-node end-of-day report runs three or four lines deep.
	// hangIndent covers leftFixed-2 cells because `accent` already
	// occupies the first two. contW = textW + rightW so the tinted
	// bg reaches the full row width; without it, wrapSelection's
	// per-line truncation + pad would leave a ragged right edge on
	// multi-line rows.
	if len(bodyLines) > 1 {
		hangIndent := gapStyle.Render(strings.Repeat(" ", leftFixed-2))
		contW := textW + rightW
		if contW < textW {
			contW = textW
		}
		for _, bl := range bodyLines[1:] {
			cont := accent + hangIndent + text.Render(padOrTruncate(bl, contW))
			row += "\n" + cont
		}
	}
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
//
// Own ("mine") rows go through myCallsign() so rows sent BEFORE
// MyNodeInfo arrived (persisted with from="—" or from="me") also
// upgrade to the real callsign as soon as we learn it. Without
// this the first BLE session's outbound history would stay stuck
// on the placeholder forever.
func (m model) displayFrom(msg messageItem) string {
	if msg.mine {
		if cs := m.myCallsign(); cs != "" && cs != "—" {
			return cs
		}
		return msg.from
	}
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

	// No marker — just keep the 3-col left pad for alignment. If a
	// rowBg was provided, force every line to the full inner width
	// with that bg so the tint covers the whole row (no drop-off
	// at the right).
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
		// Stronger tint than the old mhDrained. That value was
		// #3b4261 which read almost identical to zebra-odd (#24283b)
		// and made the ██ gutter carry all the "I am here" weight
		// on its own. Bumping to a saturated dark-teal so the whole
		// row reads as selected from any column, not just the left
		// gutter. Still dark enough that text stays readable.
		bg = selectionRowBg
	} else {
		gutter = lipgloss.NewStyle().
			Foreground(lipgloss.Color(meshGreen)).
			Bold(true).
			Render("│ ") + " "
		bg = "#0e2618" // dim neon-green background for a subtle row pop
	}

	// Width + MaxWidth clamp to exactly innerW so the bg tint covers
	// the whole row — no more, no less. Prevents terminal auto-wrap
	// from punching holes in the highlight block.
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

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
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// View composes the screen as a Component tree: a VStack of the chrome
// regions wrapped around a body region. The frame box is m.w-1 wide
// (NOT m.w) to dodge terminal pending-wrap (DECAWM auto-margin) — no
// component is ever asked to render content into the very last column,
// which is the architectural fix for the duplicate-input-row bug class.
//
// Each child of the VStack is sized explicitly:
//
//   - status:   1 row
//   - divider:  1 row
//   - body:     -1 (flex; takes whatever's left after the others)
//   - chanRow:  1 row
//   - inputBar: 1 row
//
// Components are responsible for filling their allocated Box exactly.
// Row + Cell + padCells in box.go enforce that contract; nothing here
// has to compute m.w-N math by hand.
func (m model) View() string {
	if m.w == 0 || m.h == 0 {
		return ""
	}
	out := frameView(m).Render(Box{Width: m.w - 1, Height: m.h})
	if path := os.Getenv("MESHX_DEBUG_VIEW"); path != "" {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
			lines := strings.Split(out, "\n")
			for i, l := range lines {
				_, _ = fmt.Fprintf(f, "[%2d] w=%d %q\n", i, ansi.StringWidth(l), ansi.Strip(l))
			}
			_ = f.Close()
		}
		_ = os.Unsetenv("MESHX_DEBUG_VIEW")
	}
	return out
}

// frameView builds the top-level VStack for a frame. Body is a
// ComponentFunc that delegates to renderIrssiBody / renderHelpView
// — those still produce a complete bordered pane string of exactly
// the requested size, so they slot into the layout untouched. A
// follow-up will convert them to native Components with explicit
// cell budgets per row, which is when message rows become real
// fixed-slot cards.
func frameView(m model) Component {
	body := ComponentFunc(func(box Box) string {
		var s string
		switch m.mode {
		case modeHelp:
			s = m.renderHelpView(box.Width, box.Height)
		default:
			s = m.renderIrssiBody(box.Width, box.Height)
		}
		return fitToBox(s, box)
	})
	// Single trailing Spacer below the input bar keeps the cursor /
	// typed text off the very last terminal row so it isn't jammed
	// against the tmux / iTerm status bar. The chanRow sits directly
	// against the bottom pane border — no spacer between body and
	// channelTabsRow because that gap reads as wasted vertical real
	// estate, not breathing room.
	return VStack{Children: []SizedChild{
		{Comp: statusBar{m: m}, Size: 1},
		{Comp: topDivider{}, Size: 1},
		{Comp: body, Size: -1},
		{Comp: channelTabsRow{m: m}, Size: 1},
		{Comp: inputBar{m: m}, Size: 1},
		{Comp: Spacer{}, Size: 1},
	}}
}

// fitToBox normalizes a pre-rendered pane string to exactly box.Width
// × box.Height. Right-pads short lines with spaces, truncates over-
// width lines, drops over-tall content, appends blank lines for
// short content. The body component renderers (renderIrssiBody etc.)
// produce strings whose dimensions they computed from box.Height /
// the legacy width arg; this function ensures they slot into the
// VStack contract regardless. Once the body renderers are converted
// to native Components, fitToBox can be deleted.
func fitToBox(s string, box Box) string {
	lines := strings.Split(s, "\n")
	out := make([]string, box.Height)
	for i := 0; i < box.Height; i++ {
		var line string
		if i < len(lines) {
			line = padCells(lines[i], box.Width)
		} else {
			line = strings.Repeat(" ", box.Width)
		}
		out[i] = line
	}
	return strings.Join(out, "\n")
}

// renderIrssiBody — main log takes the whole width. When an overlay
// is active (channels / nodes / nearby / radar), it replaces the
// log. ESC always closes the overlay and returns to the input bar.
// renderIrssiBody renders the body region at exactly width × height.
// Width comes from the parent VStack's Box, NOT m.w — that's the
// indirection that lets the global frame box (m.w - 1, the safeW
// margin) propagate down so no row hits the right edge.
func (m model) renderIrssiBody(width, height int) string {
	switch m.overlay {
	case overlayChannels:
		return m.renderChannelsPane(width, height)
	case overlayNodes:
		return m.renderNodesPane(width, height)
	case overlayNearby:
		return m.renderNearbyPane(width, height)
	case overlayRadar:
		return m.renderRadarPane(width, height)
	default:
		return m.renderMessagesPane(width, height)
	}
}

// renderHelpView draws a full-pane help overlay listing every keybind
// and every `:` command, organized by category. Any key dismisses it.
// Width parameter takes the parent VStack's Box.Width so the help
// pane stays within the safeW frame.
func (m model) renderHelpView(width, height int) string {
	head := lipgloss.NewStyle().
		Foreground(lipgloss.Color(meshGreen)).
		Bold(true)
	sec := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhCyan)).
		Bold(true).
		Underline(true)
	dim := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained))

	// Width budget for kv lines = pane width - frame (2) - padding (6).
	// Routes through helpKVLine so the cell math (key column 14 cells,
	// description as the flex slot) lives in components_overlays.go.
	kvW := width - 2 - 6
	if kvW < 30 {
		kvW = 30
	}
	kv := func(k, d string) string {
		return helpKVLine(k, d, 14, kvW)
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

	// Wrap in Bordered so the help overlay obeys the same component-
	// tree contract as every other pane: an inner box exactly sized
	// to width × height minus the frame, padded via ansiCells (no
	// runewidth surprise on keycap/zwj content). Padding [1,3,1,3] —
	// 1 row top/bottom, 3 cols left/right — matches the original
	// lipgloss Padding(1,3).
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen))
	innerComp := ComponentFunc(func(box Box) string {
		ls := strings.Split(strings.Join(viewLines, "\n"), "\n")
		out := make([]string, box.Height)
		for i := 0; i < box.Height; i++ {
			if i < len(ls) {
				out[i] = padCells(ls[i], box.Width)
			} else {
				out[i] = strings.Repeat(" ", box.Width)
			}
		}
		return strings.Join(out, "\n")
	})
	return Bordered{
		Inner:       innerComp,
		Chars:       DoubleBorder,
		BorderStyle: style,
		Padding:     [4]int{1, 3, 1, 3},
	}.Render(Box{Width: width, Height: height})
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

// renderBorderedPane wraps pre-rendered inner content (each line
// already padded to width-4 cells per ansiCells) in the same ║/═
// frame paneStyle draws — but using our Bordered Component instead
// of lipgloss.Style.Width() / Padding(). Lipgloss measures with
// runewidth, which under-counts keycap emoji and VS16-promoted glyphs
// by 1 cell; when lipgloss then pads its inner content to its
// declared Width, the keycap row ends up 1 visual cell wider than
// the box and the right ║ frame walks out of column. Bordered uses
// ansiCells (keycap-aware) for every padding decision, so a row
// containing "7️⃣" lands the right ║ at the same column as a row of
// plain text. This is the architectural promise — no overflow ever.
func renderBorderedPane(
	inner string, width, height, paneIdx int, focused bool,
) string {
	chars := NormalBorder
	color := mhDrained
	if focused {
		chars = DoubleBorder
		color = paneAccentColor(paneIdx)
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	innerComp := ComponentFunc(func(box Box) string {
		lines := strings.Split(inner, "\n")
		out := make([]string, box.Height)
		for i := 0; i < box.Height; i++ {
			if i < len(lines) {
				out[i] = padCells(lines[i], box.Width)
			} else {
				out[i] = strings.Repeat(" ", box.Width)
			}
		}
		return strings.Join(out, "\n")
	})
	return Bordered{
		Inner:       innerComp,
		Chars:       chars,
		BorderStyle: style,
		Padding:     [4]int{1, 1, 1, 1},
	}.Render(Box{Width: width, Height: height})
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
	return renderBorderedPane(
		strings.Join(lines, "\n"),
		width, height, paneChannels, m.focused == paneChannels,
	)
}

func (m model) renderChannelRow(c channelItem, selected bool, inner int) string {
	contentW := inner - gutterWidth
	if contentW < 1 {
		contentW = 1
	}
	row := channelRowLine(c.name, c.private, c.unread, contentW)
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

	return renderBorderedPane(
		strings.Join(lines, "\n"),
		width, height, paneNodes, m.focused == paneNodes,
	)
}

// renderUserCell renders one [ @callsign ] bracketed cell via the
// userCellLine Component, which owns the entire styling switch
// (sigil + colors + bold) through nodePresentationFor — same source
// of truth /nearby uses, so the BitchX users grid and the distance
// roster always render the same peer state with the same chrome.
func (m model) renderUserCell(n nodeItem, selected bool, cellW int) string {
	isSelf := m.myNodeNum != 0 && n.nodeNum == m.myNodeNum
	cell := userCellLine(n, isSelf, selected, cellW)
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
		// Pre-sync placeholder: applyChannel labels the firmware
		// PRIMARY (empty-name LongFast) channel as "#default" once
		// the radio's ChannelInfo packet arrives, so use the same
		// label here. Avoids the header flashing "#PRIMARY (271
		// msgs)" during the NodeDB drain on a freshly-attached BLE
		// connection — that name doesn't match what any other pane
		// will show a moment later.
		chanName = "#default"
	}
	header := paneHeader(strings.ToUpper(chanName), paneMessages, m.focused == paneMessages)
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Render(fmt.Sprintf("  (%d msgs)", len(m.messages)))

	// Total content budget inside the bordered pane: height − 2
	// (border) − 2 (Padding(1,1)). The pane fills exactly that many
	// lines; anything less and the lipgloss frame stretches with
	// trailing blanks before the bottom ╚══╝ — those blank rows are
	// what looks like dead space at the bottom of the messages pane.
	contentRows := height - 4
	if contentRows < 3 {
		contentRows = 3
	}
	// Two of the content rows go to the pane header + blank separator
	// below it; the rest is the message-row budget tailStartList uses
	// when deciding how many messages to keep on screen.
	rowsFree := contentRows - 2
	if rowsFree < 1 {
		rowsFree = 1
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
		// Pin the cursor at the TOP of the viewport during scrollback
		// so each `k` scrolls the message list up by exactly one row.
		// The previous "rowsFree/3" placement jumped the cursor 1/3
		// down the viewport the instant scrollback activated, which
		// looked like `k` snapping the screen by ~7 rows on the first
		// press and then doing nothing on the next several presses
		// — `j/k` behaved as gross-step scrolls, while `Ctrl+U` and
		// `G` "felt" like the only working scroll keys. With the
		// cursor pinned at row 0, a single `k` reveals exactly one
		// earlier message and the viewport advances 1-for-1 with the
		// keystroke, which is what irssi/mutt/vim users expect.
		startIdx = m.selectedMsg
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
		// Route through the messageRow Component so the row's output
		// is forced through the layout contract (every line padded to
		// paneInnerWidth, total visual rows padded to the precomputed
		// visual height). The legacy renderMessageRow + dimRow path
		// is preserved INSIDE the component, but the cell-width
		// guarantee now lives at the Component boundary instead of
		// being scattered across each pane caller.
		row := messageRow{
			m: m, msg: msg, selected: isSelected, rowBg: bg,
			pinFirst: pinFirst, pinLast: pinLast, faded: faded,
			rowsInner: paneInnerWidth(width),
		}
		h := messageRowVisualHeight(msg)
		line := row.Render(Box{Width: paneInnerWidth(width), Height: h})
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
	// Trim/pad against the FULL content budget (header + separator
	// + messages), not just the message budget. Comparing against
	// rowsFree alone leaves the pane 2 rows under capacity and
	// renders 2 trailing blanks before ╚══╝ — the dead space the
	// user reported as "lots of unused spacing at the bottom".
	if scrollback {
		for visualRows(lines) > contentRows && len(lines) > 2 {
			lines = lines[:len(lines)-1]
		}
	} else {
		for visualRows(lines) > contentRows && len(lines) > 2 {
			lines = append(lines[:2:2], lines[3:]...)
		}
	}
	if pad := contentRows - visualRows(lines); pad > 0 {
		rebuilt := make([]string, 0, len(lines)+pad)
		rebuilt = append(rebuilt, lines[:2]...) // preserve header + separator
		for i := 0; i < pad; i++ {
			rebuilt = append(rebuilt, "")
		}
		rebuilt = append(rebuilt, lines[2:]...)
		lines = rebuilt
	}
	return renderBorderedPane(
		strings.Join(lines, "\n"),
		width, height, paneMessages, m.focused == paneMessages,
	)
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
	if selected {
		rowBg = selectionRowBg
	}
	style := noticeStyle{}
	if msg.style != nil {
		style = *msg.style
	}
	fade := 0.0
	if m.mode != modeNav {
		fade = noticeFadeAlpha(msg, time.Now())
	}
	bodyFg := style.fg
	if bodyFg == "" {
		bodyFg = mhLavender
	}
	bodyFg = lerpHex(bodyFg, rowBg, fade)
	lav := lerpHex(mhLavender, rowBg, fade)

	parts := noticeRowFor(rowBg, msg.time, pinFirst, pinLast, fade)
	contentW := inner - gutterWidth
	if contentW < 20 {
		contentW = 20
	}

	sys := lipgloss.NewStyle().
		Foreground(lipgloss.Color(lav)).
		Background(lipgloss.Color(rowBg)).
		Italic(true)

	// Fast path — default styling: one sys.Render over the whole
	// msg.text gives the terminal a single uninterrupted ANSI span,
	// painted as one clean lavender-italic band. Every storage /
	// whois / identified line lands here.
	if style.fg == "" && !style.center && !style.bold {
		body := sys.Render(msg.text)
		line := noticeRowLine(parts, body, contentW)
		return wrapSelection(line, selected, false, inner, rowBg)
	}

	// Styled path — body takes a custom fg / bold / center. Split
	// the "-!- " prefix off so it stays flush-left in the standard
	// sys style; only the content after it receives override styling.
	// Keeping the prefix uniform across every notice row is what
	// makes the splash banner visually stack with regular `-!-`.
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
	styled := bodyStyle.Render(bodyContent)

	// `-!-` is ALWAYS anchored at the leftmost body chrome column —
	// never floats. style.center only changes the alignment of the
	// content AFTER the prefix: the prefix gets its own fixed-width
	// cell in noticeRowLineSplit, and the content cell takes
	// Align: AlignCenter so the art body-cell-centers in the space
	// to the right of the prefix while the prefix stays put.
	if style.center {
		line := noticeRowLineSplit(
			parts, sys.Render(prefix), styled, AlignCenter, contentW,
		)
		return wrapSelection(line, selected, false, inner, rowBg)
	}
	body := sys.Render(prefix) + styled
	line := noticeRowLine(parts, body, contentW)
	return wrapSelection(line, selected, false, inner, rowBg)
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
	contentW := inner - gutterWidth
	if contentW < 40 {
		contentW = 40
	}

	// System messages — single-line. Multi-line blocks are emitted
	// as multiple messageItems sharing a `group` ID; the pane loop
	// binds them visually by reusing the same zebra bg. We route
	// through the same noticeRowLine the `-!-` notice path uses, so
	// the "left chrome (accent + time) → body (flex) → pin tail"
	// column structure is one Component for both row types.
	if msg.status == "system" {
		// Continuation rows in a system block (e.g. /whois block,
		// /config block) hide the timestamp so only the header row
		// carries it — makes the block read as one card.
		timeCell := msg.time
		if msg.group != 0 && !strings.HasPrefix(msg.text, "-!- whois") &&
			!strings.HasPrefix(msg.text, "-!- config") &&
			!strings.HasPrefix(msg.text, "-!- env") &&
			!strings.HasPrefix(msg.text, "-!- ping") &&
			!strings.HasPrefix(msg.text, "-!- traceroute") {
			timeCell = ""
		}
		parts := noticeRowFor(rowBg, timeCell, false, false, 0)
		sys := lipgloss.NewStyle().
			Foreground(lipgloss.Color(mhLavender)).
			Background(lipgloss.Color(rowBg)).
			Italic(true)
		// Ghost-glyph special case — systemBlock lines whose body
		// contains "👻 " render the glyph in muted drained while
		// the prose stays in the regular sys (lavender italic)
		// style. Splitting lets each half render with its own
		// style, concatenated on the same zebra bg.
		body := msg.text
		var bodyStyled string
		if idx := strings.Index(body, "👻 "); idx >= 0 {
			ghost := lipgloss.NewStyle().
				Foreground(lipgloss.Color(mhDrained)).
				Background(lipgloss.Color(rowBg))
			pre := body[:idx]
			post := body[idx+len("👻 "):]
			bodyStyled = sys.Render(pre) +
				ghost.Render("👻") +
				sys.Render(" "+post)
		} else {
			bodyStyled = sys.Render(body)
		}
		line := noticeRowLine(parts, bodyStyled, contentW)
		return wrapSelection(line, selected, m.isMsgSearchHit(msg), inner, rowBg)
	}

	// Regular chat row — the entire visual structure lives in the
	// chatRow Component family (components_chat.go). chatRowFor
	// computes the per-cell styled strings (accent, flag, time,
	// sender, hop, snr, status); chatRowMainLine stitches them with
	// the body cell via Row{Cells:...}. No more inline string concat
	// for the dense case.
	parts := chatRowFor(m, msg, rowBg)
	bodyLines := strings.Split(msg.text, "\n")
	if len(bodyLines) == 0 {
		bodyLines = []string{""}
	}
	// Corrupted bodies — sanitizeMessageText replaced bad bytes with
	// '?' and dropped non-printable runes, so the text is still
	// readable but no longer trustworthy. Re-style in dim lavender
	// italic and prefix "(?) " so the user sees "this row had
	// garbage in it" without us throwing away the salvageable chars.
	bodyText := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhFG)).
		Background(lipgloss.Color(rowBg))
	bodyForFirst := bodyLines[0]
	if msg.corrupted {
		bodyText = lipgloss.NewStyle().
			Foreground(lipgloss.Color(mhLavender)).
			Background(lipgloss.Color(rowBg)).
			Italic(true)
		bodyForFirst = "(?) " + bodyForFirst
	}
	sys := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhLavender)).
		Background(lipgloss.Color(rowBg)).
		Italic(true)
	row := chatRowMainLine(parts, bodyForFirst, bodyText, contentW)

	// Continuation lines, ack subline, and threading-quote header all
	// flow through Row{Cells} now via the chat* helpers in
	// components_chat.go — same Component primitive that builds the
	// first line. Each helper produces exactly contentW cells per
	// ansiCells, so the whole row is a vertical stack of guaranteed-
	// width lines and wrapSelection just adds the gutter.
	if len(bodyLines) > 1 {
		for _, bl := range bodyLines[1:] {
			row += "\n" + chatContinuationLine(parts, bl, bodyText, contentW)
		}
	}
	if msg.acks != "" {
		row += "\n" + chatAckLine(parts, msg.acks, sys, contentW)
	}
	if msg.replyID != 0 {
		if parent := m.findMessageByPacketID(msg.replyID); parent != nil {
			row = chatThreadingQuote(
				m.displayFrom(*parent), parent.time, parent.text,
				rowBg, contentW,
			) + "\n" + row
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
// senderUnresolved reports whether the message's sender is a peer
// we've only synthesized a firmware-default callsign for (no real
// NodeInfo has arrived). Used by the row renderer to dim the FROM
// column + accent tick and prepend the 👻 marker. Own messages and
// rows with no fromNum (demo seeds, system rows) are never flagged.
func (m model) senderUnresolved(msg messageItem) bool {
	if msg.mine || msg.fromNum == 0 {
		return false
	}
	idx, ok := m.nodesByNum[msg.fromNum]
	if !ok || idx < 0 || idx >= len(m.nodes) {
		return false
	}
	return m.nodes[idx].unresolved
}

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
	// Belt-and-suspenders for any path that bypassed ghost backfill
	// (race, future code) — synthesize the firmware default from
	// fromNum so the row never shows the legacy "node 0x<hex>"
	// string we used to bake into the SQLite from column.
	long, _ := defaultCallsign(msg.fromNum)
	return long
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
//
// Uses charmbracelet/x/ansi.StringWidth for measurement — that's the
// SAME library bubbletea's diff renderer, lipgloss/cellbuf word-wrap,
// and the standard-renderer's EraseLineRight check all use, so our
// padding stays self-consistent with everyone downstream. uniseg
// (and runewidth) under-count VS16-promoted keycaps "2️⃣ 6️⃣ ⚠️"
// which the terminal AND ansi render as 2 cells — using uniseg here
// shaved one cell off every keycap row, sliding the right-aligned
// metrics column left by one and breaking the bordered box layout.
//
// Iterates by grapheme cluster so a single emoji is never split: no
// chopping the digit out of a 2️⃣ keycap, no severing skin-tone
// modifiers, no halving 🙋🏼‍♂️ ZWJ sequences.
func padOrTruncate(s string, w int) string {
	// Funnel through padCells so every measurement applies the keycap
	// correction (VS16 / U+20E3 promote to 2 cells per Unicode TR51).
	// Without it, "7️⃣" measures 1 by ansi.StringWidth but renders as
	// 2 in every modern terminal, so padded rows visually overflow by
	// 1 column on keycap messages and the right ║ frame walks left.
	return padCells(s, w)
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
			// Pad/truncate the line to exactly innerW cells using our
			// keycap-corrected ansiCells measurement. Going through
			// lipgloss.NewStyle().Width(innerW) instead introduces a
			// lipgloss/runewidth measurement that under-counts keycap
			// emoji ("7️⃣" reads as 1 cell while every modern terminal
			// renders 2), so a row whose body contains a keycap ends
			// up 1 visual cell wider than the frame and the right ║
			// border walks out of column. padCells is the authoritative
			// measurement for the layout pipeline.
			//
			// Tail-pad coloring: each inner styled span ends in `\e[0m`
			// which resets EVERYTHING including any outer bg wrapper, so
			// `lipgloss.Background().Render(line)` doesn't actually tint
			// the trailing spaces — they end up plain and the zebra row
			// reads as a styled fragment with a "drop-off" to terminal
			// background past the body's last character. Solution: emit
			// the trailing pad with its OWN explicit bg-styled span so
			// the row is a solid rectangle from gutter to right ║ frame.
			cur := ansiCells(p)
			line := p
			if cur > innerW {
				line = padCells(p, innerW)
			} else if cur < innerW && neutralBg != "" {
				tail := strings.Repeat(" ", innerW-cur)
				tail = lipgloss.NewStyle().
					Background(lipgloss.Color(neutralBg)).
					Render(tail)
				line = p + tail
			} else if cur < innerW {
				line = p + strings.Repeat(" ", innerW-cur)
			}
			parts[i] = pad + line
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

	// Bg tint covers the whole row — no more, no less. We pad to
	// exactly innerW cells via padCells (keycap-aware) BEFORE handing
	// to lipgloss, so the lipgloss style only paints; it does not
	// resize. Going through Width()+MaxWidth() instead lets lipgloss's
	// runewidth-based measurement undercount keycap emoji and overpad
	// the row by 1 cell, which lands the right ║ frame outside the
	// column on rows whose body contains a keycap.
	lineStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(bg)).
		Foreground(lipgloss.Color(mhFG))

	parts := strings.Split(content, "\n")
	for i, p := range parts {
		parts[i] = gutter + lineStyle.Render(padCells(p, innerW))
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

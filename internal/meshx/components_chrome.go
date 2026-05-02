// Copyright (c) 2026 John Dewey
//
// Chrome components — top status bar, divider, channel-tabs row, and
// input bar. Each is a struct that captures the model data it needs
// and implements Component.Render(box). All cell-width math goes
// through Row + Cell so overflow is impossible: the parent's Box
// budget is the contract, not a guideline.
//
// Replaces the ad-hoc renderStatusBar / renderTopDivider /
// renderChannelStatus / renderInputRow renderers. Those scattered
// width literals (m.w-2, m.w-4, m.w-1-prefix-1, the `\e[K` line-erase
// at end-of-status) are the bug class this refactor exists to kill —
// here, every region asks the layout for its budget and the Row
// component truncates anything that would overflow.

package meshx

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// statusBar renders the top status bar with brand + radio telemetry
// segments. Width is whatever the parent gives via Box; segments are
// dropped from the middle when over budget so brand (left) and state
// (right) always stay visible.
type statusBar struct {
	m model
}

// Render fills box with one styled status row.
func (s statusBar) Render(box Box) string {
	m := s.m
	call := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen)).Bold(true)
	label := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	val := lipgloss.NewStyle().Foreground(lipgloss.Color(mhCyan)).Bold(true)
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(mhYellow)).Bold(true)
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color(mhGreen)).Bold(true)
	pink := lipgloss.NewStyle().Foreground(lipgloss.Color(mhPink)).Bold(true)
	demoTag := lipgloss.NewStyle().Foreground(lipgloss.Color(mhOrange)).Bold(true)
	chrome := mhDrained

	var segs []string

	// Segment 1: brand mark + callsign, both in mesh-green.
	brand := call.Render(`//\`) + "  "
	if sn := m.myShortName(); sn != "" {
		brand += call.Render(sn) + " " + call.Render(m.myCallsign())
	} else {
		brand += call.Render(m.myCallsign())
	}
	segs = append(segs, statusSegment(brand, chrome))

	// Hardware + firmware.
	n := m.myNode()
	hw := "—"
	if n != nil && n.hwModel != "" {
		hw = n.hwModel
	}
	fw := shortFirmware(m.radioFirmware)
	segs = append(segs, statusSegment(
		label.Render("⌂ ")+val.Render(hw)+"  "+label.Render("⚙ ")+val.Render(fw),
		chrome,
	))

	// Channel + modem preset.
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

	// TX power.
	tx := "—"
	if m.radioTxPower != 0 {
		tx = fmt.Sprintf("%d dBm", m.radioTxPower)
	}
	segs = append(segs, statusSegment(label.Render("⟐ ")+warn.Render(tx), chrome))

	// Battery.
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

	if m.radioRole != "" {
		segs = append(segs, statusSegment(label.Render("⌖ ")+val.Render(m.radioRole), chrome))
	}
	if m.radioRegion != "" {
		segs = append(segs, statusSegment(label.Render("⌘ ")+val.Render(m.radioRegion), chrome))
	}
	if m.myGrid != "" {
		segs = append(segs, statusSegment(label.Render("☖ ")+val.Render(m.myGrid), chrome))
	}
	segs = append(
		segs,
		statusSegment(label.Render("⚭ ")+val.Render(fmt.Sprintf("%d", len(m.nodes))), chrome),
	)

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

	// Drop middle segments until the joined content fits the budget,
	// preserving brand (first) + state (last). Use the same library
	// (ansi.StringWidth via padCells) the renderer downstream uses,
	// so what fits here also fits in the wire output.
	content := strings.Join(segs, "")
	for cells(content) > box.Width-2 && len(segs) > 2 {
		mid := len(segs) / 2
		if mid == 0 || mid == len(segs)-1 {
			break
		}
		segs = append(segs[:mid], segs[mid+1:]...)
		content = strings.Join(segs, "")
	}

	// Single-row Row: leading space + content + flex pad. Row will
	// truncate or pad as needed to fit box.Width exactly.
	row := Row{Cells: []Cell{
		{Content: " " + content + " ", Width: -1},
	}}
	return row.Render(Box{Width: box.Width, Height: 1})
}

// topDivider is the full-width double-line ruler that separates the
// status bar from the body.
type topDivider struct{}

// Render fills box with one ═══...═══ row, padded/truncated to width.
func (topDivider) Render(box Box) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen))
	bar := strings.Repeat("═", box.Width)
	return style.Render(bar)
}

// channelTabCell renders one channel tab in the bottom chanRow:
// active tabs are bracketed in pink; inactive in dim lavender. An
// unread > 0 emits a yellow `(N)` badge (orange `(N!)` for private
// channels — encrypted DMs and secret rooms get a louder cue).
func channelTabCell(name string, idx int, active, private bool, unread int) string {
	activeTab := lipgloss.NewStyle().Foreground(lipgloss.Color(mhPink)).Bold(true)
	activeBracket := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	other := lipgloss.NewStyle().Foreground(lipgloss.Color(mhLavender))
	otherIdx := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	unreadStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mhYellow)).Bold(true)
	alertStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mhOrange)).Bold(true)

	marker := ""
	if unread > 0 {
		if private {
			marker = " " + alertStyle.Render(fmt.Sprintf("(%d!)", unread))
		} else {
			marker = " " + unreadStyle.Render(fmt.Sprintf("(%d)", unread))
		}
	}
	idxStr := fmt.Sprintf("%d:", idx+1)
	if active {
		return activeBracket.Render("[") +
			activeTab.Render(idxStr+name) + marker +
			activeBracket.Render("]")
	}
	return " " + otherIdx.Render(idxStr) + other.Render(name) + marker + " "
}

// modeTagCell renders the `[INPUT]` / `[NAV]` / `[SEARCH]` / `[HELP]`
// mode badge at the right edge of the chanRow. Color tracks mode:
// mesh-green when typing, yellow when navigating/searching, cyan
// when help is open. Bracketed in dim drained either way.
func modeTagCell(mode mode) string {
	label := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	tag := "INPUT"
	color := meshGreen
	switch mode {
	case modeNav:
		tag = "NAV"
		color = mhYellow
	case modeSearch:
		tag = "SEARCH"
		color = mhYellow
	case modeHelp:
		tag = "HELP"
		color = mhCyan
	}
	return label.Render("[") +
		lipgloss.NewStyle().
			Foreground(lipgloss.Color(color)).
			Bold(true).
			Render(tag) +
		label.Render("]")
}

// byteCounterCell renders the live N/228 wire-byte counter. Color
// ramps from drained → fg → yellow → orange → pink as the message
// approaches the Meshtastic 228-byte text-payload cap, so the user
// sees they're about to be truncated before they hit Enter.
func byteCounterCell(used, capBytes int) string {
	pct := float64(used) / float64(capBytes)
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	switch {
	case used >= capBytes:
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(mhPink)).Bold(true)
	case pct >= 0.9:
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(mhOrange)).Bold(true)
	case pct >= 0.75:
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(mhYellow)).Bold(true)
	case pct >= 0.5:
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(mhFG))
	}
	return style.Render(fmt.Sprintf("%d/%d", used, capBytes))
}

// flashBannerCell renders the optional transient `m.flash` message
// emitted by /command dispatch. Affirmative messages (acks, hints)
// render in mesh-green; rejection-style messages ("unknown",
// "usage:", "use ", "no ") render in dim italic so the user reads
// them as advisory rather than success.
func flashBannerCell(flash string) string {
	if flash == "" {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(mhGreen))
	lower := strings.ToLower(flash)
	if strings.HasPrefix(lower, "unknown") ||
		strings.HasPrefix(lower, "usage") ||
		strings.HasPrefix(lower, "use ") ||
		strings.HasPrefix(lower, "no ") {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained)).Italic(true)
	}
	return style.Render(flash)
}

// channelTabsRow is the bottom status row showing channel tabs +
// optional flash banner + byte counter + mode tag. The Render
// method just composes the per-cell builders into a Row — the
// per-segment styling logic lives in the *Cell helpers above so
// each piece is independently testable + reusable.
type channelTabsRow struct {
	m model
}

// Render produces the chanRow at exactly box.Width × 1.
func (c channelTabsRow) Render(box Box) string {
	m := c.m

	// Left side: channel tabs (one per known channel; pre-sync
	// placeholder when the radio's ChannelInfo packet hasn't landed).
	var tabs []string
	for i, ch := range m.channels {
		tabs = append(tabs, channelTabCell(
			ch.name, i, ch.name == m.currentChannel, ch.private, ch.unread,
		))
	}
	if len(tabs) == 0 {
		tabs = append(tabs, channelTabCell("#default", 0, true, false, 0))
	}
	tabsStr := strings.Join(tabs, " ")

	// Right side: flash banner (optional) + byte counter (input mode
	// only) + mode tag (always). Composed as one styled string so the
	// flex-pad cell can split the leftover width evenly.
	modeTag := modeTagCell(m.mode)
	var right string
	if m.mode == modeInput {
		right = byteCounterCell(
			wirePayloadBytes(m.input.Value()),
			meshtasticMaxTextBytes,
		) + " " + modeTag
	} else {
		right = modeTag
	}
	if flash := flashBannerCell(m.flash); flash != "" {
		right = flash + "  " + right
	}

	tabsW := cells(tabsStr)
	rightW := cells(right)
	row := Row{Cells: []Cell{
		{Content: "    " + tabsStr, Width: 4 + tabsW},
		{Content: "", Width: -1},
		{Content: right + " ", Width: rightW + 1},
	}}
	return row.Render(Box{Width: box.Width, Height: 1})
}

// inputPromptCell renders the `[chan] › ` chrome prefix that
// prefixes the always-on textinput in modeInput. Channel name in
// mesh-green, brackets dim, the irssi-style `›` prompt arrow in
// amber so the cursor's about-to-type position pops visually.
func inputPromptCell(channel string) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen)).Bold(true)
	amber := lipgloss.NewStyle().Foreground(lipgloss.Color(mhOrange)).Bold(true)
	if channel == "" {
		channel = "#default"
	}
	return dim.Render("[") + green.Render(channel) +
		dim.Render("] ") + amber.Render("› ")
}

// searchPromptCell renders the modeSearch prompt: amber `/ ` lead,
// the textinput, and a trailing dim hint string. The textinput is
// passed in pre-rendered because it's a stateful bubbles view that
// the parent owns; this Component just stitches the chrome around
// it.
func searchPromptCell(searchInput string) string {
	amber := lipgloss.NewStyle().Foreground(lipgloss.Color(mhOrange)).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	return " " + amber.Render("/ ") + searchInput +
		"  " + dim.Render("ESC cancel · Enter match")
}

// navHintCell renders the dim modeNav hint strip ("NAV · j/k · r
// reply · …") that replaces the input prompt while the user is
// walking the scrollback. Single-line, dim drained throughout so
// it reads as a passive cheatsheet, not active chrome.
func navHintCell() string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	return " " + dim.Render(
		"NAV · j/k · r reply · R resend · w whois · t trace · p ping · "+
			"P pin · * star · ESC back to input · / search · ? help",
	)
}

// inputBar renders the bottom input row. Composed from the
// per-mode cell builders above: searchPromptCell in modeSearch,
// navHintCell in modeNav, inputPromptCell + textinput in modeInput.
// The textinput's Width is sized FROM the box budget so no row
// ever targets the very last column — the architectural fix for
// the pending-wrap bug class.
type inputBar struct {
	m model
}

// Render fills box with one input row.
func (i inputBar) Render(box Box) string {
	m := i.m

	if m.mode == modeSearch {
		return Row{Cells: []Cell{
			{Content: searchPromptCell(m.searchInput.View()), Width: -1},
		}}.Render(Box{Width: box.Width, Height: 1})
	}
	if m.mode == modeNav {
		return Row{Cells: []Cell{
			{Content: navHintCell(), Width: -1},
		}}.Render(Box{Width: box.Width, Height: 1})
	}

	// Input mode: " " + `[chan] › ` prefix + textinput. The textinput
	// Width is computed from box.Width minus chrome so it never
	// overflows. cursorPad = 1 reserves 1 cell for bubbles/textinput's
	// off-by-one cursor: when the value is non-empty it returns
	// Width+1 visible cells (cursor block emitted AFTER the typed
	// text, not over it).
	prefix := inputPromptCell(m.currentChannel)
	const leading = " "
	const cursorPad = 1
	tiW := box.Width - cells(leading) - cells(prefix) - cursorPad
	if tiW < 1 {
		tiW = 1
	}
	m.input.Width = tiW
	return Row{Cells: []Cell{
		{Content: leading + prefix + m.input.View(), Width: -1},
	}}.Render(Box{Width: box.Width, Height: 1})
}

// cells is a thin local shorthand that funnels every measurement
// through ansi.StringWidth (via padCells's contract). Centralizing
// the call site means one place to swap the measurement library
// during a future audit.
func cells(s string) int {
	// padCells(s, ansi.StringWidth(s)) is the identity, but we want
	// just the measurement. Inline ansi.StringWidth so the chrome
	// renderers don't have to import ansi directly.
	return ansiCells(s)
}

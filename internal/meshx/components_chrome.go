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

// channelTabsRow is the bottom status row showing channel tabs +
// optional flash banner + sync indicator + byte counter + mode tag.
type channelTabsRow struct {
	m model
}

// Render produces the chanRow at exactly box.Width × 1.
func (c channelTabsRow) Render(box Box) string {
	m := c.m
	label := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	activeTab := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhPink)).
		Bold(true)
	activeBracket := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	other := lipgloss.NewStyle().Foreground(lipgloss.Color(mhLavender))
	otherIdx := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	unread := lipgloss.NewStyle().Foreground(lipgloss.Color(mhYellow)).Bold(true)
	alertStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mhOrange)).Bold(true)

	// Channel tabs.
	var tabs []string
	for i, ch := range m.channels {
		marker := ""
		if ch.unread > 0 {
			if ch.private {
				marker = " " + alertStyle.Render(fmt.Sprintf("(%d!)", ch.unread))
			} else {
				marker = " " + unread.Render(fmt.Sprintf("(%d)", ch.unread))
			}
		}
		idx := fmt.Sprintf("%d:", i+1)
		if ch.name == m.currentChannel {
			tab := activeBracket.Render("[") +
				activeTab.Render(idx+ch.name) + marker +
				activeBracket.Render("]")
			tabs = append(tabs, tab)
		} else {
			tab := " " + otherIdx.Render(idx) + other.Render(ch.name) + marker + " "
			tabs = append(tabs, tab)
		}
	}
	// Pre-sync placeholder.
	if len(tabs) == 0 {
		tab := activeBracket.Render("[") +
			activeTab.Render("1:#default") +
			activeBracket.Render("]")
		tabs = append(tabs, tab)
	}
	tabsStr := strings.Join(tabs, " ")

	// Mode tag.
	modeTag := "INPUT"
	modeTagColor := meshGreen
	switch m.mode {
	case modeNav:
		modeTag = "NAV"
		modeTagColor = mhYellow
	case modeSearch:
		modeTag = "SEARCH"
		modeTagColor = mhYellow
	case modeHelp:
		modeTag = "HELP"
		modeTagColor = mhCyan
	}
	modeTagStyled := label.Render("[") +
		lipgloss.NewStyle().
			Foreground(lipgloss.Color(modeTagColor)).
			Bold(true).
			Render(modeTag) +
		label.Render("]")

	// Right-side composite: byte counter (in input mode) + mode tag,
	// optionally prefixed by flash banner.
	var right string
	if m.mode == modeInput {
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
		right = counterStyle.Render(counterTxt) + " " + modeTagStyled
	} else {
		right = modeTagStyled
	}
	if m.flash != "" {
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

	// Three-cell layout: leading "    " + tabs (fixed) + flex spacer
	// + right (fixed) + trailing space. Row truncates if overall too
	// wide for box; pads with the flex spacer if too narrow.
	tabsW := cells(tabsStr)
	rightW := cells(right)
	row := Row{Cells: []Cell{
		{Content: "    " + tabsStr, Width: 4 + tabsW},
		{Content: "", Width: -1},
		{Content: right + " ", Width: rightW + 1},
	}}
	return row.Render(Box{Width: box.Width, Height: 1})
}

// inputBar renders the bottom input row: the always-on textinput in
// modeInput, the search prompt in modeSearch, the nav-mode hint in
// modeNav. The textinput's Width is sized FROM the box budget rather
// than statically — this is the architectural fix for pending-wrap:
// no row will ever target the very last column.
type inputBar struct {
	m model
}

// Render fills box with one input row.
func (i inputBar) Render(box Box) string {
	m := i.m
	amber := lipgloss.NewStyle().Foreground(lipgloss.Color(mhOrange)).Bold(true)
	green := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen)).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))

	if m.mode == modeSearch {
		left := " " + amber.Render("/ ") + m.searchInput.View() +
			"  " + dim.Render("ESC cancel · Enter match")
		return Row{Cells: []Cell{
			{Content: left, Width: -1},
		}}.Render(Box{Width: box.Width, Height: 1})
	}
	if m.mode == modeNav {
		hint := " " + dim.Render(
			"NAV · j/k · r reply · R resend · w whois · t trace · p ping · "+
				"P pin · * star · ESC back to input · / search · ? help",
		)
		return Row{Cells: []Cell{
			{Content: hint, Width: -1},
		}}.Render(Box{Width: box.Width, Height: 1})
	}

	// Input mode: " [chan] › " prefix followed by the textinput. The
	// textinput's Width is computed from box, leaving the leading
	// space + prefix consumed and never overflowing.
	chName := m.currentChannel
	if chName == "" {
		chName = "#default"
	}
	prefix := dim.Render("[") + green.Render(chName) +
		dim.Render("] ") + amber.Render("› ")
	leading := " "
	prefixW := cells(prefix)
	leadingW := cells(leading)
	// bubbles/textinput.View() has an off-by-one: when the value is
	// non-empty it returns Width+1 visible cells (the cursor block is
	// emitted at the position AFTER the typed text rather than over
	// it). If we set m.input.Width = tiW directly the row overflows
	// by 1 cell on every keystroke, padCells's truncate kicks in, and
	// the "…" tail clobbers the styled prefix back to plain white. So
	// we shave 1 cell off the textinput budget when there's content.
	const cursorPad = 1
	tiW := box.Width - leadingW - prefixW - cursorPad
	if tiW < 1 {
		tiW = 1
	}
	m.input.Width = tiW
	row := Row{Cells: []Cell{
		{Content: leading + prefix + m.input.View(), Width: -1},
	}}
	return row.Render(Box{Width: box.Width, Height: 1})
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

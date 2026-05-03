// Copyright (c) 2026 John Dewey
//
// Pane Components — every overlay (channels, nodes, help) and the
// messages pane lives here as a typed Component with a Render(Box)
// method. The pane structs hold a model snapshot; Render is the
// implementation, not a shim — bodies were folded out of the legacy
// (m model) renderXxxPane methods so the Component IS the source of
// truth for pane composition.
//
// /nearby and /radar Components live in panes_map.go alongside the
// peerPlot geo data prep that powers them.

package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// channelsPane is the /channels overlay — a flex VStack of channel
// rows under a CHANNELS header. Render builds the [header, blank,
// row, row, …] line slice and routes through renderBorderedPane to
// drop the result into the focused/unfocused frame.
type channelsPane struct{ m model }

// Render returns the bordered pane sized exactly to box. Each row
// (including header + blank separator) is built by the per-segment
// cell helpers in components_overlays.go; this method just stitches
// them in order and hands the join to renderBorderedPane for the
// frame.
func (p channelsPane) Render(box Box) string {
	m := p.m
	header := paneHeaderCell("CHANNELS", m.focused == paneChannels)

	lines := make([]string, 0, 2+len(m.Channels))
	lines = append(lines, header, "")
	for i, c := range m.Channels {
		// Skip DISABLED slots — applyChannel keeps them in the slice
		// for slot allocation but they're not real channels to display.
		if c.Role == roleDisabled {
			continue
		}
		selected := i == m.selectedCh && m.focused == paneChannels
		inner := paneInnerWidth(box.Width)
		contentW := inner - gutterWidth
		if contentW < 1 {
			contentW = 1
		}
		row := channelRowLine(c.Name, c.Private, c.Unread, contentW)
		lines = append(lines, wrapSelection(
			row, selected, m.isStringSearchHit(c.Name), inner,
		))
	}
	return renderBorderedPane(
		strings.Join(lines, "\n"),
		box.Width, box.Height, paneChannels, m.focused == paneChannels,
	)
}

// nodesPane is the BitchX-style users grid — bracketed cells laid
// out in a fixed-width grid under a NODES header + count + legend.
type nodesPane struct{ m model }

// Render returns the bordered pane sized exactly to box. Computes
// the cell-width + columns from box.Width, walks the sorted node
// list filling rows of fixed-width [@callsign] cells via
// userCellLine + nodePresentationFor (so /nodes and /nearby render
// the same peer state with identical chrome).
func (p nodesPane) Render(box Box) string {
	m := p.m
	total := len(m.Nodes)
	online := 0
	for i := range m.Nodes {
		if m.Nodes[i].CurrentState() == stateOnline {
			online++
		}
	}

	header := paneHeaderCell("NODES", m.focused == paneNodes)
	count := paneCountSuffix(fmt.Sprintf(
		"  (#mesh: %d/%d · sort: %s)", online, total, m.nodeSort.label(),
	))
	legend := paneLegendLine(
		"legend:  @online  +pinned  ⊘muted  ✗failed  ·stale",
	)

	sorted := m.sortedNodes()

	// Grid layout — fixed-width cells, as many columns as fit.
	// Each cell: "[ @callsign    ] " → up to ~20 visible cells.
	inner := box.Width - 4 // minus pane border + pane padding
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
			n := sorted[idx]
			selected := idx == m.selectedNd && m.focused == paneNodes
			isSelf := m.MyNodeNum != 0 && n.NodeNum == m.MyNodeNum
			cell := userCellLine(n, isSelf, selected, cellW)
			// Search-hit highlight — only when not currently selected
			// (selection wins). Same dim-green tint /nearby and the
			// messages pane use, hoisted to searchHitRowBg.
			if m.isStringSearchHit(n.Callsign) && !selected {
				cell = lipgloss.NewStyle().
					Background(lipgloss.Color(searchHitRowBg)).
					Render(cell)
			}
			cells = append(cells, cell)
		}
		gridLines = append(gridLines, strings.Join(cells, strings.Repeat(" ", cellPad)))
	}

	lines := make([]string, 0, 4+len(gridLines))
	lines = append(lines, header+count, "", legend, "")
	lines = append(lines, gridLines...)

	return renderBorderedPane(
		strings.Join(lines, "\n"),
		box.Width, box.Height, paneNodes, m.focused == paneNodes,
	)
}

// messagesPane is the chat log — zebra-striped rows scrolling under
// the active channel name + msg count. The implementation lives in
// messagesPaneRender (in ui.go) — split out only so the diff stayed
// manageable; logically this Render is the source of truth.
type messagesPane struct{ m model }

// Render forwards to messagesPaneRender. The function-form body in
// ui.go can be inlined here; the indirection survives only to keep
// the file split practical.
func (p messagesPane) Render(box Box) string {
	return messagesPaneRender(p.m, box.Width, box.Height)
}

// helpPane is the full-screen /help overlay — section dividers + kv
// rows under a SQUELCH · HELP banner, with a Viewport that scrolls the
// content + a footer indicator showing the position. j/k/d/u/g/G nav
// is wired in input.go's nav handler; the Component just consumes
// m.helpScroll as the current offset.
type helpPane struct{ m model }

// Render lays out the help content as one big slice of pre-styled
// lines, then hands the slice to a Viewport. The viewport owns scroll
// clamping, padding, and footer placement; all the per-line styling
// stays here so future kv tweaks land in one place.
func (p helpPane) Render(box Box) string {
	m := p.m
	head := lipgloss.NewStyle().
		Foreground(lipgloss.Color(meshGreen)).
		Bold(true)
	dim := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained))

	// Width budget for kv lines = pane width - frame (2) - padding (6).
	// Routes through helpKVLine so the cell math (key column 14 cells,
	// description as the flex slot) lives in components_overlays.go.
	kvW := box.Width - 2 - 6
	if kvW < 30 {
		kvW = 30
	}
	kv := func(k, d string) string {
		return helpKVLine(k, d, 14, kvW)
	}
	sec := func(s string) string { return helpSectionLine(s) }

	lines := []string{
		head.Render("S Q U E L C H   ·   H E L P"),
		dim.Render("j/k scroll · q/Esc/? close · irssi-style modal UI"),
		"",
		sec("MODES"),
		kv("INPUT (default)", "cursor in the input bar — type a message or /command"),
		kv("NAV (Esc)", "cursor in the scrollback — single letters act on highlight"),
		kv("SEARCH (/ in nav)", "live-filter current pane. Enter commits, Esc cancels"),
		kv("HELP (?)", "this screen — Esc / q / ? to dismiss"),
		"",
		sec("GLOBAL"),
		kv("Ctrl+X", "exit app (also Ctrl+C on empty input)"),
		kv("?", "open help"),
		kv("Enter", "send message / run /command / activate selection"),
		kv("Esc", "input → nav mode / nav → back to input / cancel modal"),
		kv("Tab", "complete /command, #channel, or nick (cycles)"),
		kv("Shift+Tab", "cycle completion backwards"),
		"",
		sec("CHANNEL SWITCHING"),
		kv("Alt+1..4", "jump to channel by index"),
		kv("Ctrl+N / Ctrl+P", "cycle to next / prev channel"),
		kv("/join <name>", "switch to named channel"),
		kv("/channel list", "show all known channels"),
		"",
		sec("WINDOW NAV"),
		kv("Ctrl+W k", "from input → jump up to the message log (nav mode)"),
		kv("Ctrl+W j", "from nav   → drop down to the input bar"),
		kv("Esc (input)", "same as Ctrl+W k — enter scrollback nav"),
		kv("Esc (nav)", "same as Ctrl+W j — return to input bar"),
		"",
		sec("OVERLAYS"),
		kv("/channels", "open channels list — j/k walk, Enter opens"),
		kv("/nodes /users /names", "open users grid — h/j/k/l walk, Enter whois"),
		kv("/nearby", "distance-sorted peer roster (closest first; requires own GPS fix)"),
		kv("/radar", "polar scope — peers plotted by bearing + distance around you"),
		kv("/help or ?", "this help screen — j/k scroll, Esc closes"),
		"",
		sec("NAV MODE (after Esc)"),
		kv("j / k", "walk selection down / up"),
		kv("gg / G", "top / bottom"),
		kv("Ctrl+D / Ctrl+U", "half-page down / up"),
		kv("/", "search within focused pane"),
		kv("n / N", "next / prev search hit"),
		kv("Enter", "detail view (hop, SNR, RSSI, hex id)"),
		kv("Esc / i / q", "back to input mode"),
		"",
		sec("NAV-MODE QUICK-KEYS (on message/node selection)"),
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
		sec("HAM RADIO /COMMANDS"),
		kv("/cq [tail]", "broadcast CQ with optional custom tail"),
		kv("/cqr <call>", "respond to someone's CQ with a real copy report"),
		kv("/rs <call>", "send a signal report (real SNR/RSSI/hops)"),
		kv("/73 [call]", "sign-off (\"best regards\")"),
		kv("/88", "love-and-kisses ham slang"),
		kv("/qsl", "acknowledge / confirm receipt"),
		kv("/qth [grid]", "broadcast your location / grid square"),
		kv("/qrz", "\"who is calling me?\" — prompt for ID"),
		kv("/qrm <call>", "report interference on their signal"),
		kv("/qsb <call>", "report that their signal is fading"),
		kv("/sk", "final sign-off — stronger than /73"),
		kv("/wx [conditions]", "weather report at your QTH"),
		kv("/grid [locator]", "broadcast just your Maidenhead grid"),
		kv("/mesh", "summarize the mesh you can hear (meshtastic-specific)"),
		kv("/k <call>", "\"over — go ahead\" ragchew turn-taking"),
		"",
		sec("MESSAGING /COMMANDS"),
		kv("/msg <call> <text>", "direct message to node"),
		kv("/reply [call] [text]", "reply (uses highlighted sender if omitted)"),
		kv("/r", "alias for /reply"),
		kv("/ping <call>", "RTT + signal check"),
		kv("/tr <call>", "traceroute — show mesh hop path to <call>"),
		kv("/whois <call>", "node metadata — alias /w"),
		"",
		sec("CHANNEL / UTIL /COMMANDS"),
		kv("/join <channel>", "switch to named channel"),
		kv("/channel list", "list known channels (alias /list)"),
		kv("/config", "open interactive radio config panel (Enter toggles radio buzzer)"),
		kv("/mute", "toggle meshX terminal ding (does not touch radio buzzer)"),
		kv("/me <action>", "IRC-style action — broadcasts \"* <action>\""),
		kv("/ignore <call>", "hide chat messages from <call> locally"),
		kv("/unignore [call]", "drop /ignore filter, or list currently ignored"),
		kv("/version", "meshX build identity + radio firmware version"),
		kv("/reboot", "AdminMessage reboot — radio restarts in 5s"),
		kv("/who", "alias for /nodes"),
		kv("/whoami", "alias for /info"),
		kv(
			"/lastlog [call|text]",
			"jump to the most recent message — last from <call>, or body match",
		),
		kv("/search <pattern>", "highlight matching rows; n / N to next / prev, Esc clears"),
		kv("/clear", "clear local scrollback (does not unsend)"),
		kv("/help", "open this help"),
		kv("/q, /quit", "hint — use Ctrl+X to exit"),
		"",
		sec("NOTES ON CHANNELS"),
		kv("", "Channels are configured on the RADIO, not in meshx."),
		kv("", "A channel = a name + a shared PSK (encryption key)."),
		kv("", "Create channels via the official Meshtastic app / CLI;"),
		kv("", "meshx imports them once the radio is configured."),
		kv("", "Future: /channel add <meshtastic://url> to import by URL,"),
		kv("", "/channel share <name> to emit a QR for another client."),
	}

	// Footer = blank separator + the position-aware scroll indicator.
	// Reserve = len(Footer) so the viewport leaves room beneath it.
	// The viewport's visible window slides over `lines` keyed off
	// m.helpScroll; clamp lives inside Viewport.Render so the model
	// can over-bump scroll on G/PgDn without tracking total length.
	footer := []string{
		"",
		helpScrollIndicator(m.helpScroll, len(lines), box.Height-2-2-2),
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen))
	return Bordered{
		Inner: Viewport{
			Lines:    lines,
			Scroll:   m.helpScroll,
			Reserved: len(footer),
			Footer:   footer,
		},
		Chars:       DoubleBorder,
		BorderStyle: style,
		Padding:     [4]int{1, 3, 1, 3},
	}.Render(box)
}

// configEntryKind classifies a /config row. Interactive rows respond
// to Enter; read-only rows are display-only — they exist so the panel
// doubles as the radio-state reference /config used to systemBlock-
// dump, without forking the surface into two commands.
//
//   - cfgEntryReadOnly — value rendered, Enter does nothing
//   - cfgEntryToggle   — Enter flips a bool in cfgDraft
//   - cfgEntryString   — Enter swaps the row to inline-edit (focuses
//     cfgEditInput pre-filled with the current draft string; inner
//     Enter commits to draft, Esc cancels back to nav)
//
// All edits stage in cfgDraft. Nothing reaches the radio until the
// user presses Ctrl+S (commitConfigDraft).
type configEntryKind int

const (
	cfgEntryReadOnly configEntryKind = iota
	cfgEntryToggle
	cfgEntryString
)

// configEntry describes one row in the /config overlay. label is the
// left-column key (cell-padded by configPane.Render); value is the
// CURRENT DRAFT value (rendered, not the saved one). saved is the
// live radio value, kept alongside so the renderer can compare and
// mark the row dirty without re-deriving it. kind controls Enter
// behaviour — see configEntryKind.
type configEntry struct {
	label string
	value string // draft value as a string
	saved string // live value as a string — "" means N/A (read-only)
	kind  configEntryKind
	// action runs when the user presses Enter on a kind=Toggle row.
	// String rows route through a separate path (Enter focuses the
	// inline textinput) so action stays nil for them. Read-only rows
	// have action nil and are skipped by selectableConfigEntryIndices.
	action func(m *model)
	// field names which cfgDraft slot a string row binds to —
	// "longname" / "shortname". Used by activate() to stash the
	// current value in cfgEditInput and by the edit-commit path to
	// route the typed value back into the draft. Empty for non-string
	// rows.
	field string
}

// boolToOnOff renders true/false as the "on"/"off" tokens the panel
// uses for toggle row values. Centralized so the dirty-marker check
// (saved == value) compares against the same string the renderer
// emits, and a future "enabled / disabled" rebrand lands in one
// place instead of N inline ternaries.
func boolToOnOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// configEntries returns the rows the /config overlay should render in
// display order. Interactive rows come first (j-only-from-the-top
// lands on something useful); read-only rows below the divider mirror
// the radio state reference /config used to systemBlock-dump. Order:
//
//  1. radio buzzer (toggle — alert_message_buzzer)
//  2. longname     (string — User.long_name; round-trips with shortname)
//  3. shortname    (string — User.short_name)
//  4. — separator —
//  5. read-only firmware / region / preset / role / telemetry
//
// All interactive values read from cfgDraft, not the live state, so
// per-row Enter mutations show up immediately while staying off the
// wire until Ctrl+S. The saved-value column lets the renderer mark
// dirty rows without re-deriving live state per render.
func (m model) configEntries() []configEntry {
	// Buzzer "saved" reflects what we know the radio actually thinks.
	// Until mdl.ModuleBuzzer lands (handshake dump or the proactive
	// AdminMessage_GetModuleConfigRequest fired at ConfigComplete), we
	// show "(querying)" so the user doesn't trust a default-true guess
	// — same shape the rest of the panel uses for not-yet-known fields.
	buzzerSaved := boolToOnOff(m.RadioBuzzerEnabled)
	if !m.RadioBuzzerKnown {
		buzzerSaved = "querying…"
	}
	out := []configEntry{
		{
			label: "radio buzzer",
			value: boolToOnOff(m.cfgDraft.buzzer),
			saved: buzzerSaved,
			kind:  cfgEntryToggle,
			action: func(mm *model) {
				mm.cfgDraft.buzzer = !mm.cfgDraft.buzzer
			},
		},
		{
			label: "longname",
			value: m.cfgDraft.longName,
			saved: m.myCallsign(),
			kind:  cfgEntryString,
			field: "longname",
		},
		{
			label: "shortname",
			value: m.cfgDraft.shortName,
			saved: m.myShortName(),
			kind:  cfgEntryString,
			field: "shortname",
		},
		// Separator row — rendered as a dim divider; non-selectable.
		{label: "", value: "", kind: cfgEntryReadOnly},
	}

	add := func(k, v string) {
		if v == "" {
			return
		}
		out = append(out, configEntry{label: k, value: v, kind: cfgEntryReadOnly})
	}

	if n := m.myNode(); n != nil {
		add("hw", n.HwModel)
	}
	add("firmware", m.RadioFirmware)
	if m.CurrentChannel != "" {
		add("channel", m.CurrentChannel)
	}
	add("modem preset", m.RadioModemPreset)
	add("region", m.RadioRegion)
	add("role", m.RadioRole)
	if m.RadioTxPower != 0 {
		add("tx power", fmt.Sprintf("%d dBm", m.RadioTxPower))
	}
	add("grid", m.MyGrid)
	if m.HasTelemetry {
		add("battery", fmt.Sprintf("%.2f V (%d%%)", m.BatteryVoltage, m.BatteryLevel))
		add("chan use", fmt.Sprintf("%.1f%%", m.ChannelUtil))
	}
	add("peers", fmt.Sprintf("%d known", len(m.Nodes)))
	return out
}

// selectableConfigEntryIndices returns the slice indices of rows that
// j/k should land on — interactive rows only. The cursor jumps over
// the separator + read-only block so j from "radio buzzer" wraps
// straight back to the same row instead of marching through eight
// non-actionable lines.
func (m model) selectableConfigEntryIndices() []int {
	entries := m.configEntries()
	out := make([]int, 0, 1)
	for i, e := range entries {
		if e.kind != cfgEntryReadOnly {
			out = append(out, i)
		}
	}
	return out
}

// configPane is the /config overlay — interactive radio configuration
// with vim nav (j/k walks selectable rows, Enter toggles bools or
// opens an inline string-edit). Edits stage in cfgDraft; Ctrl+S
// commits them to the radio in one shot, Esc on a dirty draft
// prompts y/n via cfgConfirmDiscard.
type configPane struct{ m model }

// Render lays the entries from configEntries() onto bordered-pane
// rows. Selectable rows pass through wrapSelection so the cursor
// renders the same selection chrome /channels uses; read-only rows
// render as dim "  key  value" lines. Dirty rows (draft != saved)
// get a "*" prefix in the value column so the user sees what they're
// about to commit. The pane header counts unsaved changes to match.
func (p configPane) Render(box Box) string {
	m := p.m
	entries := m.configEntries()

	header := paneHeaderCell("CONFIG", m.focused == paneConfig)
	editable := 0
	unsaved := 0
	for _, e := range entries {
		if e.kind != cfgEntryReadOnly {
			editable++
			if e.value != e.saved {
				unsaved++
			}
		}
	}
	dirtyTag := ""
	if unsaved > 0 {
		dirtyTag = fmt.Sprintf(", %d unsaved", unsaved)
	}
	count := paneCountSuffix(fmt.Sprintf(
		"  (%d editable, %d info%s)",
		editable, len(entries)-editable-1, dirtyTag,
	))
	legend := paneLegendLine(
		"j/k walk · Enter edit · Ctrl+S save · Esc close",
	)

	inner := paneInnerWidth(box.Width)
	contentW := inner - gutterWidth
	if contentW < 1 {
		contentW = 1
	}

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mhCyan))
	onStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen)).Bold(true)
	offStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	dirtyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mhPink)).Bold(true)
	editPromptStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhPink)).Bold(true)

	lines := make([]string, 0, 6+len(entries))
	lines = append(lines, header+count, "", legend, "")

	for i, e := range entries {
		// Empty separator row — rendered as a dim divider line.
		if e.label == "" && e.value == "" && e.kind == cfgEntryReadOnly {
			lines = append(lines, dim.Render(strings.Repeat("─", contentW)))
			continue
		}
		dirty := e.kind != cfgEntryReadOnly && e.value != e.saved
		marker := "  "
		if dirty {
			marker = dirtyStyle.Render(" *")
		}
		// Determine value rendering. If this row is currently being
		// edited inline, render the live textinput View() instead of
		// the static draft value so the cursor is visible. The other
		// rows render their typed values per kind.
		var styledVal string
		isEditingThis := m.cfgEditing != "" && m.cfgEditing == e.field &&
			i == m.selectedCfg && m.focused == paneConfig
		switch {
		case isEditingThis:
			// "‹ longname › typing here _" — give the editor an
			// obvious bracket so it doesn't blend with surrounding
			// text. m.cfgEditInput holds the textinput.Model.
			styledVal = editPromptStyle.Render("‹ ") +
				m.cfgEditInput.View() +
				editPromptStyle.Render(" ›")
		case e.kind == cfgEntryToggle:
			if e.value == "on" {
				styledVal = onStyle.Render(e.value)
			} else {
				styledVal = offStyle.Render(e.value)
			}
			if dirty {
				styledVal += dim.Render(fmt.Sprintf("  (was %s)", e.saved))
			}
		case e.kind == cfgEntryString:
			val := e.value
			if val == "" {
				val = dim.Render("(empty)")
			} else {
				val = keyStyle.Render(val)
			}
			styledVal = val
			if dirty {
				styledVal += dim.Render(fmt.Sprintf("  (was %s)", e.saved))
			}
		default:
			styledVal = keyStyle.Render(e.value)
		}
		labelStyle := keyStyle
		if e.kind == cfgEntryReadOnly {
			labelStyle = dim
		}
		row := fmt.Sprintf("%s %s  %s",
			marker,
			labelStyle.Render(padCells(e.label, 14)),
			styledVal,
		)
		row = padCells(row, contentW)
		selected := i == m.selectedCfg && m.focused == paneConfig && e.kind != cfgEntryReadOnly
		lines = append(lines, wrapSelection(row, selected, false, inner))
	}

	// Trailing footer — Esc-on-dirty confirmation prompt or status
	// hint. Renders below the row list, dim italic so it doesn't
	// fight the entries for attention.
	lines = append(lines, "")
	switch {
	case m.cfgConfirmDiscard:
		lines = append(lines, dirtyStyle.Render(
			" discard "+fmt.Sprintf("%d", unsaved)+
				" unsaved change(s)?  y / n",
		))
	case unsaved > 0:
		lines = append(lines, dim.Italic(true).Render(
			fmt.Sprintf(" %d unsaved change(s) — Ctrl+S to commit, Esc to discard", unsaved),
		))
	default:
		lines = append(lines, dim.Italic(true).Render(
			" no pending changes",
		))
	}

	return renderBorderedPane(
		strings.Join(lines, "\n"),
		box.Width, box.Height, paneConfig, m.focused == paneConfig,
	)
}

// sortedNodes returns a view of m.Nodes with pinned (fav) nodes first,
// then sorted by the current sort mode. The returned slice is a copy
// so we don't mutate storage order.
func (m model) sortedNodes() []nodeItem {
	out := make([]nodeItem, len(m.Nodes))
	copy(out, m.Nodes)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Fav != out[j].Fav {
			return out[i].Fav // pinned first, always
		}
		switch m.nodeSort {
		case sortByName:
			return out[i].Callsign < out[j].Callsign
		case sortByState:
			return stateWeight(out[i].State) < stateWeight(out[j].State)
		default: // sortByLastHeard
			return out[i].HeardRank < out[j].HeardRank
		}
	})
	return out
}

// stateWeight orders node states: online < offline < muted < failed.
func stateWeight(s nodeState) int {
	switch s {
	case stateOnline:
		return 0
	case stateOffline:
		return 1
	case stateMuted:
		return 2
	case stateFailed:
		return 3
	default:
		return 4
	}
}

// frameView builds the top-level VStack for a frame. Body delegates
// to the active overlay's pane Component (or renderIrssiBody for the
// default messages-pane case). Both branches already contract to
// box.Height × box.Width per ansiCells (helpPane.Render goes through
// Bordered which uses padCells; renderIrssiBody dispatches to a pane
// Component that does the same), so the body Component returns those
// strings directly — wrapping in RawBlock would just re-pad already-
// padded lines.
func frameView(m model) Component {
	body := ComponentFunc(func(box Box) string {
		switch m.mode {
		case modeHelp:
			return helpPane{m: m}.Render(box)
		default:
			return m.renderIrssiBody(box.Width, box.Height)
		}
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

// renderIrssiBody dispatches to the active overlay's pane Component.
// Width comes from the parent VStack's Box, NOT m.w — that's the
// indirection that lets the global frame box (m.w - 1, the safeW
// margin) propagate down so no row hits the right edge.
func (m model) renderIrssiBody(width, height int) string {
	box := Box{Width: width, Height: height}
	var pane Component
	switch m.overlay {
	case overlayChannels:
		pane = channelsPane{m: m}
	case overlayNodes:
		pane = nodesPane{m: m}
	case overlayNearby:
		pane = nearbyPane{m: m}
	case overlayRadar:
		pane = radarPane{m: m}
	case overlayConfig:
		pane = configPane{m: m}
	default:
		pane = messagesPane{m: m}
	}
	return pane.Render(box)
}

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

// paneInnerWidth returns the content-area width inner renderers
// should target given a `width` argument from View(). One place to
// change the math instead of hunting down `width-4` literals.
func paneInnerWidth(width int) int {
	return width - 4
}

// renderBorderedPane wraps pre-rendered inner content (each line
// already padded to width-4 cells per ansiCells) in the same ║/═
// frame paneStyle draws — using the Bordered Component so the math
// goes through ansiCells (keycap-aware) instead of lipgloss's
// runewidth measurement that under-counts keycap emoji and would
// land the right ║ frame off-column on rows containing "7️⃣"-style
// glyphs. This is the architectural promise — no overflow ever.
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
	return Bordered{
		Inner:       RawBlock{Content: inner},
		Chars:       chars,
		BorderStyle: style,
		Padding:     [4]int{1, 1, 1, 1},
	}.Render(Box{Width: width, Height: height})
}

// tailStartList is the row-budget calculator for messagesPane — each
// message is 1 row, +1 if it has an acks sub-line; system multi-line
// blocks count their embedded newlines.
func tailStartList(msgs []messageItem, rowsBudget int) int {
	rows := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		cost := 1 + strings.Count(msgs[i].Text, "\n")
		if msgs[i].Acks != "" {
			cost++
		}
		if rows+cost > rowsBudget {
			return i + 1
		}
		rows += cost
	}
	return 0
}

// messagesPaneRender is the body of messagesPane.Render — split into
// a function only so the diff stays local; the matching one-line
// Render method up top is the canonical Component entry point.
func messagesPaneRender(m model, width, height int) string {
	chanName := m.CurrentChannel
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
	header := paneHeaderCell(chanName, m.focused == paneMessages)
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Render(fmt.Sprintf("  (%d msgs)", len(m.Messages)))

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

	// irssi-Style: always the dense one-row-per-message list.
	// Default anchors on the tail (show latest rows). If the user
	// has scrolled the selection above the natural tail via j/k or
	// Ctrl+F / Ctrl+U, drop the viewport back so the selected row
	// stays visible — otherwise nav feels broken because moving
	// selectedMsg doesn't seem to move anything on screen.
	naturalStart := tailStartList(m.Messages, rowsFree)
	startIdx := naturalStart
	scrollback := m.selectedMsg < naturalStart
	if scrollback {
		// Pin the cursor at the TOP of the viewport during scrollback
		// so each `k` scrolls the message list up by exactly one row.
		// Cursor placement at rowsFree/3 jumps the view 1/3 down on
		// the first scroll-back press and freezes after; pin-at-top
		// gives 1-for-1 keystroke-to-row movement that irssi/mutt/vim
		// users expect.
		startIdx = m.selectedMsg
		if startIdx < 0 {
			startIdx = 0
		}
	}
	if startIdx > 0 {
		lines = append(lines, earlierCountLine(startIdx))
	}
	selected := m.focused == paneMessages && m.mode == modeNav
	var lastGroup uint64
	var groupBg string
	for i := startIdx; i < len(m.Messages); i++ {
		msg := m.Messages[i]
		// /ignore filter — drop chat rows from peers the user has
		// silenced. System rows (status=="system") and our own
		// messages always render so the user can see what they
		// typed and read system status. Substring match against the
		// from column handles the "[shortname] longname" rendering
		// vs. raw callsign — same lowercase comparison /whois uses.
		if msg.Status != mdl.StatusSystem && !msg.Mine && m.isIgnored(msg.From) {
			continue
		}
		faded := m.nodeFilter != "" && !m.msgMatchesFilter(msg)

		// Group rows share one zebra stripe — every line in a /whois
		// or /config block gets the same bg so the block reads as one
		// card instead of alternating stripes.
		var bg string
		switch {
		case msg.Group != 0 && msg.Group == lastGroup:
			bg = groupBg
		case msg.Group != 0:
			// System blocks (/whois, /config, /ping, /env, /info
			// cards) always use the lighter zebra tint so the block
			// reads consistently as one shaded card — never the
			// near-black rowBgEven that would make a block look
			// like terminal-bg depending on parity.
			bg = rowBgOdd
			lastGroup = msg.Group
			groupBg = bg
		case msg.Status == mdl.StatusSystem || msg.Status == mdl.StatusNotice:
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

		// Search-hit highlight — when /search has an active query,
		// override the zebra/group bg with searchHitRowBg on rows
		// whose from + text matches the query. Selection still wins
		// (handled by isSelected below), so cursoring through hits
		// via n/N visually swaps the highlight without losing the
		// at-a-glance "these N rows match" cue. Skipped on system
		// rows since the search semantics target chat content.
		if msg.Status != mdl.StatusSystem && m.isMsgSearchHit(msg) {
			bg = searchHitRowBg
		}

		// Highlight the whole group when any row in it is selected —
		// j/k lands the cursor on the header row, but visually the
		// entire block shows as the current selection.
		isSelected := i == m.selectedMsg && selected
		if !isSelected && msg.Group != 0 && selected {
			if sel := m.Messages[m.selectedMsg]; sel.Group == msg.Group {
				isSelected = true
			}
		}

		// Pin-corner boundaries — `⌜` goes on the first row of a
		// pinned group, `⌟` on the last. For singleton pinned rows
		// (group == 0) both are true so the row reads as self-
		// bracketed. Computing here rather than inside noticeRowRender
		// keeps the renderer oblivious to message-list indices.
		pinFirst := false
		pinLast := false
		if msg.Pinned {
			pinFirst = msg.Group == 0 || i == 0 || m.Messages[i-1].Group != msg.Group
			pinLast = msg.Group == 0 || i+1 >= len(m.Messages) || m.Messages[i+1].Group != msg.Group
		}
		// messageRow.Render owns the dispatch (notice/system →
		// noticeRowRender, regular chat → chatRowRender) AND forces
		// every line through padCells so a buggy inner emitter can't
		// blow out the pane.
		row := messageRow{
			m: m, msg: msg, selected: isSelected, rowBg: bg,
			pinFirst: pinFirst, pinLast: pinLast, faded: faded,
			rowsInner: paneInnerWidth(width),
		}
		h := messageRowVisualHeight(m, msg)
		line := row.Render(Box{Width: paneInnerWidth(width), Height: h})
		lines = append(lines, line)
	}
	// BitchX / irssi gravity — pin the log to the BOTTOM of the
	// pane. When the message list doesn't fill the available rows,
	// pad the top with blank lines so content rises from the input
	// bar upward. Once the log outgrows rowsFree, tailStartList
	// trims the head so we're always showing the newest rows flush
	// against the bottom.
	//
	// Reply-threaded rows render as TWO visual lines (quote-line +
	// row, joined with "\n" inside one string). tailStartList +
	// naive `len(lines)` padding both treat each slice element as
	// one row, which under-counts the real row usage and would
	// shear the top status bar off the terminal top. Count visual
	// rows instead (1 + strings.Count(line, "\n")) for both the
	// trim and the pad passes.
	visualRows := func(ls []string) int {
		n := 0
		for _, l := range ls {
			n += 1 + strings.Count(l, "\n")
		}
		return n
	}
	// Overflow trim — anchor depends on scrollback direction. Tail-
	// pinned drops oldest so newest stays flush with input bar.
	// Scrollback drops newest so cursor + earlier-context rows stay
	// on screen.
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

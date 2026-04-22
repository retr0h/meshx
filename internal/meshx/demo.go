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
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Mode constants — mutt-style modal UI. Normal is the default
// three-pane view; command drops you into a `:` prompt at the bottom;
// insert takes over the middle pane with a compose editor.
type mode int

const (
	// modeInput — irssi default. Input bar at the bottom is focused.
	// Typing composes a message (or a /command if the line starts with /).
	// Enter dispatches. ESC moves to modeNav.
	modeInput mode = iota
	// modeNav — selection cursor is in the scrollback. j/k walks
	// messages. r / t / p / w / * / m act on the highlighted sender.
	// ESC or i returns to input.
	modeNav
	// modeSearch — `/` from nav opens a live-filter prompt against the
	// focused list (scrollback / channels drawer / nodes drawer).
	modeSearch
	// modeHelp — full-screen scrollable keymap overlay.
	modeHelp
	// modeSplash — BitchX-style graffiti art banner shown on launch.
	// Any key dismisses; auto-dismisses after ~3s.
	modeSplash
)

// Pane indices — used for overlay-focus accounting and accent colors.
const (
	paneChannels = 0
	paneMessages = 1
	paneNodes    = 2
)

// overlayKind — the main log is replaced by a contextual overlay when
// the user types /channels or /nodes. ESC closes and returns to input.
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayChannels
	overlayNodes
)

type channelItem struct {
	name    string
	private bool
	unread  int
}

type nodeItem struct {
	callsign  string
	state     string // "online", "offline", "failed", "muted"
	fav       bool
	lastHeard string // display string like "2m", "14:02", "3h"; caller-computed
	heardRank int    // lower = more recent; used purely for sort stability
	// Telemetry from the most recently heard packet — maps directly to
	// Meshtastic MeshPacket.rx_snr / rx_rssi, and hops computed as
	// hop_start - hop_limit. Used to build real /rs, /cqr, /ping
	// reports rather than faking numbers.
	lastSNR  string // e.g. "-8.5" (dB)
	lastRSSI string // e.g. "-92" (dBm)
	lastHops int    // 0 = direct, 1+ via intermediate mesh nodes
	hwModel  string // HardwareModel protobuf value — e.g. "T-Beam v1.1"
	firmware string // firmware_version — e.g. "2.3.4"
}

type messageItem struct {
	time   string
	from   string
	text   string
	mine   bool
	bang   string // empty or "!cq", "!cqr", "!qth", etc.
	status string // "", "ack", "fail", "system"
	acks   string // optional child line — "↳ 3 acks — ..."
	hops   int    // mesh hop count; 0 = direct/self
	snr    string // "-8.5" etc., empty to hide
}

type sortMode int

const (
	sortByLastHeard sortMode = iota
	sortByName
	sortByState
)

func (s sortMode) label() string {
	switch s {
	case sortByName:
		return "name"
	case sortByState:
		return "state"
	default:
		return "heard"
	}
}

type model struct {
	w, h int

	mode mode

	// Overlay state. The main log is always the default view; /channels
	// and /nodes commands (or their key shortcuts) pop an overlay pane
	// that temporarily replaces the log. ESC always returns to input.
	overlay overlayKind
	focused int // paneChannels / paneMessages / paneNodes — which overlay is active; meaningful as nav target

	selectedMsg int
	selectedCh  int
	selectedNd  int
	nodeSort    sortMode

	channels       []channelItem
	messages       []messageItem
	nodes          []nodeItem
	currentChannel string

	input       textinput.Model // always-on bottom input (messages OR /commands)
	searchInput textinput.Model
	searchQuery string        // committed search term
	nodeFilter  string        // callsign filter on scrollback; "" = all
	helpScroll  int           // scroll offset for help overlay
	splash      splashVariant // which BitchX-style banner is showing this launch
	tab         *tabState     // non-nil while cycling through Tab completions
	ctrlWPend   bool          // Ctrl+W armed — next key is a window nav (j/k/i/h/l/1/2/3)

	flash string
}

// handleTab runs one step of the Tab-completion cycle in the input.
// First press computes matches for the word under the cursor and
// inserts match 0. Subsequent presses cycle through the same set.
// dir == +1 cycles forward, dir == -1 cycles backward (Shift+Tab).
// Any non-Tab key clears m.tab in the Update dispatcher.
func (m *model) handleTab(dir int) {
	value := m.input.Value()
	cursor := m.input.Position()

	// First Tab of the cycle — compute matches for the current word.
	if m.tab == nil {
		matches, start, end := m.computeCompletions(value, cursor)
		if len(matches) == 0 {
			m.flash = "no completions"
			return
		}
		stem := value[start:end]
		m.tab = &tabState{matches: matches, cursor: 0, stem: stem, start: start, end: end}
	} else {
		// Already cycling — step and replace at last insertion range.
		n := len(m.tab.matches)
		if n == 0 {
			return
		}
		m.tab.cursor = (m.tab.cursor + dir + n) % n
	}

	match := m.tab.matches[m.tab.cursor]
	newText, newCursor := applyCompletion(value, m.tab.start, m.tab.end, match)
	m.input.SetValue(newText)
	m.input.SetCursor(newCursor)
	// Update end to the new replacement end so next cycle replaces
	// exactly what we just inserted (without the trailing space).
	m.tab.end = m.tab.start + len(match)

	// Feedback when multiple choices exist — irssi shows the set.
	if len(m.tab.matches) > 1 {
		m.flash = fmt.Sprintf("%d/%d  %s", m.tab.cursor+1, len(m.tab.matches),
			strings.Join(m.tab.matches, "  "))
	} else {
		m.flash = ""
	}
}

// RunDemo launches the Bubble Tea demo model.
func RunDemo() error {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func initialModel() model {
	// Always-on input bar at the bottom — composes messages, or runs
	// /commands when the line begins with "/". irssi-style.
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 200
	in.Placeholder = "type a message, or /help for commands"
	in.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(mhFG))
	in.CursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen))
	in.Focus()

	return model{
		mode:           modeSplash,
		focused:        paneMessages,
		selectedMsg:    2,
		currentChannel: "#primary",
		splash:         pickSplash(),
		channels: []channelItem{
			{name: "#primary", unread: 3},
			{name: "#admin", unread: 0},
			{name: "#emcomm", unread: 0},
			{name: "*secret*", private: true, unread: 1},
		},
		nodes: []nodeItem{
			{
				callsign: "KC7XYZ 🦀", state: "online", fav: true, lastHeard: "2m", heardRank: 2,
				lastSNR: "-8.5", lastRSSI: "-92", lastHops: 2, hwModel: "T-Beam v1.1", firmware: "2.3.4",
			},
			{
				callsign: "N0CALL", state: "online", lastHeard: "14s", heardRank: 0,
				lastSNR: "-5.0", lastRSSI: "-87", lastHops: 1, hwModel: "Heltec v3", firmware: "2.3.4",
			},
			{
				callsign: "W1ABC ⚡", state: "online", lastHeard: "1m", heardRank: 1,
				lastSNR: "-5.0", lastRSSI: "-89", lastHops: 1, hwModel: "RAK4631", firmware: "2.3.4",
			},
			{
				callsign: "KE0ABC", state: "failed", lastHeard: "8m", heardRank: 5,
				lastSNR: "-14.2", lastRSSI: "-108", lastHops: 4, hwModel: "T-Beam v1.1", firmware: "2.2.1",
			},
			{
				callsign: "Rural Signal 📡", state: "muted", lastHeard: "4m", heardRank: 3,
				lastSNR: "-11.2", lastRSSI: "-103", lastHops: 3, hwModel: "Station-G2", firmware: "2.3.4",
			},
			{
				callsign: "W9XYZ 🏔", state: "offline", lastHeard: "2h", heardRank: 99,
				lastSNR: "-16.0", lastRSSI: "-115", lastHops: 5, hwModel: "T-Deck", firmware: "2.1.0",
			},
		},
		messages: []messageItem{
			{time: "14:02", from: "KC7XYZ 🦀", text: "hello world", hops: 2, snr: "-8.5"},
			{time: "14:03", from: "me", mine: true, text: "hi", status: "ack", hops: 0},
			{
				time: "14:05",
				from: "Rural Signal 📡",
				bang: "!cq",
				text: "who's out there?",
				acks: "↳ 3 acks — KC7XYZ -8dB  W1ABC -11dB  N0CALL -14dB",
				hops: 3,
				snr:  "-11.2",
			},
			{
				time:   "14:06",
				from:   "me",
				mine:   true,
				bang:   "!cqr",
				text:   "copy 9/9, SNR -8.5, hop 1",
				status: "ack",
			},
			{time: "14:07", from: "W1ABC ⚡", text: "thanks for the test", hops: 1, snr: "-5.0"},
			{time: "14:08", from: "me", mine: true, text: "73 👋", status: "fail"},
			{
				time: "14:09",
				from: "KC7XYZ 🦀",
				bang: "!qth",
				text: "CN87 Seattle",
				hops: 2,
				snr:  "-9.1",
			},
			{
				time:   "14:10",
				from:   "me",
				mine:   true,
				text:   "roger, CN85 Portland here 🌲",
				status: "ack",
			},
			{time: "14:12", from: "", text: "N7DEF went offline", status: "system"},
		},
		input:       in,
		searchInput: func() textinput.Model { s := textinput.New(); s.Prompt = ""; s.CharLimit = 80; return s }(),
	}
}

func (m model) Init() tea.Cmd {
	// Auto-dismiss the splash after ~3s if the user hasn't touched a key.
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return splashTimeoutMsg{}
	})
}

// splashTimeoutMsg fires once ~3s after launch to auto-dismiss the
// BitchX-style banner even if the user doesn't press a key.
type splashTimeoutMsg struct{}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case splashTimeoutMsg:
		if m.mode == modeSplash {
			m.mode = modeInput
			m.input.Focus()
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.w = msg.Width
		m.h = msg.Height
		m.input.Width = m.w - 8
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeSplash:
			// Any key dismisses the splash and drops into input mode.
			if msg.String() == "ctrl+x" || msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			m.mode = modeInput
			m.input.Focus()
			return m, nil
		case modeSearch:
			return m.updateSearch(msg)
		case modeHelp:
			return m.updateHelp(msg)
		case modeNav:
			return m.updateNav(msg)
		default:
			return m.updateInput(msg)
		}
	}
	return m, nil
}

// openOverlay pops one of the named overlays (channels/nodes) over
// the log area and flips to nav mode so j/k immediately work inside
// it. ESC from the overlay returns to input mode.
func (m *model) openOverlay(kind overlayKind) {
	m.overlay = kind
	m.mode = modeNav
	m.input.Blur()
	switch kind {
	case overlayChannels:
		m.focused = paneChannels
	case overlayNodes:
		m.focused = paneNodes
	}
}

// closeOverlayToInput dismisses any open overlay, returns focus to the
// log, and moves the cursor back to the input bar. This is the
// canonical "land on typing" action that ESC always triggers.
func (m *model) closeOverlayToInput() {
	m.overlay = overlayNone
	m.focused = paneMessages
	m.mode = modeInput
	m.input.Focus()
	m.flash = ""
}

// updateInput is the irssi default mode — cursor is in the bottom
// input bar, typing composes. Special keys switch modes / open
// overlays / switch channels.
func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Ctrl+W prefix — vim window-nav across the stacked log / input.
	if m.ctrlWPend {
		m.ctrlWPend = false
		switch key {
		case "k", "up":
			m.mode = modeNav
			m.focused = paneMessages
			m.input.Blur()
		}
		return m, nil
	}

	// Any key that isn't Tab/Shift+Tab clears completion cycle state.
	if key != "tab" && key != "shift+tab" {
		m.tab = nil
	}

	switch key {
	case "ctrl+x":
		return m, tea.Quit
	case "ctrl+w":
		m.ctrlWPend = true
		return m, nil
	case "ctrl+c":
		// Ctrl+C on an empty input quits; on a populated input, clears.
		if m.input.Value() == "" {
			return m, tea.Quit
		}
		m.input.SetValue("")
		return m, nil
	case "esc":
		// ESC from input enters scrollback nav on the log. Another ESC
		// from nav lands you right back here — always <= 1 keystroke
		// to the input bar.
		m.mode = modeNav
		m.focused = paneMessages
		m.input.Blur()
		m.flash = ""
		return m, nil
	case "alt+1":
		m.switchChannelByIndex(0)
		return m, nil
	case "alt+2":
		m.switchChannelByIndex(1)
		return m, nil
	case "alt+3":
		m.switchChannelByIndex(2)
		return m, nil
	case "alt+4":
		m.switchChannelByIndex(3)
		return m, nil
	case "ctrl+n":
		m.cycleChannel(+1)
		m.tab = nil
		return m, nil
	case "ctrl+p":
		m.cycleChannel(-1)
		m.tab = nil
		return m, nil
	case "tab":
		m.handleTab(+1)
		return m, nil
	case "shift+tab":
		m.handleTab(-1)
		return m, nil
	case "enter":
		raw := strings.TrimSpace(m.input.Value())
		if raw == "" {
			return m, nil
		}
		m.input.SetValue("")
		if strings.HasPrefix(raw, "/") {
			cmd := m.executeCommand(strings.TrimPrefix(raw, "/"))
			return m, cmd
		}
		m.sendPlainMessage(raw)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// updateNav — scrollback / overlay selection mode. j/k walks the
// focused list, single letters run contextual commands. ESC (or i/q)
// always lands back at the input bar — canonical "where I type."
func (m model) updateNav(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Ctrl+W prefix — window-nav. `j` drops to the input bar.
	if m.ctrlWPend {
		m.ctrlWPend = false
		switch key {
		case "j", "down":
			m.closeOverlayToInput()
		}
		return m, nil
	}

	switch key {
	case "ctrl+x":
		return m, tea.Quit
	case "ctrl+c":
		return m, tea.Quit
	case "ctrl+w":
		m.ctrlWPend = true
		return m, nil
	case "esc", "i", "q":
		// Close any active overlay and land on the input bar.
		m.closeOverlayToInput()
		return m, nil
	case "j", "down":
		m.moveSelectionGrid(0, +1)
	case "k", "up":
		m.moveSelectionGrid(0, -1)
	case "h", "left":
		m.moveSelectionGrid(-1, 0)
	case "l", "right":
		m.moveSelectionGrid(+1, 0)
	case "g":
		m.jumpSelection(0)
	case "G":
		m.jumpSelection(-1)
	case "ctrl+d":
		for i := 0; i < 10; i++ {
			m.moveSelectionGrid(0, +1)
		}
	case "ctrl+u":
		for i := 0; i < 10; i++ {
			m.moveSelectionGrid(0, -1)
		}
	case "enter", " ":
		m.activate()
	case "/":
		m.mode = modeSearch
		m.searchInput.SetValue("")
		m.searchInput.Focus()
		return m, nil
	case "n":
		if m.searchQuery != "" {
			m.jumpToSearchHit(+1)
		}
	case "N":
		if m.searchQuery != "" {
			m.jumpToSearchHit(-1)
		}
	case "r":
		// Reply to the highlighted message — prefill /reply <sender>.
		target := m.selectedSender()
		if target != "" {
			m.prefillInput("/reply " + target + " ")
		}
	case "t":
		target := m.selectedSender()
		if target != "" {
			m.executeCommand("tr " + target)
		}
	case "p":
		target := m.selectedSender()
		if target != "" {
			m.executeCommand("ping " + target)
		}
	case "w":
		target := m.selectedSender()
		if target != "" {
			m.flash = fmt.Sprintf(
				"%s  ·  T-Beam v1.1  ·  fw 2.3.4  ·  last heard 2m  ·  online",
				target,
			)
		}
	case "*":
		m.actOnSelectedNode(func(n *nodeItem) {
			n.fav = !n.fav
			m.flash = fmt.Sprintf(
				"%s %s",
				n.callsign,
				toggleFlash(n.fav, "favorited", "unfavorited"),
			)
		})
	case "m":
		m.actOnSelectedNode(func(n *nodeItem) {
			if n.state == "muted" {
				n.state = "online"
				m.flash = fmt.Sprintf("%s unmuted", n.callsign)
			} else {
				n.state = "muted"
				m.flash = fmt.Sprintf("%s muted", n.callsign)
			}
		})
	case "s":
		if m.focused == paneNodes {
			m.nodeSort = (m.nodeSort + 1) % 3
			m.flash = fmt.Sprintf("nodes sorted by %s", m.nodeSort.label())
		}
	case "F":
		if m.focused == paneNodes {
			sorted := m.sortedNodes()
			if m.selectedNd < len(sorted) {
				m.nodeFilter = sorted[m.selectedNd].callsign
				m.focused = paneMessages
				m.selectedMsg = m.firstFilteredMsgIndex()
				m.flash = fmt.Sprintf("filter: %s  (X to clear)", m.nodeFilter)
			}
		}
	case "X":
		if m.nodeFilter != "" {
			m.nodeFilter = ""
			m.flash = "filter cleared"
		}
	case "?":
		m.mode = modeHelp
	}
	return m, nil
}

// selectedSender returns the callsign associated with the current
// selection. In the messages pane, that's the message's sender. In
// the nodes drawer, the highlighted callsign. Empty if no valid
// target (e.g. selection is a "me" message or a system notification).
func (m model) selectedSender() string {
	switch m.focused {
	case paneMessages:
		if m.selectedMsg < 0 || m.selectedMsg >= len(m.messages) {
			return ""
		}
		msg := m.messages[m.selectedMsg]
		if msg.mine || msg.from == "" {
			return ""
		}
		return msg.from
	case paneNodes:
		sorted := m.sortedNodes()
		if m.selectedNd < len(sorted) {
			return sorted[m.selectedNd].callsign
		}
	}
	return ""
}

// prefillInput returns focus to the input bar with the given text
// pre-populated and the cursor at the end — used by `r` reply to
// start composing a /reply without forcing the user to type the
// whole command from scratch.
func (m *model) prefillInput(text string) {
	m.mode = modeInput
	m.input.SetValue(text)
	m.input.CursorEnd()
	m.input.Focus()
}

// sendPlainMessage appends text as an outgoing message from "me" on
// the current channel. Real radio wiring will enqueue to ToRadio.
func (m *model) sendPlainMessage(text string) {
	m.messages = append(m.messages, messageItem{
		time: "14:13", from: "me", mine: true, text: text, status: "ack",
	})
	m.selectedMsg = len(m.messages) - 1
	m.flash = fmt.Sprintf("sent in %s", m.currentChannel)
}

// switchChannelByIndex jumps the active channel to the given index;
// out-of-range is a no-op.
func (m *model) switchChannelByIndex(i int) {
	if i < 0 || i >= len(m.channels) {
		return
	}
	m.currentChannel = m.channels[i].name
	m.channels[i].unread = 0
	m.selectedCh = i
	m.flash = fmt.Sprintf("switched to %s", m.channels[i].name)
}

// cycleChannel moves to the previous (-1) or next (+1) channel.
func (m *model) cycleChannel(dir int) {
	if len(m.channels) == 0 {
		return
	}
	cur := 0
	for i, c := range m.channels {
		if c.name == m.currentChannel {
			cur = i
			break
		}
	}
	next := (cur + dir + len(m.channels)) % len(m.channels)
	m.switchChannelByIndex(next)
}

// actOnSelectedNode resolves the selection to the CURRENT sorted view
// (what the user actually sees), finds that node in the underlying
// storage by callsign, and runs fn on it. Without this shim, every node
// action would index into the unsorted storage array and hit the wrong
// row — which is the "I selected KE0ABC but it muted Rural Signal" bug.
func (m *model) actOnSelectedNode(fn func(*nodeItem)) {
	if m.focused != paneNodes {
		return
	}
	sorted := m.sortedNodes()
	if m.selectedNd < 0 || m.selectedNd >= len(sorted) {
		return
	}
	target := sorted[m.selectedNd].callsign
	for i := range m.nodes {
		if m.nodes[i].callsign == target {
			fn(&m.nodes[i])
			return
		}
	}
}

// activate is the "open/select" action — Enter and Space in normal mode.
// Meaning depends on which pane is focused:
//   - channels: switch the messages pane to that channel
//   - nodes:    show whois / node info flash
//   - messages: expand selected message (hop, SNR, RSSI, hex id)
func (m *model) activate() {
	switch m.focused {
	case paneChannels:
		if m.selectedCh < len(m.channels) {
			c := m.channels[m.selectedCh]
			m.currentChannel = c.name
			m.channels[m.selectedCh].unread = 0
			m.flash = fmt.Sprintf("switched to %s", c.name)
			// Auto-jump focus to messages pane, mutt-style.
			m.focused = paneMessages
		}
	case paneNodes:
		sorted := m.sortedNodes()
		if m.selectedNd < len(sorted) {
			n := sorted[m.selectedNd]
			m.flash = fmt.Sprintf(
				"%s  ·  T-Beam v1.1  ·  fw 2.3.4  ·  last heard %s  ·  %s",
				n.callsign,
				n.lastHeard,
				n.state,
			)
		}
	case paneMessages:
		if m.selectedMsg < len(m.messages) {
			msg := m.messages[m.selectedMsg]
			if msg.status == "system" {
				m.flash = "system message — no metadata"
			} else if msg.mine {
				m.flash = fmt.Sprintf("to #primary  ·  hop 0  ·  ACK %s  ·  id 0x3f2a1b", ackWord(msg.status))
			} else {
				m.flash = fmt.Sprintf("from %s  ·  hop 2  ·  SNR -8.5  ·  RSSI -92  ·  id 0x3f2a1b", msg.from)
			}
		}
	}
}

func ackWord(status string) string {
	switch status {
	case "ack":
		return "ok"
	case "fail":
		return "timeout"
	default:
		return "pending"
	}
}

func (m *model) moveSelection(delta int) {
	switch m.focused {
	case paneChannels:
		m.selectedCh = clamp(m.selectedCh+delta, 0, len(m.channels)-1)
	case paneMessages:
		if m.nodeFilter != "" {
			m.selectedMsg = m.nextFilteredMsgIndex(delta)
			return
		}
		m.selectedMsg = clamp(m.selectedMsg+delta, 0, len(m.messages)-1)
	case paneNodes:
		m.selectedNd = clamp(m.selectedNd+delta, 0, len(m.nodes)-1)
	}
}

// moveSelectionGrid does 2D-aware navigation. On the users grid
// (paneNodes), j/k step one row (== `cols` cells) and h/l step one
// column. On linear panes (messages, channels), both axes collapse
// to a single linear step — j is "down one", k is "up one",
// and h/l behave the same as k/j so muscle memory still walks.
func (m *model) moveSelectionGrid(dx, dy int) {
	if m.focused != paneNodes {
		// Linear list: combine the two axes into one step.
		m.moveSelection(dx + dy)
		return
	}
	cols := m.userGridCols()
	if cols < 1 {
		cols = 1
	}
	delta := dx + dy*cols
	m.selectedNd = clamp(m.selectedNd+delta, 0, len(m.nodes)-1)
}

// userGridCols mirrors the layout math in renderNodesPane so
// navigation arithmetic matches what's actually on screen.
func (m model) userGridCols() int {
	inner := m.w - 4
	if inner < 18 {
		inner = 18
	}
	cellW := 22
	if inner >= 100 {
		cellW = 24
	}
	if inner < 60 {
		cellW = 18
	}
	cols := (inner + 1) / (cellW + 1)
	if cols < 1 {
		cols = 1
	}
	return cols
}

// firstFilteredMsgIndex returns the index of the first message whose
// sender matches the active node filter; falls back to 0 if none.
func (m model) firstFilteredMsgIndex() int {
	for i, msg := range m.messages {
		if m.msgMatchesFilter(msg) {
			return i
		}
	}
	return 0
}

// nextFilteredMsgIndex advances/rewinds selectedMsg to the next (+1)
// or previous (-1) message that matches the active node filter,
// skipping messages that don't match — so j/k jumps only through
// the filtered set.
func (m model) nextFilteredMsgIndex(delta int) int {
	if len(m.messages) == 0 {
		return 0
	}
	i := m.selectedMsg
	step := delta
	if step == 0 {
		step = 1
	}
	for k := 1; k <= len(m.messages); k++ {
		j := i + step*k
		if j < 0 || j >= len(m.messages) {
			return i
		}
		if m.msgMatchesFilter(m.messages[j]) {
			return j
		}
	}
	return i
}

// msgMatchesFilter is true when no filter is set or when the message
// is from the filtered node.
func (m model) msgMatchesFilter(msg messageItem) bool {
	if m.nodeFilter == "" {
		return true
	}
	return msg.from == m.nodeFilter
}

func (m *model) jumpSelection(to int) {
	switch m.focused {
	case paneChannels:
		m.selectedCh = resolveJump(to, len(m.channels))
	case paneMessages:
		m.selectedMsg = resolveJump(to, len(m.messages))
	case paneNodes:
		m.selectedNd = resolveJump(to, len(m.nodes))
	}
}

// lookupNode returns a pointer to the nodeItem matching callsign (exact
// case-insensitive match). nil if no such node is known. Every
// argumented ham command routes through this so we build reports from
// actual telemetry, never from placeholder text.
func (m *model) lookupNode(callsign string) *nodeItem {
	if callsign == "" {
		return nil
	}
	target := strings.ToLower(callsign)
	for i := range m.nodes {
		if strings.ToLower(m.nodes[i].callsign) == target {
			return &m.nodes[i]
		}
	}
	return nil
}

// signalReport renders the real-telemetry signal report for a node
// using its most recently heard packet's SNR/RSSI/hops. Used by /rs,
// /cqr, /ping — anywhere we'd otherwise fake a "copy 9/9" line.
func signalReport(n *nodeItem) string {
	parts := []string{}
	if n.lastHops > 0 {
		parts = append(parts, fmt.Sprintf("hop %d", n.lastHops))
	}
	if n.lastSNR != "" {
		parts = append(parts, fmt.Sprintf("SNR %s dB", n.lastSNR))
	}
	if n.lastRSSI != "" {
		parts = append(parts, fmt.Sprintf("RSSI %s dBm", n.lastRSSI))
	}
	if len(parts) == 0 {
		return "no telemetry yet"
	}
	return strings.Join(parts, ", ")
}

// executeCommand handles a slash command with the `/` prefix already
// stripped. Returns a tea.Cmd (e.g. tea.Quit) when the command needs
// to drive the runtime; nil otherwise.
func (m *model) executeCommand(raw string) tea.Cmd {
	if raw == "" {
		return nil
	}
	// Split into verb + rest for arg-taking commands.
	verb := raw
	rest := ""
	if sp := strings.IndexByte(raw, ' '); sp >= 0 {
		verb = raw[:sp]
		rest = strings.TrimSpace(raw[sp+1:])
	}

	switch verb {
	case "q", "quit", "exit":
		return tea.Quit

	// ── Ham-radio bang shortcuts ──────────────────────────────────
	// These are quick-command shorthands that compose and send the
	// underlying !bang message. Geeky, fast, and keeps the protocol
	// payload visible as normal message text so every other
	// Meshtastic client sees it as plain chat.

	case "cq":
		body := "CQ CQ CQ de KC7XYZ testing signals, please ack"
		if rest != "" {
			body = "CQ de KC7XYZ " + rest
		}
		m.sendBang("!cq", body)
		m.flash = "!cq broadcast — awaiting acks…"
	case "cqr":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /cqr <callsign>  (or highlight their CQ in nav mode)"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("no telemetry for %s — node unknown", target)
			return nil
		}
		m.sendBang("!cqr "+target, signalReport(n))
		m.flash = fmt.Sprintf("!cqr %s — copy report sent (%s)", target, signalReport(n))
	case "rs":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /rs <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("no telemetry for %s — node unknown", target)
			return nil
		}
		m.sendBang("!rs "+target, signalReport(n))
		m.flash = fmt.Sprintf("!rs %s — %s", target, signalReport(n))
	case "73":
		target := rest
		body := "73"
		if target != "" {
			body = "73 " + target
		}
		m.sendBang("!73", body)
		m.flash = "!73 sent"
	case "88":
		m.sendBang("!88", "88")
		m.flash = "!88 sent"
	case "qsl":
		m.sendBang("!qsl", "QSL")
		m.flash = "!qsl — acknowledged"
	case "qth":
		grid := rest
		if grid == "" {
			grid = "CN85 Portland"
		}
		m.sendBang("!qth", grid)
		m.flash = "!qth " + grid
	case "sked":
		target := rest
		if target == "" {
			m.flash = "usage: /sked <callsign>"
			return nil
		}
		m.sendBang("!sked "+target, "proposing scheduled contact, 24h from now")
		m.flash = fmt.Sprintf("!sked %s — proposal sent", target)

	// ── Extra ham/Meshtastic slang ────────────────────────────────
	case "qrz":
		// "Who is calling me?" — broadcast a prompt for identification.
		m.sendBang("!qrz", "QRZ? who's calling?")
		m.flash = "!qrz — asking for ID"
	case "qrm":
		// "You have man-made interference." Report to a station.
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /qrm <callsign>"
			return nil
		}
		m.sendBang("!qrm "+target, "QRM — interference on your signal")
		m.flash = fmt.Sprintf("!qrm %s — interference reported", target)
	case "qsb":
		// "Your signal is fading."
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /qsb <callsign>"
			return nil
		}
		m.sendBang("!qsb "+target, "QSB — signal fading, copy weak")
		m.flash = fmt.Sprintf("!qsb %s — fade reported", target)
	case "sk":
		// Final sign-off — stronger than /73. "Signing off clear."
		m.sendBang("!sk", "SK — clear and out 73")
		m.flash = "!sk — clear"
	case "wx":
		// Weather at my QTH. Optional argument supplies the conditions;
		// without one we emit a placeholder so the user types their own.
		wx := rest
		if wx == "" {
			wx = "clear 55°F light wind"
		}
		m.sendBang("!wx", wx)
		m.flash = "!wx — weather broadcast"
	case "grid":
		// Just the Maidenhead locator — shorter / more data-friendly
		// than /qth which also names the city.
		grid := rest
		if grid == "" {
			grid = "CN85"
		}
		m.sendBang("!grid", grid)
		m.flash = "!grid " + grid
	case "mesh":
		// Meshtastic-specific — summarize what the mesh looks like
		// from our vantage: number of nodes we can hear, by state.
		online, muted, offline := 0, 0, 0
		for _, n := range m.nodes {
			switch n.state {
			case "online":
				online++
			case "muted":
				muted++
			case "offline", "failed":
				offline++
			}
		}
		body := fmt.Sprintf("mesh view: %d online, %d muted, %d stale", online, muted, offline)
		m.sendBang("!mesh", body)
		m.flash = body
	case "k":
		// "Over — go ahead." Ragchew turn-taking.
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /k <callsign>"
			return nil
		}
		m.sendBang("!k "+target, "K — over, go ahead")
		m.flash = fmt.Sprintf("!k %s — over to you", target)

	// ── IRC-style operational commands ────────────────────────────
	case "tr", "traceroute", "trace":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /tr <callsign>  (or highlight a message in nav mode)"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("no route data for %s — node unknown", target)
			return nil
		}
		m.flash = fmt.Sprintf("trace %s — %d hops · %s", target, n.lastHops, signalReport(n))
	case "ping":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /ping <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("can't ping %s — node unknown", target)
			return nil
		}
		m.flash = fmt.Sprintf("ping %s — last heard %s · %s", target, n.lastHeard, signalReport(n))
	case "w", "whois":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /whois <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.flash = fmt.Sprintf("no record of %s", target)
			return nil
		}
		hw := n.hwModel
		if hw == "" {
			hw = "unknown hw"
		}
		fw := n.firmware
		if fw == "" {
			fw = "?"
		}
		m.flash = fmt.Sprintf("%s · %s · fw %s · heard %s · %s · %s",
			n.callsign, hw, fw, n.lastHeard, n.state, signalReport(n))
	case "r", "reply":
		if rest == "" {
			target := m.selectedSender()
			if target == "" {
				m.flash = "usage: /reply <callsign> <text>"
				return nil
			}
			m.prefillInput("/reply " + target + " ")
			return nil
		}
		// /reply <call> <text>
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			m.prefillInput("/reply " + rest + " ")
			return nil
		}
		target := rest[:sp]
		body := strings.TrimSpace(rest[sp+1:])
		if body == "" {
			m.prefillInput("/reply " + target + " ")
			return nil
		}
		m.messages = append(m.messages, messageItem{
			time: "14:13", from: "me", mine: true,
			text: "→" + target + ": " + body, status: "ack",
		})
		m.selectedMsg = len(m.messages) - 1
		m.flash = fmt.Sprintf("reply sent to %s", target)
	case "msg":
		// /msg <call> <text> — direct message, same shape as /reply with args.
		sp := strings.IndexByte(rest, ' ')
		if sp < 0 {
			m.flash = "usage: /msg <callsign> <text>"
			return nil
		}
		target := rest[:sp]
		body := strings.TrimSpace(rest[sp+1:])
		m.messages = append(m.messages, messageItem{
			time: "14:13", from: "me", mine: true,
			text: "→" + target + ": " + body, status: "ack",
		})
		m.selectedMsg = len(m.messages) - 1
		m.flash = fmt.Sprintf("DM sent to %s", target)
	case "join":
		if rest == "" {
			m.flash = "usage: /join <channel>"
			return nil
		}
		// Join by matching name; if not found, flash.
		for i, c := range m.channels {
			if c.name == rest || strings.TrimPrefix(c.name, "#") == rest {
				m.switchChannelByIndex(i)
				return nil
			}
		}
		m.flash = fmt.Sprintf("no channel named %s — /channel list", rest)
	case "part":
		m.flash = "/part — channel leave needs radio transport to wire"
	case "channels":
		m.openOverlay(overlayChannels)
	case "nodes", "users", "names":
		// nodes == users on a Meshtastic mesh — alias freely.
		m.openOverlay(overlayNodes)
	case "channel":
		if rest == "list" || rest == "" {
			m.openOverlay(overlayChannels)
			return nil
		}
		m.flash = "usage: /channel list  |  /channel add <meshtastic://url>"
	case "config":
		m.flash = "radio — KC7XYZ on T-Beam v1.1, fw 2.3.4, LongFast ch0, 3.94V 87%"
	case "help":
		m.mode = modeHelp
	case "search", "find":
		if rest == "" {
			m.flash = "usage: /search <pattern>"
			return nil
		}
		m.searchQuery = strings.ToLower(rest)
		if ok, count := m.jumpToSearchHit(+1); ok {
			m.flash = fmt.Sprintf("search: %d matches for %q", count, rest)
			m.mode = modeNav
			m.input.Blur()
		} else {
			m.flash = fmt.Sprintf("no match for %q", rest)
		}
	case "clear":
		m.messages = nil
		m.selectedMsg = 0
		m.flash = "scrollback cleared"

	default:
		m.flash = fmt.Sprintf("unknown /%s — see /help", verb)
	}
	return nil
}

// sendBang appends a new "me" message carrying a !bang protocol prefix
// and a body. Used by the `:cq` / `:73` / `:qth` etc. command family.
func (m *model) sendBang(bang, body string) {
	m.messages = append(m.messages, messageItem{
		time:   "14:13",
		from:   "me",
		mine:   true,
		bang:   bang,
		text:   body,
		status: "ack",
	})
	m.selectedMsg = len(m.messages) - 1
	m.focused = paneMessages
}

// updateHelp handles keys while the help overlay is visible. Vim-style
// scroll: j/k lines, d/u half-page, g/G top/bottom, q / ? / Enter /
// ESC dismiss. Ctrl+X still exits the whole app.
func (m model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+x", "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "?", "enter":
		m.mode = modeNav
		m.helpScroll = 0
		return m, nil
	case "j", "down":
		m.helpScroll++
	case "k", "up":
		if m.helpScroll > 0 {
			m.helpScroll--
		}
	case "d", "ctrl+d", "pgdown":
		m.helpScroll += 10
	case "u", "ctrl+u", "pgup":
		m.helpScroll -= 10
		if m.helpScroll < 0 {
			m.helpScroll = 0
		}
	case "g", "home":
		m.helpScroll = 0
	case "G", "end":
		m.helpScroll = 10000 // clamped in render
	}
	return m, nil
}

// updateSearch runs the `/` live-filter prompt. Enter commits the
// query, jumps the selection to the first match in the focused pane,
// and exits back to normal mode. ESC cancels + clears query.
func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNav
		m.searchInput.Blur()
		m.searchQuery = ""
		m.flash = "search cleared"
		return m, nil
	case "enter":
		q := strings.TrimSpace(m.searchInput.Value())
		m.searchQuery = strings.ToLower(q)
		m.mode = modeNav
		m.searchInput.Blur()
		if q == "" {
			m.flash = ""
			return m, nil
		}
		if ok, count := m.jumpToSearchHit(+1); ok {
			m.flash = fmt.Sprintf("search: %d matches for %q", count, q)
		} else {
			m.flash = fmt.Sprintf("no match for %q", q)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

// jumpToSearchHit scans the focused pane's list from the current
// selection and moves the selection to the next (+1) or previous (-1)
// row whose content contains searchQuery. Returns (found, totalMatches).
func (m *model) jumpToSearchHit(dir int) (bool, int) {
	q := m.searchQuery
	if q == "" {
		return false, 0
	}
	match := func(s string) bool { return strings.Contains(strings.ToLower(s), q) }

	var items []string
	var cur *int
	switch m.focused {
	case paneChannels:
		for _, c := range m.channels {
			items = append(items, c.name)
		}
		cur = &m.selectedCh
	case paneNodes:
		for _, n := range m.sortedNodes() {
			items = append(items, n.callsign)
		}
		cur = &m.selectedNd
	default:
		for _, msg := range m.messages {
			items = append(items, msg.from+" "+msg.text)
		}
		cur = &m.selectedMsg
	}
	total := 0
	for _, s := range items {
		if match(s) {
			total++
		}
	}
	if total == 0 {
		return false, 0
	}
	n := len(items)
	start := *cur + dir
	if dir == 0 {
		start = 0
	}
	for k := 0; k < n; k++ {
		i := (start + k*dir + n) % n
		if match(items[i]) {
			*cur = i
			return true, total
		}
	}
	return false, total
}

// ─── VIEW ─────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.w == 0 || m.h == 0 {
		return ""
	}

	// Splash takes over the whole screen — no chrome, no bars.
	if m.mode == modeSplash {
		return renderSplash(m.w, m.h, m.splash)
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

	left := label.Render("KC7XYZ")

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
			"NAV · j/k · r reply · t trace · p ping · * star · ESC back to input · / search · ? help",
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

func (m model) renderStatusBar() string {
	mesh := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen)).Bold(true)
	call := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen)).Bold(true)
	label := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	val := lipgloss.NewStyle().Foreground(lipgloss.Color(mhCyan))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(mhYellow))
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color(mhGreen)).Bold(true)
	sep := label.Render(" · ")

	left := mesh.Render(`//\`) + "  " + call.Render("KC7XYZ") + "  " + label.Render("Retr0h Base")

	mid := strings.Join([]string{
		val.Render("T-Beam v1.1") + " " + label.Render("fw 2.3.4"),
		val.Render("LongFast ch0"),
		warn.Render("14dBm"),
		val.Render("3.94V") + " " + label.Render("87%"),
		label.Render("noise ") + val.Render("-92dB"),
	}, sep)

	right := ok.Render("online") + label.Render("  [DEMO]")

	content := left + sep + mid + sep + right
	if lipgloss.Width(content) > m.w-2 {
		content = left + sep + right
	}
	if lipgloss.Width(content) > m.w-2 {
		content = left
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

// bigDigit renders the pane number as 5-row block-art in the pane's
// accent color. Used for the tmux-prefix-q-style overlay when the
// user triggers Ctrl+W.
func bigDigit(n int, color string) string {
	digits := map[int][]string{
		1: {
			"  ██  ",
			"████  ",
			"  ██  ",
			"  ██  ",
			"██████",
		},
		2: {
			"██████",
			"    ██",
			"██████",
			"██    ",
			"██████",
		},
		3: {
			"██████",
			"    ██",
			"██████",
			"    ██",
			"██████",
		},
	}
	s := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true)
	lines := digits[n]
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = s.Render(l)
	}
	return strings.Join(out, "\n")
}

// paneNumberOverlay returns a pane-filling block that centers a giant
// number — used when Ctrl+W is armed and we want to flash the pane
// index like tmux does with prefix+q.
func paneNumberOverlay(paneIdx, width, height int, focused bool) string {
	accent := paneAccentColor(paneIdx)
	border := lipgloss.NormalBorder()
	borderFg := lipgloss.Color(mhDrained)
	if focused {
		border = lipgloss.DoubleBorder()
		borderFg = lipgloss.Color(accent)
	}
	content := lipgloss.Place(
		width-2, height-2,
		lipgloss.Center, lipgloss.Center,
		bigDigit(paneIdx+1, accent),
	)
	return lipgloss.NewStyle().
		Border(border).
		BorderForeground(borderFg).
		Width(width - 2).
		Height(height - 2).
		Render(content)
}

// paneHeader renders a plain bold uppercase header in the pane's
// signature accent color when focused, quiet drained when not.
func paneHeader(text string, paneIdx int, focused bool) string {
	s := lipgloss.NewStyle().Bold(true)
	if focused {
		s = s.Foreground(lipgloss.Color(paneAccentColor(paneIdx)))
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

	var lines []string
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

	header := paneHeader("USERS", paneNodes, m.focused == paneNodes)
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

	lines := []string{header + count, "", legend, ""}
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

	nameColor := mhFG
	switch n.state {
	case "online":
		nameColor = mhGreen
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
	if selected {
		nameColor = meshGreen
	}

	sigilStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(sigilColor)).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(nameColor))
	if selected {
		nameStyle = nameStyle.Bold(true)
	}
	bracketStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	if selected {
		bracketStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen)).Bold(true)
	}

	// Compute how many cells the name can occupy. cellW = "[ S name  ] "
	// where S = sigil. Fixed chrome: "[ " (2) + sigil (1) + " " (1) + " ]" (2) = 6.
	nameBudget := cellW - 6
	if nameBudget < 3 {
		nameBudget = 3
	}
	name := padOrTruncate(n.callsign, nameBudget)

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
	startIdx := tailStartList(m.messages, rowsFree)
	if startIdx > 0 {
		lines = append(lines,
			lipgloss.NewStyle().
				Foreground(lipgloss.Color(mhDrained)).
				Italic(true).
				Render(fmt.Sprintf("   … %d earlier", startIdx)))
	}
	selected := m.focused == paneMessages && m.mode == modeNav
	for i := startIdx; i < len(m.messages); i++ {
		msg := m.messages[i]
		faded := m.nodeFilter != "" && !m.msgMatchesFilter(msg)
		bg := zebraBg(i)
		line := m.renderMessageRow(msg, i == m.selectedMsg && selected, width-4, bg)
		if faded {
			line = dimRow(line)
		}
		lines = append(lines, line)
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
		cost := 1
		if msgs[i].acks != "" {
			cost = 2
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
	tstamp := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhDrained)).
		Background(lipgloss.Color(rowBg))
	me := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhMagenta)).
		Background(lipgloss.Color(rowBg)).
		Bold(true)
	peer := lipgloss.NewStyle().
		Foreground(lipgloss.Color(mhCyan)).
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
	//    to show "who's talking" at a glance. Color maps to sender
	//    class:
	senderAccentColor := mhCyan
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

	// System messages render as a single italic line — still on tinted bg.
	if msg.status == "system" {
		line := accent + tstamp.Render("   "+msg.time+"  ") + sys.Render(msg.text)
		return wrapSelection(line, selected, m.isMsgSearchHit(msg), inner, rowBg)
	}

	// Flag column (1 col + space).
	flag := " "
	flagStyle := tstamp
	switch {
	case msg.status == "fail":
		flag = "!"
		flagStyle = fail
	case msg.bang != "":
		flag = "*"
		flagStyle = bang
	case msg.mine:
		flag = "›"
		flagStyle = me
	}

	// From column — 16 visible cells, truncated w/ ellipsis.
	fromRaw := msg.from
	if msg.mine {
		fromRaw = "me"
	}
	const fromW = 16
	senderStyle := peer
	if msg.mine {
		senderStyle = me
	}
	fromPadded := padOrTruncate(fromRaw, fromW)

	// Right-side metadata column: hops / SNR / ack — always in the
	// same column positions so rows line up visually as a grid.
	// ↝N = routed via N mesh hops; SNR in dB.
	hopCol := "      "
	if msg.hops > 0 {
		hopCol = fmt.Sprintf("↝%dh   ", msg.hops)
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

	txtRaw := msg.text
	if msg.bang != "" {
		txtRaw = msg.bang + " " + msg.text
	}
	txtClamped := padOrTruncate(txtRaw, textW)

	// Style the text: if it starts with a bang command, colorize just
	// the bang prefix in yellow; otherwise plain white.
	var styledTxt string
	if msg.bang != "" && strings.HasPrefix(txtClamped, msg.bang) {
		rest := strings.TrimPrefix(txtClamped, msg.bang)
		styledTxt = bang.Render(msg.bang) + text.Render(rest)
	} else {
		styledTxt = text.Render(txtClamped)
	}

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
	return wrapSelection(row, selected, m.isMsgSearchHit(msg), inner, rowBg)
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

// tailStart returns the index of the first message that should be
// rendered so that only the last `rowsBudget` rows of card output
// fit in view. Each regular card takes 3 rows (header+body+blank);
// cards with an acks sub-line take 4. The most recent message must
// always render, so worst case we show only the very last card.
func tailStart(msgs []messageItem, rowsBudget int) int {
	rows := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		cost := 3
		if msgs[i].acks != "" {
			cost = 4
		}
		if msgs[i].status == "system" {
			cost = 2
		}
		if rows+cost > rowsBudget {
			return i + 1
		}
		rows += cost
	}
	return 0
}

func (m model) renderMessageBlock(msg messageItem, selected bool, inner int) string {
	tstamp := lipgloss.NewStyle().Foreground(lipgloss.Color(mhDrained))
	me := lipgloss.NewStyle().Foreground(lipgloss.Color(mhMagenta)).Bold(true)
	peer := lipgloss.NewStyle().Foreground(lipgloss.Color(mhCyan)).Bold(true)
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(mhFG))
	ack := lipgloss.NewStyle().Foreground(lipgloss.Color(mhGreen))
	fail := lipgloss.NewStyle().Foreground(lipgloss.Color(mhPink)).Bold(true)
	bang := lipgloss.NewStyle().Foreground(lipgloss.Color(mhYellow)).Bold(true)
	sys := lipgloss.NewStyle().Foreground(lipgloss.Color(mhLavender)).Italic(true)
	hopStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen))

	// Content width must leave room for the selection gutter (3 cells)
	// that wrapSelection prepends. Anything composed here targets this
	// width, otherwise wrapSelection truncates and strips the right side.
	contentW := inner - gutterWidth
	if contentW < 20 {
		contentW = 20
	}

	if msg.status == "system" {
		return wrapSelection(
			tstamp.Render(msg.time+" ")+sys.Render(msg.text),
			selected,
			m.isMsgSearchHit(msg),
			inner,
		)
	}

	// ── Avatar: 2-char block of the sender's initials (or "me"), bg
	//    colored, like the circular avatars in the reference mobile UI.
	avatarFg, avatarBg := meshGreen, "#1c3a28"
	if msg.mine {
		avatarFg, avatarBg = mhMagenta, "#3a1c36"
	}
	avatar := lipgloss.NewStyle().
		Foreground(lipgloss.Color(avatarFg)).
		Background(lipgloss.Color(avatarBg)).
		Bold(true).
		Padding(0, 1).
		Render(initials(msg.from, msg.mine))
	avatarW := lipgloss.Width(avatar)

	// ── Header row: avatar · sender · time, then hop / SNR right-aligned.
	senderStyle := peer
	sender := msg.from
	if msg.mine {
		senderStyle = me
		sender = "me"
	}
	left := avatar + " " + senderStyle.Render(sender) + "  " + tstamp.Render(msg.time)

	var rightParts []string
	if msg.hops > 0 {
		rightParts = append(rightParts, hopStyle.Render(fmt.Sprintf("↝ %d hops", msg.hops)))
	}
	if msg.snr != "" {
		rightParts = append(rightParts, hopStyle.Render("SNR "+msg.snr))
	}
	switch msg.status {
	case "ack":
		rightParts = append(rightParts, ack.Render("✓"))
	case "fail":
		rightParts = append(rightParts, fail.Render("✗"))
	}
	right := strings.Join(rightParts, "  ")

	// Shed right-side fields progressively if the header won't fit.
	for len(rightParts) > 0 && lipgloss.Width(left)+lipgloss.Width(right)+2 > contentW {
		rightParts = rightParts[:len(rightParts)-1]
		right = strings.Join(rightParts, "  ")
	}
	headerRow := padBetween(left, right, contentW)

	// ── Body row: bang prefix + text, indented under the sender name.
	indent := strings.Repeat(" ", avatarW+1)
	body := indent
	if msg.bang != "" {
		body += bang.Render(msg.bang + " ")
	}
	body += text.Render(msg.text)

	lines := []string{headerRow, body}
	if msg.acks != "" {
		lines = append(lines, indent+sys.Render(msg.acks))
	}
	return wrapSelection(strings.Join(lines, "\n"), selected, m.isMsgSearchHit(msg), inner)
}

// initials returns 2 display-cells worth of sender marker for the
// avatar block. For "me" messages it uses "me"; otherwise the first
// alphanumeric/emoji pair found in the sender name.
func initials(from string, mine bool) string {
	if mine {
		return "me"
	}
	// Take the first two runes that aren't whitespace.
	var out []rune
	for _, r := range from {
		if r == ' ' || r == '\t' {
			if len(out) > 0 {
				break
			}
			continue
		}
		out = append(out, r)
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "??"
	}
	if len(out) == 1 {
		return string(out[0]) + " "
	}
	return string(out)
}

// padBetween joins left + right so that total visible width == targetW,
// with spaces filling the gap. If left+right already exceeds targetW,
// drops right.
func padBetween(left, right string, targetW int) string {
	if right == "" {
		return left
	}
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	if lw+rw+1 > targetW {
		return left
	}
	return left + strings.Repeat(" ", targetW-lw-rw) + right
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
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

// ─── utilities ────────────────────────────────────────────────────────

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

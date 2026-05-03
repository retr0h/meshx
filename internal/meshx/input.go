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

// input.go wires keyboard handling + mode transitions:
//
//   - updateInput / updateNav / updateSearch / updateHelp — the
//     per-mode KeyMsg handlers the top-level Update dispatcher
//     routes to.
//   - handleTab — command / channel / nick completion cycling.
//   - openOverlay / closeOverlayToInput / revealMessages /
//     prefillInput — the "swap mode and focus the right surface"
//     helpers shared across mode transitions.
//   - Selection movement (moveSelection, moveSelectionGrid,
//     jumpSelection, nextMsgIndexSkipGroups) and search/filter
//     helpers (jumpToSearchHit, firstFilteredMsgIndex, etc.) that
//     the nav handlers rely on.
//
// Top-level Update dispatcher + radio-message apply handlers stay
// in app.go. Slash-command handlers stay in commands.go. Pure
// rendering stays in ui.go.

package meshx

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

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
	newText, newCursor := applyCompletion(value, m.tab.start, m.tab.end, match.insert)
	m.input.SetValue(newText)
	m.input.SetCursor(newCursor)
	// Update end to the new replacement end so next cycle replaces
	// exactly what we just inserted (without the trailing space).
	m.tab.end = m.tab.start + len(match.insert)

	// Feedback when multiple choices exist — maxheadroom palette:
	// pink counter, active match in pink+bold against the drained
	// inactive set, dim · separators. Makes it obvious which of
	// three "retr0h" entries Tab is currently substituting.
	if len(m.tab.matches) > 1 {
		m.flash = tabCompletionFlashCell(m.tab.matches, m.tab.cursor)
	} else {
		m.flash = ""
	}
}

// RunDemo launches the Bubble Tea model with the canonical Demo
// fixture and no radio transport. Used for UI iteration, screenshots,
// and smoke testing the interface without a LoRa device handy.

func (m *model) openOverlay(kind overlayKind) {
	m.overlay = kind
	m.mode = modeNav
	m.input.Blur()
	switch kind {
	case overlayChannels:
		m.focused = paneChannels
	case overlayConfig:
		// /config gets its own pane focus so j/k routes to
		// selectedCfg instead of selectedMsg / selectedNd. Reset
		// the cursor to the top entry so a fresh open always
		// lands on the same row regardless of where it was the
		// last time the panel closed. Snapshot live state into
		// the draft buffer so per-row Enter mutates a clean copy
		// — no leaked draft state between sessions.
		m.focused = paneConfig
		m.selectedCfg = 0
		m.resetConfigDraft()
	case overlayNodes, overlayNearby, overlayRadar:
		// /nearby + /radar are peer-oriented surfaces — keep focus
		// on the nodes pane so j/k stepping lands where the user
		// expects, and `w` / `t` / `p` nav quick-keys resolve
		// against the highlighted peer in either view.
		m.focused = paneNodes
		// Reset the cursor when we switch BETWEEN peer-surface
		// overlays. Each one renders a different slice (/nodes =
		// all peers by sortedNodes; /nearby /radar = GPS-fix
		// subset by distance), so a selectedNd carried over from
		// /nodes would land on an arbitrary / out-of-range row
		// in the new overlay.
		m.selectedNd = 0
	}
}

// closeOverlayToInput dismisses any open overlay, returns focus to the
// log, and moves the cursor back to the input bar. This is the
// canonical "land on typing" action that ESC always triggers.
// Returns the cmd textinput.Focus() emits — callers MUST return it
// from Update so the cursor blink chain stays alive; see
// splashTimeoutMsg for why.
func (m *model) closeOverlayToInput() tea.Cmd {
	m.overlay = overlayNone
	m.focused = paneMessages
	m.mode = modeInput
	m.flash = ""
	return m.input.Focus()
}

// revealMessages is the "I just produced a message-pane entry, show
// it to the user" helper used by nav-mode keys like p/t/w that fire
// commands whose output lands as a systemBlock. Closes any open
// overlay, focuses the messages pane, and lands in input mode so the
// freshly-appended card renders in its plain drained sys style — not
// nav-mode's full-row selection highlight that extends past the
// text into the right margin and looks different from the same
// card produced via /whois from input mode.
func (m *model) revealMessages(flash string) tea.Cmd {
	m.overlay = overlayNone
	m.focused = paneMessages
	m.mode = modeInput
	m.flash = flash
	return m.input.Focus()
}

// updateInput is the irssi default mode — cursor is in the bottom
// input bar, typing composes. Special keys switch modes / open
// overlays / switch channels.
func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Ctrl+W prefix — vim window-nav across the stacked log / input.
	// From input (bottom pane), any direction hop lands on the
	// messages pane — there's nowhere else to go. Accepting j/k/up/
	// down all as aliases avoids the "I mashed Ctrl+W+j and nothing
	// happened" dead-end when the user's muscle memory disagrees
	// with vim's strict "j=down-only" semantics.
	if m.ctrlWPend {
		m.ctrlWPend = false
		switch key {
		case "j", "k", "up", "down":
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
	case "ctrl+u":
		// Readline / bash / vim-insert convention: kill the input
		// line back to the start. Takes priority over the
		// messages-pane scroll we used to bind here — that scroll
		// is still one Esc + Ctrl+U away in nav mode, plus PgUp
		// still works in input mode for the same effect. Clearing
		// the line is much more useful while composing.
		m.input.SetValue("")
		m.tab = nil
		return m, nil
	case "ctrl+k":
		// Readline kill-to-end-of-line — chops everything from
		// the cursor to the end of the input. Rounds out the
		// Ctrl+U (kill-to-start) and Ctrl+W (kill word, Windows
		// precedent) line-editing set.
		pos := m.input.Position()
		v := m.input.Value()
		if pos < len(v) {
			m.input.SetValue(v[:pos])
			m.input.SetCursor(pos)
		}
		m.tab = nil
		return m, nil
	case "pgup":
		// Messages-pane scroll kept on PgUp/PgDn for input-mode
		// users who want to glance back without Esc-ing to nav.
		// Ctrl+F / Ctrl+U freed up for readline-style line
		// editing now that shell/vim muscle memory wins.
		m.focused = paneMessages
		for i := 0; i < 10; i++ {
			m.moveSelectionGrid(0, -1)
		}
		return m, nil
	case "pgdown":
		m.focused = paneMessages
		for i := 0; i < 10; i++ {
			m.moveSelectionGrid(0, +1)
		}
		return m, nil
	case "tab":
		m.handleTab(+1)
		return m, nil
	case "shift+tab":
		m.handleTab(-1)
		return m, nil
	case "up":
		// Recall previous input. On first Up, stash whatever's in the
		// buffer so the user can Down-arrow back to it.
		if len(m.inputHistory) == 0 {
			return m, nil
		}
		if m.historyCursor == len(m.inputHistory) {
			m.historyDraft = m.input.Value()
		}
		if m.historyCursor > 0 {
			m.historyCursor--
		}
		m.input.SetValue(m.inputHistory[m.historyCursor])
		m.input.CursorEnd()
		return m, nil
	case "down":
		// Walk forward through history; past the newest entry restores
		// the user's in-progress draft.
		if m.historyCursor >= len(m.inputHistory) {
			return m, nil
		}
		m.historyCursor++
		if m.historyCursor == len(m.inputHistory) {
			m.input.SetValue(m.historyDraft)
		} else {
			m.input.SetValue(m.inputHistory[m.historyCursor])
		}
		m.input.CursorEnd()
		return m, nil
	case "enter":
		raw := strings.TrimSpace(m.input.Value())
		if raw == "" {
			m.flash = "nothing to send"
			return m, nil
		}
		m.input.SetValue("")
		// Push to history — deduplicate consecutive repeats so the
		// ring isn't full of the same thing.
		if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != raw {
			m.inputHistory = append(m.inputHistory, raw)
			// Cap at 200 entries — plenty for a session.
			if len(m.inputHistory) > 200 {
				m.inputHistory = m.inputHistory[len(m.inputHistory)-200:]
			}
		}
		m.historyCursor = len(m.inputHistory)
		m.historyDraft = ""

		if strings.HasPrefix(raw, "/") {
			cmd := m.executeCommand(strings.TrimPrefix(raw, "/"))
			return m, cmd
		}
		m.sendPlainMessage(raw)
		return m, nil
	}
	// Forward the keypress to the textinput, then enforce the
	// Meshtastic wire-level byte cap on the BODY portion only. The
	// "/verb " and optional "<target> " prefix is meshx chrome, not
	// wire bytes — counting them would force users to write a shorter
	// reply than the firmware actually supports. wirePayloadBytes
	// strips the command prefix so the cap reflects what'll actually
	// hit the LoRa link.
	prev := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if wirePayloadBytes(m.input.Value()) > meshtasticMaxTextBytes {
		m.input.SetValue(prev)
		m.input.SetCursor(len(prev))
		m.flash = byteCapFlash
	} else if m.flash == byteCapFlash {
		// User has edited off the cap — clear the sticky flash so
		// it doesn't linger after backspace / clear-line / send.
		m.flash = ""
	}
	return m, cmd
}

// byteCapFlash is the exact string used when the wire-limit enforcer
// rejects a keypress. Held as a constant so the clear-on-recovery
// branch above can match it precisely and drop the flash the moment
// the input drops below the cap.
var byteCapFlash = fmt.Sprintf(
	"message at %d-byte cap (Meshtastic payload limit)",
	meshtasticMaxTextBytes,
)

// wirePayloadBytes returns the byte length of the portion of `input`
// that would end up in Data.payload on the wire. For plain chat the
// whole line is payload. For /commands the verb + any target arg is
// meshx-internal chrome that doesn't go over the air; only the body
// (when the verb has one) counts. Local-only verbs (/help, /clear,
// /nodes, …) produce no payload at all and return 0.
//
// Intentionally simple: recognizes the verbs that *let the user
// author the body*. Verbs that auto-generate a body (/cqr, /rs,
// /73, /qsl, /sked, /qrz …) aren't listed here because whatever
// the user types after them isn't what gets transmitted — the
// dispatcher builds the body from templates.
func wirePayloadBytes(input string) int {
	if !strings.HasPrefix(input, "/") {
		return len(input)
	}
	rest := strings.TrimPrefix(input, "/")
	verb, rest, _ := strings.Cut(rest, " ")
	switch strings.ToLower(verb) {
	case "reply", "r":
		// /reply <target> <body> — body is what goes over the wire;
		// threading to the parent is carried in Data.reply_id, not
		// in the body, so the target arg is pure meshx chrome.
		_, body, hasBody := strings.Cut(rest, " ")
		if !hasBody {
			return 0
		}
		return len(body)
	case "msg":
		// /msg <target> <body> becomes "<target>: <body>" on the
		// wire (Meshtastic has no true DM on a public channel, so
		// the addressing has to live in the payload). Account for
		// the ": " separator so the counter reflects reality.
		target, body, hasBody := strings.Cut(rest, " ")
		if !hasBody {
			return 0
		}
		return len(target) + len(": ") + len(body)
	case "cq", "qth":
		// /cq [tail]  /qth [text] — the arg IS the body.
		return len(rest)
	default:
		// Any other /verb — either produces no wire payload (local
		// overlays, local config) or has an auto-generated body the
		// user can't size. Treat as 0 for the counter; sendBang's
		// own body is always comfortably under the cap.
		return 0
	}
}

// updateConfigEdit handles key events while the user is typing into
// an inline /config string-row textinput. Inner Enter commits the
// typed value into cfgDraft (per cfgEditing), Esc cancels the edit
// without touching the draft. All other keys flow through to the
// textinput so editing keys (arrows, backspace, etc.) work normally.
//
// On commit / cancel the model returns to modeNav so j/k resumes
// walking the panel. The Component re-renders the row from cfgDraft
// — what the user just typed shows up immediately if they committed,
// or reverts to whatever was in the draft if they Esc'd.
func (m model) updateConfigEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		v := strings.TrimSpace(m.cfgEditInput.Value())
		switch m.cfgEditing {
		case "longname":
			m.cfgDraft.longName = v
		case "shortname":
			m.cfgDraft.shortName = v
		}
		m.cfgEditing = ""
		m.cfgEditInput.Blur()
		m.mode = modeNav
		return m, nil
	case "esc":
		m.cfgEditing = ""
		m.cfgEditInput.Blur()
		m.mode = modeNav
		return m, nil
	}
	var cmd tea.Cmd
	m.cfgEditInput, cmd = m.cfgEditInput.Update(msg)
	return m, cmd
}

// updateNav — scrollback / overlay selection mode. j/k walks the
// focused list, single letters run contextual commands. ESC (or i/q)
// always lands back at the input bar — canonical "where I type."
func (m model) updateNav(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Discard-confirmation prompt — Esc on a dirty /config panel sets
	// m.cfgConfirmDiscard, the panel renders "discard X unsaved
	// changes? y/n", and any key other than y/n is a no-op while the
	// prompt is up. y discards and closes; n cancels the prompt and
	// keeps the user in nav so they can Ctrl+S instead.
	if m.cfgConfirmDiscard {
		switch key {
		case "y", "Y":
			m.cfgConfirmDiscard = false
			m.resetConfigDraft()
			return m, m.closeOverlayToInput()
		case "n", "N", "esc":
			m.cfgConfirmDiscard = false
			return m, nil
		}
		return m, nil
	}

	// Ctrl+S inside /config commits the draft. Lives at the top of
	// updateNav so it works regardless of selectedCfg / which row the
	// cursor is on — same shape vim's :w semantics have.
	if key == "ctrl+s" && m.overlay == overlayConfig {
		m.commitConfigDraft()
		return m, nil
	}

	// Ctrl+W prefix — window-nav. `j` drops to the input bar.
	if m.ctrlWPend {
		m.ctrlWPend = false
		switch key {
		case "j", "down":
			return m, m.closeOverlayToInput()
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
		// Close any active overlay and land on the input bar. If the
		// /config panel has unsaved edits, raise the discard prompt
		// instead — the next iteration of updateNav will read y/n
		// out of m.cfgConfirmDiscard and route accordingly.
		if m.overlay == overlayConfig && m.configDraftDirty() {
			m.cfgConfirmDiscard = true
			return m, nil
		}
		return m, m.closeOverlayToInput()
	// Channel hop is a global action — bind Alt+digit in nav mode
	// too so the keys don't silently no-op when the user is reading
	// /help or has an overlay open. Reuses the same closeOverlayToInput
	// path /channels selection takes so the post-switch state matches
	// (input bar focused, cursor blinking).
	case "alt+1":
		m.switchChannelByIndex(0)
		return m, m.closeOverlayToInput()
	case "alt+2":
		m.switchChannelByIndex(1)
		return m, m.closeOverlayToInput()
	case "alt+3":
		m.switchChannelByIndex(2)
		return m, m.closeOverlayToInput()
	case "alt+4":
		m.switchChannelByIndex(3)
		return m, m.closeOverlayToInput()
	case "ctrl+n":
		m.cycleChannel(+1)
		return m, m.closeOverlayToInput()
	case "ctrl+p":
		m.cycleChannel(-1)
		return m, m.closeOverlayToInput()
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
	// Half-page scroll — Ctrl+F / Ctrl+U bindings. Ctrl+D is kept
	// as an alias for Ctrl+F since both are common "half-page
	// down" shapes across vim / less / irssi; PgDown / PgUp cover
	// off-laptop keymaps. `d` / `u` retained as single-key
	// shortcuts in the grid where no textinput is active.
	case "ctrl+f", "ctrl+d", "f", "d", "pgdown":
		for i := 0; i < 10; i++ {
			m.moveSelectionGrid(0, +1)
		}
	case "ctrl+u", "u", "pgup":
		for i := 0; i < 10; i++ {
			m.moveSelectionGrid(0, -1)
		}
	case "enter", " ":
		// activate may return a tea.Cmd (textinput.Focus) when the
		// channels overlay just landed the user back on the input
		// bar — pass it through so the cursor blink chain stays
		// alive in the new mode.
		if cmd := m.activate(); cmd != nil {
			return m, cmd
		}
	case "/":
		m.mode = modeSearch
		m.searchInput.SetValue("")
		m.searchInput.Focus()
		return m, nil
	case "n":
		if m.searchQuery == "" {
			m.flash = "n: no search query — press `/` to start one"
			break
		}
		if ok, _ := m.jumpToSearchHit(+1); !ok {
			m.flash = fmt.Sprintf("n: no more matches for %q", m.searchQuery)
		}
	case "N":
		if m.searchQuery == "" {
			m.flash = "N: no search query — press `/` to start one"
			break
		}
		if ok, _ := m.jumpToSearchHit(-1); !ok {
			m.flash = fmt.Sprintf("N: no more matches for %q", m.searchQuery)
		}
	case "r":
		// Reply to the highlighted message — prefill /reply <sender>
		// AND stash the highlighted row's packetID in m.replyParent.
		// /reply consumes the stash (when set) instead of falling back
		// to replyTargetFor's "most-recent from this callsign" lookup,
		// so a thread anchors to message #3 when that's what the user
		// pointed at — not the latest from the same sender.
		target := m.selectedSender()
		if target != "" {
			m.replyParent = 0
			if m.selectedMsg >= 0 && m.selectedMsg < len(m.messages) {
				m.replyParent = m.messages[m.selectedMsg].packetID
			}
			return m, m.prefillInput("/reply " + target + " ")
		}
	case "R":
		// Resend a failed outbound message — rebuilds the ToRadio
		// envelope with a fresh packetID and flips the row back to
		// "pending" so the user sees the retransmit in flight.
		// Only valid on an own-row with status=="fail".
		if m.focused == paneMessages && m.selectedMsg >= 0 && m.selectedMsg < len(m.messages) {
			m.resend(m.selectedMsg)
		}
	case "P":
		// Pin/unpin the selected notice (whole group if grouped).
		// Pauses the TTL clock while pinned; unpin resumes the row
		// with the same remaining time budget it had when pinned.
		// Only fires on notice rows — toggleNoticePin is a no-op on
		// chat rows and permanent notices (expireAt == nil).
		if m.focused != paneMessages || m.selectedMsg < 0 || m.selectedMsg >= len(m.messages) {
			break
		}
		target := m.messages[m.selectedMsg]
		if target.expireAt == nil {
			m.flash = "P: this row isn't pinnable (chat / permanent notice)"
			break
		}
		willPin := !target.pinned
		m.toggleNoticePin(m.selectedMsg)
		if willPin {
			m.flash = "📌 pinned — timer paused"
		} else {
			m.flash = "↻ unpinned — timer resumed"
		}
	case "t":
		target := m.selectedSender()
		if target != "" {
			m.executeCommand("tr " + target)
			return m, m.revealMessages(fmt.Sprintf("traced %s — see messages", target))
		}
	case "p":
		target := m.selectedSender()
		if target != "" {
			m.executeCommand("ping " + target)
			return m, m.revealMessages(fmt.Sprintf("pinged %s — see messages", target))
		}
	case "w":
		target := m.selectedSender()
		if target != "" {
			// Delegate to the same code path /whois uses so nav-key
			// output stays in lock-step with the slash command.
			m.executeCommand("whois " + target)
			return m, m.revealMessages(fmt.Sprintf("whois %s — see messages", target))
		}
	case "*":
		var persistNum uint32
		var persistFav, persistMute bool
		m.actOnSelectedNode(func(n *nodeItem) {
			n.fav = !n.fav
			persistNum = m.nodeNumOf(n.callsign)
			persistFav = n.fav
			persistMute = n.state == stateMuted
			m.flash = fmt.Sprintf(
				"%s %s",
				n.callsign,
				toggleFlash(n.fav, "favorited", "unfavorited"),
			)
		})
		if persistNum != 0 {
			if m.store != nil {
				m.storagePersist(
					m.store.SaveNodePrefs(m.radioID, persistNum, persistFav, persistMute),
				)
			}
		}
	case "m":
		var persistNum uint32
		var persistFav, persistMute bool
		m.actOnSelectedNode(func(n *nodeItem) {
			if n.state == stateMuted {
				n.state = stateOnline
				m.flash = fmt.Sprintf("%s unmuted", n.callsign)
			} else {
				n.state = stateMuted
				m.flash = fmt.Sprintf("%s muted", n.callsign)
			}
			persistNum = m.nodeNumOf(n.callsign)
			persistFav = n.fav
			persistMute = n.state == stateMuted
		})
		if persistNum != 0 {
			if m.store != nil {
				m.storagePersist(
					m.store.SaveNodePrefs(m.radioID, persistNum, persistFav, persistMute),
				)
			}
		}
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

// selectedSender returns the target identifier associated with the
// current selection. In the messages pane, that's the message's
// sender callsign (using !<hex> via fromNum if the callsign is
// ambiguous across multiple radios). In the nodes drawer, the
// highlighted node's !<hex> when its callsign collides with another
// row's, otherwise the plain callsign. Empty if no valid target
// (e.g. selection is a "me" message or a system notification).
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
		// If the sender's callsign collides with another node in
		// m.nodes, prefer its node num so /whois /tr /ping land
		// exactly on the peer that sent THIS message, not another
		// radio that happens to share the longname.
		if msg.fromNum != 0 && m.isCallsignAmbiguous(msg.from) {
			return fmt.Sprintf("!%08x", msg.fromNum)
		}
		return msg.from
	case paneNodes:
		// The displayed slice depends on which overlay is open —
		// /nearby sorts by distance, /radar's "closest list" uses
		// the same order. /nodes uses the user-toggled nodeSort
		// (heard / name / state). Without overlay-aware resolution
		// here, j/k on /nearby walks the rendered rows but `w` /
		// `p` / `t` / `r` would look up `m.selectedNd` in a
		// totally different slice and target the wrong peer.
		sel, ok := m.selectedNodeItem()
		if !ok {
			return ""
		}
		if sel.nodeNum != 0 && m.isCallsignAmbiguous(sel.callsign) {
			return fmt.Sprintf("!%08x", sel.nodeNum)
		}
		return sel.callsign
	}
	return ""
}

// selectedNodeItem returns the nodeItem under the cursor in the
// currently-focused peer surface. Dispatches by overlay so /nodes,
// /nearby, and /radar's legend all resolve `m.selectedNd` against
// the slice they actually render. Returns (nil, false) when the
// slice is empty or the index is out of range.
//
// Without this indirection, any nav quick-key (w / p / t / r) that
// calls selectedSender in a non-/nodes overlay would index into the
// default /nodes sort and target an arbitrary peer — that was the
// "whois shows the wrong user" bug reported on /nearby.
func (m model) selectedNodeItem() (*nodeItem, bool) {
	switch m.overlay {
	case overlayNearby:
		// /nearby renders nearbyRoster (self prepended + sorted
		// plots). selection must use the same slice so `w` / `p`
		// / `t` / `r` target the highlighted row.
		roster := m.nearbyRoster()
		if m.selectedNd < 0 || m.selectedNd >= len(roster) {
			return nil, false
		}
		return roster[m.selectedNd].node, true
	case overlayRadar:
		// /radar's selection tracks the "closest peers" legend —
		// same distance-sorted plots slice (without self, which
		// is drawn at canvas centre).
		plots := m.collectPeerPlots()
		sortPlotsByDistance(plots)
		if m.selectedNd < 0 || m.selectedNd >= len(plots) {
			return nil, false
		}
		return plots[m.selectedNd].node, true
	default:
		sorted := m.sortedNodes()
		if m.selectedNd < 0 || m.selectedNd >= len(sorted) {
			return nil, false
		}
		sel := sorted[m.selectedNd]
		return &sel, true
	}
}

// isCallsignAmbiguous reports whether two or more nodes in m.nodes
// share the given callsign — triggers the !<hex> fallback used by
// selectedSender so nav-key commands (w, t, p, r) route to the
// exact selected radio rather than an arbitrary map-iteration pick.
func (m model) isCallsignAmbiguous(callsign string) bool {
	if callsign == "" {
		return false
	}
	count := 0
	for i := range m.nodes {
		if m.nodes[i].callsign == callsign {
			count++
			if count > 1 {
				return true
			}
		}
	}
	return false
}

// prefillInput returns focus to the input bar with the given text
// pre-populated and the cursor at the end — used by `r` reply to
// start composing a /reply without forcing the user to type the
// whole command from scratch. Returns the cmd textinput.Focus()
// emits — callers MUST return it from Update so the cursor blink
// chain stays alive.
func (m *model) prefillInput(text string) tea.Cmd {
	m.mode = modeInput
	m.input.SetValue(text)
	m.input.CursorEnd()
	return m.input.Focus()
}

// sendPlainMessage appends text as an outgoing message from "me" on
// the current channel. In live-radio mode it also enqueues a ToRadio
// text packet and persists the row so it survives a restart; in demo
// mode it just updates local state.

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
func (m *model) moveSelection(delta int) {
	switch m.focused {
	case paneConfig:
		// /config skips read-only / separator rows so j on the bottom
		// interactive entry doesn't dead-end on a divider line. Walk
		// the selectable index list, find where we currently sit, and
		// step delta entries through THAT slice — then map back to
		// the underlying configEntries() index.
		sel := m.selectableConfigEntryIndices()
		if len(sel) == 0 {
			return
		}
		cur := 0
		for i, idx := range sel {
			if idx == m.selectedCfg {
				cur = i
				break
			}
		}
		next := clamp(cur+delta, 0, len(sel)-1)
		m.selectedCfg = sel[next]
	case paneChannels:
		m.selectedCh = clamp(m.selectedCh+delta, 0, len(m.channels)-1)
	case paneMessages:
		if m.nodeFilter != "" {
			m.selectedMsg = m.nextFilteredMsgIndex(delta)
			return
		}
		m.selectedMsg = m.nextMsgIndexSkipGroups(delta)
	case paneNodes:
		// The visible slice depends on the active overlay. /nodes
		// shows every node; /nearby renders self-prepended roster;
		// /radar uses the peer-only plot list. Clamping against
		// the wrong slice lets the cursor wander past the last
		// rendered row — user-visible as "j stops highlighting
		// anything" for the middle of the list.
		maxIdx := len(m.nodes) - 1
		switch m.overlay {
		case overlayNearby:
			if count := len(m.nearbyRoster()); count > 0 {
				maxIdx = count - 1
			}
		case overlayRadar:
			if count := len(m.collectPeerPlots()); count > 0 {
				maxIdx = count - 1
			}
		}
		m.selectedNd = clamp(m.selectedNd+delta, 0, maxIdx)
	}
}

// nextMsgIndexSkipGroups moves the selection cursor by `delta` rows
// but treats a multi-line group (e.g. /whois output) as ONE unit.
// j lands on the first row of the next message or group; k lands on
// the first row of the previous one. Continuation rows are skipped.
func (m model) nextMsgIndexSkipGroups(delta int) int {
	if len(m.messages) == 0 {
		return 0
	}
	cur := m.selectedMsg
	step := 1
	if delta < 0 {
		step = -1
	}
	for k := 0; k < abs(delta); k++ {
		next := cur + step
		// Skip continuation rows of groups — land only on first rows.
		for next >= 0 && next < len(m.messages) {
			g := m.messages[next].group
			if g == 0 {
				break // not grouped — always a valid landing row
			}
			// Grouped — landing valid only if it's the group's first row.
			if next-step < 0 || next-step >= len(m.messages) ||
				m.messages[next-step].group != g {
				break
			}
			next += step
		}
		if next < 0 || next >= len(m.messages) {
			break
		}
		cur = next
	}
	return clamp(cur, 0, len(m.messages)-1)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
	// /nearby and /radar are single-column lists that SHARE the
	// paneNodes focus but don't use the 2D users-grid layout. A
	// `j` press on those overlays should step one row, not
	// `cols` rows like /nodes' bracketed grid needs. Route them
	// through the linear mover which already has the correct
	// overlay-aware bounds clamp.
	if m.overlay == overlayNearby || m.overlay == overlayRadar {
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

// nodeNumOf returns the Meshtastic node ID for a given callsign, or 0
// if the callsign isn't in our NodeDB. Used by /whois /qth /env to
// cross-reference peerPositions / peerEnv keyed by node ID. Uses the
// same exact → prefix → substring match order as lookupNode so
// emoji-suffixed callsigns resolve from partial input.

func (m model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+x", "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "?", "enter":
		// Return to the typing bar rather than nav mode —
		// ESC-from-help should land the user back where they
		// type, not drop them into scrollback selection. Matches
		// how the overlay-close helper treats /channels /nodes.
		m.helpScroll = 0
		return m, m.closeOverlayToInput()
	case "j", "down":
		m.helpScroll++
	case "k", "up":
		if m.helpScroll > 0 {
			m.helpScroll--
		}
	case "ctrl+f", "ctrl+d", "d", "pgdown":
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

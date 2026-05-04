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

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/retr0h/meshx/internal/driver"
	mdl "github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/pump"
	"github.com/retr0h/meshx/internal/meshx/storage"
	"github.com/retr0h/meshx/internal/sdk"
)

// model uses the canonical item types from model/. Local aliases
// keep TUI call sites readable.
type (
	channelItem = mdl.ChannelItem
	messageItem = mdl.MessageItem
	nodeItem    = mdl.NodeItem
)

// clientTag is the meshx self-identifier we splice into `/cq` beacons
// ("via meshx (github.com/retr0h/meshx)") — ham-customary "via <rig>"
// suffix so anyone who copies the CQ knows what client the caller is
// running. Kept as a named constant so any future command that wants
// to identify the client (e.g. a `/meshx` announcement) has one
// source of truth. Only the /cq path uses this today; everyday
// messages + reply verbs stay clean to keep the LoRa byte budget low.
const clientTag = "meshx (github.com/retr0h/meshx)"

// meshtasticMaxTextBytes is the practical byte cap on a single
// TEXT_MESSAGE_APP payload. Meshtastic's LoRa link carries ~237
// bytes of MeshPacket; once you subtract the protobuf header +
// encryption overhead, ~228 bytes remain for the Data.payload
// itself. Longer payloads get silently truncated by the firmware
// on the TX side — the wire only shows the cut-off version, and
// the sender's ACK fires regardless, so without a client-side cap
// users can silently lose the tail of anything they type. Matches
// the official Meshtastic Android / iOS apps' input limit.
const meshtasticMaxTextBytes = 228

// reconnectState is the persistent banner backing the "radio dropped
// — retry N/M in Ns" status while the pump cycles through its backoff
// schedule. Lives on the model so each noticeTickMsg can recompute the
// remaining-time portion without losing the original attempt count or
// error. readyAt is when the pump's backoff sleep is expected to end
// (the next dial attempt fires) — we display the diff against now.
// reconnectFlashText renders the current reconnect banner.
// Pump retries forever, so the previous "retry 3/∞" fraction read as
// truncation to users — replaced with prose: a leading "reconnecting"
// label, an attempt counter, a live countdown (or "dialing now" when
// the backoff has elapsed and a redial is in flight), and the last
// error trimmed to a digestible length so the line fits a typical
// terminal width without lipgloss eating its tail. Refreshed every
// noticeTickMsg so the countdown ticks live.
const reconnectErrMaxLen = 64

func (m model) reconnectFlashText() string {
	if m.Reconnect == nil {
		return ""
	}
	r := m.Reconnect
	remaining := time.Until(r.ReadyAt).Truncate(time.Second)
	tail := fmt.Sprintf("next try in %s", remaining)
	if remaining <= 0 {
		tail = "dialing now"
	}
	if r.Initial {
		// Startup banner. No attempts have failed yet, and we don't
		// know whether the pump is still inside its first
		// transport.Dial (e.g. BLE scan, max 8s) or has moved into
		// runSession and is waiting on the radio's NodeDB dump — so
		// we deliberately don't say "dialing now" (which would be a
		// lie post-Dial) or show a countdown (no retry timer is
		// running). Just a single, honest "connecting…" until either
		// the first dial fails (banner flips to the retry form via
		// mdl.Reconnecting) or ConfigComplete arrives (banner
		// clears via clearReconnectBanner).
		return "connecting…"
	}
	errText := ""
	if r.Err != nil {
		errText = " · " + truncateForFlash(r.Err.Error(), reconnectErrMaxLen)
	}
	return fmt.Sprintf(
		"reconnecting · attempt %d · %s%s",
		r.Attempt, tail, errText,
	)
}

// truncateForFlash clips a long error message to `n` runes, appending
// `…` when it had to cut. Operates on runes (not bytes) so unicode
// (the BLE error contains an em-dash) doesn't get sliced mid-codepoint.
func truncateForFlash(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// clearReconnectBanner drops the persistent reconnect status — call
// this whenever something proves the radio is back (any inbound frame
// that updates model state). Idempotent: zero-cost when there's no
// banner active. Also clears the flash slot itself so the status row
// goes blank rather than freezing on the last "retry N/M" line.
func (m *model) clearReconnectBanner() {
	if m.Reconnect == nil {
		return
	}
	m.Reconnect = nil
	m.flash = ""
	m.flashSeen = ""
}

// flashTTL is how long a flash message lingers in the status row
// after it stops changing. 5s is enough to read a typical line
// ("ack received", "search: 12 matches") without sticking past
// the user's attention; errors and reject messages also auto-fade
// rather than camping there forever. Drives the auto-clear path
// in the noticeTick handler.
const flashTTL = 5 * time.Second

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
	// modeConfigEdit — inline string-row edit inside /config. Active
	// when the user pressed Enter on the longname / shortname row;
	// key events route to cfgEditInput instead of the panel's nav
	// handler. Inner Enter commits to cfgDraft, Esc cancels.
	modeConfigEdit
)

// configDraft is the staged-edits buffer for the /config overlay.
// One field per row, populated from live state on open and diffed
// against live state on Ctrl+S. No wire traffic happens until commit.
type configDraft struct {
	buzzer    bool
	longName  string
	shortName string
}

// pingTimeoutMsg fires N seconds after a /ping goes out — same
// pattern as tracerouteTimeoutMsg. packetID guards against stale
// ticks colliding with a fresh /ping.
type pingTimeoutMsg struct {
	packetID uint32
}

// tracerouteTimeoutMsg fires N seconds after a /tr request goes out;
// if it still matches m.PendingTraceroute (i.e. no reply arrived)
// the handler emits a "no reply" systemBlock and clears the pending
// slot. packetID is captured at enqueue time so a stale tick from a
// previous /tr can't clobber a fresh one — same correlation pattern
// mdl.Routing uses.
type tracerouteTimeoutMsg struct {
	packetID uint32
}

// Pane indices — used for overlay-focus accounting and accent colors.
const (
	paneChannels = 0
	paneMessages = 1
	paneNodes    = 2
	// paneConfig — the /config overlay shares the messages-pane slot
	// (full-width) but has its own focus index so j/k/Enter route to
	// configPane's selectedCfg cursor instead of paneMessages's
	// selectedMsg. Same pattern as paneChannels / paneNodes.
	paneConfig = 3
)

// overlayKind — the main log is replaced by a contextual overlay when
// the user types /channels or /nodes. ESC closes and returns to input.
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayChannels
	overlayNodes
	// overlayNearby — "/nearby" list of peers sorted by distance
	// with a per-row bar, absolute km, bearing, and compass
	// abbreviation. Cold-sorted on open; doesn't auto-refresh
	// mid-display because a re-sort mid-scroll would yank the
	// cursor off whichever row the user was reading.
	overlayNearby
	// overlayRadar — "/radar" polar scope. You sit at the centre;
	// peers are plotted by bearing (azimuth) and distance (ring)
	// on a fixed grid. Re-renders every tick so new NodeInfo
	// + Position arrivals flow in live.
	overlayRadar
	// overlayConfig — "/config" interactive radio configuration
	// panel. Same chrome as channels/nodes overlays (j/k walks,
	// Enter activates). Currently surfaces the radio buzzer toggle
	// (writes ModuleConfig.external_notification.alert_message_buzzer
	// via AdminMessage.SetModuleConfig) plus a read-only block of
	// connection / firmware / region info. Future config knobs land
	// as additional rows in configEntries() — every interactive row
	// shares the same Enter-to-toggle UX so muscle memory carries.
	overlayConfig
)

// model is the Bubble Tea state. Session-state fields (nodes,
// messages, NodeDB indices, radio telemetry, in-flight ping/tr
// bookkeeping, …) live on the embedded *driver.State — which is
// also what a future headless `meshx serve` daemon will construct
// directly without dragging Bubble Tea in. TUI-only state (focus
// cursors, search query, splash, key bindings, flash banner, config
// draft, etc.) stays on model proper.
type model struct {
	*driver.State

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

	input       textinput.Model // always-on bottom input (messages OR /commands)
	searchInput textinput.Model
	searchQuery string // committed search term
	nodeFilter  string // callsign filter on scrollback; "" = all
	// replyParent is the packetID `r reply` captured from the row that
	// was highlighted when the user pressed `r` in nav mode. The
	// /reply command uses it to thread the outgoing message to the
	// SPECIFIC message the user pointed at — vs replyTargetFor()'s
	// fallback of "most recent from this sender", which is wrong when
	// the same callsign has 5 messages in the log and the user wants
	// to reply to message #3. Cleared on send so the stash never
	// bleeds into the next composition.
	replyParent uint32
	helpScroll  int           // scroll offset for help overlay
	splash      splashVariant // which BitchX-style banner is showing this launch
	tab         *tabState     // non-nil while cycling through Tab completions
	ctrlWPend   bool          // Ctrl+W armed — next key is a window nav (j/k/i/h/l/1/2/3)

	// Input history — every committed line (message or /command) is
	// pushed to inputHistory. Up / Down arrow in input mode walk the
	// ring, classic shell / irssi style. historyCursor = len(history)
	// means "at the blank line past the end" (i.e. fresh input).
	inputHistory  []string
	historyCursor int
	historyDraft  string // the line the user was typing before Up-arrowing — restored on Down past end

	// driver is the headless radio session layer — owns Pump (outbound
	// + reconnect) and Store (persistence) along with the *driver.State
	// the model embeds. Typed as the narrow radioDriver interface
	// (declared in tui/driver.go) so a test double or future in-process
	// variant can satisfy it without the concrete *driver.Driver.
	// Nil-safe: demo mode leaves PumpHandle and StoreHandle nil and
	// the session runs in-memory.
	driver radioDriver

	// remoteMode is true when the model is talking to a remote daemon
	// over HTTP+SSE rather than owning the radio in-process. Init()
	// branches on this — local mode fires openPumpMsg to spawn the
	// transport pump; remote mode skips it because the daemon owns
	// the pump and feeds events back via SSE.
	remoteMode bool

	// initialFocusCmd captures the tea.Cmd returned by
	// textinput.Focus() in newModel — the bubbles cursor blink
	// chain is driven by a cmd-per-tick loop, and the FIRST cmd
	// comes out of Focus(). Returning it from Init() is what
	// actually gets the cursor blinking; discarding it (which we
	// were doing) leaves the cursor stuck "on" with no animation.
	initialFocusCmd tea.Cmd

	// programSlot holds the *tea.Program once the bubbletea runtime is
	// up. The pump goroutine needs program.Send(), but tea.NewProgram
	// won't return the pointer until AFTER it has captured the model
	// value. The slot is a heap-allocated struct whose address is
	// stable across model copies — RunRadio creates one, stashes its
	// address here BEFORE NewProgram captures the model, then writes
	// program into the slot. Update reads slot.p when spawning the
	// pump. Replaces the previous package-level globalProgramRef so
	// state is per-Run and not visible outside this file.
	programSlot *programSlot

	// selectedCfg is the cursor index for the /config overlay (one
	// entry per row; j/k walks). Same accounting shape as selectedCh
	// / selectedNd / selectedMsg — clamped against configEntries() in
	// moveSelection.
	selectedCfg int

	// cfgDraft holds pending /config edits before they're committed
	// to the radio. Populated from live state when /config opens
	// (resetConfigDraft); per-row Enter mutates fields here without
	// any wire traffic. Ctrl+S walks the diff between draft and live
	// in commitConfigDraft and fires the appropriate AdminMessages.
	// Esc on a dirty draft prompts y/n via cfgConfirmDiscard.
	cfgDraft configDraft
	// cfgEditing names the field currently being edited via the
	// inline textinput — "" when no edit is active, "longname" or
	// "shortname" while the user types into cfgEditInput. Mode
	// transitions to modeConfigEdit so key events route to the
	// textinput instead of the panel's nav handler.
	cfgEditing string
	// cfgEditInput is the textinput used by the inline string-row
	// edit. Pre-filled with the current draft value on focus;
	// Enter commits the typed value to cfgDraft and returns to
	// nav, Esc cancels and reverts to whatever was in the draft.
	cfgEditInput textinput.Model
	// cfgConfirmDiscard is set when the user pressed Esc on a dirty
	// /config panel; while true the panel renders a y/n prompt and
	// the input handler short-circuits all keys except y/n.
	cfgConfirmDiscard bool

	flash string

	// flashSeen / flashSeenAt drive the auto-clear timer for flash.
	// 109 distinct sites set m.flash; refactoring all of them through
	// a setter would be churn for nothing. Instead, the noticeTick
	// handler observes flash on each tick — if the value hasn't
	// changed for flashTTL, it clears. flashSeen captures the last
	// observed text, flashSeenAt stamps when that observation began.
	flashSeen   string
	flashSeenAt time.Time
}

// RunRadio launches the Bubble Tea model connected to a live
// Meshtastic radio at `dest`. Starts empty — no canned channels,
// nodes, or messages — and populates as the handshake's FromRadio
// stream arrives. While the handshake is in progress the splash
// shows a small "connecting to <dest>…" status line.
//
// We defer spawning the transport pump until after program.Run()
// begins; tea.Program.Send() blocks until the program's main loop
// is accepting messages, so any p.Send() from a goroutine launched
// before Run() deadlocks. The model's Init() fires a tea.Cmd that
// returns a startPumpMsg, which is when we actually open the
// transport — by then the main loop is pumping messages.
func RunRadio(dest string) error {
	m := newModel(dest)
	// Close the persistence handle when the tea loop exits. Nil-safe:
	// if storage.New failed inside newModel, StoreHandle() is nil and
	// the close is a no-op.
	defer func() {
		if st := m.driver.StoreHandle(); st != nil {
			_ = st.Close()
		}
	}()
	// Allocate the program slot BEFORE handing the model to NewProgram —
	// tea takes the model by value, but m.programSlot is a pointer so
	// every copy of the model (the one tea holds, the ones produced by
	// Update) sees the same underlying struct. We fill slot.p AFTER
	// NewProgram returns; openPumpMsg reads it via m.programSlot.p.
	slot := &programSlot{}
	m.programSlot = slot
	program := tea.NewProgram(m, tea.WithAltScreen())
	slot.p = program
	defer func() { slot.p = nil }()

	_, err := program.Run()
	return err
}

// RunRadioRemote launches the TUI against a remote daemon. The daemon
// owns the radio (pump, storage, persistence); the TUI consumes its
// HTTP+SSE API. State is seeded from /radios/{id} + /channels +
// /nodes + /messages, then the SSE stream injects mdl.X events into
// the model's Update loop where the existing apply* handlers run.
//
// No Pump.Send happens client-side — outbound mdl.SendText goes
// through Remote.Send which POSTs to /radios/{id}/messages. No store
// either — StoreHandle returns nil and the apply* handlers' nil-check
// pattern (already present for demo mode) becomes a no-op.
func RunRadioRemote(serverURL, radioID string) error {
	r, err := sdk.NewRemote(serverURL, radioID)
	if err != nil {
		return err
	}
	defer r.Stop()

	m := newRemoteModel(r, serverURL)
	slot := &programSlot{}
	m.programSlot = slot
	program := tea.NewProgram(m, tea.WithAltScreen())
	slot.p = program
	defer func() { slot.p = nil }()

	// Wire SSE into the running tea program. program.Send is the
	// thread-safe way to inject messages from a goroutine into the
	// model's Update loop. tea.Program.Send takes a tea.Msg (which is
	// any), so the func(any) shape on Remote stays bubbletea-free.
	r.Start(func(msg any) { program.Send(msg) })

	_, err = program.Run()
	return err
}

// newRemoteModel builds the model for remote-client mode. Mirrors
// newModel's textinput + splash setup but skips storage open / replay
// / NodeDB hydration (the daemon owns persistence; State arrives
// pre-populated via Remote.seed).
func newRemoteModel(r *sdk.Remote, serverURL string) model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 0
	in.Placeholder = "type a message, or /help for commands"
	in.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(mhFG))
	in.Cursor.Style = lipgloss.NewStyle().
		Background(lipgloss.Color(mhPink)).
		Foreground(lipgloss.Color(mhCyan)).
		Bold(true)
	focusCmd := in.Focus()

	chosenSplash := pickSplash()
	m := model{
		State:           r.Session(),
		driver:          r,
		remoteMode:      true,
		mode:            modeInput,
		focused:         paneMessages,
		splash:          chosenSplash,
		input:           in,
		searchInput:     func() textinput.Model { s := textinput.New(); s.Prompt = ""; s.CharLimit = 80; return s }(),
		cfgEditInput:    func() textinput.Model { s := textinput.New(); s.Prompt = ""; s.CharLimit = 36; return s }(),
		initialFocusCmd: focusCmd,
	}

	// Surface the remote-mode endpoint as the first-line system row
	// so the user has a visible cue this isn't a local session — same
	// shape as local mode's "storage:" / "nodes:" notices.
	m.systemLine("remote: connected to " + serverURL)
	if n := len(m.Messages); n > 0 {
		m.systemLine(fmt.Sprintf("remote: replayed %d messages from daemon", n))
	}
	if n := len(m.Nodes); n > 0 {
		m.systemLine(fmt.Sprintf("remote: %d known peers from daemon", n))
	}
	// Splash card last so the BitchX banner sits at the bottom of the
	// log on launch — same convention as local mode.
	m.noticeCard(splashAsNotices(chosenSplash, m.myCallsign())...)
	return m
}

// teaProgramSink wraps *tea.Program to satisfy pump.Sink. tea.Msg is
// `any` and Send's bodies are identical, but Go's structural typing
// requires exact signature match — Send(tea.Msg) and Send(any) are
// distinct method sets even though the parameter types collapse to
// the same interface{}.
type teaProgramSink struct{ p *tea.Program }

// Send forwards an event into the running tea program. Called from
// the pump goroutine; tea.Program.Send is documented as goroutine-
// safe (it pushes onto an internal channel the runtime drains).
func (s teaProgramSink) Send(msg any) { s.p.Send(msg) }

// programSlot is a hand-off type that lets the model surface the
// running *tea.Program to Update without resorting to package-level
// state. RunRadio creates one, hands its address to the model before
// tea.NewProgram captures the value, then writes program into the
// slot once NewProgram returns. The pump goroutine needs Send and
// only Update calls startPump — so reads of slot.p are confined to
// the single goroutine that runs Update.
type programSlot struct {
	p *tea.Program
}

// pumpAttachedMsg hands the transport pump handle into the model so
// outbound messages (/cq, typed text) can enqueue ToRadio envelopes.
type pumpAttachedMsg struct{ p driver.Pump }

// shortFirmware trims Meshtastic's long firmware-version strings
// down to just the semver portion. "2.7.15.567b8ea" → "2.7.15" since
// the trailing git-short-sha means very little in the UI; users who
// care can see the full string in /config or /whois output.
func shortFirmware(fw string) string {
	if fw == "" {
		return "—"
	}
	// Count dots — keep up to the third (so "2.7.15" stays intact).
	dots := 0
	for i := 0; i < len(fw); i++ {
		if fw[i] == '.' {
			dots++
			if dots == 3 {
				return fw[:i]
			}
		}
	}
	return fw
}

// maidenhead converts lat/long (degrees) to a 6-character Maidenhead
// grid locator — the canonical ham location identifier (e.g. "CN85ow"
// for Portland, Oregon). Used for the top-bar display and /qth output.
func maidenhead(lat, lon float64) string {
	lon += 180.0
	lat += 90.0

	f1 := byte('A') + byte(lon/20)
	f2 := byte('A') + byte(lat/10)
	s1 := byte('0') + byte(mod(lon, 20)/2)
	s2 := byte('0') + byte(mod(lat, 10)/1)
	ss1 := byte('a') + byte(mod(lon, 2)/(5.0/60.0))
	ss2 := byte('a') + byte(mod(lat, 1)/(2.5/60.0))

	return string([]byte{f1, f2, s1, s2, ss1, ss2})
}

// mod is a positive-only float modulus — Go's builtin `math.Mod`
// preserves sign, but Maidenhead math wants the fractional part.
func mod(x, y float64) float64 {
	r := x - float64(int(x/y))*y
	if r < 0 {
		r += y
	}
	return r
}

// newModel builds the bubble-tea model for live-radio mode.
// Leaves everything at zero/empty; the transport pump fills state as
// FromRadio packets arrive and the UI shows "—" placeholders until they do.
func newModel(dest string) model {
	// Always-on input bar at the bottom — composes messages, or runs
	// /commands when the line begins with "/". irssi-style.
	in := textinput.New()
	in.Prompt = ""
	// CharLimit is textinput's own rune cap. Leave it disabled (0 =
	// unlimited) and let the byte-aware enforcer in updateInput be
	// the single source of truth for when to refuse new input. A
	// rune-based CharLimit interacts badly with the viewport scroll
	// math (Width) — empirically, setting CharLimit equal to the
	// byte cap can stop accepting new runes before the BYTE count
	// reaches the cap. Since wirePayloadBytes + the revert-on-
	// overflow guard already enforces the real wire limit, letting
	// textinput run unbounded here is simpler and correct.
	in.CharLimit = 0
	in.Placeholder = "type a message, or /help for commands"
	in.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(mhFG))
	// Hot-pink blinking block cursor — bubbles textinput defaults
	// to CursorBlink mode, so setting a bg colour on Cursor.Style
	// gives us an actual block. Pink pops against the mesh-green
	// prompt and the cyan data color without adding more green.
	in.Cursor.Style = lipgloss.NewStyle().
		Background(lipgloss.Color(mhPink)).
		Foreground(lipgloss.Color(mhCyan)).
		Bold(true)
	focusCmd := in.Focus()

	chosenSplash := pickSplash()
	sess := driver.NewState()
	sess.ConnectDest = dest
	// Defaults match a stock Meshtastic radio + a fresh meshX install
	// (radio buzzer beeps on text, terminal also dings). Overridden
	// below from the settings table when storage opens.
	sess.RadioBuzzerEnabled = true
	m := model{
		State:       sess,
		driver:      driver.New(sess, nil, nil),
		mode:        modeInput,
		focused:     paneMessages,
		splash:      chosenSplash,
		input:       in,
		searchInput: func() textinput.Model { s := textinput.New(); s.Prompt = ""; s.CharLimit = 80; return s }(),
		// cfgEditInput is the inline textinput that pops over the
		// /config longname / shortname rows. CharLimit caps at 36 —
		// the longest field is longname's 36-byte ceiling per the
		// Meshtastic User proto. Shortname rows still validate
		// separately on commit (4-byte cap there).
		cfgEditInput: func() textinput.Model {
			s := textinput.New()
			s.Prompt = ""
			s.CharLimit = 36
			return s
		}(),
		initialFocusCmd: focusCmd,
	}

	// Live-radio mode — open the persistence store and replay the
	// last chunk of history so the log survives restarts. We fail
	// open: any storage error (missing $HOME, bad perms, corrupt
	// db) just leaves m.driver.Store nil, and the session runs in-memory
	// for that boot. Losing history is preferable to crashing.
	if path, err := storage.DefaultPath(); err == nil {
		// Cast the concrete *storage.Sqlite to the Store interface
		// at the assignment site (osapi-io pattern) so the rest of
		// the model only ever sees the consumer-facing surface.
		if sqliteStore, err := storage.New(path); err == nil {
			var store driver.Store = sqliteStore
			m.driver.AttachStore(store)
			// Bind this session to a radio identity before any
			// storage call references radioID. ResolveRadioByConnection
			// returns the canonical "0xNNNNNNNN" form for radios
			// we've handshaken with before (cache hit on
			// radios.my_node_num); for never-seen-before connections
			// it returns a "pending:<transport>:<addr>" placeholder
			// the mdl.MyInfo handler swaps out via
			// ClaimRadioIdentity once the handshake reveals the
			// real my_node_num.
			transport, addr := storage.ParseRadioDest(dest)
			if rid, err := store.ResolveRadioByConnection(transport, addr); err == nil {
				m.RadioID = rid
			}
			// Hydrate persisted prefs early — /mute (terminal ding,
			// global) and /config's radio buzzer state (per-radio).
			// Both default "on" to match stock-radio + fresh-install
			// behaviour, so missing rows just leave the model
			// defaults from above untouched. ding_muted is a meshx-
			// CLIENT preference (terminal beep); pass "" for radioID
			// so it's stored once globally rather than once per radio.
			if v, ok := store.GetSetting("", "ding_muted"); ok {
				m.DingMuted = v == "on"
			}
			if v, ok := store.GetSetting(m.RadioID, "radio_buzzer"); ok {
				m.RadioBuzzerEnabled = v != "off"
			}
			// Load the cached NodeDB FIRST — every peer we've
			// ever resolved a real User for gets inserted into
			// m.Nodes / m.NodesByNum before anything else runs.
			// That way ghost-peer replay below skips any node we
			// already know by name, and message rows for
			// "node 0xd64b01be" instantly render as "WiobooJones"
			// if we've seen them in a previous session.
			if cached, err := store.LoadNodes(m.RadioID); err == nil {
				for _, n := range cached {
					name := n.LongName
					if name == "" {
						name = n.ShortName
					}
					// Carry prefs even for peers we still only
					// know by node num (user may have starred a
					// ghost before NodeInfo arrived); use the
					// placeholder longname so they still land
					// in m.Nodes.
					if name == "" {
						name = fmt.Sprintf("node 0x%x", n.NodeNum)
					}
					state := stateOffline
					if n.Muted {
						state = stateMuted
					}
					m.Nodes = append(m.Nodes, nodeItem{
						Callsign:  name,
						ShortName: n.ShortName,
						NodeNum:   n.NodeNum,
						State:     state,
						Fav:       n.Favorite,
						LastHeard: "cached",
						HwModel:   n.HwModel,
					})
					m.NodesByNum[n.NodeNum] = len(m.Nodes) - 1
				}
			}
			// Stale-pending sweep BEFORE replay — flip any
			// outbound row stuck "pending" past the cutoff to
			// "fail" so it renders as `✗` and the user can hit
			// `R` to resend.
			expired, _ := store.ExpireStalePendingMessages(m.RadioID, 5*time.Minute)
			// Primary channel is what the radio tells us, but at
			// boot we don't have it yet — replay under the name
			// we'll default to (empty string key until a channel
			// arrives). Load is by `currentChannel` so messages
			// migrate as the handshake resolves the channel name.
			if pastModels, err := store.LoadMessages(m.RadioID, "", 500); err == nil {
				past := make([]messageItem, 0, len(pastModels))
				for _, mm := range pastModels {
					// sanitizeMessageText runs on read so historic
					// rows written before sanitizeMessageText
					// landed pick up the ⚠ marker + dim styling
					// on next launch without a migration. The
					// Corrupted flag is recomputed from the text
					// every time, so a future sanitizer change
					// automatically re-evaluates the row.
					mm.Text, mm.Corrupted = sanitizeMessageText(mm.Text)
					past = append(past, messageItem{Message: mm})
				}
				baseIdx := len(m.Messages)
				m.Messages = append(m.Messages, past...)
				m.selectedMsg = len(m.Messages) - 1
				if m.selectedMsg < 0 {
					m.selectedMsg = 0
				}
				// Seed MessagesByPacketID so applyTextMessage can
				// dedupe when the radio's RAM queue replays a
				// packet we already persisted last session.
				// Entries with PacketID==0 (system rows) are
				// skipped — the zero key would collide and they
				// never arrive from the wire anyway.
				for i, msg := range past {
					if msg.PacketID == 0 {
						continue
					}
					m.MessagesByPacketID[msg.PacketID] = baseIdx + i
				}
				// Ghost-peer replay — every historical message
				// with a FromNum we haven't seen in m.Nodes gets
				// a synthesized firmware-default entry so /whois
				// / /cqr / /rs / /ping can target it by shortname
				// or hex without waiting for a fresh live packet.
				for _, msg := range past {
					if msg.FromNum == 0 {
						continue
					}
					if _, ok := m.NodesByNum[msg.FromNum]; ok {
						continue
					}
					long, short := defaultCallsign(msg.FromNum)
					m.Nodes = append(m.Nodes, nodeItem{
						Callsign:   long,
						ShortName:  short,
						NodeNum:    msg.FromNum,
						Unresolved: true,
						State:      stateOffline,
						LastHeard:  msg.Time,
						LastSNR:    msg.SNR,
						LastHops:   msg.Hops,
					})
					m.NodesByNum[msg.FromNum] = len(m.Nodes) - 1
				}
			}
			// Startup backfill — walk the replayed messages once
			// and push each sender's most recent sentAt onto the
			// corresponding node's LastHeardAt.
			touched := map[uint32]struct{}{}
			for _, past := range m.Messages {
				if past.FromNum == 0 || past.SentAt.IsZero() {
					continue
				}
				idx, ok := m.NodesByNum[past.FromNum]
				if !ok {
					continue
				}
				if past.SentAt.After(m.Nodes[idx].LastHeardAt) {
					m.Nodes[idx].LastHeardAt = past.SentAt
					m.Nodes[idx].LastHops = past.Hops
					if past.SNR != "" {
						m.Nodes[idx].LastSNR = past.SNR
					}
					touched[past.FromNum] = struct{}{}
				}
			}
			backfilled := len(touched)
			// Drain the migration trace from the process-level
			// boot buffer.
			for _, n := range store.ConsumeBootNotes() {
				m.systemLine("storage: " + n)
			}
			if backfilled > 0 {
				m.systemLine(fmt.Sprintf(
					"nodes: backfilled %d peer recency from message history", backfilled,
				))
			}
			if expired > 0 {
				m.systemLine(fmt.Sprintf(
					"messages: %d stale pending row(s) marked as failed — press R to resend",
					expired,
				))
			}
		}
	}
	// Splash notices come last so the BitchX greeter is the
	// newest entry in the log — sits right at the bottom above
	// the input bar on launch just like every other recent
	// message, and scrolls UP naturally as fresh chat arrives.
	// Pass the cached self callsign so the splash tagline reads
	// "as <callsign>" instead of a hardcoded credit.
	m.noticeCard(splashAsNotices(chosenSplash, m.myCallsign())...)
	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		// Kick off the cursor blink ticker. m.initialFocusCmd is
		// the tea.Cmd textinput.Focus() returned back in newModel —
		// it carries the correct cursor id/tag pair that subsequent
		// BlinkMsg rounds need to match for the blink chain to
		// continue. Using plain textinput.Blink here wouldn't work:
		// that cmd emits a BlinkMsg with zero id/tag, and bubbles
		// cursor silently drops mismatched BlinkMsgs so the chain
		// would die on tick #1.
		m.initialFocusCmd,
		// Start the notice TTL reaper loop — fires every second for
		// the life of the program. Cheap enough to always be on; the
		// handler is a one-pass scan of m.Messages guarded so it no-
		// ops when there's nothing expiring.
		noticeTickCmd(),
	}
	// Live-radio mode: kick off the pump from within the running
	// program. Deferring to Init (rather than RunRadio) guarantees
	// tea's main loop is up before the pump's first p.Send() — no
	// deadlock. The tea.Cmd returns an openPumpMsg which we handle
	// in Update by doing the actual Dial+spawn. Skipped in remote
	// mode — the daemon owns the pump there, we receive events over
	// SSE instead.
	if !m.remoteMode && m.ConnectDest != "" {
		cmds = append(cmds, func() tea.Msg {
			return openPumpMsg{dest: m.ConnectDest}
		})
	}
	return tea.Batch(cmds...)
}

// openPumpMsg is the "program is running, go open the radio" signal
// fired by Init(). Handled in Update which calls startPump and
// stashes the handle in the model.
type openPumpMsg struct{ dest string }

// noticeTickMsg drives the notice reap + fade redraw. Fires every
// second while the program is running; Update routes it through
// m.reapExpiredNotices (skipped in modeNav so mid-scroll reads don't
// vanish) and re-arms the next tick. 1s granularity is fine: the
// fade-window is 10s so the user sees ~10 discrete dim steps — not
// "smooth" but visibly aging, and the reap is per-second-accurate.
type noticeTickMsg struct{}

// noticeTickCmd produces the tea.Cmd that fires the next
// noticeTickMsg 1 second from now. Called from Init to start the
// loop and from the Update handler to keep it going.
func noticeTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return noticeTickMsg{}
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case noticeTickMsg:
		// Skip the reap while the user is navigating scrollback — a
		// row vanishing mid-read is the exact UX we're trying to
		// avoid. Expiry resumes as soon as they ESC back to input
		// (next tick catches up). The fade renderer also freezes in
		// modeNav, so while nav'd everything visually holds still.
		if m.mode != modeNav {
			m.reapExpiredNotices()
		}
		// Reconnect banner refresh: while the pump is in its retry
		// loop, repaint the flash every tick so the "in Ns"
		// countdown actually counts down. Also re-stamps flashSeenAt
		// so the auto-clear below treats it as fresh — important
		// because a 30s backoff would otherwise blow past flashTTL
		// silently. The banner clears the moment a radio frame
		// proves the link is back (mdl.MyInfo / ConfigComplete).
		//
		// EXCEPTION: once MyInfo has arrived (myNodeNum != 0) but
		// ConfigComplete hasn't, we're mid-handshake — let the live
		// "sync: N peers received" counter (set in mdl.NodeInfo)
		// own the flash. Otherwise this tick blows the counter away
		// every 250ms with the dial banner.
		if m.Reconnect != nil && m.MyNodeNum == 0 {
			m.flash = m.reconnectFlashText()
			m.flashSeen = m.flash
			m.flashSeenAt = time.Now()
			return m, noticeTickCmd()
		}
		// Flash auto-clear: if the flash text hasn't changed in
		// flashTTL, drop it. Stamp flashSeenAt when we first see a
		// new value so the timer restarts from when the user last
		// got new info, not from app start. Without this, transient
		// status messages ("ack received", "/tag rejected: …") sit
		// in the status row forever until something else overwrites
		// them — which often never happens.
		if m.flash != m.flashSeen {
			m.flashSeen = m.flash
			m.flashSeenAt = time.Now()
		} else if m.flash != "" && time.Since(m.flashSeenAt) > flashTTL {
			m.flash = ""
			m.flashSeen = ""
		}
		return m, noticeTickCmd()

	case openPumpMsg:
		// Program is running; safe to spawn the pump now.
		if m.programSlot == nil || m.programSlot.p == nil {
			m.flash = "internal error: program ref missing"
			return m, nil
		}
		// Park a "connecting" banner BEFORE starting the pump so the
		// status row reads "connecting · dialing now" the instant the
		// app is up — instead of going blank during the 8-second BLE
		// scan that the first dial does. The pump's reconnect path
		// will overwrite this with the full "reconnecting · attempt
		// N · …" banner if the first dial fails; the first radio
		// frame clears it.
		m.Reconnect = &driver.ReconnectState{
			Initial: true,
			ReadyAt: time.Now(),
		}
		m.flash = m.reconnectFlashText()
		m.flashSeen = m.flash
		m.flashSeenAt = time.Now()
		// Concrete *pump.Pump cast to the Pump interface at the
		// construction site (osapi-io). The compile-time assertion
		// here catches any drift the moment the interface gains a
		// method *pump.Pump doesn't implement. The Sink wrapper
		// adapts *tea.Program (whose Send takes tea.Msg) to
		// pump.Sink (whose Send takes any) — same underlying type,
		// different signature, so Go's structural typing needs the
		// trampoline.
		var p driver.Pump = pump.New(msg.dest, teaProgramSink{p: m.programSlot.p})
		m.driver.AttachPump(p)
		return m, nil

	case pumpAttachedMsg:
		m.driver.AttachPump(msg.p)
		return m, nil

	case mdl.MyInfo:
		m.MyNodeNum = msg.NodeNum
		// First MyNodeInfo of the session locks in the radio's
		// canonical identity — "0x" + hex(my_node_num), the same
		// shape the Meshtastic phone app + Python CLI use. If
		// m.RadioID is still a placeholder (a "pending:…" string
		// minted pre-handshake by resolveRadioByConnection, or the
		// legacy migration-009 seed UUID), claimRadioIdentity does
		// the atomic rewrite across radios + every FK column
		// (messages, nodes, settings) in one transaction. Already-
		// canonical IDs (we've handshaken with this radio before)
		// just refresh my_node_num + last_seen.
		if st := m.driver.StoreHandle(); st != nil {
			if newID, err := st.ClaimRadioIdentity(m.RadioID, msg.NodeNum); err == nil {
				m.RadioID = newID
			} else {
				m.storagePersist(err)
			}
		}
		// MyInfo = first frame of the handshake. Reset the received
		// counter to 0 and emit the start-of-sync notice so the
		// user sees node-list progress instead of staring at an
		// empty pane. The radio doesn't tell us the total NodeDB
		// size up front — only currently-known peers get re-broadcast
		// (the SQLite cache holds historical accumulation, which can
		// be much larger). So no denominator: just a running count.
		// Reconnect banner stays up until ConfigComplete; if the
		// link drops mid-handshake (BLE regularly does), the banner
		// re-appears in the correct state.
		if !m.Connected {
			m.SyncReceived = 0
			m.systemLine("sync: pulling NodeDB from radio…")
		}
		return m, nil

	case mdl.NodeInfo:
		m.upsertNode(msg)
		m.driver.PublishNodeInfo(msg)
		// While the handshake is still in flight (m.Connected stays
		// false until ConfigComplete), bump the received counter and
		// surface it in the chanRow flash via syncCounterFlash —
		// the running number renders bright (mesh-green bold) so it
		// reads as the active live signal, "peers received" stays
		// in the regular flash green. On ConfigComplete the
		// systemLine path takes over with the final tally.
		if !m.Connected && m.MyNodeNum != 0 {
			m.SyncReceived++
			m.flash = "sync: " + syncCounterFlash(m.SyncReceived) + " peers received"
		}
		return m, nil

	case mdl.ChannelInfo:
		m.applyChannel(msg)
		m.driver.PublishChannelInfo(msg)
		return m, nil

	case mdl.Text:
		cmd := m.applyTextMessage(msg)
		m.driver.PublishText(msg)
		return m, cmd

	case mdl.Routing:
		m.applyRouting(msg)
		m.driver.PublishRouting(msg)
		return m, nil

	case mdl.Traceroute:
		m.applyTraceroute(msg)
		m.driver.PublishTraceroute(msg)
		return m, nil

	case mdl.Ping:
		m.applyPing(msg)
		m.driver.PublishPing(msg)
		return m, nil

	case pingTimeoutMsg:
		// Same shape as tracerouteTimeoutMsg — surface "no reply"
		// only when the matching ping is still in flight.
		if m.PendingPing != nil && m.PendingPing.PacketID == msg.packetID {
			tgt := m.PendingPing.TargetCall
			m.systemBlock(
				fmt.Sprintf("ping %s", tgt),
				fmt.Sprintf("result:  no echo within %ds", pingTimeoutSeconds),
				"note:    target may be offline, out of range, or behind a dead relay",
			)
			m.flash = fmt.Sprintf("ping: no echo from %s", tgt)
			m.PendingPing = nil
		}
		return m, nil

	case mdl.ModuleBuzzer:
		// Live ExternalNotificationConfig — comes either as part of
		// the WantConfigId dump or in response to our explicit
		// AdminMessage_GetModuleConfigRequest. Either way, this is
		// the authoritative state. The buzzer "is on" only when
		// BOTH enabled AND alert_message_buzzer are true; either
		// false makes it silent regardless of the other.
		m.RadioBuzzerEnabled = msg.Enabled && msg.AlertMessageBuzzer
		m.RadioBuzzerKnown = true
		m.RadioBuzzerSnapshot = msg.Snapshot
		// Re-sync the persisted "last user intent" pref to whatever
		// the radio actually says — that way next launch's pre-
		// handshake guess matches reality instead of bouncing back
		// to a stale value the user changed via the phone app.
		v := "off"
		if m.RadioBuzzerEnabled {
			v = "on"
		}
		if st := m.driver.StoreHandle(); st != nil {
			m.storagePersist(st.PutSetting(m.RadioID, "radio_buzzer", v))
		}
		return m, nil

	case tracerouteTimeoutMsg:
		// Timeout for an outbound /tr. If the matching request is
		// still in flight (packetID matches), surface a "no reply"
		// systemBlock and clear the slot so a fresh /tr can fire.
		// Stale ticks (request already resolved or replaced) drop
		// silently — the packetID guard handles both cases.
		if m.PendingTraceroute != nil && m.PendingTraceroute.PacketID == msg.packetID {
			tgt := m.PendingTraceroute.TargetCall
			m.systemBlock(
				fmt.Sprintf("traceroute %s", tgt),
				fmt.Sprintf("result:  no reply within %ds", tracerouteTimeoutSeconds),
				"note:    target may be offline, out of range, or behind a dead relay",
			)
			m.flash = fmt.Sprintf("tr: no reply from %s", tgt)
			m.PendingTraceroute = nil
		}
		return m, nil

	case mdl.Metadata:
		m.RadioFirmware = msg.FirmwareVersion
		m.RadioDeviceState = msg.DeviceStateVer
		m.RadioHasWifi = msg.HasWifi
		m.RadioHasBT = msg.HasBluetooth
		return m, nil

	case mdl.LoraConfig:
		m.RadioTxPower = msg.TxPowerDBm
		m.RadioRegion = string(msg.Region)
		m.RadioModemPreset = string(msg.ModemPreset)
		return m, nil

	case mdl.DeviceMetrics:
		// Only apply metrics for our own node to the "my radio"
		// status fields. Peer metrics could later be upserted onto
		// their nodeItem for per-peer battery display.
		if msg.FromNodeNum == m.MyNodeNum || msg.FromNodeNum == 0 {
			m.BatteryLevel = msg.BatteryLevel
			m.BatteryVoltage = msg.Voltage
			m.ChannelUtil = msg.ChannelUtil
			m.AirUtilTx = msg.AirUtilTx
			m.HasTelemetry = true
		}
		return m, nil

	case mdl.DeviceConfig:
		m.RadioRole = string(msg.Role)
		return m, nil

	case mdl.Position:
		if m.PeerPositions == nil {
			m.PeerPositions = make(map[uint32]driver.PeerPosition)
		}
		m.PeerPositions[msg.FromNodeNum] = driver.PeerPosition{
			Latitude:  msg.Latitude,
			Longitude: msg.Longitude,
			Altitude:  msg.Altitude,
			Grid:      maidenhead(msg.Latitude, msg.Longitude),
			At:        msg.At,
		}
		// If this is our own position, also populate the top-bar grid.
		if msg.FromNodeNum == m.MyNodeNum {
			m.MyLatitude = msg.Latitude
			m.MyLongitude = msg.Longitude
			m.MyAltitude = msg.Altitude
			m.MyGrid = maidenhead(msg.Latitude, msg.Longitude)
		}
		return m, nil

	case mdl.EnvMetrics:
		if m.PeerEnv == nil {
			m.PeerEnv = make(map[uint32]driver.PeerEnvMetrics)
		}
		m.PeerEnv[msg.FromNodeNum] = driver.PeerEnvMetrics{
			Temperature: msg.Temperature,
			Humidity:    msg.Humidity,
			Pressure:    msg.Pressure,
			Gas:         msg.Gas,
			At:          time.Now(),
		}
		return m, nil

	case mdl.ConfigComplete:
		wasDisconnected := !m.Connected
		m.Connected = true
		// Definitive end of the handshake — NodeDB and config dump
		// have all arrived, the user can see live state. Drop the
		// reconnect banner now and not before; MyInfo isn't strong
		// enough on its own (see comment in the mdl.MyInfo case).
		m.clearReconnectBanner()
		// Initial-connect handshake (was disconnected, no /sync
		// pending): emit a completion notice with the peer count so
		// the user sees that the NodeDB pull finished. The earlier
		// "sync: pulling NodeDB" notice on MyInfo gives the start;
		// this one closes the loop.
		if wasDisconnected && m.SyncPendingGhosts == 0 {
			m.systemLine(fmt.Sprintf(
				"sync: complete — %d peers identified", len(m.Nodes),
			))
		}
		// Clear the handshake-progress counter so a future /sync
		// or post-disconnect rehandshake starts from a clean slate.
		m.SyncReceived = 0
		// If the user issued /sync and we snapshotted a ghost count,
		// emit a completion systemBlock with the delta so they see
		// what the re-dump actually changed. syncPendingGhosts > 0
		// means the snapshot had placeholders; == -1 is the sentinel
		// for "/sync fired with zero ghosts baseline"; == 0 means
		// this is the startup handshake and we stay quiet.
		if m.SyncPendingGhosts != 0 {
			current := 0
			for _, n := range m.Nodes {
				if strings.HasPrefix(n.Callsign, "node 0x") {
					current++
				}
			}
			baseline := m.SyncPendingGhosts
			if baseline < 0 {
				baseline = 0
			}
			resolved := baseline - current
			total := len(m.Nodes)
			m.systemBlock("sync complete",
				fmt.Sprintf("NodeDB re-dump done — %d peers in NodeDB", total),
				fmt.Sprintf("placeholders: %d → %d  (%d resolved this sync)", baseline, current, resolved),
			)
			m.SyncPendingGhosts = 0
		}
		// Otherwise no flash — the top status bar's "● online" dot is
		// the canonical connection indicator; flashing "radio
		// connected" at the bottom was duplicate signal in the same
		// mesh-green.
		//
		// Proactively pull the radio's actual ExternalNotification
		// module config — some firmware doesn't push it during the
		// WantConfigId dump, so without an explicit GetModuleConfig
		// the /config buzzer row would render our default-true
		// guess forever. The reply lands as another
		// FromRadio_ModuleConfig and routes through the same
		// mdl.ModuleBuzzer handler. Skipped if we already know
		// the state (the dump did contain it).
		if !m.RadioBuzzerKnown && m.driver.PumpHandle() != nil {
			m.driver.Send(mdl.RequestBuzzerConfig{})
		}
		return m, nil

	case mdl.Disconnected:
		m.Connected = false
		// Disconnect IS worth a flash — "● online" flips to
		// "● connecting" up top but users staring at the messages
		// pane need louder in-band feedback for a state change.
		m.flash = "radio disconnected"
		return m, nil

	case mdl.Reconnecting:
		// Transient drop — pump is going to retry. Flip the connected
		// flag so the top status bar shows "connecting" and stash a
		// reconnectState so noticeTickMsg can repaint the countdown
		// every second. The banner is sticky (auto-clear is bypassed
		// while m.Reconnect != nil) so the user can watch the retry
		// counter climb instead of seeing a 5s flash and then nothing
		// for the rest of a 30s backoff.
		m.Connected = false
		m.Reconnect = &driver.ReconnectState{
			Attempt: msg.Attempt,
			Err:     msg.Err,
			ReadyAt: time.Now().Add(msg.After),
		}
		m.flash = m.reconnectFlashText()
		m.flashSeen = m.flash
		m.flashSeenAt = time.Now()
		return m, nil

	case mdl.TransportError:
		// Pump exhausted its retry budget. Drop the reconnect banner
		// — there's nothing more happening — and switch to a regular
		// (auto-clearing) flash carrying the final error.
		m.Connected = false
		m.Reconnect = nil
		m.flash = fmt.Sprintf("radio error: %v", msg.Err)
		return m, nil

	case tea.WindowSizeMsg:
		m.w = msg.Width
		m.h = msg.Height
		m.input.Width = m.w - 8
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeSearch:
			return m.updateSearch(msg)
		case modeHelp:
			return m.updateHelp(msg)
		case modeConfigEdit:
			return m.updateConfigEdit(msg)
		case modeNav:
			return m.updateNav(msg)
		default:
			return m.updateInput(msg)
		}
	}
	// Any other message (cursor.BlinkMsg in particular — bubbles
	// uses it to alternate the cursor's on/off state every ~500ms)
	// gets forwarded to the input widget. Without this the blink
	// chain dies after the first tick since we'd never feed
	// BlinkMsg back into textinput.Update.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// openOverlay pops one of the named overlays (channels/nodes) over
// the log area and flips to nav mode so j/k immediately work inside
// it. ESC from the overlay returns to input mode.
func timeNowHHMM() string {
	return time.Now().Format("15:04")
}

// executeCommand handles a slash command with the `/` prefix already
// stripped. Returns a tea.Cmd (e.g. tea.Quit) when the command needs
// to drive the runtime; nil otherwise.

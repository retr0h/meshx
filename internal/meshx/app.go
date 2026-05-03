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
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
type reconnectState struct {
	// initial is true for the very first connect at app startup —
	// renders as "connecting" instead of "reconnecting" and skips
	// the attempt counter (no retries have happened yet). Cleared
	// the moment the pump actually emits its first
	// radioReconnectingMsg or the radio sends its first frame.
	initial bool
	attempt int
	err     error
	readyAt time.Time
}

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
	if m.reconnect == nil {
		return ""
	}
	r := m.reconnect
	remaining := time.Until(r.readyAt).Truncate(time.Second)
	tail := fmt.Sprintf("next try in %s", remaining)
	if remaining <= 0 {
		tail = "dialing now"
	}
	if r.initial {
		// Startup banner. No attempts have failed yet, and we don't
		// know whether the pump is still inside its first
		// transport.Dial (e.g. BLE scan, max 8s) or has moved into
		// runSession and is waiting on the radio's NodeDB dump — so
		// we deliberately don't say "dialing now" (which would be a
		// lie post-Dial) or show a countdown (no retry timer is
		// running). Just a single, honest "connecting…" until either
		// the first dial fails (banner flips to the retry form via
		// radioReconnectingMsg) or ConfigComplete arrives (banner
		// clears via clearReconnectBanner).
		return "connecting…"
	}
	errText := ""
	if r.err != nil {
		errText = " · " + truncateForFlash(r.err.Error(), reconnectErrMaxLen)
	}
	return fmt.Sprintf(
		"reconnecting · attempt %d · %s%s",
		r.attempt, tail, errText,
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
	if m.reconnect == nil {
		return
	}
	m.reconnect = nil
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

// pendingTraceroute is the in-flight /tr request the model tracks
// from outbound enqueue until the matching TRACEROUTE_APP reply (or
// timeout) lands. packetID is the MeshPacket.id we stamped on the
// request; matching against radioTracerouteMsg.requestID ignores
// foreign traceroutes that happen to be on the air. target is the
// callsign / node num the user asked to trace, kept around so the
// reply card can render "traceroute <call> — N hops" without
// re-resolving. requestedAt drives the round-trip-time readout in
// the result block + the timeout deadline.
type pendingTraceroute struct {
	packetID    uint32
	targetNum   uint32
	targetCall  string
	requestedAt time.Time
}

// tracerouteTimeoutMsg fires N seconds after a /tr request goes out;
// if it still matches m.pendingTraceroute (i.e. no reply arrived)
// the handler emits a "no reply" systemBlock and clears the pending
// slot. packetID is captured at enqueue time so a stale tick from a
// previous /tr can't clobber a fresh one — same correlation pattern
// radioRoutingMsg uses.
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

type channelItem struct {
	name    string
	private bool
	unread  int
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
	// group — non-zero value binds a sequence of rows as a single
	// logical entry. Used for /whois / /config / /env multi-line
	// "server reply" blocks so every line in the block shares the
	// same zebra stripe and visually reads as one card. Timestamp
	// is rendered only on the first row of a group; continuation
	// rows hide it.
	group uint64

	// packetID / replyID — Meshtastic text-message threading.
	// packetID is MeshPacket.id as seen on the wire; replyID is
	// Data.reply_id pointing at the message this one is answering.
	// Both zero for in-memory-only entries (system lines, demo
	// seeds that pre-date the feature).
	packetID uint32
	replyID  uint32

	// sentAt — absolute timestamp the message was received (live)
	// or originally persisted (replay from sqlite). Populated from
	// messages.created_at on replay; set to time.Now() on live
	// inbound/outbound. Used by newModel's startup backfill to
	// recompute each sender's lastHeardAt from replayed history so
	// /whois and the nodes overlay don't show stale state right
	// after a restart. Zero for in-memory-only entries.
	sentAt time.Time

	// fromNum — Meshtastic node num of the sender, captured at
	// ingest. Persisted so the renderer can backfill the displayed
	// callsign from m.nodesByNum LIVE (the stored `from` field is
	// only a snapshot at receive time — if NodeInfo arrives later
	// we'd otherwise be stuck showing "node 0xabc" forever). Zero
	// for "me" / system lines / demo seeds.
	fromNum uint32

	// corrupted — true when sanitizeMessageText replaced bad bytes
	// or dropped non-printable runes from the body. Drives the ⚠
	// marker + dim italic styling on the row so the user knows the
	// content isn't trustworthy without throwing away the salvageable
	// printable bits. Not persisted — recomputed from msg.text on
	// every replay so a future sanitizer change automatically
	// re-evaluates historic rows.
	corrupted bool

	// style — notice-row styling knob set by the m.notice() writer.
	// nil for chat rows; non-nil for every `-!-` entry (storage,
	// whois, splash banner, future error/success pulses). Lets the
	// renderer pick body fg / bold / center off one struct instead
	// of branching on ad-hoc fields. See notices.go for the shape
	// and defaults.
	style *noticeStyle

	// expireAt — non-nil for command-triggered notices (/whois,
	// /ping, /config, …) stamped by m.notice / m.noticeCard.
	// The reap tick drops rows whose expireAt has passed (whole
	// groups atomically); during the last noticeFadeWindow the
	// renderer lerps fg toward rowBg so the row visibly dims
	// before it vanishes. Nil = permanent — chat rows, splash
	// banner, storage/error alerts. See notices.go.
	expireAt *time.Time

	// pinned — user has paused this row's TTL via `p` in modeNav
	// or `/pin`. While true, the reap tick skips the row and the
	// renderer suppresses the fade. `⌜ … ⌟` diagonal corners mark
	// the group in the pane. pinnedRemaining captures time.Until
	// (expireAt) at the moment pin was toggled on so a subsequent
	// unpin can re-stamp expireAt = now + pinnedRemaining and the
	// row resumes with the same time budget it had.
	pinned          bool
	pinnedRemaining time.Duration
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

	// Demo fixture — non-nil means "running off canned data, no radio
	// transport". When set, its values pre-populate the same model
	// fields the live radio would fill (myNodeNum, radioFirmware,
	// batteryLevel, etc.), so every renderer has one code path: read
	// model state. isDemo() is `m.demo != nil`.
	demo *Demo

	// SQLite handle — non-nil ONLY in live-radio mode. Every incoming
	// text packet and every outgoing /command message gets persisted
	// so the log survives a restart. Demo mode never persists (canned
	// data has no business in the real log). See storage.go for the
	// schema and the open / save helpers.
	db *sql.DB

	// initialFocusCmd captures the tea.Cmd returned by
	// textinput.Focus() in newModel — the bubbles cursor blink
	// chain is driven by a cmd-per-tick loop, and the FIRST cmd
	// comes out of Focus(). Returning it from Init() is what
	// actually gets the cursor blinking; discarding it (which we
	// were doing) leaves the cursor stuck "on" with no animation.
	initialFocusCmd tea.Cmd

	// syncPendingGhosts — snapshot of unresolved-peer count at the
	// moment a /sync fires. When the next radioConfigCompleteMsg
	// lands we diff against the current count to emit a summary
	// systemLine ("sync complete — N peers identified"). Zero means
	// no /sync is in flight; setting it to -1 signals "pending but
	// initial count was zero" to disambiguate from the start-of-day
	// ConfigComplete that fires after handshake.
	syncPendingGhosts int

	// syncReceived backs the live peer counter shown in the chanRow
	// flash during the NodeDB handshake. Bumps once per
	// radioNodeInfoMsg between MyInfo and ConfigComplete; reset to 0
	// at MyInfo (start) and at ConfigComplete (end). No denominator
	// because meshtastic doesn't surface the radio's expected
	// NodeDB total up front.
	syncReceived int

	// storageAlerted — once any saveMessage / saveNode /
	// saveNodePrefs fails, flip this flag and emit a single
	// systemLine so the user knows persistence is degraded. Every
	// subsequent save-error stays silent so a bad db doesn't
	// machine-gun the messages pane. In-memory operation continues
	// normally — losing persistence is preferable to crashing.
	storageAlerted bool

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

	// Live-radio state. Zero-value is "demo mode — no transport".
	pump        *pump          // non-nil when connected to a real radio
	connectDest string         // "" = demo, else "/dev/cu.usbmodem2101" / tcp host
	connected   bool           // true once ConfigComplete arrives
	myNodeNum   uint32         // populated by MyNodeInfo
	nodesByNum  map[uint32]int // radio node id → m.nodes index, for O(1) upsert
	// messagesByPacketID — Meshtastic wire packet id → m.messages
	// index, used to dedupe replays. On startup we populate this
	// from the SQLite backfill; when the radio drains its RAM queue
	// after WantConfigId, applyTextMessage checks here first and
	// upgrades the existing row (ack state, signal telemetry)
	// instead of appending a duplicate. Entries with packetID==0
	// (system rows, demo seeds, pre-2.x local-only sends) never
	// go in this map because their zero key would collide.
	messagesByPacketID map[uint32]int

	// Telemetry + config snapshot — populated as FromRadio packets
	// arrive. Zero-value means "not yet received" so renderers can
	// show a — placeholder without branching on a separate flag.
	radioFirmware    string // FromRadio.Metadata.firmware_version
	radioDeviceState uint32 // FromRadio.Metadata.device_state_version
	radioHasWifi     bool
	radioHasBT       bool
	radioTxPower     int32   // Config.lora.tx_power (dBm)
	radioRegion      string  // Config.lora.region (e.g. "US")
	radioModemPreset string  // Config.lora.modem_preset (e.g. "LONG_FAST")
	batteryLevel     uint32  // DeviceMetrics.battery_level (0-100, >100 = powered)
	batteryVoltage   float32 // DeviceMetrics.voltage
	channelUtil      float32 // DeviceMetrics.channel_utilization (%)
	airUtilTx        float32 // DeviceMetrics.air_util_tx (%)
	hasTelemetry     bool    // true once first DeviceMetrics arrives
	radioRole        string  // Config.device.role — CLIENT / ROUTER / etc.
	myLatitude       float64 // our own GPS position
	myLongitude      float64
	myAltitude       int32
	myGrid           string // Maidenhead grid square for myLat/myLon

	// Per-peer position + environment metrics. Keyed by node num,
	// matches nodesByNum so /qth and /env can look these up.
	peerPositions map[uint32]peerPosition
	peerEnv       map[uint32]peerEnvMetrics

	flash string

	// dingMuted gates the terminal BEL emit on inbound text packets.
	// Toggled by /mute, persisted under settings.ding_muted so the
	// preference rides across restarts. Default false (=ding on)
	// matches a fresh-install Meshtastic radio's stock notification
	// behavior; the user has to deliberately silence meshX.
	dingMuted bool
	// radioBuzzerEnabled mirrors the radio's
	// ModuleConfig.external_notification.alert_message_buzzer field
	// — true when the LoRa radio beeps on incoming text. We track it
	// locally because the firmware doesn't push module config on
	// every connect; the value is persisted to settings.radio_buzzer
	// so the /config panel renders the right state immediately on
	// next launch instead of showing a stale guess until the user
	// re-runs the toggle. Defaults true to match a stock radio.
	radioBuzzerEnabled bool
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

	// pendingTraceroute tracks an in-flight /tr request so the
	// inbound TRACEROUTE_APP reply can correlate back. Non-nil
	// while a discovery is on the wire; cleared by applyTraceroute
	// when the matching reply lands or by tracerouteTimeoutMsg when
	// the deadline elapses with no reply.
	pendingTraceroute *pendingTraceroute
	// reconnect is non-nil while the pump is in its retry loop. Each
	// radioReconnectingMsg refreshes the struct; noticeTickMsg uses it
	// to repaint the flash with a live "in Ns" countdown so the user
	// can watch the backoff clock tick instead of staring at a frozen
	// "in 30s" for half a minute. Cleared the moment a radio frame
	// proves we're back (radioMyInfoMsg or radioConfigCompleteMsg) or
	// the pump gives up (radioErrorMsg). While set, the flash
	// auto-clear is bypassed — the user explicitly wants persistent
	// status during a retry storm, otherwise the message disappears
	// after 5s and looks like meshx forgot it was reconnecting.
	reconnect *reconnectState

	// flashSeen / flashSeenAt drive the auto-clear timer for flash.
	// 109 distinct sites set m.flash; refactoring all of them through
	// a setter would be churn for nothing. Instead, the noticeTick
	// handler observes flash on each tick — if the value hasn't
	// changed for flashTTL, it clears. flashSeen captures the last
	// observed text, flashSeenAt stamps when that observation began.
	flashSeen   string
	flashSeenAt time.Time
}

type peerPosition struct {
	latitude  float64
	longitude float64
	altitude  int32
	grid      string
	at        time.Time
}

type peerEnvMetrics struct {
	temperature float32
	humidity    float32
	pressure    float32
	gas         float32
	at          time.Time
}

// RunDemo launches the tea program with the canonical Demo fixture
// and no radio transport — used for screenshots, UI iteration, and
// try-before-you-buy without a LoRa device on hand.
func RunDemo() error {
	p := tea.NewProgram(newModel(DefaultDemo(), ""), tea.WithAltScreen())
	_, err := p.Run()
	return err
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
	m := newModel(nil, dest)
	// Close the persistence handle when the tea loop exits. Nil-safe:
	// if openStorage failed inside newModel, m.db is nil and the
	// close is a no-op.
	defer func() {
		if m.db != nil {
			_ = m.db.Close()
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

// pumpAttachedMsg hands the transport pump pointer into the model so
// outbound messages (/cq, typed text) can enqueue ToRadio envelopes.
type pumpAttachedMsg struct{ p *pump }

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

// newModel builds the bubble-tea model for either demo mode
// (demo != nil) or live-radio mode (demo == nil, dest != "").
//
// Demo mode plugs the Demo fixture's values into the SAME model
// fields a live radio would populate (myNodeNum, nodes, channels,
// radioFirmware, batteryLevel, …) so every renderer runs one code
// path that reads model state — no parallel demo-vs-live branches.
//
// Live-radio mode leaves everything at zero/empty; the transport
// pump fills state as FromRadio packets arrive and the UI shows
// "—" placeholders until they do.
func newModel(demo *Demo, dest string) model {
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
	m := model{
		mode:               modeInput,
		focused:            paneMessages,
		splash:             chosenSplash,
		connectDest:        dest,
		demo:               demo,
		nodesByNum:         make(map[uint32]int),
		messagesByPacketID: make(map[uint32]int),
		peerPositions:      make(map[uint32]peerPosition),
		peerEnv:            make(map[uint32]peerEnvMetrics),
		input:              in,
		searchInput:        func() textinput.Model { s := textinput.New(); s.Prompt = ""; s.CharLimit = 80; return s }(),
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
		initialFocusCmd:    focusCmd,
		// Defaults match a stock Meshtastic radio + a fresh meshX
		// install (radio buzzer beeps on text, terminal also dings).
		// Overridden below from the settings table when storage opens.
		radioBuzzerEnabled: true,
	}

	if demo == nil {
		// Live-radio mode — open the persistence store and replay the
		// last chunk of history so the log survives restarts. We fail
		// open: any storage error (missing $HOME, bad perms, corrupt
		// db) just leaves m.db nil, and the session runs in-memory
		// for that boot. Losing history is preferable to crashing.
		if path, err := defaultStoragePath(); err == nil {
			if db, notes, err := openStorage(path); err == nil {
				m.db = db
				// Hydrate persisted prefs early — /mute (terminal ding)
				// and /config's radio buzzer state. Both default "on"
				// to match stock-radio + fresh-install behaviour, so
				// missing rows just leave the model defaults from
				// above untouched.
				if v, ok := getSetting(db, "ding_muted"); ok {
					m.dingMuted = v == "on"
				}
				if v, ok := getSetting(db, "radio_buzzer"); ok {
					m.radioBuzzerEnabled = v != "off"
				}
				// Load the cached NodeDB FIRST — every peer we've
				// ever resolved a real User for gets inserted into
				// m.nodes / m.nodesByNum before anything else runs.
				// That way ghost-peer replay below skips any node we
				// already know by name, and message rows for
				// "node 0xd64b01be" instantly render as "WiobooJones"
				// if we've seen them in a previous session. This is
				// the equivalent of the phone app's persistent
				// NodeDB — the radio itself forgets NodeInfo after
				// a while, so a client that trusts only the radio's
				// live dump has amnesia on every reconnect.
				if cached, err := loadNodes(db); err == nil {
					for _, n := range cached {
						name := n.longName
						if name == "" {
							name = n.shortName
						}
						// Carry prefs even for peers we still only
						// know by node num (user may have starred a
						// ghost before NodeInfo arrived); use the
						// placeholder longname so they still land
						// in m.nodes.
						if name == "" {
							name = fmt.Sprintf("node 0x%x", n.nodeNum)
						}
						state := "offline"
						if n.muted {
							state = "muted"
						}
						m.nodes = append(m.nodes, nodeItem{
							callsign:  name,
							shortName: n.shortName,
							nodeNum:   n.nodeNum,
							state:     state,
							fav:       n.favorite,
							lastHeard: "cached",
							hwModel:   n.hwModel,
						})
						m.nodesByNum[n.nodeNum] = len(m.nodes) - 1
					}
				}
				// Stale-pending sweep BEFORE replay — any outbound
				// row still marked "pending" from a prior session
				// crashed mid-flight; its ACK window is long closed
				// and nothing will ever land. Flip those to "fail"
				// so they render as `✗` and the user can hit `R` to
				// resend from history. 5 minutes is plenty for any
				// in-flight ACK the radio might deliver on this
				// launch; anything older is dead.
				expired, _ := expireStalePendingMessages(db, 5*time.Minute)
				// Primary channel is what the radio tells us, but at
				// boot we don't have it yet — replay under the name
				// we'll default to (empty string key until a channel
				// arrives). Load is by `currentChannel` so messages
				// migrate as the handshake resolves the channel name.
				if past, err := loadMessages(db, "", 500); err == nil {
					baseIdx := len(m.messages)
					m.messages = append(m.messages, past...)
					m.selectedMsg = len(m.messages) - 1
					if m.selectedMsg < 0 {
						m.selectedMsg = 0
					}
					// Seed messagesByPacketID so applyTextMessage can
					// dedupe when the radio's RAM queue replays a
					// packet we already persisted last session.
					// Entries with packetID==0 (system rows, demo
					// seeds) are skipped — the zero key would
					// collide and they never arrive from the wire
					// anyway.
					for i, msg := range past {
						if msg.packetID == 0 {
							continue
						}
						m.messagesByPacketID[msg.packetID] = baseIdx + i
					}
					// Ghost-peer replay — every historical message
					// with a fromNum we haven't seen in m.nodes gets
					// a synthesized firmware-default entry so /whois
					// / /cqr / /rs / /ping can target it by shortname
					// or hex without waiting for a fresh live packet.
					// Using defaultCallsign rather than msg.from keeps
					// the row consistent with how live applyTextMessage
					// ingest now ghost-creates peers — single source
					// of truth, and historical rows that were saved
					// with the legacy "node 0x<hex>" string resolve
					// to the same "[c7f7] Meshtastic c7f7" form every
					// other Meshtastic client renders. Marked
					// unresolved so the UI dims them and the
					// "identified" notification fires when real
					// NodeInfo finally lands.
					for _, msg := range past {
						if msg.fromNum == 0 {
							continue
						}
						if _, ok := m.nodesByNum[msg.fromNum]; ok {
							continue
						}
						long, short := defaultCallsign(msg.fromNum)
						m.nodes = append(m.nodes, nodeItem{
							callsign:   long,
							shortName:  short,
							nodeNum:    msg.fromNum,
							unresolved: true,
							state:      "offline",
							lastHeard:  msg.time,
							lastSNR:    msg.snr,
							lastHops:   msg.hops,
						})
						m.nodesByNum[msg.fromNum] = len(m.nodes) - 1
					}
				}
				// Startup backfill — walk the replayed messages once
				// and push each sender's most recent sentAt onto the
				// corresponding node's lastHeardAt. Without this,
				// /whois and the nodes overlay open a fresh session
				// showing peers as "offline, heard 30m ago" even
				// when we've got a recent chat from them sitting
				// right there in the log. After this sweep, the
				// currentState / currentLastHeard derivations give
				// the expected "online / Xm ago" on the first render.
				touched := map[uint32]struct{}{}
				for _, past := range m.messages {
					if past.fromNum == 0 || past.sentAt.IsZero() {
						continue
					}
					idx, ok := m.nodesByNum[past.fromNum]
					if !ok {
						continue
					}
					if past.sentAt.After(m.nodes[idx].lastHeardAt) {
						m.nodes[idx].lastHeardAt = past.sentAt
						// Stamp lastHops + lastSNR off the most-
						// recent message in history. Without this,
						// /tr's offline fall-back path (no live
						// pump) reads zero hops + empty snr even
						// when the row right above shows the real
						// values — applyTextMessage updates these
						// for live packets, but historical messages
						// replay directly into m.messages and never
						// touch the node telemetry slots.
						m.nodes[idx].lastHops = past.hops
						if past.snr != "" {
							m.nodes[idx].lastSNR = past.snr
						}
						touched[past.fromNum] = struct{}{}
					}
				}
				backfilled := len(touched)
				// Emit storage notes AFTER NodeDB + message replay so
				// they land at the tail of the log (bottom-pinned in
				// the pane) where the user naturally looks for the
				// latest activity — not buried above a wall of
				// replayed chat.
				for _, n := range notes {
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

	// Demo mode — pour the fixture into the same model slots that the
	// radio would populate. Anything derived from these (status bar,
	// /config, /grid, /qth, node list) then renders from model state
	// without any isDemo() branch in the render code.
	m.channels = append([]channelItem(nil), demo.Channels...)
	m.nodes = append([]nodeItem(nil), demo.Nodes...)
	m.messages = append([]messageItem(nil), demo.Messages...)
	// Splash notices land at the tail so the BitchX greeter is the
	// newest entry in the log — same behaviour as live mode. In
	// demo mode the "me" callsign is set up below from demo.NodeNum;
	// pass the demo's first-row callsign so the splash tagline
	// renders against the demo identity.
	demoCallsign := ""
	if len(demo.Nodes) > 0 {
		demoCallsign = demo.Nodes[0].callsign
	}
	m.noticeCard(splashAsNotices(chosenSplash, demoCallsign)...)
	if len(demo.Channels) > 0 {
		m.currentChannel = demo.Channels[0].name
	}
	m.selectedMsg = len(m.messages) - 1
	if m.selectedMsg < 0 {
		m.selectedMsg = 0
	}

	// "Me" node is the first entry in demo.Nodes by convention — bind
	// myNodeNum + nodesByNum so myNode() and myCallsign() resolve the
	// same way they do on a live radio.
	m.myNodeNum = demo.NodeNum
	if len(demo.Nodes) > 0 {
		m.nodesByNum[demo.NodeNum] = 0
		m.nodes[0].nodeNum = demo.NodeNum
	}
	// Demo peers don't individually carry node-nums in the fixture,
	// but the Messages slice does via fromNum. Backfill each
	// not-yet-mapped peer's node num from the first message that
	// mentions them so tab-completion disambiguation + nav-w have a
	// hex to address, same as live mode.
	for _, msg := range demo.Messages {
		if msg.fromNum == 0 || msg.mine {
			continue
		}
		if _, ok := m.nodesByNum[msg.fromNum]; ok {
			continue
		}
		for i := range m.nodes {
			if i == 0 {
				continue
			}
			if m.nodes[i].nodeNum != 0 {
				continue
			}
			if m.nodes[i].callsign == msg.from {
				m.nodes[i].nodeNum = msg.fromNum
				m.nodesByNum[msg.fromNum] = i
				break
			}
		}
	}

	// Telemetry + config snapshot — same fields live mode sets from
	// DeviceMetrics / LoraConfig / DeviceConfig / Position packets.
	m.radioFirmware = demo.Firmware
	m.radioHasWifi = demo.HasWifi
	m.radioHasBT = demo.HasBT
	m.radioRegion = demo.Region
	m.radioModemPreset = demo.ModemPreset
	m.radioTxPower = demo.TxPowerDBm
	m.radioRole = demo.Role
	m.batteryLevel = demo.BatteryLevel
	m.batteryVoltage = demo.Voltage
	m.channelUtil = demo.ChannelUtil
	m.airUtilTx = demo.AirUtilTx
	m.hasTelemetry = true
	m.myLatitude = demo.Latitude
	m.myLongitude = demo.Longitude
	m.myGrid = demo.Grid
	m.connected = true

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
		// handler is a one-pass scan of m.messages guarded so it no-
		// ops when there's nothing expiring.
		noticeTickCmd(),
	}
	// Live-radio mode: kick off the pump from within the running
	// program. Deferring to Init (rather than RunRadio) guarantees
	// tea's main loop is up before the pump's first p.Send() — no
	// deadlock. The tea.Cmd returns an openPumpMsg which we handle
	// in Update by doing the actual Dial+spawn.
	if m.connectDest != "" {
		cmds = append(cmds, func() tea.Msg {
			return openPumpMsg{dest: m.connectDest}
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
		// proves the link is back (radioMyInfoMsg / ConfigComplete).
		//
		// EXCEPTION: once MyInfo has arrived (myNodeNum != 0) but
		// ConfigComplete hasn't, we're mid-handshake — let the live
		// "sync: N peers received" counter (set in radioNodeInfoMsg)
		// own the flash. Otherwise this tick blows the counter away
		// every 250ms with the dial banner.
		if m.reconnect != nil && m.myNodeNum == 0 {
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
		m.reconnect = &reconnectState{
			initial: true,
			readyAt: time.Now(),
		}
		m.flash = m.reconnectFlashText()
		m.flashSeen = m.flash
		m.flashSeenAt = time.Now()
		m.pump = startPump(msg.dest, m.programSlot.p)
		return m, nil

	case pumpAttachedMsg:
		m.pump = msg.p
		return m, nil

	case radioMyInfoMsg:
		m.myNodeNum = msg.nodeNum
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
		if !m.connected {
			m.syncReceived = 0
			m.systemLine("sync: pulling NodeDB from radio…")
		}
		return m, nil

	case radioNodeInfoMsg:
		m.upsertNode(msg)
		// While the handshake is still in flight (m.connected stays
		// false until ConfigComplete), bump the received counter and
		// surface it in the chanRow flash via syncCounterFlash —
		// the running number renders bright (mesh-green bold) so it
		// reads as the active live signal, "peers received" stays
		// in the regular flash green. On ConfigComplete the
		// systemLine path takes over with the final tally.
		if !m.connected && m.myNodeNum != 0 {
			m.syncReceived++
			m.flash = "sync: " + syncCounterFlash(m.syncReceived) + " peers received"
		}
		return m, nil

	case radioChannelMsg:
		m.applyChannel(msg)
		return m, nil

	case radioTextMsg:
		m.applyTextMessage(msg)
		return m, nil

	case radioRoutingMsg:
		m.applyRouting(msg)
		return m, nil

	case radioTracerouteMsg:
		m.applyTraceroute(msg)
		return m, nil

	case tracerouteTimeoutMsg:
		// Timeout for an outbound /tr. If the matching request is
		// still in flight (packetID matches), surface a "no reply"
		// systemBlock and clear the slot so a fresh /tr can fire.
		// Stale ticks (request already resolved or replaced) drop
		// silently — the packetID guard handles both cases.
		if m.pendingTraceroute != nil && m.pendingTraceroute.packetID == msg.packetID {
			tgt := m.pendingTraceroute.targetCall
			m.systemBlock(
				fmt.Sprintf("traceroute %s", tgt),
				fmt.Sprintf("result:  no reply within %ds", tracerouteTimeoutSeconds),
				"note:    target may be offline, out of range, or behind a dead relay",
			)
			m.flash = fmt.Sprintf("tr: no reply from %s", tgt)
			m.pendingTraceroute = nil
		}
		return m, nil

	case radioMetadataMsg:
		m.radioFirmware = msg.firmwareVersion
		m.radioDeviceState = msg.deviceStateVer
		m.radioHasWifi = msg.hasWifi
		m.radioHasBT = msg.hasBluetooth
		return m, nil

	case radioLoraConfigMsg:
		m.radioTxPower = msg.txPowerDBm
		m.radioRegion = msg.region
		m.radioModemPreset = msg.modemPreset
		return m, nil

	case radioDeviceMetricsMsg:
		// Only apply metrics for our own node to the "my radio"
		// status fields. Peer metrics could later be upserted onto
		// their nodeItem for per-peer battery display.
		if msg.fromNodeNum == m.myNodeNum || msg.fromNodeNum == 0 {
			m.batteryLevel = msg.batteryLevel
			m.batteryVoltage = msg.voltage
			m.channelUtil = msg.channelUtil
			m.airUtilTx = msg.airUtilTx
			m.hasTelemetry = true
		}
		return m, nil

	case radioDeviceConfigMsg:
		m.radioRole = msg.role
		return m, nil

	case radioPositionMsg:
		if m.peerPositions == nil {
			m.peerPositions = make(map[uint32]peerPosition)
		}
		m.peerPositions[msg.fromNodeNum] = peerPosition{
			latitude:  msg.latitude,
			longitude: msg.longitude,
			altitude:  msg.altitude,
			grid:      maidenhead(msg.latitude, msg.longitude),
			at:        msg.at,
		}
		// If this is our own position, also populate the top-bar grid.
		if msg.fromNodeNum == m.myNodeNum {
			m.myLatitude = msg.latitude
			m.myLongitude = msg.longitude
			m.myAltitude = msg.altitude
			m.myGrid = maidenhead(msg.latitude, msg.longitude)
		}
		return m, nil

	case radioEnvMetricsMsg:
		if m.peerEnv == nil {
			m.peerEnv = make(map[uint32]peerEnvMetrics)
		}
		m.peerEnv[msg.fromNodeNum] = peerEnvMetrics{
			temperature: msg.temperature,
			humidity:    msg.humidity,
			pressure:    msg.pressure,
			gas:         msg.gas,
			at:          time.Now(),
		}
		return m, nil

	case radioConfigCompleteMsg:
		wasDisconnected := !m.connected
		m.connected = true
		// Definitive end of the handshake — NodeDB and config dump
		// have all arrived, the user can see live state. Drop the
		// reconnect banner now and not before; MyInfo isn't strong
		// enough on its own (see comment in the radioMyInfoMsg case).
		m.clearReconnectBanner()
		// Initial-connect handshake (was disconnected, no /sync
		// pending): emit a completion notice with the peer count so
		// the user sees that the NodeDB pull finished. The earlier
		// "sync: pulling NodeDB" notice on MyInfo gives the start;
		// this one closes the loop.
		if wasDisconnected && m.syncPendingGhosts == 0 {
			m.systemLine(fmt.Sprintf(
				"sync: complete — %d peers identified", len(m.nodes),
			))
		}
		// Clear the handshake-progress counter so a future /sync
		// or post-disconnect rehandshake starts from a clean slate.
		m.syncReceived = 0
		// If the user issued /sync and we snapshotted a ghost count,
		// emit a completion systemBlock with the delta so they see
		// what the re-dump actually changed. syncPendingGhosts > 0
		// means the snapshot had placeholders; == -1 is the sentinel
		// for "/sync fired with zero ghosts baseline"; == 0 means
		// this is the startup handshake and we stay quiet.
		if m.syncPendingGhosts != 0 {
			current := 0
			for _, n := range m.nodes {
				if strings.HasPrefix(n.callsign, "node 0x") {
					current++
				}
			}
			baseline := m.syncPendingGhosts
			if baseline < 0 {
				baseline = 0
			}
			resolved := baseline - current
			total := len(m.nodes)
			m.systemBlock("sync complete",
				fmt.Sprintf("NodeDB re-dump done — %d peers in NodeDB", total),
				fmt.Sprintf("placeholders: %d → %d  (%d resolved this sync)", baseline, current, resolved),
			)
			m.syncPendingGhosts = 0
		}
		// Otherwise no flash — the top status bar's "● online" dot is
		// the canonical connection indicator; flashing "radio
		// connected" at the bottom was duplicate signal in the same
		// mesh-green.
		return m, nil

	case radioDisconnectedMsg:
		m.connected = false
		// Disconnect IS worth a flash — "● online" flips to
		// "● connecting" up top but users staring at the messages
		// pane need louder in-band feedback for a state change.
		m.flash = "radio disconnected"
		return m, nil

	case radioReconnectingMsg:
		// Transient drop — pump is going to retry. Flip the connected
		// flag so the top status bar shows "connecting" and stash a
		// reconnectState so noticeTickMsg can repaint the countdown
		// every second. The banner is sticky (auto-clear is bypassed
		// while m.reconnect != nil) so the user can watch the retry
		// counter climb instead of seeing a 5s flash and then nothing
		// for the rest of a 30s backoff.
		m.connected = false
		m.reconnect = &reconnectState{
			attempt: msg.attempt,
			err:     msg.err,
			readyAt: time.Now().Add(msg.after),
		}
		m.flash = m.reconnectFlashText()
		m.flashSeen = m.flash
		m.flashSeenAt = time.Now()
		return m, nil

	case radioErrorMsg:
		// Pump exhausted its retry budget. Drop the reconnect banner
		// — there's nothing more happening — and switch to a regular
		// (auto-clearing) flash carrying the final error.
		m.connected = false
		m.reconnect = nil
		m.flash = fmt.Sprintf("radio error: %v", msg.err)
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

// isDemo reports whether we're running off the canned Demo fixture
// rather than a real radio transport.
func (m model) isDemo() bool {
	return m.demo != nil
}

// executeCommand handles a slash command with the `/` prefix already
// stripped. Returns a tea.Cmd (e.g. tea.Quit) when the command needs
// to drive the runtime; nil otherwise.

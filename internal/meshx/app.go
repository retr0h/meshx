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
	"math"
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
	shortName string // Meshtastic User.short_name — 4-ish char badge
	// nodeNum is the unique Meshtastic radio identity
	// (MyNodeInfo.my_node_num). Carried on the nodeItem so tab
	// completion and /whois can disambiguate multiple radios that
	// share a longname — three "retr0h" radios all have the same
	// callsign but different nodeNums.
	nodeNum   uint32
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

	// fromNum — Meshtastic node num of the sender, captured at
	// ingest. Persisted so the renderer can backfill the displayed
	// callsign from m.nodesByNum LIVE (the stored `from` field is
	// only a snapshot at receive time — if NodeInfo arrives later
	// we'd otherwise be stuck showing "node 0xabc" forever). Zero
	// for "me" / system lines / demo seeds.
	fromNum uint32

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

	// storageAlerted — once any saveMessage / saveNode /
	// saveNodePrefs fails, flip this flag and emit a single
	// systemLine so the user knows persistence is degraded. Every
	// subsequent save-error stays silent so a bad db doesn't
	// machine-gun the messages pane. In-memory operation continues
	// normally — losing persistence is preferable to crashing.
	storageAlerted bool

	// Live-radio state. Zero-value is "demo mode — no transport".
	pump        *pump          // non-nil when connected to a real radio
	connectDest string         // "" = demo, else "/dev/cu.usbmodem2101" / tcp host
	connected   bool           // true once ConfigComplete arrives
	myNodeNum   uint32         // populated by MyNodeInfo
	nodesByNum  map[uint32]int // radio node id → m.nodes index, for O(1) upsert

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
	program := tea.NewProgram(m, tea.WithAltScreen())

	// We need a reference to the program inside the pump so it can
	// call program.Send() from its goroutine. Stash it on a shared
	// ptr that Init() fills once the loop is up.
	globalProgramRef = program
	defer func() { globalProgramRef = nil }()

	_, err := program.Run()
	return err
}

// globalProgramRef is a small hand-off slot used because tea.Program
// doesn't expose itself to models. The pump goroutine needs program.Send.
// Scoped to one running Bubble Tea program at a time (which is the norm).
var globalProgramRef *tea.Program

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

// haversineKm returns the great-circle distance in kilometers between
// two lat/lon points (degrees). Used by /ping and /whois to surface
// peer-to-self distance when we have a GPS fix on both ends. Returns
// 0 if either coordinate is the (0, 0) origin (Meshtastic's "no fix"
// sentinel).
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	if (lat1 == 0 && lon1 == 0) || (lat2 == 0 && lon2 == 0) {
		return 0
	}
	const r = 6371.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	phi1 := toRad(lat1)
	phi2 := toRad(lat2)
	sinLat := math.Sin(dLat / 2)
	sinLon := math.Sin(dLon / 2)
	a := sinLat*sinLat + math.Cos(phi1)*math.Cos(phi2)*sinLon*sinLon
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return r * c
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
	in.CharLimit = 200
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
		mode:            modeInput,
		focused:         paneMessages,
		splash:          chosenSplash,
		connectDest:     dest,
		demo:            demo,
		nodesByNum:      make(map[uint32]int),
		peerPositions:   make(map[uint32]peerPosition),
		peerEnv:         make(map[uint32]peerEnvMetrics),
		input:           in,
		searchInput:     func() textinput.Model { s := textinput.New(); s.Prompt = ""; s.CharLimit = 80; return s }(),
		initialFocusCmd: focusCmd,
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
				// Primary channel is what the radio tells us, but at
				// boot we don't have it yet — replay under the name
				// we'll default to (empty string key until a channel
				// arrives). Load is by `currentChannel` so messages
				// migrate as the handshake resolves the channel name.
				if past, err := loadMessages(db, "", 500); err == nil {
					m.messages = append(m.messages, past...)
					m.selectedMsg = len(m.messages) - 1
					if m.selectedMsg < 0 {
						m.selectedMsg = 0
					}
					// Ghost-peer replay — every historical message
					// with a fromNum we haven't seen in m.nodes gets
					// a placeholder so /whois / /cqr / /rs / /ping
					// can target it by id without waiting for a
					// fresh live packet. The entry is upgraded in
					// place once NodeInfo arrives during handshake.
					for _, msg := range past {
						if msg.fromNum == 0 {
							continue
						}
						if _, ok := m.nodesByNum[msg.fromNum]; ok {
							continue
						}
						m.nodes = append(m.nodes, nodeItem{
							callsign:  msg.from,
							nodeNum:   msg.fromNum,
							state:     "offline",
							lastHeard: msg.time,
							lastSNR:   msg.snr,
							lastHops:  msg.hops,
						})
						m.nodesByNum[msg.fromNum] = len(m.nodes) - 1
					}
				}
				// Emit storage notes AFTER NodeDB + message replay so
				// they land at the tail of the log (bottom-pinned in
				// the pane) where the user naturally looks for the
				// latest activity — not buried above a wall of
				// replayed chat.
				for _, n := range notes {
					m.systemLine("storage: " + n)
				}
			}
		}
		// Splash notices come last so the BitchX greeter is the
		// newest entry in the log — sits right at the bottom above
		// the input bar on launch just like every other recent
		// message, and scrolls UP naturally as fresh chat arrives.
		m.noticeCard(splashAsNotices(chosenSplash)...)
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
	// newest entry in the log — same behaviour as live mode.
	m.noticeCard(splashAsNotices(chosenSplash)...)
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
		return m, noticeTickCmd()

	case openPumpMsg:
		// Program is running; safe to spawn the pump now.
		if globalProgramRef == nil {
			m.flash = "internal error: program ref missing"
			return m, nil
		}
		p, err := startPump(msg.dest, globalProgramRef)
		if err != nil {
			m.flash = fmt.Sprintf("radio error: %v", err)
			return m, nil
		}
		m.pump = p
		return m, nil

	case pumpAttachedMsg:
		m.pump = msg.p
		return m, nil

	case radioMyInfoMsg:
		m.myNodeNum = msg.nodeNum
		return m, nil

	case radioNodeInfoMsg:
		m.upsertNode(msg)
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
		m.connected = true
		// If the user issued /sync and we snapshotted a ghost count,
		// emit a completion systemLine with the delta so they see
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

	case radioErrorMsg:
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

// myCallsign returns the call to use for "me" in outbound messages,
// the status bar, etc. Demo mode: whatever the Demo fixture's
// Callsign says. Live mode: look up our own node by myNodeNum in
// the NodeDB.
func (m model) myCallsign() string {
	if m.demo != nil {
		return m.demo.Callsign
	}
	if m.myNodeNum == 0 {
		return "—" // MyNodeInfo hasn't arrived yet
	}
	if idx, ok := m.nodesByNum[m.myNodeNum]; ok && idx < len(m.nodes) {
		return m.nodes[idx].callsign
	}
	return fmt.Sprintf("node 0x%x", m.myNodeNum)
}

// myShortName returns our own Meshtastic shortname (4-ish char
// badge) — the tight identifier that fits on a radio OLED and
// matches what the phone app shows next to the longname. Demo
// mode pulls from Demo.ShortName; live mode looks up our own
// nodeItem. Empty when we don't know yet.
func (m model) myShortName() string {
	if m.demo != nil {
		return m.demo.ShortName
	}
	if n := m.myNode(); n != nil {
		return n.shortName
	}
	return ""
}

// myNode returns a pointer to our own node record — works in both
// demo and live mode since demo-mode initialisation seeds m.nodes
// and m.nodesByNum the same way a real radio's MyInfo + NodeInfo
// stream would. Returns nil only when MyNodeInfo hasn't arrived yet
// on a live radio.
func (m model) myNode() *nodeItem {
	if m.myNodeNum == 0 {
		return nil
	}
	if idx, ok := m.nodesByNum[m.myNodeNum]; ok && idx < len(m.nodes) {
		return &m.nodes[idx]
	}
	return nil
}

// upsertNode inserts a NodeInfo arrival or updates the existing row
// by node num. Uses nodesByNum for O(1) lookup. Falls back to
// short/long name for display text, and chooses state from lastHeard.
func (m *model) upsertNode(msg radioNodeInfoMsg) {
	callsign := msg.longName
	if callsign == "" {
		callsign = msg.shortName
	}
	if callsign == "" {
		callsign = fmt.Sprintf("node 0x%x", msg.nodeNum)
	}

	// Derive state from lastHeard age.
	state := "offline"
	if !msg.lastHeardAt.IsZero() {
		age := time.Since(msg.lastHeardAt)
		switch {
		case age < 15*time.Minute:
			state = "online"
		case age < 2*time.Hour:
			state = "offline"
		default:
			state = "offline"
		}
	}
	lastHeard := "never"
	if !msg.lastHeardAt.IsZero() {
		lastHeard = humanDuration(time.Since(msg.lastHeardAt))
	}

	item := nodeItem{
		callsign:  callsign,
		shortName: msg.shortName,
		nodeNum:   msg.nodeNum,
		state:     state,
		lastHeard: lastHeard,
		heardRank: int(time.Since(msg.lastHeardAt).Seconds()),
		lastSNR:   msg.snr,
		lastRSSI:  msg.rssi,
		lastHops:  msg.hops,
		hwModel:   msg.hwModel,
	}

	// Persist to the cross-session NodeDB cache so once we've learned
	// a peer's real User info we remember it on every subsequent
	// launch — same behavior as the official phone app. Placeholder
	// "node 0x…" callsigns (both longname and shortname empty) are
	// skipped inside saveNode itself.
	m.storagePersist(saveNode(m.db, msg.nodeNum, msg.longName, msg.shortName, msg.hwModel))

	if idx, ok := m.nodesByNum[msg.nodeNum]; ok {
		// Preserve fav flag across updates.
		item.fav = m.nodes[idx].fav
		prev := m.nodes[idx].callsign
		m.nodes[idx] = item
		// Ghost upgrade notification — when a peer that was
		// previously showing as the "node 0x<hex>" placeholder
		// (because NodeInfo hadn't arrived yet) just got
		// resolved to a real callsign, drop a grey inline
		// system line in the log so the user sees the name
		// flip happen. Skipped when the callsign didn't
		// actually change (re-applied same NodeInfo) or when
		// we're still stuck on the placeholder (NodeInfo
		// lacked both long and short names).
		placeholder := fmt.Sprintf("node 0x%x", msg.nodeNum)
		if prev == placeholder && item.callsign != placeholder && prev != item.callsign {
			m.systemLine(fmt.Sprintf("identified %s (was %s)", item.callsign, placeholder))
		}
		return
	}
	m.nodesByNum[msg.nodeNum] = len(m.nodes)
	m.nodes = append(m.nodes, item)
}

// applyChannel sets or replaces a channel slot. Skips DISABLED
// channels so they don't clutter the tab strip.
func (m *model) applyChannel(msg radioChannelMsg) {
	if msg.role == "DISABLED" {
		return
	}
	name := msg.name
	if name == "" {
		// Empty-name PRIMARY is the default "LongFast" channel — give
		// it a readable label in the UI.
		name = "#default"
	} else if msg.hasPSK {
		name = "*" + msg.name + "*"
	} else {
		name = "#" + msg.name
	}
	c := channelItem{name: name, private: msg.hasPSK}
	// Upsert by index; grow the slice if needed.
	for len(m.channels) <= msg.index {
		m.channels = append(m.channels, channelItem{})
	}
	// Preserve unread count across re-apply.
	c.unread = m.channels[msg.index].unread
	m.channels[msg.index] = c
	if m.currentChannel == "" {
		m.currentChannel = name
	}
}

// applyTextMessage appends a received text packet to the message log.
// Resolves fromNum to a callsign via the NodeDB; unread count bumps
// on the destination channel when it's not the active one.
func (m *model) applyTextMessage(msg radioTextMsg) {
	from := fmt.Sprintf("node 0x%x", msg.fromNum)
	if idx, ok := m.nodesByNum[msg.fromNum]; ok {
		from = m.nodes[idx].callsign
	} else if msg.fromNum != 0 {
		// We've heard a text packet from a peer whose NodeInfo we
		// haven't received yet — ghost them into m.nodes so
		// /cqr, /rs, /whois, /ping can find them by id or
		// substring. The entry gets upgraded by upsertNode the
		// moment a real NodeInfo arrives (nodesByNum index is
		// stable so all references stay valid).
		m.nodes = append(m.nodes, nodeItem{
			callsign:  from,
			nodeNum:   msg.fromNum,
			state:     "online",
			lastHeard: "now",
			lastSNR:   msg.snr,
			lastRSSI:  msg.rssi,
			lastHops:  msg.hops,
		})
		m.nodesByNum[msg.fromNum] = len(m.nodes) - 1
	}
	mine := msg.fromNum == m.myNodeNum

	item := messageItem{
		time:     msg.at.Format("15:04"),
		from:     from,
		mine:     mine,
		text:     msg.text,
		status:   "ack",
		hops:     msg.hops,
		snr:      msg.snr,
		packetID: msg.packetID,
		replyID:  msg.replyID,
		fromNum:  msg.fromNum,
	}
	// Snapshot whether the user was anchored at the tail BEFORE we
	// append. If they were (selectedMsg was on the last row of the
	// log, or the log was empty) we auto-follow new traffic by
	// advancing selectedMsg to the fresh tail. If they'd scrolled up
	// to read history, leave selectedMsg alone — irssi convention:
	// scrollback is sticky, new messages appear at the bottom
	// invisibly until the user returns to tail. Without this
	// incoming texts would arrive but never scroll into view because
	// renderMessagesPane anchors its viewport on selectedMsg.
	wasAtTail := len(m.messages) == 0 || m.selectedMsg == len(m.messages)-1
	m.messages = append(m.messages, item)
	if wasAtTail {
		m.selectedMsg = len(m.messages) - 1
	}

	// Persist the incoming message so it survives a restart. Channel
	// name is resolved from the packet's channel index against the
	// NodeDB we've built up; falls back to the active channel if the
	// index is out of range (shouldn't happen on a real radio).
	channelName := m.currentChannel
	if msg.channel < len(m.channels) {
		channelName = m.channels[msg.channel].name
	}
	m.storagePersist(saveMessage(m.db, channelName, item))

	// Bump unread count on non-active channels.
	if msg.channel < len(m.channels) && m.channels[msg.channel].name != m.currentChannel && !mine {
		m.channels[msg.channel].unread++
	}
}

// applyRouting flips the status of the local messageItem whose
// packetID matches the Routing reply's request_id. NONE → "ack"
// (delivery succeeded), anything else → "fail" (the errorName
// hints at why: TIMEOUT, MAX_RETRANSMIT, NO_INTERFACE...).
// Routing replies for packets we didn't originate silently drop —
// request_id won't match any of our outbound rows.
func (m *model) applyRouting(msg radioRoutingMsg) {
	if msg.requestID == 0 {
		return
	}
	for i := range m.messages {
		if m.messages[i].packetID != msg.requestID || !m.messages[i].mine {
			continue
		}
		if msg.ok {
			m.messages[i].status = "ack"
			m.flash = "ack received"
		} else {
			m.messages[i].status = "fail"
			m.flash = "delivery failed: " + msg.errorName + "  (R to resend)"
		}
		return
	}
}

// resend takes a prior outbound messageItem, re-enqueues it over
// the pump with a fresh packetID, and flips the original row's
// status back to "pending" so the user sees the retransmit in
// flight. Bound to `R` in nav mode when the cursor is on a
// status=="fail" row.
func (m *model) resend(idx int) {
	if idx < 0 || idx >= len(m.messages) {
		return
	}
	msg := &m.messages[idx]
	if !msg.mine || msg.status != "fail" {
		return
	}
	if m.pump == nil {
		m.flash = "no radio connected — cannot resend"
		return
	}
	envelope, pid := newTextToRadio(msg.text, m.currentChannelIndex(), msg.replyID)
	msg.packetID = pid
	msg.status = "pending"
	m.pump.Enqueue(envelope)
	m.flash = "retransmit sent — awaiting ack"
}

// humanDuration formats a time.Duration as a compact label like "2m",
// "1h", "3d" — the style used in the nodes grid's last-heard column.
func humanDuration(d time.Duration) string {
	s := int(d.Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh", s/3600)
	default:
		return fmt.Sprintf("%dd", s/86400)
	}
}

// switchChannelByIndex jumps the active channel to the given index;
// out-of-range is a no-op.
func (m *model) nodeNumOf(callsign string) uint32 {
	target := strings.ToLower(strings.TrimSpace(callsign))
	// Meshtastic node-id notation: "!<hex>" or "0x<hex>" — the
	// unambiguous way to address a specific radio when multiple
	// peers share a longname. Parsed first so /whois !103d20cd
	// always resolves to that exact node num regardless of
	// NodeDB state.
	if num, ok := parseNodeHex(target); ok {
		if _, exists := m.nodesByNum[num]; exists {
			return num
		}
		return num // still return the num even if not in m.nodes so callers can see we parsed it
	}
	// Exact.
	for num, idx := range m.nodesByNum {
		if idx < len(m.nodes) && strings.ToLower(m.nodes[idx].callsign) == target {
			return num
		}
	}
	// Prefix.
	for num, idx := range m.nodesByNum {
		if idx < len(m.nodes) && strings.HasPrefix(strings.ToLower(m.nodes[idx].callsign), target) {
			return num
		}
	}
	// Substring.
	for num, idx := range m.nodesByNum {
		if idx < len(m.nodes) && strings.Contains(strings.ToLower(m.nodes[idx].callsign), target) {
			return num
		}
	}
	return 0
}

// parseNodeHex recognises the two Meshtastic node-id spellings:
// "!<hex>" (canonical ! notation the phone app uses) and "0x<hex>"
// (our own "node 0x…" fallback). Returns the node num and true on a
// successful parse.
func parseNodeHex(s string) (uint32, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "!") {
		s = s[1:]
	} else if strings.HasPrefix(strings.ToLower(s), "0x") {
		s = s[2:]
	} else if strings.HasPrefix(strings.ToLower(s), "node 0x") {
		s = s[len("node 0x"):]
	} else {
		return 0, false
	}
	if s == "" {
		return 0, false
	}
	var n uint64
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= '0' && r <= '9':
			n = n<<4 | uint64(r-'0')
		case r >= 'a' && r <= 'f':
			n = n<<4 | uint64(r-'a'+10)
		default:
			return 0, false
		}
		if n > 0xFFFFFFFF {
			return 0, false
		}
	}
	return uint32(n), true
}

// lookupNode returns a pointer to the nodeItem matching callsign (exact
// case-insensitive match). nil if no such node is known. Every
// argumented ham command routes through this so we build reports from
// actual telemetry, never from placeholder text.
// lookupNode resolves a user-supplied callsign to a nodeItem. Tries
// three matches in order:
//
//  1. Exact case-insensitive — fast path
//  2. Prefix — "/whois KC7XYZ" matches "KC7XYZ 🦀"
//  3. Substring — "/whois rural" matches "Rural Signal 📡"
//
// Callsigns in Meshtastic often carry trailing emoji / badges / qth
// suffixes, so the flexibility is important for ergonomics.
func (m *model) lookupNode(callsign string) *nodeItem {
	if callsign == "" {
		return nil
	}
	target := strings.ToLower(strings.TrimSpace(callsign))
	// Meshtastic node-id notation lands here straight from tab
	// completion's collision-disambiguation path — "!<hex>" means
	// "exactly this radio, don't fuzzy-match". Resolve via
	// nodesByNum so three radios sharing a longname each address
	// uniquely.
	if num, ok := parseNodeHex(target); ok {
		if idx, mapped := m.nodesByNum[num]; mapped && idx < len(m.nodes) {
			return &m.nodes[idx]
		}
		return nil
	}
	// Pass 1: exact.
	for i := range m.nodes {
		if strings.ToLower(m.nodes[i].callsign) == target {
			return &m.nodes[i]
		}
	}
	// Pass 2: prefix.
	for i := range m.nodes {
		if strings.HasPrefix(strings.ToLower(m.nodes[i].callsign), target) {
			return &m.nodes[i]
		}
	}
	// Pass 3: substring — case-insensitive.
	for i := range m.nodes {
		if strings.Contains(strings.ToLower(m.nodes[i].callsign), target) {
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

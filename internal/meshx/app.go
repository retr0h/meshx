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
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"
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

// RunDemo launches the Bubble Tea model with the canonical Demo
// fixture and no radio transport. Used for UI iteration, screenshots,
// and smoke testing the interface without a LoRa device handy.
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
	in.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(meshGreen))
	in.Focus()

	m := model{
		mode:          modeSplash,
		focused:       paneMessages,
		splash:        pickSplash(),
		connectDest:   dest,
		demo:          demo,
		nodesByNum:    make(map[uint32]int),
		peerPositions: make(map[uint32]peerPosition),
		peerEnv:       make(map[uint32]peerEnvMetrics),
		input:         in,
		searchInput:   func() textinput.Model { s := textinput.New(); s.Prompt = ""; s.CharLimit = 80; return s }(),
	}

	if demo == nil {
		// Live-radio mode — open the persistence store and replay the
		// last chunk of history so the log survives restarts. We fail
		// open: any storage error (missing $HOME, bad perms, corrupt
		// db) just leaves m.db nil, and the session runs in-memory
		// for that boot. Losing history is preferable to crashing.
		if path, err := defaultStoragePath(); err == nil {
			if db, err := openStorage(path); err == nil {
				m.db = db
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
							state:     "offline",
							lastHeard: msg.time,
							lastSNR:   msg.snr,
							lastHops:  msg.hops,
						})
						m.nodesByNum[msg.fromNum] = len(m.nodes) - 1
					}
				}
			}
		}
		return m
	}

	// Demo mode — pour the fixture into the same model slots that the
	// radio would populate. Anything derived from these (status bar,
	// /config, /grid, /qth, node list) then renders from model state
	// without any isDemo() branch in the render code.
	m.channels = append([]channelItem(nil), demo.Channels...)
	m.nodes = append([]nodeItem(nil), demo.Nodes...)
	m.messages = append([]messageItem(nil), demo.Messages...)
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
		// Auto-dismiss the splash after ~3s if the user hasn't touched a key.
		tea.Tick(3*time.Second, func(time.Time) tea.Msg {
			return splashTimeoutMsg{}
		}),
		// DEMO — fire a one-shot fake "ghost upgrade" notification
		// 10s after launch so the user can see what the systemLine
		// looks like even when no real NodeInfo arrives. Mirrors
		// exactly what upsertNode emits when a real radio identifies
		// a previously-placeholder peer. Safe to keep around as a
		// living example; if the in-app notification later changes,
		// this demo updates in lock-step.
		tea.Tick(10*time.Second, func(time.Time) tea.Msg {
			return demoGhostUpgradeMsg{
				prev:     "node 0xdeadbeef",
				callsign: "KE6DEMO",
			}
		}),
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

// splashTimeoutMsg fires once ~3s after launch to auto-dismiss the
// BitchX-style banner even if the user doesn't press a key.
type splashTimeoutMsg struct{}

// demoGhostUpgradeMsg fires once ~10s after launch to drop a fake
// "ghost peer identified" systemLine into the log, so the user can
// see the notification shape without waiting for a real mid-session
// NodeInfo to arrive for a placeholder peer.
type demoGhostUpgradeMsg struct {
	prev     string // the "node 0x…" placeholder callsign
	callsign string // the real callsign we'd upgrade to
}

// openPumpMsg is the "program is running, go open the radio" signal
// fired by Init(). Handled in Update which calls startPump and
// stashes the handle in the model.
type openPumpMsg struct{ dest string }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case splashTimeoutMsg:
		// Only auto-dismiss if we're not still waiting for the radio.
		if m.mode == modeSplash && (m.connectDest == "" || m.connected) {
			m.mode = modeInput
			m.input.Focus()
		}
		return m, nil

	case demoGhostUpgradeMsg:
		// Fires the exact shape of notification upsertNode would
		// emit in a real session. Written here (rather than in
		// upsertNode directly) so it runs in both demo and live
		// modes — anyone launching meshx sees the message format
		// 10 seconds in without needing a cooperating peer.
		m.systemLine(fmt.Sprintf("identified %s (was %s)", msg.callsign, msg.prev))
		return m, nil

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
		// If splash already timed out, hand the user into input mode now.
		if m.mode == modeSplash {
			m.mode = modeInput
			m.input.Focus()
		}
		// No flash — the top status bar's "● online" dot is the
		// canonical connection indicator; flashing "radio connected"
		// at the bottom was duplicate signal in the same mesh-green.
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

// revealMessages is the "I just produced a message-pane entry, show
// it to the user" helper used by nav-mode keys like p/t/w that fire
// commands whose output lands as a systemBlock. Closes any open
// overlay, focuses the messages pane, keeps nav mode so the cursor
// sits on the fresh entry (j/k navigate the new block), and drops a
// flash so the action feels acknowledged even when the user was
// already looking at messages.
func (m *model) revealMessages(flash string) {
	m.overlay = overlayNone
	m.focused = paneMessages
	m.mode = modeNav
	m.input.Blur()
	m.flash = flash
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
			m.revealMessages(fmt.Sprintf("traced %s — see messages", target))
		}
	case "p":
		target := m.selectedSender()
		if target != "" {
			m.executeCommand("ping " + target)
			m.revealMessages(fmt.Sprintf("pinged %s — see messages", target))
		}
	case "w":
		target := m.selectedSender()
		if target != "" {
			// Delegate to the same code path /whois uses so nav-key
			// output stays in lock-step with the slash command.
			m.executeCommand("whois " + target)
			m.revealMessages(fmt.Sprintf("whois %s — see messages", target))
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
// the current channel. In live-radio mode it also enqueues a ToRadio
// text packet and persists the row so it survives a restart; in demo
// mode it just updates local state.
func (m *model) sendPlainMessage(text string) {
	item := messageItem{
		time: timeNowHHMM(), from: "me", mine: true, text: text, status: "pending",
	}
	m.messages = append(m.messages, item)
	m.selectedMsg = len(m.messages) - 1
	m.flash = fmt.Sprintf("sent in %s", m.currentChannel)

	_ = saveMessage(m.db, m.currentChannel, item)

	if m.pump != nil {
		m.pump.Enqueue(newTextToRadio(text, m.currentChannelIndex(), 0))
	}
}

// newTextToRadio builds the ToRadio envelope for a plain text chat
// message on a named channel index. Broadcast (to = 0xFFFFFFFF) on
// PortNum TEXT_MESSAGE_APP (1) — the canonical Meshtastic chat path.
// When replyID != 0 the packet threads to the referenced parent
// (Data.reply_id) — this is how /73 <call> and friends tie their
// outgoing text to the specific message from that operator.
func newTextToRadio(text string, channel, replyID uint32) *pb.ToRadio {
	return &pb.ToRadio{
		PayloadVariant: &pb.ToRadio_Packet{Packet: &pb.MeshPacket{
			To:      0xFFFFFFFF,
			Channel: channel,
			WantAck: true,
			PayloadVariant: &pb.MeshPacket_Decoded{Decoded: &pb.Data{
				Portnum: pb.PortNum_TEXT_MESSAGE_APP,
				Payload: []byte(text),
				ReplyId: replyID,
			}},
		}},
	}
}

// currentChannelIndex maps m.currentChannel back to the Meshtastic
// channel index used on the wire. Defaults to 0 (PRIMARY) when the
// channel name isn't in our list.
func (m model) currentChannelIndex() uint32 {
	for i, c := range m.channels {
		if c.name == m.currentChannel {
			return uint32(i)
		}
	}
	return 0
}

// timeNowHHMM returns the current wall time in HH:MM for message
// timestamps. Extracted so tests can override if needed.
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
		state:     state,
		lastHeard: lastHeard,
		heardRank: int(time.Since(msg.lastHeardAt).Seconds()),
		lastSNR:   msg.snr,
		lastRSSI:  msg.rssi,
		lastHops:  msg.hops,
		hwModel:   msg.hwModel,
	}

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
	m.messages = append(m.messages, item)

	// Persist the incoming message so it survives a restart. Channel
	// name is resolved from the packet's channel index against the
	// NodeDB we've built up; falls back to the active channel if the
	// index is out of range (shouldn't happen on a real radio).
	channelName := m.currentChannel
	if msg.channel < len(m.channels) {
		channelName = m.channels[msg.channel].name
	}
	_ = saveMessage(m.db, channelName, item)

	// Bump unread count on non-active channels.
	if msg.channel < len(m.channels) && m.channels[msg.channel].name != m.currentChannel && !mine {
		m.channels[msg.channel].unread++
	}
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
			hw := n.hwModel
			if hw == "" {
				hw = "?"
			}
			fw := n.firmware
			if fw == "" {
				fw = "?"
			}
			m.flash = fmt.Sprintf(
				"%s  ·  %s  ·  fw %s  ·  last heard %s  ·  %s",
				n.callsign, hw, fw, n.lastHeard, n.state,
			)
		}
	case paneMessages:
		if m.selectedMsg < len(m.messages) {
			msg := m.messages[m.selectedMsg]
			switch {
			case msg.status == "system":
				m.flash = "system message — no metadata"
			case msg.mine:
				m.flash = fmt.Sprintf("to %s  ·  hop %d  ·  ACK %s",
					m.currentChannel, msg.hops, ackWord(msg.status))
			default:
				parts := []string{"from " + msg.from}
				if msg.hops > 0 {
					parts = append(parts, fmt.Sprintf("hop %d", msg.hops))
				}
				if msg.snr != "" {
					parts = append(parts, "SNR "+msg.snr+" dB")
				}
				m.flash = strings.Join(parts, "  ·  ")
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
		m.selectedMsg = m.nextMsgIndexSkipGroups(delta)
	case paneNodes:
		m.selectedNd = clamp(m.selectedNd+delta, 0, len(m.nodes)-1)
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
func (m *model) nodeNumOf(callsign string) uint32 {
	target := strings.ToLower(strings.TrimSpace(callsign))
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
		// Ham-customary "via <rig/app>" suffix on the beacon so
		// anyone copying the CQ knows what client the caller runs.
		// Only /cq carries this tag — routine chat + reply verbs
		// stay clean so a 237-byte LoRa payload isn't wasted on
		// attribution on every packet.
		call := m.myCallsign()
		body := fmt.Sprintf("CQ CQ CQ de %s via %s — testing signals, please ack", call, clientTag)
		if rest != "" {
			body = fmt.Sprintf("CQ de %s via %s %s", call, clientTag, rest)
		}
		m.sendBang("/cq", body)
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
		m.sendBangReply("/cqr "+target, signalReport(n), m.replyTargetFor(target))
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
		m.sendBangReply("/rs "+target, signalReport(n), m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!rs %s — %s", target, signalReport(n))
	case "73":
		// /73           → broadcast best-regards
		// /73 <call>    → directed "73 <call>" — aimed at a specific
		//                 operator you're signing off to cordially.
		//                 Threads via Data.reply_id to that operator's
		//                 most recent message when we have one.
		target := strings.TrimSpace(rest)
		if target == "" {
			m.sendBang("/73", "73")
			m.flash = "!73 sent"
			return nil
		}
		m.sendBangReply("/73 "+target, "73 "+target, m.replyTargetFor(target))
		m.flash = "!73 " + target + " — best regards"
	case "88":
		m.sendBang("/88", "88")
		m.flash = "!88 sent"
	case "qsl":
		// /qsl           → broadcast acknowledgment
		// /qsl <call>    → directed "QSL <call>" — aimed at a specific
		//                  operator whose last transmission we copied.
		//                  Threads via Data.reply_id to that operator's
		//                  most recent message.
		target := strings.TrimSpace(rest)
		if target == "" {
			m.sendBang("/qsl", "QSL")
			m.flash = "!qsl — acknowledged"
			return nil
		}
		body := "QSL " + target
		m.sendBangReply("/qsl "+target, body, m.replyTargetFor(target))
		m.flash = "!qsl " + target + " — copy confirmed"
	case "qth":
		// PRIVACY — /qth only transmits when the user runs it
		// explicitly, and only the coarse Maidenhead grid (~20 km
		// precision). Never exact lat/long.
		//
		// Two forms:
		//   /qth                → broadcast your own grid (from radio GPS)
		//   /qth <text>         → broadcast a custom QTH string
		//
		// To look up a PEER's QTH, use /whois <call> — keeps send vs.
		// query unambiguous.
		arg := strings.TrimSpace(rest)
		if arg == "" {
			if m.myGrid == "" {
				m.flash = "no GPS fix — /qth <text> to send a custom QTH, or configure position on the radio"
				return nil
			}
			m.sendBang("/qth", "QTH: "+m.myGrid)
			m.flash = "QTH: " + m.myGrid
			return nil
		}
		m.sendBang("/qth", "QTH: "+arg)
		m.flash = "QTH: " + arg
	case "env":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /env <callsign>"
			return nil
		}
		nodeNum := m.nodeNumOf(target)
		if nodeNum == 0 {
			m.systemLine(fmt.Sprintf("env: no record of %s", target))
			return nil
		}
		n := m.lookupNode(target)
		env, ok := m.peerEnv[nodeNum]
		if !ok {
			m.systemLine(fmt.Sprintf("env: %s has no environmental telemetry on file", n.callsign))
			m.systemLine("     (only peers with temp/humidity/pressure sensors broadcast this)")
			return nil
		}
		var lines []string
		if env.temperature != 0 {
			lines = append(lines, fmt.Sprintf("temp:     %.1f °C", env.temperature))
		}
		if env.humidity != 0 {
			lines = append(lines, fmt.Sprintf("humidity: %.0f %%", env.humidity))
		}
		if env.pressure != 0 {
			lines = append(lines, fmt.Sprintf("pressure: %.0f hPa", env.pressure))
		}
		if env.gas != 0 {
			lines = append(lines, fmt.Sprintf("gas:      %.0f Ω", env.gas))
		}
		lines = append(lines, fmt.Sprintf("age:      %s ago", humanDuration(time.Since(env.at))))
		m.systemBlock(fmt.Sprintf("env %s", n.callsign), lines...)
	case "sked":
		target := rest
		if target == "" {
			m.flash = "usage: /sked <callsign>"
			return nil
		}
		m.sendBang("/sked "+target, "proposing scheduled contact, 24h from now")
		m.flash = fmt.Sprintf("!sked %s — proposal sent", target)

	// ── Extra ham/Meshtastic slang ────────────────────────────────
	case "qrz":
		// "Who is calling me?" — broadcast a prompt for identification.
		m.sendBang("/qrz", "QRZ? who's calling?")
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
		m.sendBangReply(
			"/qrm "+target,
			"QRM — interference on your signal",
			m.replyTargetFor(target),
		)
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
		m.sendBangReply("/qsb "+target, "QSB — signal fading, copy weak", m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!qsb %s — fade reported", target)
	case "sk":
		// Final sign-off — stronger than /73. "Signing off clear."
		// /sk           → broadcast SK
		// /sk <call>    → directed "SK <call>" — aimed at a specific
		//                 operator you're closing a contact with.
		//                 Threads via Data.reply_id to that operator's
		//                 most recent message.
		target := strings.TrimSpace(rest)
		if target == "" {
			m.sendBang("/sk", "SK — clear and out 73")
			m.flash = "!sk — clear"
			return nil
		}
		body := "SK — clear and out 73, " + target
		m.sendBangReply("/sk "+target, body, m.replyTargetFor(target))
		m.flash = "!sk " + target + " — cleared"
	case "wx":
		// Weather at my QTH. Optional argument supplies the conditions;
		// without one we emit a placeholder so the user types their own.
		wx := rest
		if wx == "" {
			wx = "clear 55°F light wind"
		}
		m.sendBang("/wx", "wx: "+wx)
		m.flash = "wx: " + wx + " — broadcast"
	case "grid":
		// Just the Maidenhead locator — shorter / more data-friendly
		// than /qth which also names the city.
		grid := rest
		if grid == "" {
			grid = m.myGrid
		}
		if grid == "" {
			m.flash = "no GPS fix — /grid <locator> to send a custom grid"
			return nil
		}
		m.sendBang("/grid", "grid: "+grid)
		m.flash = "grid: " + grid + " — broadcast"
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
		m.sendBang("/mesh", body)
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
		m.sendBangReply("/k "+target, "K — over, go ahead", m.replyTargetFor(target))
		m.flash = fmt.Sprintf("!k %s — over to you", target)

	// ── IRC-style operational commands ────────────────────────────
	case "tr", "traceroute", "trace":
		target := rest
		if target == "" {
			target = m.selectedSender()
		}
		if target == "" {
			m.flash = "usage: /tr <callsign>"
			return nil
		}
		n := m.lookupNode(target)
		if n == nil {
			m.systemLine(fmt.Sprintf("tr: no route data for %s", target))
			return nil
		}
		m.systemBlock(
			fmt.Sprintf("traceroute %s", n.callsign),
			fmt.Sprintf("hops:   %d", n.lastHops),
			fmt.Sprintf("signal: %s", signalReport(n)),
			"note:   live path not yet queried — showing last-known telemetry",
		)
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
			m.systemLine(fmt.Sprintf("ping: node %s unknown", target))
			return nil
		}
		m.systemBlock(
			fmt.Sprintf("ping %s", n.callsign),
			fmt.Sprintf("last heard: %s ago", n.lastHeard),
			fmt.Sprintf("signal:     %s", signalReport(n)),
		)
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
			m.systemLine(fmt.Sprintf("whois: no record of %s", target))
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

		// Multi-line "server reply" block, irssi-style.
		lines := []string{
			fmt.Sprintf("hw:     %s", hw),
			fmt.Sprintf("fw:     %s", fw),
			fmt.Sprintf("heard:  %s ago", n.lastHeard),
			fmt.Sprintf("state:  %s", n.state),
			fmt.Sprintf("signal: %s", signalReport(n)),
		}
		if nodeNum := m.nodeNumOf(target); nodeNum != 0 {
			if pos, ok := m.peerPositions[nodeNum]; ok {
				lines = append(
					lines,
					fmt.Sprintf("grid:   %s", pos.grid),
					fmt.Sprintf(
						"coord:  %.5f, %.5f  alt %d m",
						pos.latitude,
						pos.longitude,
						pos.altitude,
					),
					fmt.Sprintf("pos:    %s ago", humanDuration(time.Since(pos.at))),
				)
			}
		}
		lines = append(lines, "end of /whois")
		m.systemBlock(fmt.Sprintf("whois %s", n.callsign), lines...)
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
	case "nodes":
		// "Node" is the canonical Meshtastic term — radios on the
		// mesh are nodes, not users. We dropped the /users and
		// /names IRC aliases to keep the vocabulary consistent.
		m.openOverlay(overlayNodes)
	case "channel":
		if rest == "list" || rest == "" {
			m.openOverlay(overlayChannels)
			return nil
		}
		m.flash = "usage: /channel list  |  /channel add <meshtastic://url>"
	case "config":
		// Single render path — demo and live both read from model
		// state since demo mode pre-populates these fields. The only
		// difference is a [DEMO] tag on the block header.
		n := m.myNode()
		lines := []string{fmt.Sprintf("callsign: %s", m.myCallsign())}
		if n != nil && n.hwModel != "" {
			lines = append(lines, fmt.Sprintf("hw:       %s", n.hwModel))
		}
		if m.radioFirmware != "" {
			lines = append(lines, fmt.Sprintf("fw:       %s", m.radioFirmware))
		}
		if m.currentChannel != "" {
			lines = append(
				lines,
				fmt.Sprintf("channel:  %s  %s", m.currentChannel, m.radioModemPreset),
			)
		}
		if m.radioRole != "" {
			lines = append(lines, fmt.Sprintf("role:     %s", m.radioRole))
		}
		if m.radioRegion != "" {
			lines = append(lines, fmt.Sprintf("region:   %s", m.radioRegion))
		}
		if m.radioTxPower != 0 {
			lines = append(lines, fmt.Sprintf("tx power: %d dBm", m.radioTxPower))
		}
		if m.myGrid != "" {
			lines = append(lines, fmt.Sprintf("grid:     %s", m.myGrid))
		}
		if m.hasTelemetry {
			lines = append(lines,
				fmt.Sprintf("battery:  %.2f V  %d%%", m.batteryVoltage, m.batteryLevel),
				fmt.Sprintf("chan use: %.1f%%", m.channelUtil),
			)
		}
		lines = append(lines, fmt.Sprintf("peers:    %d known", len(m.nodes)))
		header := "config"
		if m.isDemo() {
			header = "config [DEMO]"
		}
		m.systemBlock(header, lines...)
	case "help", "h":
		// /help             → open the full scrollable overlay
		// /help <verb>      → irssi / BitchX-style per-command usage
		//                     + summary card dropped inline as a
		//                     systemBlock so it lives in the log
		//                     alongside the exchange it's helping
		//                     with (no modal context switch).
		verb := strings.ToLower(strings.TrimSpace(rest))
		if verb == "" {
			m.mode = modeHelp
			return nil
		}
		verb = strings.TrimPrefix(verb, "/")
		entry, ok := helpEntries[verb]
		if !ok {
			m.flash = fmt.Sprintf("no help for /%s — try /help alone for the full index", verb)
			return nil
		}
		m.systemBlock(
			fmt.Sprintf("help /%s", verb),
			"usage:   "+entry.usage,
			"summary: "+entry.summary,
		)
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

// systemLine appends a single-line system/meta entry to the message
// log. Prefixed with `-!-` irssi-style. Never transmits over LoRa —
// display-only. Used for short one-shot notices.
func (m *model) systemLine(text string) {
	m.messages = append(m.messages, messageItem{
		time:   timeNowHHMM(),
		text:   "-!- " + text,
		status: "system",
	})
	m.selectedMsg = len(m.messages) - 1
}

// systemBlock emits a multi-line "server reply" block. Each line
// becomes its own messageItem, but all carry the same `group` ID —
// the renderer uses this to (a) give every row in the block the
// same zebra stripe color, (b) hide the timestamp on continuation
// rows so only the header carries it, and (c) let j/k navigation
// keep cursor movement smooth across blocks.
func (m *model) systemBlock(header string, lines ...string) {
	gid := nextGroupID()
	t := timeNowHHMM()
	m.messages = append(m.messages, messageItem{
		time:   t,
		text:   "-!- " + header,
		status: "system",
		group:  gid,
	})
	for _, l := range lines {
		m.messages = append(m.messages, messageItem{
			time:   t,
			text:   "-!-    " + l,
			status: "system",
			group:  gid,
		})
	}
	m.selectedMsg = len(m.messages) - 1
}

// groupCounter is a monotonically-increasing counter used to tag
// members of a systemBlock with a shared ID so the renderer can
// bind them visually.
var groupCounter uint64

func nextGroupID() uint64 {
	groupCounter++
	return groupCounter
}

// sendBang appends an outgoing command-originated message to the
// local log AND (in live-radio mode) transmits it over LoRa via the
// pump. The `bang` field is kept purely for local UI styling — the
// on-wire text is just `body`, clean enough that any other
// Meshtastic client reads it as plain chat.
//
// Used by /cq, /73, /qsl, /qth, /grid, /rs, /cqr, /sk, /qrz, /qrm,
// /qsb, /wx, /k, /mesh. Commands that don't transmit (/whois, /ping,
// /tr, /env, /config) use systemLine() instead.
func (m *model) sendBang(bang, body string) {
	m.sendBangReply(bang, body, 0)
}

// sendBangReply is sendBang with an optional reply target — when
// replyToID is non-zero, the outgoing packet carries Data.reply_id
// pointing at the parent message, and the local log entry records
// the same replyID so the renderer can draw a quoted-parent line
// above the reply.
func (m *model) sendBangReply(bang, body string, replyToID uint32) {
	status := "ack"
	if !m.isDemo() {
		status = "pending" // flipped to "ack" when the radio echoes our packet back
	}
	item := messageItem{
		time:    timeNowHHMM(),
		from:    "me",
		mine:    true,
		bang:    bang,
		text:    body,
		status:  status,
		replyID: replyToID,
	}
	m.messages = append(m.messages, item)
	m.selectedMsg = len(m.messages) - 1
	m.focused = paneMessages

	// Persist the outgoing so the log survives restart. Skipped in
	// demo mode (m.db is always nil there).
	_ = saveMessage(m.db, m.currentChannel, item)

	if m.pump != nil {
		m.pump.Enqueue(newTextToRadio(body, m.currentChannelIndex(), replyToID))
	}
}

// replyTargetFor returns the packetID of the most recent message
// from the given callsign, or 0 if none exists. Used by directed
// ham verbs (/73 <call>, /qsl <call>, /sk <call>, /rs <call>, etc.)
// to thread the outgoing reply to whatever <call> most recently
// said — the Meshtastic "reply to" semantic.
func (m *model) replyTargetFor(call string) uint32 {
	if call == "" {
		return 0
	}
	target := strings.ToLower(strings.TrimSpace(call))
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.mine || msg.status == "system" || msg.packetID == 0 {
			continue
		}
		if strings.Contains(strings.ToLower(msg.from), target) {
			return msg.packetID
		}
	}
	return 0
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
		status := ""
		if m.connectDest != "" {
			if m.connected {
				status = fmt.Sprintf("✓ connected to %s", m.connectDest)
			} else {
				status = fmt.Sprintf("connecting to %s …", m.connectDest)
			}
		}
		return renderSplash(m.w, m.h, m.splash, status)
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
	brand := call.Render(`//\`) + "  " + call.Render(m.myCallsign())
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

	// Name stays neutral fg for online nodes — the green `@` sigil
	// alone carries the "alive" pulse (irssi / BitchX user-list
	// convention). Non-online states do tint the name since those
	// ARE worth surfacing at a glance.
	nameColor := mhFG
	switch n.state {
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
			bg = zebraBg(i)
			lastGroup = msg.group
			groupBg = bg
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

		line := m.renderMessageRow(msg, isSelected, width-4, bg)
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
	// real callsigns as NodeInfo arrives mid-session.
	fromRaw := m.displayFrom(msg)
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
	// Hops cell is always rendered for incoming packets so absence
	// never has to be inferred — direct (0 hop) reads "↝ dx",
	// multi-hop reads "↝Nh". Our own outgoing messages (mine) skip
	// the cell since "hops from me to mesh" isn't a thing we know
	// until the packet echoes back.
	hopCol := "      "
	switch {
	case msg.mine:
		// blank — our outbound packet, no RX hops to report.
	case msg.hops > 0:
		hopCol = fmt.Sprintf("↝%dh   ", msg.hops)
	default:
		hopCol = "↝ dx   "
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

	// The on-wire content is msg.text only — msg.bang is purely a
	// local marker identifying which /command emitted the message.
	// We render the text as-is, so what you see matches what went
	// out (a clean ham-style payload like "73" or "QTH: CN85", not
	// meshx-internal "!grid CN85" chrome).
	txtClamped := padOrTruncate(msg.text, textW)
	styledTxt := text.Render(txtClamped)
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
		styledTxt

	row := left + right
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
func (m model) displayFrom(msg messageItem) string {
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

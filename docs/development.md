# Development guide

## Prerequisites

- macOS or Linux (terminal with ANSI + unicode block character support)
- [Go](https://go.dev/dl/) 1.21+
- [just](https://github.com/casey/just) — command runner
- [golangci-lint](https://golangci-lint.run/) — Go linter

## Getting started

```bash
git clone https://github.com/retr0h/meshx.git
cd meshx
just fetch    # fetch shared justfiles
just deps     # install tool dependencies
```

## Common commands

```bash
just deps          # install all dependencies
just test          # all tests (lint + format check + unit + coverage)
just ready         # format + lint before committing
just go::unit      # unit tests only
just go::vet       # golangci-lint
just go::fmt       # auto-format (gofumpt + golines)
just just::fmt     # format justfiles
```

## Running

```bash
go run . demo                              # canned-fixture UI, no radio
go run .                                   # auto-connect: USB → saved BLE
go run . usb probe                         # list USB candidates
go run . usb connect /dev/cu.usbmodem2101  # explicit serial path
go run . tcp connect 10.0.0.50:4403        # meshtasticd / WiFi radio
go run . ble scan                          # nearby Bluetooth radios
go run . ble pair <uuid>                   # save for later connects
go run . ble connect <uuid|name>           # open TUI over Bluetooth
go run . ble probe <uuid>                  # 15s diagnostic packet dump

# Pump events to a log file when the TUI is up (alt-screen swallows
# stderr so this is the only way to inspect live transport flow).
MESHX_DEBUG=1 go run . ble connect <uuid>  # writes /tmp/meshx-pump.log
```

## Architecture

```
meshx/
├── main.go                       # tiny — forwards to cmd.Execute()
├── cmd/
│   ├── root.go                   # cobra root + auto-connect chain
│   ├── demo.go                   # `meshx demo`
│   ├── usb.go                    # `meshx usb {probe,connect}`
│   ├── probe.go                  # body of `meshx usb probe`
│   ├── tcp.go                    # `meshx tcp connect`
│   ├── ble.go                    # `meshx ble {scan,pair,list,forget,connect,disconnect,fav}`
│   └── ble_probe.go              # `meshx ble probe` diagnostic dump
└── internal/meshx/
    ├── app.go                    # Bubble Tea model: state, Update, View,
    │                             # newModel, autoConnect, myCallsign …
    ├── ui.go                     # View dispatcher, model getters, generic utils
    ├── fixture.go                # Demo struct + DefaultDemo() persona
    ├── pump.go                   # transport → tea.Msg pump (+ MESHX_DEBUG)
    ├── commands.go               # /command dispatcher + ham bangs
    ├── input.go                  # key bindings, nav mode, tab wiring
    ├── components_box.go         # Box/Component/Cell/Row + RawBlock, Viewport, Centered
    ├── components_stack.go       # VStack, HStack, Bordered, Styled
    ├── components_chrome.go      # statusBar / topDivider / channelTabsRow / inputBar
    ├── components_chat.go        # chatRow* cell builders + nick/zebra colors
    ├── components_notice.go      # noticeRow* cell builders
    ├── components_message.go     # messageRow Component + notice/chat dispatch
    ├── components_overlays.go    # overlay row builders + selection chrome
    ├── components_panes.go       # channels/nodes/messages/help pane Components + frameView
    ├── components_panes_geo.go   # nearby/radar pane Components + peerPlot data prep
    ├── components_radar.go       # radarCanvas Component + radar legend cells
    ├── components_splash.go      # BitchX rotating splash data + builder
    ├── notices.go                # TTL + pin + fade for `-!-` rows
    ├── storage.go                # SQLite: nodes, messages, ble_devices,
    │                             # backfills, stale-pending sweep
    ├── ble_cli.go                # `meshx ble` CLI helpers
    ├── complete.go               # Tab completion — /cmd, #chan, nicks
    ├── palette.go                # maxheadroom color constants
    ├── node.go                   # nodeItem + state derivation
    ├── radio.go                  # outbound packet construction
    ├── geo.go                    # haversine / bearing / compass math
    ├── help.go                   # /help entry data
    ├── logger.go                 # debug log file helper
    ├── migrations/               # embedded goose SQL migrations
    └── transport/
        ├── client.go             # Client interface + Dial dispatcher
        ├── framing.go            # 0x94 0xc3 <hi> <lo> <proto> frame codec
        ├── stream.go             # Shared framed-stream runner (serial/tcp)
        ├── serial.go             # USB-serial transport
        ├── tcp.go                # TCP transport (meshtasticd / WiFi)
        ├── ble.go                # Bluetooth LE transport
        └── identify.go           # AutoDetectMeshtastic USB probe
```

### Public API

```go
meshx.RunDemo()                            // demo fixture, no radio
meshx.RunRadio("/dev/cu.usbmodem2101")     // live — serial / TCP / "ble:<uuid>"
meshx.RunBLE("<uuid|name>")                // resolve saved BLE device + open TUI
meshx.AutoConnectTarget()                  // bare-`meshx` resolution chain
meshx.BLEScan / BLEPair / BLEListDevices
meshx.BLEForget / BLEMarkFavorite / BLESetFavorite
meshx.DefaultDemo() *Demo                  // canonical persona
```

`RunDemo` / `RunRadio` both boil down to
`tea.NewProgram(newModel(demo, dest), tea.WithAltScreen()).Run()`. `RunBLE` is a
thin wrapper that resolves a name-or-uuid against `ble_devices` and delegates to
`RunRadio("ble:<uuid>")` — `transport.Dial` routes the prefix to `DialBLE`.

## Dependencies

| Package                         | Purpose                                        |
| ------------------------------- | ---------------------------------------------- |
| `charmbracelet/bubbletea`       | Elm-style TUI framework                        |
| `charmbracelet/bubbles`         | textinput widget for input + search prompts    |
| `charmbracelet/lipgloss`        | colors, borders, layout primitives             |
| `spf13/cobra`                   | CLI command tree                               |
| `lmatte7/gomesh/...gomeshproto` | Meshtastic protobuf definitions                |
| `go.bug.st/serial`              | cross-platform USB-serial                      |
| `tinygo.org/x/bluetooth`        | cross-platform Bluetooth LE (macOS / Linux)    |
| `google.golang.org/protobuf`    | proto marshal / unmarshal                      |
| `mattn/go-sqlite3`              | SQLite driver (CGo) for scrollback persistence |
| `pressly/goose`                 | embedded SQL migrations                        |

## Modal UI — where the code lives

- **Mode constants** — `modeSplash`, `modeInput`, `modeNav`, `modeSearch`,
  `modeHelp` in `app.go`
- **Dispatcher** — `(m model) Update(tea.Msg)` routes by mode to `updateInput` /
  `updateNav` / `updateSearch` / `updateHelp` (splash is inlined)
- **Overlays** — `overlayNone` / `overlayChannels` / `overlayNodes`; set by
  `openOverlay()`, closed by `closeOverlayToInput()`
- **ESC is always "back to input"** — any sub-state maps back via
  `closeOverlayToInput()`

## Renderer conventions

- **Palette** lives in `palette.go`. Every color used by the UI is a named
  constant there; no inline hex elsewhere.
- **Zebra rows** — `rowBgEven` / `rowBgOdd`; message log picks via
  `zebraBg(index)`.
- **Selection highlight** —
  `wrapSelection(content, selected, isSearchHit, width, rowBg...)` wraps any row
  with a gutter + tinted bg. Used by the message list, channels overlay, and
  users grid. Tail-pads use an explicit bg-styled span (not a lipgloss outer
  wrap) because each inner SGR ends in `\e[0m` which would reset any outer bg
  before the trailing spaces — without the explicit span the zebra row drops off
  after the body's last character.
- **Truncation** — `padCells` (in `box.go`) is the canonical pad/truncate
  funnel; it builds on `ansi.Truncate` so styled prefixes survive the cut and
  ANSI SGR sequences are never split mid-byte.
- **Pane accents** — `paneAccentColor(paneIdx)` returns the per-pane signature
  color (channels = cyan, messages = mesh-green, nodes = magenta). Used by
  focused-pane borders and the giant pane-number overlay.

### Layout primitives — Component tree

`components_box.go` and `components_stack.go` define the layout vocabulary the
`View()` tree is built from. Every region of the UI is a `Component` whose
`Render(box Box) string` returns precisely `box.Height` lines, each precisely
`box.Width` cells per `ansiCells`. There is no upward negotiation — parents own
the math, children fill what they're given.

- `Box{Width, Height}` — the cell budget a Component must fill exactly.
- `Component` — interface; one method, `Render(box Box) string`.
- `Row` / `Cell` — single-row horizontal layout. Cells declare width (or `-1`
  for flex), an optional `PadStyle` to tint cell-internal padding, and an
  alignment; `Row.Render` truncates anything that would overflow the box and
  pads anything short. `Row.FillStyle` tints the trailing flex fill so a zebra
  row stays a solid rectangle past the last cell.
- `Text`, `Spacer` — leaf renderers (single string filling a box, blank fill).
- `RawBlock` — wraps a pre-rendered multi-line string and fits it into a Box;
  the bridge between legacy string emitters and the layout tree, used by
  `renderBorderedPane` and `frameView`.
- `Viewport` — scrollable single-pane window over a slice of pre-styled lines
  with optional footer chrome. Owns scroll-clamp + visible-row math; consumed by
  `helpPane`.
- `Centered` — pane-aware horizontal + vertical centering (each line padded
  against the parent Box, not its own width).
- `VStack` / `HStack` — vertical / horizontal stack of `SizedChild` with flex
  (-1) sharing.
- `Bordered` — wraps an inner Component in a `╔═══╗` / `┌───┐` frame with
  optional padding, subtracting border + padding from the inner box. Replaces
  the legacy lipgloss `paneStyle` so message panes / overlays measure with
  `ansiCells` (keycap-aware) instead of runewidth (which under-counts VS16 emoji
  and pushes the right `║` out of column).
- `Styled` — applies a styler to an already-composed Component without changing
  cell count.

`ansiCells` is the single source of truth for measurement. It starts from
`ansi.StringWidth` and promotes any grapheme cluster containing VS16 (U+FE0F) or
COMBINING ENCLOSING KEYCAP (U+20E3) to 2 cells per Unicode TR51
emoji-presentation rules — without this, "7️⃣"-bodied rows render 1 cell wider
than the layout pipeline thinks they are and the right `║` frame walks out of
column.

Concrete Components live in:

- `components_chrome.go` — `statusBar`, `topDivider`, `channelTabsRow`,
  `inputBar` plus per-segment cell builders.
- `components_panes.go` — pane Components (`channelsPane`, `nodesPane`,
  `messagesPane`, `helpPane`) plus `frameView`, `renderIrssiBody`,
  `renderBorderedPane`, `paneAccentColor`, `paneInnerWidth`,
  `messagesPaneRender`, `tailStartList`. Each pane Component owns its
  implementation directly — no model-method shims.
- `components_panes_geo.go` — `nearbyPane`, `radarPane` and the `peerPlot` data
  prep both consume.
- `components_message.go` — `messageRow` Component owns the notice/system/chat
  dispatch via `noticeRowRender` / `chatRowRender` and forces every line through
  `padCells`.
- `components_chat.go` / `components_notice.go` / `components_overlays.go` /
  `components_radar.go` — leaf cell builders the rows compose. Selection chrome
  (`wrapSelection`, `gutterWidth`, `dimRow`) lives in `components_overlays.go`.

The frame `View()` builds:

```
VStack:
  statusBar       (1 row)
  topDivider      (1 row)
  body (flex)     ← renderIrssiBody → channelsPane | nodesPane |
                                       messagesPane | nearbyPane |
                                       radarPane | helpPane
  channelTabsRow  (1 row)
  inputBar        (1 row)
  Spacer          (1 row trailing — keeps cursor off the last terminal row)
```

Set `MESHX_LAYOUT_ASSERT=1` to enable dev-mode invariant panics: every
`Component.Render` is checked to return exactly the requested box, so a
regression in cell-counting math surfaces as an immediate panic at the offending
call site instead of as visible drift two rerenders later. The env lookup is
hoisted to a package-level once-read in `components_box.go`, so the check is
free in production. Run the test suite with this flag set in CI.

## Tab completion

`complete.go`:

- `slashCommands` — canonical command list for tab cycling
- `computeCompletions(text, cursor)` — returns `(matches, start, end)` based on
  current word context:
  - Word starts with `/` → command universe
  - Word starts with `#` or `*` → channel names
  - Otherwise → node callsigns
- `applyCompletion(text, start, end, match)` — inserts the match. At
  start-of-line + nick match, appends `: ` (irssi nick-address idiom); otherwise
  a plain space.
- Cycling state lives in `tabState` on the model; any non-Tab keypress clears
  it.

## Ham command dispatch

Every ham `/command` runs through `executeCommand(raw string) tea.Cmd` in
`commands.go`. Target-taking commands default to the highlighted sender in nav
mode via `selectedSender()`.

Reports use real node telemetry:

```go
n := m.lookupNode(target)          // pointer to node or nil
report := signalReport(n)          // "hop 2, SNR -8.5 dB, RSSI -92 dBm"
```

Every field on `nodeItem` (`lastSNR`, `lastRSSI`, `lastHops`, `hwModel`,
`firmware`) is populated from Meshtastic protobuf in live-radio mode —
`MeshPacket.rx_snr`, `rx_rssi`, `hop_start - hop_limit`,
`MyNodeInfo.HardwareModel`, `firmware_version`. In demo mode the same fields are
seeded from `DefaultDemo()` so the render code has one path.

## Demo fixture — one model, two producers

There is no "demo renderer" or "live renderer" — the tea model has a single set
of fields (`myNodeNum`, `nodes`, `channels`, `messages`, `radioFirmware`,
`radioRegion`, `radioTxPower`, `batteryLevel`, `myGrid`, …) that every view
function reads from. Two producers populate those fields:

1. **Live radio** — the transport pump (`pump.go`) decodes each `FromRadio`
   envelope into a `radio<Name>Msg` and sends it to the tea program; `Update` in
   `demo.go` writes into the model fields.
2. **Demo fixture** — `newModel(DefaultDemo(), "")` copies the Demo struct's
   values into those same fields at construction time, sets `connected = true`
   and `hasTelemetry = true`, and hands control straight to the UI.

Two `isDemo()` checks survive because those semantics genuinely differ:

- The `[DEMO]` badge on the rightmost status-bar segment.
- `sendBang`'s fake-ack status (demo flips pending → ack immediately since no
  radio will echo back).

Adding a new field to the UI means: add it to `Demo`, set it in `DefaultDemo`,
copy it inside `newModel` when demo != nil, and read `m.<Field>` in whatever
renderer needs it. Works in both modes with zero branching.

## Radio transport

`internal/meshx/transport` wraps the Meshtastic USB-serial / TCP wire protocol.
`Dial(dest)` returns a `Client` whose `Send(*ToRadio)` enqueues outbound
envelopes and `Stream(ctx)` returns a `<-chan *FromRadio`. The framing is
identical across serial and TCP: `0x94 0xc3 <hi> <lo> <protobuf>` — see
`framing.go`.

`AutoDetectMeshtastic(timeout)` walks `/dev/cu.*` ports, handshakes each, and
returns the first that talks Meshtastic. Used by `cmd.usbConnect` with no
explicit device path, and by `meshx.AutoConnectTarget` for the bare-`meshx`
resolution chain.

`pump.go` runs as a goroutine kicked off from the model's `Init()` via
`openPumpMsg` — deferring the spawn until after `tea.Program.Run()` avoids a
`program.Send()` deadlock. Each `FromRadio` envelope is mapped to exactly one
`radio<Name>Msg` type and sent to the tea loop.

## Persistence — SQLite scrollback

Live-radio mode opens `~/.meshx/meshx.db` (WAL journal, `_busy_timeout=5000`)
via `openStorage(path)` and replays the last 500 messages on boot. `storage.go`
is the whole surface: `defaultStoragePath`, `openStorage`, `saveMessage`,
`loadMessages`. The schema is one flat `messages` table mirroring `messageItem`
plus a `channel` column.

Demo mode never touches the DB (`m.db == nil`). System / flash rows are skipped
on save — stale by the time you read them back. Write errors are
logged-then-swallowed; losing history beats crashing the UI.

## Threading

Directed ham verbs (`/73 <call>`, `/qsl <call>`, `/sk <call>`, `/rs <call>`,
`/cqr <call>`, `/k <call>`, `/qrm <call>`, `/qsb <call>`) set `Data.reply_id` on
the outgoing packet pointing at the target's most recent message's
`MeshPacket.id`. The lookup runs via `replyTargetFor(call)` in `demo.go`;
`newTextToRadio(text, channel, replyID)` threads it onto the wire.

Receive side: `radioTextMsg` captures both `packetID` (the incoming packet's id)
and `replyID`, and `applyTextMessage` records them on `messageItem`. The
renderer checks `msg.replyID != 0` and, when the parent is findable in
`m.messages`, prepends a dim quoted-parent line above the reply row:

```
  ┌ KC7XYZ 🦀 13:52  "Test, plz confirm"
› me  13:53  /73 KC7XYZ — 73 KC7XYZ                                  ✓
```

`findMessageByPacketID` resolves parent lookups; `truncateRunes` caps the quoted
body so long parents don't blow the width budget.

## Testing

```bash
go test ./internal/meshx/...            # all tests
go test -run TestSnapshotView -v ./internal/meshx/  # print a visual snapshot
```

`demo_snapshot_test.go` prints the rendered `View()` at a fixed terminal size
(`160×40`) so you can eyeball layout changes in `go test -v` output.

## Color palette (Max Headroom)

All constants in `palette.go`:

```
#ffb86c  orange    - timer / battery warnings
#00d4ff  cyan      - inactive channel tabs, unfocused headers
#c678dd  magenta   - "me" messages, nodes pane accent
#50fa7b  green     - online node state, ACK ✓
#e5c07b  yellow    - unread counts, !bang command prefix
#ff6ec7  pink      - ACTIVE channel tab, error flashes
#6272a4  lavender  - muted states, "other" tab names
#c0caf5  fg        - default text
#3b4261  drained   - labels, separators, dim italic hints
#67ea94  meshgreen - focused pane border, input prompt, brand
```

## Sister projects

| Project                                                        | Description                              |
| -------------------------------------------------------------- | ---------------------------------------- |
| [tlock](https://github.com/retr0h/tlock)                       | Terminal lock screen with Touch ID       |
| [grind](https://github.com/retr0h/grind)                       | 8-bit retro terminal timer               |
| [osapi](https://github.com/osapi-io/osapi)                     | Linux system management REST API and CLI |
| [osapi-justfiles](https://github.com/osapi-io/osapi-justfiles) | Shared justfile recipes                  |

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
go run .                                   # bare meshx — prints help (no auto-connect)
go run . usb scan                          # identify Meshtastic radios on USB
go run . usb connect                       # auto-detect single USB radio + open TUI
go run . usb connect /dev/cu.usbmodem2101  # explicit serial path
go run . usb probe --port /dev/cu.usb…     # deep diagnostic — dumps every FromRadio packet
go run . ble scan                          # nearby Bluetooth radios
go run . ble pair <uuid>                   # save for later connects
go run . ble list                          # show paired devices
go run . ble fav <uuid|name>               # mark bare-launch favorite
go run . ble connect <uuid|name>           # open TUI over Bluetooth
go run . ble probe <uuid>                  # 15s diagnostic FromRadio dump

# Debug logging — `--debug` (or MESHX_DEBUG=1) flips the global slog
# level so each subcommand's "running" line becomes visible. `--json` / `-j`
# swaps to JSON for log aggregators.
go run . --debug ble pair <uuid>
```

## Architecture

```
transport (USB / BLE / TCP)
         │
         ▼
   internal/meshx/pump      ← proto ↔ model translation, reconnect
         │
    ┌────┴────┐
    ▼         ▼
  bbolt     event bus        ← storage + fan-out
  store       │
    └────┬────┘
         ▼
      internal/tui           ← Bubble Tea render loop
```

Package-level overview (file-level detail lives in each package's headers —
`ls internal/<pkg>/` + read the top-of-file comments):

| Package                     | Role                                                                                                                              |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| `cmd/`                      | Cobra command tree — `usb`, `ble`, `tcp` subcommands                                                                              |
| `internal/radio/`           | Per-radio session layer — canonical `*State`, `Apply*` handlers, `ops_*` (channels/config/radio/send)                             |
| `internal/bus/`             | In-process publish/subscribe event bus — fan-out to TUI and any other subscriber; slow consumers drop events rather than blocking |
| `internal/meshx/model/`     | Canonical wire/persisted shapes — the lingua franca all layers share                                                              |
| `internal/meshx/pump/`      | Transport ↔ tea bridge — reconnect policy, proto ↔ model translation                                                            |
| `internal/meshx/storage/`   | bbolt persistence — messages, nodes, BLE devices (`~/.meshx/meshx.bolt`)                                                          |
| `internal/meshx/transport/` | `Client` interface + serial/TCP/BLE implementations + frame codec                                                                 |
| `internal/tui/`             | Bubble Tea rendering — `Component` tree, layout primitives, pane Components, input/commands                                       |
| `internal/cli/`             | CLI-only output helpers (themed lipgloss rendering, banner) — imported only by `cmd/`, never by the TUI                           |
| `internal/version/`         | Build identity (Version / Commit / Date / BuiltBy)                                                                                |

### Public API

`internal/` packages are not re-exported. The cmd tree consumes them directly:

```go
tui.RunRadio("/dev/cu.usbmodem2101")         // live — serial or "ble:<uuid>"
transport.ScanBLE(timeout)                   // BLE discovery
transport.PairBLE(uuid)                      // OS-level bonding
transport.IdentifyAllSerial(timeout)         // USB scan + handshake probe
transport.AutoDetectMeshtastic(timeout)      // single-Meshtastic-port helper
storage.New(path) → *Bolt                    // bbolt handle (BLE devices, messages, …)
```

`tui.RunRadio` calls
`tea.NewProgram(newModel(dest), tea.WithAltScreen()).Run()`.
`ble connect <name>` resolves the name through `storage.LookupBLEDevice` and
hands off to `tui.RunRadio("ble:<uuid>")` — `transport.Dial` routes the prefix
to `DialBLE`.

### `model` is the lingua franca

`internal/meshx/model/` holds the canonical wire/persisted shapes every boundary
in the codebase speaks. Three consumers all traffic in `mdl.X`:

```mermaid
flowchart TB
  M["**model package**<br/>Message · NodeInfo · Position · Routing · Ping<br/>LoraConfig · ExternalNotification · …"]
  P["**pump**<br/>proto→model"]
  S["**storage**<br/>CRUD · *Bolt"]
  B["**bus**<br/>fan-out events"]
  T["**TUI Update**<br/>case mdl.Text / NodeInfo / Position / …"]
  P --> M
  S --> M
  M --> B
  B --> T
```

Inbound, `pump/translate.go` projects `*pb.FromRadio` → `model.X` events.
Outbound, `pump/outbound.go::Send(model.Command)` is a type-switch dispatcher
that builds the matching `*pb.ToRadio` envelope. The pump package is the
**only** place in the codebase where `gomeshproto` types meet `model` types in
either direction. Everywhere else — the meshx TUI, the storage layer — sees only
`mdl.X`. The proto↔model bridges for full-record configs that need round-trip
preservation (today: `ExternalNotification`) live in `pump/config.go`;
`commands.go` calls those bridges when crafting outbound `AdminMessage`
envelopes so it never directly assembles a config proto.

### Consumer interfaces (osapi-io pattern)

Both `pump.Pump` and `storage.Bolt` are concrete structs in their own packages.
Where they're consumed (the meshx TUI), the consumer declares a narrow interface
listing only the methods it uses:

- `internal/meshx/pump.go` — `Pump` interface (`Enqueue`, `Stop`)
- `internal/meshx/store.go` — `Store` interface (the methods the TUI calls)

Both interfaces sit next to each other so future consumers can declare their own
— likely larger — interfaces without bloating the TUI's view of the contract.
The compile-time binding `var p Pump = pump.New(...)` at the construction site
catches drift the moment a method gets renamed.

The same pattern applies in `cmd/`: `cmd/transports_deps.go` declares
`bleScanner` / `blePairer` / `bleStore` interfaces and `cmd/usb_*.go` declares
`usbScanner`. Production wiring sits at the bottom of each file and tests can
swap the package-level vars to fake the host. The transport adapters all
delegate into `internal/meshx/transport`, which means **`meshx ble scan`,
`meshx usb scan`, etc. don't need a daemon to be running** — they're direct OS
interrogations.

## Logging

Logging is a single package-level `slog.Logger` set up in
`cmd/root.go::initLogger` via `cobra.OnInitialize`. The default handler is
`lmittmann/tint` (colored when stderr is a TTY, plain otherwise); `--json` /
`-j` swaps in `slog.NewJSONHandler` for log aggregators; `--debug` / `-d` flips
the level. Subcommands tag their child logger with `subsystem=<verb>.<action>`
and emit a `Debug("running", …)` line at the top of each `RunE` so debugging
shows the parsed inputs without polluting default UX.

## Dependencies

| Package                         | Purpose                                     |
| ------------------------------- | ------------------------------------------- |
| `charmbracelet/bubbletea`       | Elm-style TUI framework                     |
| `charmbracelet/bubbles`         | textinput widget for input + search prompts |
| `charmbracelet/lipgloss`        | colors, borders, layout primitives          |
| `spf13/cobra`                   | CLI command tree                            |
| `spf13/viper`                   | flag/env/default config resolution          |
| `lmittmann/tint`                | colored slog handler                        |
| `lmatte7/gomesh/...gomeshproto` | Meshtastic protobuf definitions             |
| `go.bug.st/serial`              | cross-platform USB-serial                   |
| `tinygo.org/x/bluetooth`        | cross-platform Bluetooth LE (macOS / Linux) |
| `google.golang.org/protobuf`    | proto marshal / unmarshal                   |
| `go.etcd.io/bbolt`              | embedded key-value store for persistence    |

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

```mermaid
flowchart TB
  V["VStack"]
  S1["statusBar (1 row)"]
  D["topDivider (1 row)"]
  B["body (flex)<br/>renderIrssiBody → channelsPane · nodesPane · messagesPane · nearbyPane · radarPane · helpPane"]
  T["channelTabsRow (1 row)"]
  I["inputBar (1 row)"]
  SP["Spacer (1 row trailing — keeps cursor off the last terminal row)"]
  V --> S1 --> D --> B --> T --> I --> SP
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

Every field on `NodeItem` (`LastSNR`, `LastRSSI`, `LastHops`, `HwModel`,
`Firmware`) is populated from Meshtastic protobuf — `MeshPacket.rx_snr`,
`rx_rssi`, `hop_start - hop_limit`, `MyNodeInfo.HardwareModel`,
`firmware_version`.

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

## Persistence — bbolt scrollback

Live-radio mode opens `~/.meshx/meshx.bolt` via the `internal/meshx/storage`
package and replays the last 500 messages on boot. The TUI consumes a narrow
`Store` interface (defined in `store.go`); the concrete `*storage.Bolt`
implements it. The bucket layout is:

```
meta/
  schema_version → "1"
radios/
  <uuid> → JSON-encoded radio record (name, favorite flag, …)
messages/
  <channel>/<seq> → JSON-encoded message record
nodes/
  <node-num> → JSON-encoded node record
```

System / flash rows are skipped on save. Write errors are logged-then-swallowed;
losing history beats crashing the UI.

## Threading

Directed ham verbs (`/73 <call>`, `/qsl <call>`, `/sk <call>`, `/rs <call>`,
`/cqr <call>`, `/k <call>`, `/qrm <call>`, `/qsb <call>`) set `Data.reply_id` on
the outgoing packet pointing at the target's most recent message's
`MeshPacket.id`. The lookup runs via `replyTargetFor(call)`;
`newTextToRadio(text, channel, replyID)` threads it onto the wire.

Receive side: the pump's `mdl.Text` event carries both `PacketID` (the incoming
packet's id) and `ReplyID`, and `applyTextMessage` records them on the embedded
`mdl.Message` of `messageItem`. The renderer checks `msg.ReplyID != 0` and, when
the parent is findable in `m.messages`, prepends a dim quoted-parent line above
the reply row:

```
  ┌ KC7XYZ 🦀 13:52  "Test, plz confirm"
› me  13:53  /73 KC7XYZ — 73 KC7XYZ                                  ✓
```

`findMessageByPacketID` resolves parent lookups; `truncateRunes` caps the quoted
body so long parents don't blow the width budget.

## Testing

Every PR that adds or changes behavior ships with the tests that verify it. **No
"Test plan" sections in PR descriptions that ask the human to verify by hand.**
Manual checklists rot; automated tests don't. If a behavior genuinely cannot be
tested (real-radio integration, hardware reset), call it out explicitly — don't
bury it.

### The shape rule (non-negotiable)

**One test function per public surface. Every test is table-driven.** A test
that exercises only one scenario today is still written as a one-row table — the
second scenario is a new row, not a new function. Don't write
`TestFooHappyPath`, `TestFooMissingField`, `TestFooNotFound` as three separate
functions; those are three rows of `TestFoo`. Ad-hoc sibling test functions are
forbidden.

A "public surface" is:

- An exported type's public method (one `Test<Type>_<Method>` per method — see
  naming below).
- A package-level function (one `Test<Function>` per function).

#### Naming

| Subject in code                        | Test name               |
| -------------------------------------- | ----------------------- |
| `LoadAuthToken` (function)             | `TestLoadAuthToken`     |
| `Session.ApplyText` (method on a type) | `TestSession_ApplyText` |

The underscore is reserved for the `Type.Method` separator — Go-stdlib idiom
(`TestFile_Stat`, `TestBuffer_WriteByte`), and `go test -run Type/Method`
formats it cleanly. Never use the underscore as a `_HappyPath` / `_Failure`
discriminator — those are table rows, not function names.

#### Tables, always

Scenarios go in a single `[]struct{name, ...}` table. When their mechanics are
uniform (same setup, same act/assert shape, different inputs and expectations),
the loop body is one block. When scenarios genuinely diverge in mechanics (one
tests cancellation, another tests delivery), the table holds a
`run func(t *testing.T)` field per row OR use
`t.Run("scenario-name", func(t *testing.T) { ... })` sub-tests under the same
parent — _still one parent function_, never sibling top-levels.

```go
func TestSession_ApplyText(t *testing.T) {
    t.Parallel()

    cases := []struct {
        name     string
        input    mdl.Text
        wantLen  int
        wantBody string
    }{
        {
            name:     "appends-message",
            input:    mdl.Text{Body: "hi"},
            wantLen:  1,
            wantBody: "hi",
        },
        {
            name:    "ignores-empty-body",
            input:   mdl.Text{Body: ""},
            wantLen: 0,
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) { /* shared act/assert */ })
    }
}
```

### File naming (non-negotiable)

**One test file per production file: `foo.go` is tested by `foo_test.go` —
nothing else.** When you write a test for code in `transport_ble.go`, the test
goes in `transport_ble_test.go`, not `handlers_transport_ble_test.go` or
`ble_handlers_test.go` or anything else. The pairing must be obvious from a
directory listing.

Two narrow exceptions, and only these two:

1. **Shared test fixtures / fakes** can live in a `*_test.go` file with no
   production counterpart — e.g., `helpers_test.go`, `fakes_test.go`. Use only
   when fixtures are genuinely shared across multiple production files' tests;
   otherwise inline them.
2. **`main_test.go`** for `TestMain` / package-wide setup.

Test files for code in a file that grew enough to split is a signal that the
production file itself should be split first — never invent a `_test.go` file
that doesn't pair with production.

### Tooling

- **`testing` + `testify/require`** for unit tests. Standard library first;
  reach for `testify` only when an assertion would otherwise need a long custom
  message.
- **In-process `*radio.Session`** (constructed via `radio.New(nil, nil, nil)`)
  for testing the apply paths without a real radio. For radio-dispatch
  verification (commands reaching the pump), satisfy `radio.Pump` with a fake
  that captures dispatched commands.
- **bbolt test patterns** — use `storage.New(t.TempDir() + "/test.bolt")` to get
  a real database in a temp directory; `t.Cleanup` handles removal.
- **Race detector** — `go test -race ./...` for anything with goroutines (bus,
  pump). Cheapest way to catch a slipped lock.

### Other rules

- **Test public-facing surfaces** — the exported type, the wire shape. Internal-
  only helpers can be tested incidentally through their public callers; don't
  write a parallel test for every unexported function.
- **Bound waits.** Anything blocking takes
  `select { case … : case <-time.After(time.Second): t.Fatal("timed out") }`. A
  test that hangs the whole suite on regression is a bad test.
- **No t.Skip on production code paths.** A test you skip is a regression you
  ship.

### Test plan in PR descriptions

The PR template's "Test plan" section is for **what the test suite covers**, not
what the human needs to do. Reference the test names, not "checked locally":

```
## Test plan

- [x] `TestSession_ApplyText` — appends message, ignores empty body
- [x] `TestBolt_SaveMessage` — round-trips message through bbolt
- [x] `TestBus_Publish` — fan-out to N subscribers, slow consumer drops
```

### Running

```bash
just test                                    # full suite (lint + format + unit + coverage)
go test -race ./...                          # all tests with race detector
go test -run TestSession_ApplyText ./internal/radio/  # one test, verbose
```

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

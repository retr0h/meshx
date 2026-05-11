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

# Headless HTTP+SSE daemon
go run . server start                      # bind 127.0.0.1:4404 (default)
go run . server start --bind :4404         # listen on all interfaces
MESHX_SERVER_BIND=:9000 go run . server start  # env-var override

# Debug logging — `--debug` (or MESHX_DEBUG=1) flips the global slog
# level so each subcommand's "running" line + the daemon's request log
# become visible. `--json` / `-j` swaps to JSON for log aggregators.
go run . --debug ble pair <uuid>
```

## Architecture

```
meshx/
├── main.go                       # tiny — forwards to cmd.Execute()
├── cmd/                          # one file per subcommand; CLI commands consume *transports.Manager (no direct transport.* / storage.* calls)
│   ├── root.go                   # cobra root + global slog logger (lmittmann/tint, JSON via -j) + viper (MESHX_ env prefix) + persistent flags
│   ├── version.go                # `meshx version` JSON build identity
│   ├── usb.go                    # `meshx usb` parent + init wiring
│   ├── usb_scan.go               # `meshx usb scan` — calls mgr.ScanUSB
│   ├── usb_connect.go            # `meshx usb connect` — mgr.AutoDetectUSB or explicit, then tui.RunRadio
│   ├── usb_probe.go              # `meshx usb probe` — deep diagnostic packet dump (legitimately reaches transport.* for wire-level work)
│   ├── ble.go                    # `meshx ble` parent + init + orDash helper
│   ├── ble_scan.go               # `meshx ble scan` — calls mgr.ScanBLE (no store needed)
│   ├── ble_pair.go               # mgr.PairBLE — triggers OS bonding + persists
│   ├── ble_list.go               # mgr.ListBLEDevices
│   ├── ble_forget.go             # mgr.ForgetBLE
│   ├── ble_fav.go                # mgr.SetBLEFavorite
│   ├── ble_disconnect.go         # mgr.ClearBLEFavorite
│   ├── ble_connect.go            # mgr.ResolveBLE → tui.RunRadio("ble:<uuid>")
│   ├── ble_probe.go              # 15s FromRadio dump (wire-level)
│   ├── server.go                 # `meshx server` parent
│   ├── server_start.go           # `meshx server start` — headless daemon (binds via viper.server.bind, default 127.0.0.1:4404)
│   ├── server_deps.go            # openStore — opens sqlite handle for the daemon (CLI uses cliTransports instead)
│   └── transports_deps.go        # shared adapters (bleScannerAdapter, blePairerAdapter, usbScannerAdapter) + newTransportsManager helper + cliTransports for one-shot CLI use
└── internal/
    ├── tui/                      # Bubble Tea rendering surface (model holds *radio.Session today)
    │   ├── app.go                # Bubble Tea model + View + Update + RunRadio entrypoint
    │   ├── ui.go                 # View dispatcher, model getters, generic utils
    │   ├── commands.go           # /command dispatcher + ham bangs
    │   ├── input.go              # key bindings, nav mode, tab wiring
    │   ├── radio.go              # apply* handlers for mdl.Text / NodeInfo / Routing / … (move to driver in MR-3.5c)
    │   ├── components_box.go     # Box / Component / Cell / Row + RawBlock / Viewport / Centered
    │   ├── components_stack.go   # VStack / HStack / Bordered / Styled
    │   ├── components_chrome.go  # statusBar / topDivider / channelTabsRow / inputBar
    │   ├── components_chat.go    # chatRow* cell builders + nick/zebra colors
    │   ├── components_notice.go  # noticeRow* cell builders
    │   ├── components_message.go # messageRow Component + notice/chat dispatch
    │   ├── components_overlays.go # overlay row builders + selection chrome
    │   ├── components_panes.go   # channels/nodes/messages/help pane Components + frameView
    │   ├── components_panes_geo.go # nearby/radar pane Components + peerPlot prep
    │   ├── components_radar.go   # radarCanvas + radar legend cells
    │   ├── components_splash.go  # BitchX rotating splash data + builder
    │   ├── notices.go            # TTL + pin + fade for `-!-` rows
    │   ├── complete.go           # Tab completion — /cmd, #chan, nicks
    │   ├── palette.go            # maxheadroom color constants
    │   ├── node.go               # nodeItem + state derivation
    │   ├── geo.go                # haversine / bearing / compass math
    │   ├── help.go               # /help entry data
    │   └── qr.go                 # ASCII QR rendering for /channel share
    ├── radio/                    # headless per-radio session layer — owns canonical State, wraps Pump + Store
    │   ├── session.go            # *radio.Session + New + Send + Stop
    │   ├── state.go              # *radio.State — per-radio runtime: Channels/Nodes/Messages, indices, pending requests
    │   ├── apply.go              # Apply* handlers: Text / NodeInfo / Routing / Position / …
    │   ├── subscribe.go          # Event + Subscribe + SubscribeWithReplay + ring buffer (per-Session replay log)
    │   ├── hydrate.go            # HydrateFromStore — replay persisted history at boot
    │   ├── pump.go               # consumer interface (Pump) for internal/meshx/pump
    │   └── store.go              # consumer interface (Store) for internal/meshx/storage
    ├── transports/               # hardware-management surface — single source of truth for BLE/USB ops
    │   ├── manager.go            # *Manager — Config{Store, Scanner, Pairer, USBScanner} + New
    │   ├── types.go              # consumer interfaces (Store, BLEScanner, BLEPairer, USBScanner) + wire types (BLEDeviceView, BLESighting, USBSighting)
    │   ├── ble.go                # List / Scan / Pair / Forget / Fav / Clear / Resolve / ResolveAutoConnect
    │   └── usb.go                # Scan / AutoDetect
    ├── server/                   # HTTP+SSE daemon (Huma) — thin adapters over radio/transports
    │   ├── server.go             # *Server + Config{Radios, Transports, Logger, AuthToken}
    │   ├── registry.go           # radio_id → Driver multiplex
    │   ├── middleware.go         # request-id / request-log / panic recovery
    │   ├── session.go            # consumer interface (Driver — what the server needs from *radio.Session)
    │   ├── routes.go             # huma.Register calls
    │   ├── handlers.go           # per-route handlers
    │   ├── transport_ble.go      # /transports/ble/* HTTP routes — thin wrappers over s.transports.X
    │   └── transport_usb.go      # /transports/usb/{scan,auto} HTTP routes — same pattern
    ├── sdk/
    │   └── gen/                  # generated Go HTTP client (api.yaml + cfg.yaml + generate.go + client.gen.go)
    ├── version/                  # build identity (Version / Commit / Date / BuiltBy)
    └── meshx/                    # foundational sub-packages — model / pump / storage / transport
        ├── model/                # canonical wire/persisted shapes — the lingua franca
        │   ├── message.go        # Message + MessageStatus enum (JSON-tagged for HTTP API)
        │   ├── items.go          # ChannelItem + NodeItem + MessageItem
        │   ├── node.go           # CachedNode (NodeDB cache row)
        │   ├── ble.go            # BLEDevice (BLE pairing row)
        │   ├── events.go         # pump-emitted events: Text, NodeInfo, Position, Ping, Routing, …
        │   ├── commands.go       # consumer-issued commands: SendText, SetOwner, SetBuzzer, RequestSync, …
        │   ├── config.go         # modeled radio configs
        │   └── enums.go          # Region, ModemPreset, DeviceRole, ChannelRole, RoutingError, NodeState
        ├── pump/                 # transport ↔ tea bridge (concrete *pump.Pump)
        │   ├── pump.go           # New / Stop + run loop with reconnect policy
        │   ├── transport.go      # consumer interface (Transport)
        │   ├── translate.go      # FromRadio → []model.X
        │   ├── outbound.go       # (*Pump).Send(model.Command)
        │   ├── channel_url.go    # Parse/Build meshtastic:// share URLs
        │   └── config.go         # ExternalNotificationFromProto / ToProto bridges
        ├── storage/              # SQLite persistence (concrete *storage.Sqlite)
        │   ├── sqlite.go         # CRUD against model.Message / CachedNode / BLEDevice
        │   └── migrations/       # embedded goose SQL migrations
        └── transport/
            ├── client.go         # Client interface + Dial dispatcher
            ├── framing.go        # 0x94 0xc3 <hi> <lo> <proto> frame codec
            ├── stream.go         # Shared framed-stream runner (serial/tcp)
            ├── serial.go         # USB-serial transport
            ├── tcp.go            # TCP transport (meshtasticd / WiFi)
            ├── ble.go            # Bluetooth LE dial / connect
            ├── ble_scan.go       # ScanBLE + PairBLE + BLESighting (shared by cmd-direct + daemon adapters)
            └── identify.go       # AutoDetectMeshtastic + IdentifyAllSerial USB probes
```

### Public API

`internal/` packages are not re-exported. The cmd tree consumes them directly:

```go
tui.RunRadio("/dev/cu.usbmodem2101")         // live — serial or "ble:<uuid>"
transport.ScanBLE(timeout)                   // BLE discovery
transport.PairBLE(uuid)                      // OS-level bonding
transport.IdentifyAllSerial(timeout)         // USB scan + handshake probe
transport.AutoDetectMeshtastic(timeout)      // single-Meshtastic-port helper
storage.New(path) → *Sqlite                  // SQLite handle (BLE devices, messages, …)
server.New(server.Config{...}) → *Server     // HTTP+SSE daemon
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
  S["**storage**<br/>CRUD · *Sqlite"]
  H["**server**<br/>HTTP+SSE"]
  T["**TUI Update**<br/>case mdl.Text / NodeInfo / Position / …"]
  P --> M
  S --> M
  H --> M
  M --> T
```

Inbound, `pump/translate.go` projects `*pb.FromRadio` → `model.X` events.
Outbound, `pump/outbound.go::Send(model.Command)` is a type-switch dispatcher
that builds the matching `*pb.ToRadio` envelope. The pump package is the
**only** place in the codebase where `gomeshproto` types meet `model` types in
either direction. Everywhere else — the meshx TUI, the storage layer, future
daemon — sees only `mdl.X`. The proto<->model bridges for full-record configs
that need round-trip preservation (today: `ExternalNotification`) live in
`pump/config.go`; `commands.go` calls those bridges when crafting outbound
`AdminMessage` envelopes so it never directly assembles a config proto.

### Consumer interfaces (osapi-io pattern)

Both `pump.Pump` and `storage.Sqlite` are concrete structs in their own
packages. Where they're consumed (the meshx TUI), the consumer declares a narrow
interface listing only the methods it uses:

- `internal/meshx/pump.go` — `Pump` interface (`Enqueue`, `Stop`)
- `internal/meshx/store.go` — `Store` interface (the 17 methods the TUI calls)

Both interfaces sit next to each other so future consumers (e.g. a daemon
package) can declare their own — likely larger — interfaces without bloating the
TUI's view of the contract. The compile-time binding
`var p Pump = pump.New(...)` at the construction site catches drift the moment a
method gets renamed.

The same pattern applies in `cmd/`: `cmd/ble_deps.go` declares `bleScanner` /
`blePairer` / `bleStore` interfaces and `cmd/usb_deps.go` declares `usbScanner`.
Production wiring sits at the bottom of each file (`cliBLEScanner`,
`cliBLEPairer`, `cliOpenBLEStore`, `cliUSBScanner`) and tests can swap the
package-level vars to fake the host. The transport adapters
(`transportBLEScanner`, etc.) all delegate into `internal/meshx/transport`,
which means **`meshx ble scan`, `meshx usb scan`, etc. don't need a daemon to be
running** — they're direct OS interrogations.

## Deployment modes

Three modes share one binary:

1. **Local** — `meshx ble connect <name>` / `meshx usb connect` runs radio + TUI
   in one process.
2. **Headless** — `meshx server start` owns the radio behind HTTP+SSE; no TUI.
3. **Remote** (planned) — `meshx ble connect --server http://host:4404 <id>`
   runs the TUI against a remote daemon.

The dual-mode seam is `internal/tui/session.go::radioSession`. `*radio.Session`
satisfies it for local mode; `*sdk.RemoteDriver` (planned) satisfies it over
HTTP+SSE — it holds a `*gen.Client` for outbound calls and consumes
`/radios/{id}/events` to project events onto a local `*radio.State`. The TUI's
Update path doesn't branch on mode.

Remote mode has two independent reconnect loops: radio↔daemon (pump backoff on
the daemon side) and TUI↔daemon (SSE re-subscribe + snapshot re-fetch on
network blips). The daemon keeps the radio session alive across TUI restarts.

## Daemon, logging, config

`meshx server start` runs the HTTP+SSE daemon. Default bind is `127.0.0.1:4404`
(chosen to sit adjacent to meshtasticd's `4403` — "4403 talks to the radio, 4404
talks to clients of meshx"). The bind address resolves through viper: `--bind`
flag > `MESHX_SERVER_BIND` env > default. `MESHX_SERVER_RADIO` plus the
`--radio <dest>` flag pre-register a pending radio at startup.

Logging is a single package-level `slog.Logger` set up in
`cmd/root.go::initLogger` via `cobra.OnInitialize`. The default handler is
`lmittmann/tint` (colored when stderr is a TTY, plain otherwise); `--json` /
`-j` swaps in `slog.NewJSONHandler` for log aggregators; `--debug` / `-d` flips
the level. Subcommands tag their child logger with `subsystem=<verb>.<action>`
and emit a `Debug("running", …)` line at the top of each `RunE` so debugging
shows the parsed inputs without polluting default UX. The daemon emits `Info`
"config" + "listening" lines at boot and a structured request log line per HTTP
request.

`internal/server/middleware.go` wires three Huma middlewares (outermost-first):
panic recovery (logs stack + 500), request-id (honors inbound `X-Request-ID` or
generates 8-byte hex; echoes header, stashes on context, retrievable via
`server.RequestIDFromContext`), and a structured request log (method, path,
status, duration, request_id, remote, user-agent — Error level for 5xx, Warn for
4xx, Info otherwise).

## Server architecture

A request flows: Huma router → middleware stack (panic / request-id / log) →
`internal/server/handlers.go` → `resolveRadio({radio_id})` → `Registry.Get(id)`
→ `Driver` (the consumer interface in `internal/server/session.go`, satisfied by
`*radio.Session`). Handlers project model types (`mdl.ChannelItem`,
`mdl.NodeItem`, `mdl.MessageItem`) directly into responses — no DTO duplication,
the JSON shape on the wire IS the model shape. Multi-radio is the `Registry`
multiplex (`radio_id → Driver`, RWMutex-guarded); routes are radio-scoped under
`/radios/{radio_id}/...` and transport admin under `/transports/{ble,usb}/...`.
The SSE stream (`/radios/{id}/events`) sits on `Driver.Subscribe(ctx)` and
dispatches per-event-kind via `eventsTypeMap` so each variant gets the right
`event:` line on the wire.

## OpenAPI client SDK

The daemon emits its OpenAPI spec at `/openapi.{json,yaml}` (3.1) and a
downgraded version at `/openapi-3.0.{json,yaml}`. oapi-codegen still can't
consume 3.1 (oapi-codegen #373) so the codegen pipeline pulls the 3.0 spec.

Two-stage regen, both invoked by `just generate` — no daemon required:

1. `internal/sdk/gen/main.go` (build-tagged `ignore`, same pattern as
   `internal/tui/emoji/main.go`) imports `internal/server`, calls
   `Server.OpenAPISpec()` to pull the spec straight from Huma in-process, writes
   `api.yaml`.
2. `oapi-codegen` runs against `api.yaml` + `cfg.yaml` to produce
   `client.gen.go`.

```bash
just generate                # runs both stages via `go generate ./...`
```

Drift is enforced by `TestAPISpecMatchesVendoredCopy` (in
`internal/server/spec_test.go`), which fails `just test` if the on-disk
`api.yaml` doesn't match what the in-process daemon would emit. Breaking changes
are caught in CI by `oasdiff` between the PR branch's `api.yaml` and main's —
see `.github/workflows/go.yml`.

`internal/sdk/gen/cfg.yaml` configures oapi-codegen
(`generate.models: true, client: true`); `client.gen.go` is checked in so
consumers can build without invoking codegen.

**Schema-name caveat**: oapi-codegen auto-generates a `<OpId>Response` struct
per operation as the HTTP response wrapper. Avoid `*Response` schema names in
`internal/server/handlers.go` — that's why the send-message body is
`SendMessageResult`, not `SendMessageResponse`.

`internal/sdk/` is the consumer-facing surface. `gen/` is the generated typed
HTTP client for every route Huma registers. `remote.go` (planned) is the
hand-written companion that wraps `gen.Client` plus an SSE consumer behind the
`tui.radioSession` interface — so `meshx ble connect --server <url>` runs the
TUI against a remote daemon with no branching in the model code. SSE isn't
generated by oapi-codegen, so the event reader is hand-rolled against the
`/radios/{id}/events` stream.

## Dependencies

| Package                         | Purpose                                        |
| ------------------------------- | ---------------------------------------------- |
| `charmbracelet/bubbletea`       | Elm-style TUI framework                        |
| `charmbracelet/bubbles`         | textinput widget for input + search prompts    |
| `charmbracelet/lipgloss`        | colors, borders, layout primitives             |
| `spf13/cobra`                   | CLI command tree                               |
| `spf13/viper`                   | flag/env/default config resolution             |
| `lmittmann/tint`                | colored slog handler                           |
| `lmatte7/gomesh/...gomeshproto` | Meshtastic protobuf definitions                |
| `go.bug.st/serial`              | cross-platform USB-serial                      |
| `tinygo.org/x/bluetooth`        | cross-platform Bluetooth LE (macOS / Linux)    |
| `google.golang.org/protobuf`    | proto marshal / unmarshal                      |
| `mattn/go-sqlite3`              | SQLite driver (CGo) for scrollback persistence |
| `pressly/goose`                 | embedded SQL migrations                        |
| `danielgtaylor/huma/v2`         | HTTP+OpenAPI framework for the daemon          |
| `oapi-codegen/oapi-codegen/v2`  | Go client codegen from the OpenAPI spec (tool) |

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

## Persistence — SQLite scrollback

Live-radio mode opens `~/.meshx/meshx.db` (WAL journal, `_busy_timeout=5000`)
via the `internal/meshx/storage` package and replays the last 500 messages on
boot. The TUI consumes a narrow `Store` interface (defined in `store.go`); the
concrete `*storage.Sqlite` implements it. The schema is one flat `messages`
table mirroring `mdl.Message` (the wire/persistence shape that `MessageItem`
embeds) plus a `channel` column. System / flash rows are skipped on save. Write
errors are logged-then-swallowed; losing history beats crashing the UI.

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
tested (real-radio integration, browser-only EventSource semantics, hardware
reset), call it out explicitly — don't bury it.

### The shape rule (non-negotiable)

**One test function per public surface. Scenarios are rows in a single table.**
Don't write `TestFooHappyPath`, `TestFooMissingField`, `TestFooNotFound` as
three separate functions — those are three rows of `TestFoo`. Ad-hoc one-off
test functions sprawled across files are forbidden; consistency is the goal.

A "public surface" is:

- An HTTP route (one `TestEndpoint<OperationID>` per route registered in
  `routes.go`).
- An exported type's public method (one `Test<Type><Method>` per method).
- A meaningful behavior on a public function (one
  `Test<FunctionDescribingBehavior>` per behavior).

Scenarios go in a single `[]struct{name, ...}` table when their mechanics are
uniform — same setup, same act/assert shape, different inputs and expectations.
When scenarios genuinely diverge in mechanics (one tests cancellation, another
tests delivery), use `t.Run("scenario-name", func(t *testing.T) { ... })`
sub-tests under the same parent. **The unifying principle: one parent function
per public surface — never multiple top-level `TestXHappyPath` / `TestXFailure`
functions.**

```go
func TestEndpointSendMessage(t *testing.T) {
    t.Parallel()

    cases := []struct {
        name       string
        body       any
        wantStatus int
        wantBody   func(t *testing.T, body []byte)
    }{
        {
            name:       "happy-path-broadcast",
            body:       SendMessageRequest{Channel: 0, Text: "hi"},
            wantStatus: http.StatusOK,
            wantBody: func(t *testing.T, body []byte) {
                var got SendMessageResult
                require.NoError(t, json.Unmarshal(body, &got))
                require.NotZero(t, got.PacketID)
                require.True(t, got.OK)
            },
        },
        {
            name:       "happy-path-dm-with-to_num",
            // …
        },
        {
            name:       "rejects-empty-text-422",
            body:       SendMessageRequest{Channel: 0, Text: ""},
            wantStatus: http.StatusUnprocessableEntity,
        },
        {
            name:       "rejects-unknown-radio-404",
            // …
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

Test files for code in `handlers.go` that grew enough to split (e.g., separate
tests for send-message vs list-messages logic) is a signal that `handlers.go`
itself should be split into `handlers_send_message.go` + `handlers_messages.go`
first — never invent a `_test.go` file that doesn't pair with production.

### Tooling

- **`testing` + `testify/require`** for unit tests. Standard library first;
  reach for `testify` only when an assertion would otherwise need a long custom
  message.
- **`net/http/httptest`** for every HTTP and SSE endpoint —
  `httptest.NewServer(s.http.Handler)` against the same `*Server` production
  uses. No fake handlers, no parallel mock router.
- **In-process `*radio.Session`** (constructed via `radio.New(nil, nil, nil)`)
  for testing the apply / publish / subscribe paths without a real radio. Inject
  events via `Session.Publish`; assert on `Subscribe` / `SubscribeWithReplay`
  output. For radio-dispatch verification (commands reaching the pump), satisfy
  `radio.Pump` with a fake that captures dispatched commands — see `fakePump` in
  `internal/server/handlers_radio_ops_test.go` for the canonical shape.
- **Race detector** — `go test -race ./...` for anything with goroutines
  (Subscribe, Pump, the SSE handler). Cheapest way to catch a slipped lock.

### HTTP-specific rules

- **100% endpoint coverage.** Every route registered in
  `internal/server/routes.go` has a dedicated `TestEndpoint*` test function with
  happy-path + every distinct failure mode as table rows. If a route isn't
  tested, the PR isn't done.
- **Verify the wire shape on happy paths.** Every happy-path row decodes the
  response body into the exported response type (`mdl.MessageItem`,
  `SendMessageResult`, etc.) and asserts on key fields. Catches "we accidentally
  renamed a JSON field" before the SDK regen does.
- **Cover failure modes explicitly:** missing-required-field (422),
  unknown-radio (404), bad path-param (400/422), buffer-full / pump-down (503),
  validation rejects (422). One row per distinct failure.
- **Test naming.** `TestEndpointListMessages`, `TestEndpointSendMessage`,
  `TestEndpointMintChannel` — operation-id-shaped, predictable from the route
  registration.

### Other rules

- **Test public-facing surfaces** — the HTTP route, the exported type, the wire
  shape. Internal-only helpers can be tested incidentally through their public
  callers; don't write a parallel test for every unexported function.
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

- [x] `TestEndpointSendMessage` — happy paths (broadcast, DM via to_num, reply via reply_id) + failure rows (empty text, unknown radio, idempotency dedupe)
- [x] `TestEndpointListMessages` — empty state, with limit, with ?dm=mine filter, unknown radio
- [x] `TestSessionPublish` — monotonic IDs, fan-out to N subs, ring overflow keeps newest, cursor replay
```

### Running

```bash
just test                                    # full suite (lint + format + unit + coverage)
go test -race ./...                          # all tests with race detector
go test -run TestEndpointSendMessage ./internal/server/  # one test, verbose
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

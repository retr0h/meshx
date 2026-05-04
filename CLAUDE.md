# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when
working with code in this repository.

## Project Overview

**meshX** is a glitched-out terminal Meshtastic messenger — an
irssi-style chat client for LoRa radios with a vintage BBS aesthetic.
It connects to a Meshtastic-compatible device over USB serial, TCP
(`meshtasticd` / WiFi radio on port 4403), or Bluetooth LE, subscribes
to the mesh, and surfaces everything in a dense Bubble Tea TUI with
mutt-grade keyboard, BitchX-style splash, and a maxheadroom palette.

All three transports share one `Client` interface (`internal/meshx/
transport/client.go`) and funnel through the same pump → tea.Msg →
model path, so the renderer never branches on transport type. Every
telemetry field in the model (`lastSNR`, `lastRSSI`, `lastHops`,
`hwModel`, `firmware`) maps 1:1 to Meshtastic protobuf fields.

## Inspiration

Three lineages converge:

- **[irssi](https://irssi.org/)** — input-first modal UI, `/command`
  dispatcher, stable bottom status line, `Alt+n` channel hop.
- **[BitchX](http://bitchx.sourceforge.net/)** — rotating graffiti
  ASCII splash (different every launch), bracketed `[ @nick ]` users
  grid, unapologetic neon aesthetic.
- **[mutt](http://www.mutt.org/)** — dense one-row-per-message log,
  `j/k` scrollback nav, `r` reply on selection, modal input ↔ nav
  distinction.

Plus vim (every window scrolls with `j/k/h/l`, `Ctrl+W`, `/`, `n/N`)
and tmux (`Ctrl+N / Ctrl+P` cycle, giant flash-digit pane picker).

Sibling projects **grind** and **tlock** share the maxheadroom palette
and `░▒▓█` block-border language.

## Architecture

```
meshx/
├── main.go                       # 7-line entry → cmd.Execute()
├── cmd/                          # one file per subcommand; *_deps.go declares the cmd-local narrow consumer interfaces
│   ├── root.go                   # cobra root + global slog logger (lmittmann/tint, JSON via -j) + viper (MESHX_ env prefix) + persistent flags
│   ├── version.go                # `meshx version` — JSON build-identity dump
│   ├── usb.go                    # `meshx usb` parent + init wiring
│   ├── usb_scan.go               # `meshx usb scan` — cliUSBScanner.Identify
│   ├── usb_connect.go            # `meshx usb connect` — auto-detect or explicit path, then tui.RunRadio
│   ├── usb_probe.go              # `meshx usb probe` — deep-dump diagnostic (--port for full packet stream)
│   ├── usb_deps.go               # narrow usbScanner consumer interface + transportUSBScanner adapter wired as cliUSBScanner
│   ├── ble.go                    # `meshx ble` parent + init wiring + orDash helper
│   ├── ble_scan.go               # `meshx ble scan` — cliBLEScanner.Scan
│   ├── ble_pair.go               # `meshx ble pair` — cliBLEPairer.Pair + cliOpenBLEStore.SaveBLEDevice
│   ├── ble_list.go               # `meshx ble list` — cliOpenBLEStore.LoadBLEDevices
│   ├── ble_forget.go             # `meshx ble forget` — LookupBLEDevice + ForgetBLEDevice
│   ├── ble_connect.go            # `meshx ble connect` — resolve via storage, then tui.RunRadio("ble:<uuid>")
│   ├── ble_disconnect.go         # `meshx ble disconnect` — clear favorite
│   ├── ble_fav.go                # `meshx ble fav` — set favorite
│   ├── ble_probe.go              # `meshx ble probe` — 15s diagnostic FromRadio dump
│   ├── ble_deps.go               # narrow bleScanner / blePairer / bleStore consumer interfaces + transportBLEScanner / transportBLEPairer adapters + cliOpenBLEStore (sqlite.New)
│   ├── server.go                 # `meshx server` parent command
│   ├── server_start.go           # `meshx server start` — headless HTTP+SSE daemon (declares daemonRunner; binds via viper server.bind default 127.0.0.1:4404)
│   └── server_deps.go            # daemon-only adapters (daemonBLEScanner / daemonBLEPairer / daemonUSBScanner / openStore) wiring server.Config; every adapter delegates into internal/meshx/transport
├── internal/driver/              # headless radio session layer — owns canonical State, wraps Pump + Store
│   ├── driver.go                 # *driver.Driver type + New(state, pump, store) + Send / Stop / Session
│   ├── state.go                  # *driver.State — per-radio runtime: Channels/Nodes/Messages, indices, pending requests, reconnect banner
│   ├── pump.go                   # consumer interface (Pump) for internal/meshx/pump
│   └── store.go                  # consumer interface (Store) for internal/meshx/storage
├── internal/server/              # HTTP+SSE daemon (Huma framework) — middleman between driver + clients; multi-radio aware via Registry
│   ├── server.go                 # *server.Server type + New(Config) + Run(ctx, addr) + Drivers(); slog-tagged with subsystem=http
│   ├── registry.go               # *server.Registry — radio_id → Driver multiplex, mutex-guarded for concurrent HTTP handlers
│   ├── middleware.go             # request-id (echoed as X-Request-ID) + structured request log (status-aware level) + panic recovery; wired via api.UseMiddleware
│   ├── driver.go                 # consumer interface (Driver) at the seam — concrete *driver.Driver satisfies via Session() + Send()
│   ├── store.go                  # Store / BLEScanner / BLEPairer / USBScanner consumer interfaces (osapi-io seam) + BLESighting + USBSighting wire shapes
│   ├── routes.go                 # huma.Register calls — radio-scoped under /radios/{radio_id}/..., transports under /transports/{ble,usb}/...
│   ├── handlers.go               # per-route handlers; channels/nodes/messages emit model types directly (single source of truth, no DTO duplication)
│   ├── transport_ble.go          # /transports/ble/* HTTP routes (scan, pair, list, forget, fav, clear-favorite) for remote admin
│   └── transport_usb.go          # /transports/usb/{scan,auto} HTTP routes for remote admin
├── internal/sdk/                 # generated Go HTTP client for the daemon's API
│   └── gen/                      # api.yaml (curl /openapi-3.0.yaml from a running daemon) + cfg.yaml + generate.go + client.gen.go (oapi-codegen output)
├── internal/tui/                 # Bubble Tea rendering surface (model dispatches apply* directly today)
│   #                             # GAP: TUI consumes *driver.Driver concretely (m.driver.Pump.Send, m.driver.Store.SaveMessage,
│   #                             # m.driver.Pump = p). Should declare a narrow consumer interface per osapi-io once apply*
│   #                             # handlers + outbound dispatch land as methods on driver.Driver — then TUI calls go through
│   #                             # the interface (Send / SaveMessage / Subscribe) and a remote-driver-over-HTTP variant can
│   #                             # satisfy the same seam for the (α) "TUI-as-HTTP-client" mode.
│   ├── app.go                    # model + View() + Update wiring + RunRadio (model holds *driver.Driver)
│   ├── ui.go                     # View dispatcher, model getters, generic utils
│   ├── commands.go               # /command dispatcher + ham bangs
│   ├── input.go                  # key bindings, nav mode, tab completion entry
│   ├── components_box.go         # Box, Component, Cell/Row, Text, Spacer, RawBlock, Viewport, Centered
│   ├── components_stack.go       # VStack, HStack, Bordered, Styled
│   ├── components_chrome.go      # statusBar / topDivider / channelTabsRow / inputBar
│   ├── components_chat.go        # chatRowFor + per-cell chat row builders + nick/zebra colors
│   ├── components_notice.go      # noticeRowFor + noticeRowLine[Split] notice row builders
│   ├── components_message.go     # messageRow Component + noticeRowRender + chatRowRender dispatch
│   ├── components_overlays.go    # overlay-row helpers + selection chrome (wrapSelection, dimRow)
│   ├── components_panes.go       # channelsPane / nodesPane / messagesPane / helpPane Components + frameView
│   ├── components_panes_geo.go   # nearbyPane / radarPane Components + peerPlot data prep
│   ├── components_radar.go       # radarCanvas + radarLegendCell + radarPeerLine
│   ├── components_splash.go      # BitchX-style rotating graffiti banner data + builder
│   ├── notices.go                # TTL + pin + fade for `-!-` rows
│   ├── complete.go               # Tab completion — /cmd, #chan, nicks
│   ├── palette.go                # maxheadroom color constants
│   ├── node.go                   # nodeItem + state derivation
│   ├── radio.go                  # apply* handlers (mdl.Text, mdl.NodeInfo, mdl.Routing, …) — moves to Driver pkg in MR-3.5b
│   ├── geo.go                    # haversineKm / bearingDeg / compassAbbr math
│   ├── help.go                   # /help entry data
│   └── qr.go                     # ASCII QR rendering for /channel share
├── internal/version/             # build identity (Version / Commit / Date / BuiltBy + BuildInfo)
│   └── version.go                # consumed by cmd/version.go and tui /version slash command
├── internal/meshx/               # sub-packages — see model/, pump/, storage/, transport/
│   ├── model/                    # canonical wire/persisted shapes — the lingua franca
│   │   ├── message.go            # Message + MessageStatus enum (JSON-tagged for HTTP API)
│   │   ├── items.go              # ChannelItem + NodeItem + MessageItem — the API + storage canonical row types
│   │   ├── node.go               # CachedNode (NodeDB cache row)
│   │   ├── ble.go                # BLEDevice (BLE pairing row)
│   │   ├── events.go             # pump-emitted events: Text, NodeInfo, Position, Ping, Routing, …
│   │   ├── commands.go           # consumer-issued outbound: SendText, SetOwner, SetBuzzer, RequestSync, …
│   │   ├── config.go             # modeled radio configs (ExternalNotification today; Owner / LoRa / Device next)
│   │   └── enums.go              # Region, ModemPreset, DeviceRole, ChannelRole, RoutingError, NodeState
│   ├── pump/                     # transport ↔ tea bridge (concrete *pump.Pump)
│   │   ├── pump.go               # New / Stop + run loop with reconnect policy
│   │   ├── transport.go          # consumer interface (Transport) for the transport package — twin of meshx/pump.go and meshx/store.go
│   │   ├── translate.go          # FromRadio → []model.X (proto→model inbound boundary)
│   │   ├── outbound.go           # (*Pump).Send(model.Command) + envelope builders (model→proto outbound)
│   │   ├── channel_url.go        # ParseChannelShareURL / BuildChannelShareURL (model.ChannelInfo ↔ meshtastic://)
│   │   └── config.go             # ExternalNotificationFromProto/ToProto bridges (grows with config writes)
│   ├── storage/                  # SQLite persistence (concrete *storage.Sqlite)
│   │   ├── sqlite.go             # CRUD against model.Message / model.CachedNode / model.BLEDevice
│   │   └── migrations/           # goose SQL migrations (001…010)
│   └── transport/
│       ├── client.go             # Client interface + Dial dispatcher (cmd/probe still uses Client; pump consumes via its own pump.Transport)
│       ├── serial.go             # USB-serial transport
│       ├── tcp.go                # TCP transport (port 4403)
│       ├── ble.go                # Bluetooth LE transport
│       ├── stream.go             # Shared framed-stream runner (serial + tcp)
│       ├── framing.go            # START1/START2/LENGTH frame codec
│       └── identify.go           # AutoDetectMeshtastic USB probe
└── docs/
    ├── commands.md               # every keybinding and /command
    ├── development.md            # setup, testing, conventions
    └── contributing.md           # PR workflow
```

### Public API

- `meshx.RunRadio(dest)` — launch the TUI against a serial device path
  (`/dev/cu.usbserial-…`), a TCP endpoint (`host:port`), or a Bluetooth
  peripheral via the `ble:<uuid>` prefix. `transport.Dial` routes on
  the prefix.
- `meshx.RunBLE(uuidOrName)` — resolve a saved BLE device from
  `ble_devices` (accepting uuid, longname, or shortname) and hand off
  to `RunRadio("ble:<uuid>")`.
- `meshx.AutoConnectTarget()` — bare-`meshx` resolution chain (USB →
  single saved BLE → favorite BLE → error with hint).
- `meshx.BLEScan` / `BLEPair` / `BLEListDevices` / `BLEForget` /
  `BLEMarkFavorite` / `BLESetFavorite` — CLI entrypoints for the
  `meshx ble` subcommand tree.

Internals beyond those are not re-exported; the package is consumed
only by the `cmd/` tree.

### Dependencies

- `charmbracelet/bubbletea` — Elm-style TUI framework
- `charmbracelet/bubbles` — textinput for input + search prompts
- `charmbracelet/lipgloss` — colors, borders, layout primitives
- `spf13/cobra` — CLI framework
- `go.bug.st/serial` — USB-serial transport
- `tinygo.org/x/bluetooth` — Bluetooth LE transport (CoreBluetooth on
  macOS, BlueZ on Linux)
- `pressly/goose` — embedded sqlite migrations
- `lmatte7/gomesh/gomeshproto` — Meshtastic protobuf bindings
- `danielgtaylor/huma/v2` — HTTP framework with auto-generated OpenAPI 3.1 spec
- `oapi-codegen/oapi-codegen/v2` (tool) — Go client codegen from the spec
- `lmittmann/tint` — colored slog handler for the global logger
- `spf13/viper` — config + env var binding

## Deployment modes

Three modes from one binary:

1. **Local** (default) — `meshx ble connect <name>` runs radio + TUI in one process.
2. **Headless** — `meshx server start` owns the radio over HTTP+SSE; no TUI.
3. **Remote** (planned) — `meshx ble connect --server http://host:4404 <id>` runs TUI against a remote daemon.

The seam is `internal/tui/driver.go::radioDriver`. `*driver.Driver` satisfies it locally; `*sdk.RemoteDriver` (planned) satisfies it over HTTP+SSE by projecting events onto a local `*driver.State` via the same apply path. The TUI doesn't know which it's holding.

Remote mode has two independent reconnect loops: radio↔daemon (pump backoff 1s→30s) and TUI↔daemon (SSE client re-fetches snapshot + re-subscribes on network blips). Kill the TUI for 20 minutes — daemon keeps retrying the radio and persisting traffic; relaunching reflects the full gap.

## Daemon, logging, config

Bare `meshx` does **not** auto-connect — it prints help. Pick a transport
explicitly: `meshx usb connect`, `meshx ble connect <name>`, or
`meshx server start` for the headless daemon.

CLI logging goes through a single package-level `logger *slog.Logger`
configured in `cmd/root.go` via `cobra.OnInitialize(initConfig, initLogger)`.
Default handler is `tint.NewHandler` (colored when stderr is a TTY, plain
otherwise). `--json` / `-j` swaps in `slog.NewJSONHandler` for log
aggregators. `--debug` / `-d` flips the level. Subcommands tag their child
logger with `subsystem=<verb>.<action>` and emit a `Debug("running", …)`
line at the top of each `RunE` so `MESHX_DEBUG=1 meshx ble pair <uuid>`
shows the parsed inputs without polluting the default UX.

`viper` is wired with the `MESHX_` env prefix and dot→underscore replacer.
The daemon's bind address is `viper.serve.bind` — the precedence is
`--bind` flag > `MESHX_SERVER_BIND` env > default `127.0.0.1:4404` (4404
sits adjacent to meshtasticd's 4403 — "4403 talks to the radio, 4404
talks to clients of meshx").

The HTTP server (`internal/server/middleware.go`) wraps every request
in three middlewares — outermost first: panic recovery (logs the stack +
500s), request-id (honors inbound `X-Request-ID` or generates 8-byte hex,
echoes header, stashes on context, retrievable via
`server.RequestIDFromContext`), and a structured request log (method,
path, status, duration, request_id, remote, user-agent — Error level for
5xx, Warn for 4xx, Info otherwise).

## CLI vs HTTP for transport ops

Scan / pair / list / forget / fav are **CLI-local OS interrogations** —
they don't need a daemon. `cmd/ble_*.go` and `cmd/usb_*.go` declare narrow
consumer interfaces (`bleScanner`, `blePairer`, `bleStore`, `usbScanner`)
in their `*_deps.go` files, with adapters that delegate to
`internal/meshx/transport` and `internal/meshx/storage`. Tests can swap
the package-level vars (`cliBLEScanner`, `cliBLEPairer`, `cliOpenBLEStore`,
`cliUSBScanner`) to fake the host.

The daemon's `/transports/{ble,usb}/*` HTTP routes exist for **remote
admin** (a future web UI inspecting Bluetooth / USB state on a headless
box). They share the same `internal/meshx/transport` primitives behind
the daemon's own `server.BLEScanner` / `BLEPairer` / `USBScanner`
interfaces — adapters in `cmd/server_deps.go` (`daemonBLEScanner`,
`daemonBLEPairer`, `daemonUSBScanner`) lift `transport.*` outputs into
the server's wire shapes.

`internal/meshx/transport/ble_scan.go` is the single source of truth for
the tinygo-bluetooth scan + pair primitives — both cmd-direct and
daemon-side callers consume `transport.ScanBLE` / `transport.PairBLE`.

## OpenAPI client SDK

`internal/sdk/gen/` houses the generated Go HTTP client. The pipeline:

1. Edits to handlers/routes change the spec Huma emits.
2. Spawn the daemon and pull `/openapi-3.0.yaml` (Huma's downgraded 3.0
   spec — oapi-codegen still can't consume 3.1; see oapi-codegen #373):
   ```bash
   meshx server start --bind :19199 &
   curl -sS localhost:19199/openapi-3.0.yaml > internal/sdk/gen/api.yaml
   ```
3. `cd internal/sdk/gen && go generate .` runs oapi-codegen against
   `api.yaml` + `cfg.yaml`, producing `client.gen.go`.
4. `just generate` chains the codegen step; `just ready` runs it before
   fmt + lint.

**Schema-name caveat**: oapi-codegen auto-generates a `<OpId>Response`
struct per operation as the HTTP response wrapper. A schema named
`SendMessageResponse` would collide — that's why our send-message body
type is `SendMessageResult`. Avoid `*Response` schema names in
`internal/server/handlers.go` going forward.

## Modal UI model

Four modes plus splash:

- **`modeSplash`** — BitchX-style banner at launch, 3s auto-dismiss or any key.
- **`modeInput`** (default) — input bar focused; typing composes or runs `/command`.
- **`modeNav`** — selection cursor in the scrollback / overlay; `j/k/h/l`, `r/t/p/w/*/m`, `/` search.
- **`modeSearch`** — live-filter prompt; Enter commits, ESC cancels.
- **`modeHelp`** — scrollable `?` overlay; `j/k/g/G/d/u` navigate, ESC closes.

ESC always returns to the input bar (the canonical "where I type"
state). Ctrl+X always quits.

## Layout primitives (component tree)

Rendering is a strict component tree, not ad-hoc string concatenation:
every region of the UI implements `Component.Render(box Box) string`
and MUST return precisely `box.Height` lines, each precisely
`box.Width` cells per `ansiCells` (the same measurement the terminal
uses, with VS16/keycap promotion to 2 cells per Unicode TR51 so
"7️⃣"-bodied rows don't drift the right `║` frame out of column).

- `components_box.go` — `Box`, `Component`, `Cell`/`Row`, `Text`,
  `Spacer`, `RawBlock` (wrap a pre-rendered string into a Box),
  `Viewport` (scrollable window with optional footer; powers
  `helpPane`), `Centered` (pane-aware h/v centering), plus
  `padCells` / `ansiCells` / `renderCell`. `padCells` funnels through
  `ansi.Truncate` for ANSI-aware grapheme-aware truncation, so styled
  prefixes survive when content overflows.
- `components_stack.go` — `VStack` / `HStack` distribute a parent box
  across `SizedChild` slots with flex (-1) support; `Bordered` wraps
  an inner Component in a `╔═══╗` / `┌───┐` frame, subtracting border
  + padding from the inner box. `Styled` is the post-composition
  style wrapper.
- `components_chrome.go` — `statusBar`, `topDivider`, `channelTabsRow`,
  `inputBar` (cell-correct prefix budget; reserves 1-cell `cursorPad`
  for the off-by-one in `bubbles/textinput.View()`), plus per-segment
  cell builders (`channelTabCell`, `byteCounterCell`, `flashBannerCell`
  …) and `statusSegment`.
- `components_panes.go` — the pane Components themselves
  (`channelsPane`, `nodesPane`, `messagesPane`, `helpPane`) plus
  `frameView`, `renderIrssiBody`, `renderBorderedPane`,
  `paneAccentColor`, `paneInnerWidth`, `tailStartList`,
  `messagesPaneRender`. Each pane Component owns its implementation —
  there are no `(m model) renderXxxPane` shims.
- `components_panes_geo.go` — `nearbyPane` / `radarPane` Components +
  the `peerPlot` data prep both consume.
- `components_message.go` — `messageRow` Component owns the
  notice/system/regular-chat dispatch via `noticeRowRender` /
  `chatRowRender` and forces every line through `padCells` so a
  buggy inner emitter can't blow out the pane.
- `components_chat.go` / `components_notice.go` / `components_overlays.go`
  / `components_radar.go` — leaf cell builders the rows compose,
  organized by surface. Selection chrome (`wrapSelection`,
  `gutterWidth`, `dimRow`) lives in `components_overlays.go`
  alongside the selection-aware overlay row helpers.

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

Set `MESHX_LAYOUT_ASSERT=1` to enable dev-mode invariant panics:
every `Component.Render` is checked to return exactly the requested
box, so a regression in cell-counting math surfaces as an immediate
panic at the offending call site instead of as visible drift two
rerenders later. The env lookup is hoisted to a package-level
once-read in `components_box.go`, so the check is free in production.

## Overlays (no drawers)

The main message log is always visible by default. `/channels` and
`/nodes` (aliases: `/users`, `/names`) pop a full-pane overlay that
replaces the log until ESC. No persistent side drawers.

## Key Technical Details

- Bubble Tea alt-screen mode — no raw-term cursor wrangling
- `tea.Tick` schedules the 3s splash auto-dismiss
- Zebra-striped message rows: `rowBgEven = #1a1b26` / `rowBgOdd = #24283b`
- Ham `/commands` build reports from real node telemetry via
  `lookupNode(callsign)` + `signalReport(n)` — no faked numbers
- Active channel tab rendered in hot-pink `mhPink` to stand out from
  the cyan + mesh-green that dominate the rest of the UI
- Users overlay is a BitchX-style bracketed grid; `moveSelectionGrid`
  handles 2D `h/l/j/k` nav that matches the visual column count
- Tab completion: context-aware (commands / channels / nicks), cycles
  on repeated Tab, inserts irssi `<nick>: ` at start of line

## Color Palette (Max Headroom)

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
#67ea94  meshgreen - focused pane border, //\ brand, input prompt
```

## Commands reference (summary)

Full list in `docs/commands.md`. The ham set:

`/cq  /cqr  /rs  /73  /88  /qsl  /qth  /grid  /sked  /qrz  /qrm  /qsb  /sk  /wx  /mesh  /k`

Operational: `/msg  /reply(/r)  /ping  /tr(/traceroute)  /whois(/w)
/join  /channel(list|new|share|add|del)  /channels  /nodes(/users/names)
/search  /config  /clear  /help  /exit(/quit/q)`

## Code Standards

- Conventional Commits for messages
- Multi-line function signatures
- golangci-lint: errcheck, errname, govet, prealloc, predeclared, revive, staticcheck

## Roadmap

- [x] Full irssi-style UI (demo + live)
- [x] BitchX rotating splash
- [x] 16 ham-radio `/commands` wired to real telemetry
- [x] Bracketed BitchX-style users grid
- [x] Tab completion + `/` search + `n/N` cycling
- [x] USB-serial Meshtastic transport (go.bug.st/serial + meshtastic/go)
- [x] TCP transport — meshtasticd / WiFi radio on port 4403
- [x] BLE transport (tinygo.org/x/bluetooth; pair via `meshx ble pair`,
      fav + scan + connect subcommands)
- [x] Notice TTL + pin with `⌜ ⌟` corners and fade
- [x] Stale-pending message sweep on startup; `R` resends pending OR fail
- [x] Channel lifecycle: `/channel new` (mint w/ random PSK) + `/channel share`
      (ASCII QR via half-block) + `/channel add <meshtastic://url>` (PSK import)
      + `/channel del` (disable). PSK is RAM-only — never persisted to
      `~/.meshx/meshx.db`. Hidden `/qrtest` for renderer iteration.
- [x] `internal/driver/` extracted (canonical State + Pump/Store seams).
- [x] `meshx server start` daemon (Huma HTTP + multi-radio Registry,
      `/radios/{radio_id}/...` for radio-scoped reads + `/transports/{ble,usb}/*`
      for remote admin).
- [x] Server middleware: panic recovery + request-id + structured request log.
- [x] OpenAPI 3.0 codegen pipeline → `internal/sdk/gen/client.gen.go`
      (regen via daemon + curl, then `go generate`).
- [x] CLI one-shots (scan / pair / list / forget / fav) talk directly to
      transport+storage through cmd-local consumer interfaces — no daemon
      required for everyday CLI use.
- [ ] **MR-3.5c**: relocate `apply*` handlers from `internal/tui/radio.go`
      onto `*driver.Driver` so the daemon can mutate Session as packets
      arrive. Adds `Driver.Subscribe(...)` for the SSE fan-out seam.
- [ ] **MR-4 (events)**: implement `/radios/{radio_id}/events` SSE stream
      (currently 501) on top of the new Subscribe seam.
- [ ] **MR-5 (TUI as remote client)**: TUI consumes `internal/sdk/gen` +
      SSE so `meshx ble connect` auto-spawns or attaches to a daemon, and
      reconnect/backoff/persistence become server-side concerns. Server log
      goes to `~/.meshx/server.log` when stdout isn't a TTY (TUI case).

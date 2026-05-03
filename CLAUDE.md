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
├── cmd/
│   ├── root.go                   # cobra root + auto-connect fallback chain
│   ├── demo.go                   # `meshx demo` — canned-fixture UI
│   ├── usb.go                    # `meshx usb {probe,connect}`
│   ├── probe.go                  # body of `meshx usb probe`
│   ├── tcp.go                    # `meshx tcp connect`
│   ├── ble.go                    # `meshx ble {scan,pair,list,forget,connect,disconnect,fav}`
│   └── ble_probe.go              # `meshx ble probe` — diagnostic packet dump
├── internal/meshx/               # all implementation
│   ├── app.go                    # Bubble Tea model + View() + Update wiring
│   ├── ui.go                     # View dispatcher, model getters, generic utils
│   ├── pump.go                   # consumer interface (Pump) — twin of store.go (osapi-io)
│   ├── store.go                  # consumer interface (Store) for the storage package
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
│   ├── ble_cli.go                # `meshx ble` CLI helpers (scan, list, fav, …)
│   ├── node.go                   # nodeItem + state derivation
│   ├── radio.go                  # apply* handlers (mdl.Text, mdl.NodeInfo, mdl.Routing, …)
│   ├── geo.go                    # haversineKm / bearingDeg / compassAbbr math
│   ├── help.go                   # /help entry data
│   ├── fixture.go                # Demo struct + DefaultDemo()
│   ├── model/                    # canonical wire/persisted shapes — the lingua franca
│   │   ├── message.go            # Message + MessageStatus enum
│   │   ├── node.go               # CachedNode (NodeDB cache row)
│   │   ├── ble.go                # BLEDevice (BLE pairing row)
│   │   ├── events.go             # pump-emitted events: Text, NodeInfo, Position, Ping, Routing, …
│   │   ├── commands.go           # consumer-issued outbound: SendText, SetOwner, SetBuzzer, RequestSync, …
│   │   ├── config.go             # modeled radio configs (ExternalNotification today; Owner / LoRa / Device next)
│   │   └── enums.go              # Region, ModemPreset, DeviceRole, ChannelRole, RoutingError typed strings
│   ├── session/                  # canonical per-radio session state — no Bubble Tea import
│   │   └── session.go            # Session struct + PendingPing/Traceroute, PeerPosition/EnvMetrics, ReconnectState
│   ├── pump/                     # transport ↔ tea bridge (concrete *pump.Pump)
│   │   ├── pump.go               # Transport (consumer interface) + New / Stop + run loop with reconnect policy
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

- `meshx.RunDemo()` — launch the TUI with the canned Demo fixture.
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

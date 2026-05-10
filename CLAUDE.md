# CLAUDE.md

Architecture map + standards for Claude Code working in this repo.
Setup, dev workflow, design notes, deployment modes, and testing
standards live in [`docs/development.md`](./docs/development.md);
this file is the navigation layer on top — read here first to find
where things live, then jump to the dev guide for depth.

## Project

**meshX** — irssi-style terminal Meshtastic messenger. Connects to a
Meshtastic-compatible LoRa radio over USB-serial, TCP (`meshtasticd`,
port 4403), or BLE; surfaces the mesh in a Bubble Tea TUI; ships an
HTTP+SSE daemon for headless / remote / agent consumers.

All three transports share one `Client` interface and funnel through
the same pump → tea.Msg → model path, so the renderer never branches
on transport type. Every telemetry field maps 1:1 to Meshtastic
protobuf fields — no faked numbers.

## Where things live

```
meshx/
├── main.go                       # 7-line entry → cmd.Execute()
├── cmd/                          # one file per subcommand; *_deps.go declares cmd-local consumer interfaces
│   ├── root.go                   # cobra root + global slog logger (lmittmann/tint, JSON via -j) + viper (MESHX_ env prefix)
│   ├── version.go                # `meshx version` JSON build identity
│   ├── usb.go usb_*.go           # `meshx usb {scan,connect,probe}` + usb_deps.go
│   ├── ble.go ble_*.go           # `meshx ble {scan,pair,list,forget,connect,disconnect,fav,probe}` + ble_deps.go
│   ├── server.go server_start.go # `meshx server start` daemon entry
│   └── server_deps.go            # daemon-only adapters wiring server.Config
├── internal/session/             # per-radio session layer — owns canonical State, wraps Pump + Store
│   ├── session.go                # *Session + New(state, pump, store) + Send / Stop
│   ├── state.go                  # *State — Channels / Nodes / Messages, indices, pending requests, reconnect banner
│   ├── apply.go                  # Apply* handlers — Text, NodeInfo, Routing, Position, Ping, Traceroute, Channel, …
│   ├── subscribe.go              # Event + Publish + Subscribe + SubscribeWithReplay (ring-buffer replay log)
│   ├── pump.go                   # consumer interface (Pump) for internal/meshx/pump
│   └── store.go                  # consumer interface (Store) for internal/meshx/storage
├── internal/server/              # HTTP+SSE daemon (Huma) — multi-radio aware via Registry
│   ├── server.go                 # *Server + New(Config) + Run(ctx, addr); slog-tagged subsystem=http
│   ├── registry.go               # radio_id → Driver multiplex, mutex-guarded
│   ├── middleware.go             # request-id (X-Request-ID) + structured request log + panic recovery
│   ├── session.go                # consumer interface (Driver) — concrete *session.Session satisfies it
│   ├── store.go                  # Store / BLEScanner / BLEPairer / USBScanner consumer interfaces + sighting wire shapes
│   ├── routes.go                 # huma.Register calls
│   ├── handlers.go               # core route handlers — channels / nodes / messages emit model types directly
│   ├── handlers_config.go        # PATCH /config + POST /reboot
│   ├── handlers_channels.go      # POST /channels (mint), POST /channels/import, DELETE, GET /share
│   ├── idempotency.go            # Idempotency-Key dedupe cache for POST /messages
│   ├── events.go                 # SSE handler — Last-Event-ID + ?since= cursor, replays from Session ring buffer
│   ├── transport_ble.go          # /transports/ble/* HTTP routes (remote admin)
│   └── transport_usb.go          # /transports/usb/* HTTP routes (remote admin)
├── internal/sdk/                 # generated Go HTTP client + Remote driver shim
│   ├── gen/                      # api.yaml + cfg.yaml + generate.go + client.gen.go (oapi-codegen output)
│   ├── convert.go                # gen.* ↔ mdl.* type conversions at the SDK boundary
│   └── remote.go                 # *Remote driver (TUI-as-remote-client mode; consumes gen.Client + SSE)
├── internal/tui/                 # Bubble Tea rendering surface (model holds *session.Session today)
│   ├── app.go                    # model + View + Update + RunRadio
│   ├── ui.go                     # View dispatcher, model getters, generic utils
│   ├── commands.go               # /command dispatcher + ham bangs
│   ├── input.go                  # key bindings, nav mode, tab wiring
│   ├── radio.go                  # apply* shims (forward to *session.Session.Apply*)
│   ├── components_box.go         # Box / Component / Cell / Row + RawBlock / Viewport / Centered
│   ├── components_stack.go       # VStack / HStack / Bordered / Styled
│   ├── components_chrome.go      # statusBar / topDivider / channelTabsRow / inputBar
│   ├── components_chat.go        # chatRow* cell builders + nick/zebra colors + formatAckers
│   ├── components_notice.go      # noticeRow* cell builders
│   ├── components_message.go     # messageRow Component + notice/chat dispatch
│   ├── components_overlays.go    # overlay row builders + selection chrome
│   ├── components_panes.go       # channels/nodes/messages/help pane Components + frameView
│   ├── components_panes_geo.go   # nearby/radar pane Components + peerPlot prep
│   ├── components_radar.go       # radarCanvas + radar legend cells
│   ├── components_splash.go      # BitchX rotating splash data + builder
│   ├── notices.go                # TTL + pin + fade for `-!-` rows
│   ├── complete.go               # Tab completion — /cmd, #chan, nicks
│   ├── palette.go                # maxheadroom color constants
│   ├── node.go                   # nodeItem + state derivation
│   ├── geo.go                    # haversine / bearing / compass math
│   ├── help.go                   # /help entry data
│   ├── dms.go                    # virtual @peer DM tab state
│   └── qr.go                     # ASCII QR rendering for /channel share
├── internal/version/             # build identity (Version / Commit / Date / BuiltBy)
├── internal/meshx/               # foundational sub-packages
│   ├── model/                    # canonical wire/persisted shapes — the lingua franca
│   │   ├── message.go            # Message + MessageStatus enum
│   │   ├── items.go              # ChannelItem + NodeItem + MessageItem + Acker
│   │   ├── node.go               # CachedNode (NodeDB cache row)
│   │   ├── ble.go                # BLEDevice (pairing row)
│   │   ├── events.go             # pump-emitted events
│   │   ├── commands.go           # consumer-issued outbound commands
│   │   ├── config.go             # modeled radio configs
│   │   └── enums.go              # Region / ModemPreset / DeviceRole / ChannelRole / RoutingError / NodeState
│   ├── pump/                     # transport ↔ tea bridge (concrete *Pump)
│   │   ├── pump.go               # New / Stop + run loop + reconnect policy
│   │   ├── transport.go          # consumer interface (Transport)
│   │   ├── translate.go          # FromRadio → []model.X (proto→model inbound boundary)
│   │   ├── outbound.go           # (*Pump).Send(model.Command) — model→proto outbound
│   │   ├── channel_url.go        # ParseChannelShareURL / BuildChannelShareURL
│   │   └── config.go             # ExternalNotificationFromProto / ToProto bridges
│   ├── storage/                  # SQLite persistence
│   │   ├── sqlite.go             # CRUD against model.Message / CachedNode / BLEDevice
│   │   └── migrations/           # embedded goose SQL
│   └── transport/
│       ├── client.go             # Client interface + Dial dispatcher
│       ├── framing.go            # 0x94 0xc3 <hi> <lo> <proto> frame codec
│       ├── stream.go             # Shared framed-stream runner (serial + tcp)
│       ├── serial.go tcp.go ble.go
│       ├── ble_scan.go           # ScanBLE + PairBLE (shared by cmd-direct + daemon adapters)
│       └── identify.go           # AutoDetectMeshtastic + IdentifyAllSerial USB probes
└── docs/
    ├── commands.md               # every keybinding and /command — user-facing reference
    ├── development.md            # setup, architecture deep-dive, deployment modes, testing standards
    └── contributing.md           # PR workflow, conventional commits, scope reminders
```

## Code standards

- **Conventional Commits** for messages — see `docs/contributing.md`.
- **Multi-line function signatures** for any function with 2+ params.
- **golangci-lint** chain: `errcheck`, `errname`, `govet`, `prealloc`,
  `predeclared`, `revive`, `staticcheck`. `just ready` runs the full
  format + lint suite locally.
- **Tests, not test plans** — every PR ships with the tests that
  verify it. See [`docs/development.md`](./docs/development.md#testing)
  for the standards (table-driven, `httptest` for HTTP / SSE,
  in-process `*session.Session` for apply/publish/subscribe).
- **No inline hex colors** — palette constants live in
  `internal/tui/palette.go`. Names referenced below.

## Color palette (Max Headroom)

```
#ffb86c  orange    timer / battery warnings
#00d4ff  cyan      inactive channel tabs, unfocused headers
#c678dd  magenta   "me" messages, nodes pane accent
#50fa7b  green     online node state, ACK ✓
#e5c07b  yellow    unread counts, !bang command prefix
#ff6ec7  pink      ACTIVE channel tab, error flashes
#6272a4  lavender  muted states, "other" tab names
#c0caf5  fg        default text
#3b4261  drained   labels, separators, dim italic hints
#67ea94  meshgreen focused pane border, //\ brand, input prompt
```

## Quick pointers

- **Slash commands + keybindings** → `docs/commands.md`
- **Setup, architecture deep-dive, testing standards** → `docs/development.md`
- **PR workflow + scope reminders** → `docs/contributing.md`
- **Open work** → tracked as github issues + internal tasks; do not
  hand-curate roadmaps in this file.

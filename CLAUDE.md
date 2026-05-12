# CLAUDE.md

Architecture intent + standards for Claude Code working in this repo.
File-by-file orientation lives in each package's `doc.go` and is
cheaper to discover via `ls` / `grep` than to mirror here — that map
rots silently every PR and stale info in context is worse than none.
Setup, dev workflow, design notes, deployment modes, testing
standards are in [`docs/development.md`](./docs/development.md).

## Project

**meshX** — irssi-style terminal Meshtastic messenger. Connects to a
Meshtastic-compatible LoRa radio over USB-serial, TCP (`meshtasticd`,
port 4403), or BLE; surfaces the mesh in a Bubble Tea TUI; ships an
HTTP+SSE daemon for headless / remote / agent consumers; plus an MCP
server for LLM-agent integrations.

All three transports share one `Client` interface and funnel through
the same pump → tea.Msg → model path, so the renderer never branches
on transport type. Every telemetry field maps 1:1 to Meshtastic
protobuf fields — no faked numbers.

## Architecture in one screen

```
                radio (USB / BLE / TCP)
                         │
                         ▼
                ┌──────────────────────┐
                │  internal/meshx/     │  protobuf wire ⇄ canonical model
                │  {model,pump,        │  (the lingua franca)
                │   storage,transport} │
                └──────────┬───────────┘
                           ▼
            ┌──────────────────────────────┐
            │  internal/radio/             │  single source of truth:
            │  *radio.Session              │  State + Apply* + ops_*
            │  (Pump + Store + Subscribe)  │  (Mint/Send/Ping/Config…)
            └──────┬──────┬──────┬─────────┘
                   │      │      │
                   ▼      ▼      ▼
           ┌────────┐ ┌──────┐ ┌──────────┐
           │  tui   │ │server│ │   mcp    │  three consumers of the
           │ (local │ │(HTTP+│ │ (stdio   │  same Session methods —
           │  TUI)  │ │ SSE) │ │  JSON-RPC)│ each is a thin adapter
           └────────┘ └──┬───┘ └──────────┘
                         │
                         │ HTTP + SSE
                         │
              ┌──────────┴─────────────┐
              │  meshx client {…}      │  CLI adapter (Phase A + B)
              │  meshx mcp start       │  MCP adapter (stdio)
              │  TUI in remote mode    │  sdk.Remote (gen.Client + SSE)
              └────────────────────────┘
```

Key invariants:

- **One source of truth per operation.** Every channel/config/send/
  radio op lives once on `*radio.Session` (the `ops_*.go` files);
  HTTP, TUI, MCP are 5–10 line adapters over those methods.
- **Multi-radio.** The daemon's `Registry` multiplexes by `radio_id`;
  every route + every MCP tool is radio-scoped.
- **osapi-io / consumer-side interfaces.** Each package that consumes
  a "driver" (server, tui, mcp) declares its own narrow `Driver` /
  `radioSession` interface — concrete-type imports live only at the
  constructor (`New`), where the compiler verifies structural fit.
- **Transports are exclusive.** When the daemon is running, it owns
  the BLE/USB adapter — clients (CLI, MCP) route through HTTP.
- **`internal/radio` is framework-free.** Ops methods return
  `radio.OpError` (a domain error with an HTTP-like status code);
  `internal/server` translates to `huma.Error*` at the handler
  boundary. The radio package never imports HTTP frameworks.

## Code standards

- **Conventional Commits** for messages — see `docs/contributing.md`.
- **Multi-line function signatures** for any function with 2+ params.
- **golangci-lint** chain: `errcheck`, `errname`, `govet`, `prealloc`,
  `predeclared`, `revive`, `staticcheck`. `just ready` runs the full
  format + lint suite locally.
- **Tests, not test plans** — every PR ships with the tests that
  verify it. See [`docs/development.md`](./docs/development.md#testing)
  for the rules (table-driven, `httptest` for HTTP / SSE, in-process
  `*radio.Session` for apply/publish/subscribe, one `Test<Subject>`
  per public surface, `foo.go ↔ foo_test.go` file pairing).
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
- **Flag / env / default reference** → `docs/configuration.md`
- **PR workflow + scope reminders** → `docs/contributing.md`
- **Where does <thing> live?** → `ls internal/`, then read the
  package's `doc.go` or top-of-file header. Don't trust hand-curated
  trees — they rot silently.
- **Open work** → tracked as github issues; do not hand-curate
  roadmaps in this file.

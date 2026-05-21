# CLAUDE.md

Architecture intent + standards for Claude Code working in this repo.
File-by-file orientation lives in each package's `doc.go` and is
cheaper to discover via `ls` / `grep` than to mirror here — that map
rots silently every PR and stale info in context is worse than none.
Setup, dev workflow, testing standards are in
[`docs/development.md`](./docs/development.md).

## Project

**meshX** — irssi-style terminal Meshtastic messenger. Connects to a
Meshtastic-compatible LoRa radio over USB-serial, TCP (`meshtasticd`,
port 4403), or BLE and surfaces the mesh in a Bubble Tea TUI.

All three transports share one `Client` interface and funnel through
the same pump → event bus → TUI path, so the renderer never branches
on transport type. Every telemetry field maps 1:1 to Meshtastic
protobuf fields — no faked numbers.

## Architecture in one screen

```
            radio (USB / BLE / TCP)
                     │
                     ▼
            ┌──────────────────────┐
            │  internal/meshx/     │  protobuf wire ⇄ canonical model
            │  {model,pump,        │
            │   storage,transport} │
            └──────────┬───────────┘
                       │
                  ┌────┴────┐
                  │  write  │
                  ▼         ▼
            ┌─────────┐  ┌──────────┐
            │  bbolt   │  │ event    │
            │  Store   │  │ bus      │
            └────┬────┘  └────┬─────┘
                 │            │
                 ▼            ▼
            ┌──────────────────────┐
            │  internal/tui/       │  Bubble Tea TUI
            │  reads Store on      │  receives bus events
            │  startup + events    │  via tea.Program.Send
            └──────────────────────┘
```

Key invariants:

- **Store is the source of truth.** The pump writes to bbolt, the
  TUI reads from it. No shared mutable in-memory state.
- **Event bus for real-time.** The pump publishes typed events after
  each Store write; the TUI subscribes and re-reads. DB is
  persistence, bus is notification.
- **Store interface for testability.** Both pump and TUI depend on
  the `Store` interface, not the concrete bbolt implementation.
  Tests inject mocks/fakes.
- **`internal/radio` is framework-free.** Ops methods return
  `radio.OpError`; the TUI translates at its own boundary.

## Code standards

- **Conventional Commits** for messages — see `docs/contributing.md`.
- **Multi-line function signatures** for any function with 2+ params.
- **golangci-lint** chain: `errcheck`, `errname`, `govet`, `prealloc`,
  `predeclared`, `revive`, `staticcheck`. `just ready` runs the full
  format + lint suite locally.
- **Tests, not test plans** — every PR ships with the tests that
  verify it. See [`docs/development.md`](./docs/development.md#testing)
  for the rules (table-driven, in-process `*radio.Session` for ops,
  one `Test<Subject>` per public surface, `foo.go ↔ foo_test.go`
  file pairing).
- **Interface-driven design** — Store, Pump, and Bus are interfaces.
  Mocks for testing, concrete implementations for production.
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

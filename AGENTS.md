# AGENTS.md

## Quick start

meshX is an irssi-style terminal Meshtastic messenger — it connects to a LoRa
radio over USB, TCP, or BLE and renders the mesh in a Bubble Tea TUI, persisting
history to a bbolt store at `~/.meshx/meshx.bolt`.

```bash
just deps && go build -o meshx .   # build
just test                           # lint + unit + coverage
go run . demo                       # smoke-test the TUI with no radio
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

The pump is the only place `gomeshproto` types meet `model` types. Everything
else — TUI, storage, bus — speaks `mdl.X` exclusively.

## Key packages

| Package                     | What it does                                                          |
| --------------------------- | --------------------------------------------------------------------- |
| `cmd/`                      | Cobra command tree — `usb`, `ble`, `tcp`, `demo` subcommands          |
| `internal/radio/`           | Per-radio session: `*State`, `Apply*` handlers, `ops_*` operations    |
| `internal/bus/`             | In-process pub/sub event bus; slow consumers drop rather than block   |
| `internal/meshx/model/`     | Canonical wire + persisted shapes — the lingua franca                 |
| `internal/meshx/pump/`      | Transport ↔ tea bridge, reconnect policy, proto ↔ model translation  |
| `internal/meshx/storage/`   | bbolt persistence — messages, nodes, BLE devices                      |
| `internal/meshx/transport/` | `Client` interface + serial / TCP / BLE implementations + frame codec |
| `internal/tui/`             | Bubble Tea rendering — Component tree, panes, input, commands         |
| `internal/cli/`             | CLI-only lipgloss helpers (banner, themed output) — never in the TUI  |
| `internal/version/`         | Build identity (Version / Commit / Date / BuiltBy)                    |

## Testing

```bash
just test                                          # full suite
go test -race ./...                                # race detector
go test -run TestSession_ApplyText ./internal/radio/  # single test
```

- Table-driven tests, one `Test<Subject>` per public surface.
- `foo.go` is tested by `foo_test.go` — always, no exceptions.
- bbolt tests use `storage.New(t.TempDir() + "/test.bolt")`.
- In-process `radio.New(nil, nil, nil)` for session-layer tests without a radio.
- Race detector is mandatory for anything touching the bus or pump.

## Standards

Full coding standards, commit conventions, and PR workflow are in
[`CLAUDE.md`](./CLAUDE.md) and [`docs/development.md`](./docs/development.md).

Key invariants for agents:

- Consumer interfaces are declared where consumed (osapi-io pattern) — never
  import a concrete type from another package when an interface suffices.
- No inline hex colors — palette constants live in `internal/tui/palette.go`.
- Multi-line function signatures for any function with 2+ params.
- `just ready` before every commit (gofumpt + golines + golangci-lint).

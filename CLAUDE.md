# CLAUDE.md

**meshX** — irssi-style terminal Meshtastic messenger over USB/TCP/BLE.

Build: `just deps && go build .` | Test: `just test` | Lint: `just ready`

## Standards

- Conventional Commits — `docs/contributing.md`
- Multi-line function signatures for 2+ params
- `foo.go ↔ foo_test.go` pairing, table-driven, interfaces for mocking
- No inline hex colors — palette in `internal/tui/palette.go`
- `just ready` before every commit

## Docs

- [`AGENTS.md`](./AGENTS.md) — packages, testing patterns, screenshot
- [`docs/development.md`](./docs/development.md) — architecture, setup, testing deep dive
- [`docs/commands.md`](./docs/commands.md) — keybindings, slash commands
- [`docs/configuration.md`](./docs/configuration.md) — flags, env vars
- [`docs/contributing.md`](./docs/contributing.md) — PR workflow

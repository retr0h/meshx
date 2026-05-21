# AGENTS.md

Build: `just deps && go build .` | Test: `just test` | Lint: `just ready`

## Packages

| Package                     | What it does                                                          |
| --------------------------- | --------------------------------------------------------------------- |
| `internal/radio/`           | Per-radio session: `*State`, `Apply*` handlers, `ops_*` operations    |
| `internal/bus/`             | In-process pub/sub event bus; slow consumers drop rather than block   |
| `internal/meshx/model/`     | Canonical wire + persisted shapes — the lingua franca                 |
| `internal/meshx/pump/`      | Transport ↔ TUI bridge, reconnect policy, proto ↔ model translation  |
| `internal/meshx/storage/`   | bbolt persistence — messages, nodes, BLE devices                      |
| `internal/meshx/transport/` | `Client` interface + serial / TCP / BLE implementations + frame codec |
| `internal/tui/`             | Bubble Tea rendering — Component tree, panes, input, commands         |

## Testing

- Table-driven, `foo.go ↔ foo_test.go`, one `Test<Subject>` per public surface
- bbolt: `storage.New(t.TempDir() + "/test.bolt")`
- Session: `radio.New(nil, nil, nil)` for tests without a radio
- Race detector mandatory for bus/pump code

## Screenshot

```bash
MESHX_BLE_UUID=<uuid> vhs asset/ui.tape
```

Produces `asset/ui.gif` + `asset/ui.png`. User supplies the UUID.

## Standards

- Conventional Commits
- Consumer interfaces declared where consumed (osapi-io pattern)
- No inline hex colors — palette in `internal/tui/palette.go`
- Multi-line function signatures for 2+ params
- `just ready` before every commit
- Architecture + deep dive: [`docs/development.md`](./docs/development.md)

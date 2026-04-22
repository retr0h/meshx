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
go run . --demo                   # irssi-style UI with canned data
go run .                          # (future) connect to real Meshtastic radio
go build -o meshx . && ./meshx --demo
```

## Architecture

```
meshx/
├── main.go                   # tiny — forwards to cmd.Execute()
├── cmd/
│   └── root.go               # cobra root + --demo flag
└── internal/meshx/
    ├── demo.go               # Bubble Tea model: state, Update, View,
    │                         # executeCommand, renderers
    ├── splash.go             # BitchX-style rotating graffiti banner
    │                         # (4 variants, pickSplash at launch)
    ├── complete.go           # Tab completion — /cmd, #chan, nicks
    ├── palette.go            # maxheadroom color constants
    ├── doc.go                # package doc
    └── demo_snapshot_test.go # golden-view snapshot for visual diffs
```

### Public API

```go
meshx.RunDemo()  // tea.NewProgram(initialModel(), tea.WithAltScreen()).Run()
```

Internals (model, modes, commands) are unexported. `cmd/root.go` is the only
consumer.

## Dependencies

| Package                   | Purpose                                     |
| ------------------------- | ------------------------------------------- |
| `charmbracelet/bubbletea` | Elm-style TUI framework                     |
| `charmbracelet/bubbles`   | textinput widget for input + search prompts |
| `charmbracelet/lipgloss`  | colors, borders, layout primitives          |
| `spf13/cobra`             | CLI command tree                            |

## Modal UI — where the code lives

- **Mode constants** — `modeSplash`, `modeInput`, `modeNav`, `modeSearch`,
  `modeHelp` in `demo.go`
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
  users grid.
- **Truncation** — `truncateLine` / `padOrTruncate` honor ANSI escapes so styled
  content doesn't get clipped mid-SGR sequence.
- **Pane accents** — `paneAccentColor(paneIdx)` returns the per-pane signature
  color (channels = cyan, messages = mesh-green, nodes = magenta). Used by
  focused-pane borders and the giant pane-number overlay.

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
`demo.go`. Target-taking commands default to the highlighted sender in nav mode
via `selectedSender()`.

Reports use real node telemetry:

```go
n := m.lookupNode(target)          // pointer to node or nil
report := signalReport(n)          // "hop 2, SNR -8.5 dB, RSSI -92 dBm"
```

When the transport layer lands, every field on `nodeItem` (`lastSNR`,
`lastRSSI`, `lastHops`, `hwModel`, `firmware`) populates from Meshtastic
protobuf — `MeshPacket.rx_snr`, `rx_rssi`, `hop_start - hop_limit`,
`MyNodeInfo.HardwareModel`, `firmware_version`.

## Testing

```bash
go test ./internal/meshx/...            # all tests
go test -run TestSnapshotView -v ./internal/meshx/  # print a visual snapshot
```

`demo_snapshot_test.go` prints the rendered `View()` at a fixed terminal size
(`160×40`) so you can eyeball layout changes in `go test -v` output.

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

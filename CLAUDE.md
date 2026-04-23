# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when
working with code in this repository.

## Project Overview

**meshX** is a glitched-out terminal Meshtastic messenger — an
irssi-style chat client for LoRa radios with a vintage BBS aesthetic.
It connects to a Meshtastic-compatible device (USB serial / TCP /
future BLE), subscribes to the mesh, and surfaces everything in a
dense Bubble Tea TUI with mutt-grade keyboard, BitchX-style splash,
and a maxheadroom palette.

Today the whole UI is demo-mode only (canned data, real interaction).
Transport layer is not yet wired; every telemetry field in the model
(`lastSNR`, `lastRSSI`, `lastHops`, `hwModel`, `firmware`) maps 1:1 to
Meshtastic protobuf fields so the transport drops in without any UI
changes.

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
├── main.go                      # 7-line entry → cmd.Execute()
├── cmd/
│   └── root.go                  # cobra root + --demo flag
├── internal/meshx/              # all implementation
│   ├── demo.go                  # Bubble Tea model, Update, View, commands
│   ├── splash.go                # BitchX-style rotating graffiti banner
│   ├── complete.go              # Tab completion — /cmd, #chan, nicks
│   ├── palette.go               # maxheadroom color constants
│   └── doc.go                   # package doc
└── docs/
    ├── keymap.md                # every keybinding and /command
    ├── development.md           # setup, testing, conventions
    └── contributing.md          # PR workflow
```

### Public API

- `meshx.RunDemo()` — launch the Bubble Tea program in demo mode
  (`tea.NewProgram(initialModel(), tea.WithAltScreen())`)

Internals are not re-exported; the package is consumed only by
`cmd/root.go`.

### Dependencies

- `charmbracelet/bubbletea` — Elm-style TUI framework
- `charmbracelet/bubbles` — textinput for input + search prompts
- `charmbracelet/lipgloss` — colors, borders, layout primitives
- `spf13/cobra` — CLI framework

## Modal UI model

Four modes plus splash:

- **`modeSplash`** — BitchX-style banner at launch, 3s auto-dismiss or any key.
- **`modeInput`** (default) — input bar focused; typing composes or runs `/command`.
- **`modeNav`** — selection cursor in the scrollback / overlay; `j/k/h/l`, `r/t/p/w/*/m`, `/` search.
- **`modeSearch`** — live-filter prompt; Enter commits, ESC cancels.
- **`modeHelp`** — scrollable `?` overlay; `j/k/g/G/d/u` navigate, ESC closes.

ESC always returns to the input bar (the canonical "where I type"
state). Ctrl+X always quits.

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

Full list in `docs/keymap.md`. The ham set:

`/cq  /cqr  /rs  /73  /88  /qsl  /qth  /grid  /sked  /qrz  /qrm  /qsb  /sk  /wx  /mesh  /k`

Operational: `/msg  /reply(/r)  /ping  /tr(/traceroute)  /whois(/w)
/join  /channel  /channels  /nodes(/users/names)  /search  /config
/clear  /help  /exit(/quit/q)`

## Code Standards

- Conventional Commits for messages
- Multi-line function signatures
- golangci-lint: errcheck, errname, govet, prealloc, predeclared, revive, staticcheck

## Roadmap

- [x] Full irssi-style UI in demo mode
- [x] BitchX rotating splash
- [x] 16 ham-radio `/commands` wired to real telemetry
- [x] Bracketed BitchX-style users grid
- [x] Tab completion + `/` search + `n/N` cycling
- [ ] USB-serial Meshtastic transport (go.bug.st/serial + meshtastic/go)
- [ ] TCP transport — meshtasticd / WiFi radio on port 4403
- [ ] PSK import via `/channel add <meshtastic://url>`
- [ ] BLE transport (stretch)

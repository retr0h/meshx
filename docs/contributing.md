# Contributing to meshX

Thanks for taking the time to contribute! meshX is an irssi-style terminal
Meshtastic messenger — we care about vim-grade ergonomics, a coherent
vintage-BBS aesthetic, and honest data (no fake telemetry).

## How to contribute

### Reporting bugs

- Use the [GitHub issue tracker](https://github.com/retr0h/meshx/issues)
- Include your OS + terminal emulator + Go version
- Include steps to reproduce
- For visual glitches, attach a screenshot or terminal capture

### Suggesting features

- Open an issue describing the feature and why it'd be useful
- Consider whether it fits the project's scope (a Meshtastic messenger client
  with irssi/BitchX/mutt ergonomics — not a device configurator, not a map
  client, not a bridge to non-Meshtastic networks)

### Code contributions

**Small fixes** — typos, grammar, formatting — submit a PR directly.

**Larger changes** — bug fixes, new features, UI changes:

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Make your changes
4. Ensure everything builds: `go build -o meshx .`
5. Run the linter: `golangci-lint run`
6. Run tests: `go test ./...`
7. Commit with [Conventional Commits](https://conventionalcommits.org/) style
8. Open a PR — describe the change and link any related issues

## Commit messages

Uses [Conventional Commits](https://conventionalcommits.org/). Format:
`type(scope): description`

Types: `feat`, `fix`, `docs`, `chore`, `ci`, `build`, `test`, `refactor`

Examples:

```
feat(splash): add a fifth BitchX rotating graffiti variant
fix(nav): grid h/l nav respects column count on narrow terminals
docs(keymap): document /qrz /qrm /qsb slang commands
refactor(complete): split slashCommands into a generated list
```

## Development setup

See [development.md](development.md) for the full setup + architecture guide.
Short version:

```bash
git clone https://github.com/retr0h/meshx.git
cd meshx
just deps
go build -o meshx .
./meshx --demo
```

## Code style

- Follow existing patterns in the codebase
- Use multi-line function signatures for any function with 2+ params
- Palette colors live in `internal/meshx/palette.go` — add new colors there with
  a named constant, never inline hex elsewhere
- Every widget uses lipgloss; no direct ANSI escape codes
- Tab completion should honor the same contextual rules (commands / channels /
  nicks) — if you add a new `/command`, also add it to `slashCommands` in
  `complete.go`
- Report-producing commands must pull from real node telemetry via
  `lookupNode()` + `signalReport()` — never hardcode SNR/RSSI/hop numbers. If
  data isn't available, flash an honest "no telemetry" hint instead
- `Esc` must always return to the input bar from any sub-state — this is the
  single most important UX invariant

## Scope reminders

- meshX is a **client**, not a radio configurator. PSK/channel creation lives on
  the radio side (official Meshtastic app). meshX imports channels; it does not
  create them from scratch.
- meshX is **text + telemetry**. No maps, no audio, no voice.
- meshX is **terminal-first**. Every feature should work cleanly in a 80×24
  vt100 at minimum. If you add something that needs more pixels, make it degrade
  gracefully.

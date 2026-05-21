# Configuration

Every meshx subcommand resolves its inputs through three layers in strict
precedence:

1. **Explicit CLI flag** — e.g., `--debug`.
2. **Environment variable** — e.g., `MESHX_DEBUG=1`.
3. **Hard-coded default** — what ships when neither is set.

This is viper's standard precedence chain (`pflag > env > default`). Subcommands
tag a `subsystem=<verb>.<action>` slog field at boot so `--debug` shows exactly
what got resolved.

## Global flags

These live on the cobra root (`cmd/root.go`) and apply to every subcommand.

| Flag      | Env           | Default | Purpose                                                     |
| --------- | ------------- | ------- | ----------------------------------------------------------- |
| `--debug` | `MESHX_DEBUG` | `false` | Flip the slog level to `Debug` (show `running` lines)       |
| `--json`  | —             | `false` | JSON log output via `slog.NewJSONHandler` (for aggregators) |
| `-j`      | —             | `false` | Shorthand for `--json`                                      |

## TUI launcher (`meshx`, `meshx usb connect`, `meshx ble connect`)

Bare `meshx` (no subcommand) auto-detects: if a favorite BLE radio is saved, it
connects to that; otherwise it tries USB. Both `usb connect` and `ble connect`
accept an optional positional radio argument and open the local TUI.

| Flag     | Env          | Default | Purpose                                                                                                       |
| -------- | ------------ | ------- | ------------------------------------------------------------------------------------------------------------- |
| `--demo` | `MESHX_DEMO` | `false` | Skip the radio dial; populate state from `internal/tui.DefaultDemo()` so the UI renders for screenshots / dev |

## USB transport (`meshx usb connect`)

| Flag       | Env | Default  | Purpose                                                                     |
| ---------- | --- | -------- | --------------------------------------------------------------------------- |
| `[device]` | —   | _(auto)_ | Serial port path (e.g., `/dev/cu.usbmodem2101`). Auto-detected when omitted |

## TCP transport (`meshx tcp connect`)

| Flag          | Env | Default  | Purpose                                                                         |
| ------------- | --- | -------- | ------------------------------------------------------------------------------- |
| `host[:port]` | —   | _(none)_ | TCP address of the radio or `meshtasticd`. Port defaults to `4403` when omitted |

## BLE transport (`meshx ble *`)

BLE subcommands operate on the device database at `~/.meshx/meshx.bolt`.

| Subcommand                       | Purpose                                                                   |
| -------------------------------- | ------------------------------------------------------------------------- |
| `meshx ble scan`                 | 10s scan — table of nearby Meshtastic radios with UUID + name + RSSI      |
| `meshx ble pair <uuid>`          | Save a radio to the bbolt store; OS pairing dialog fires on first connect |
| `meshx ble list`                 | Show saved Bluetooth devices (`★` marks the auto-connect favorite)        |
| `meshx ble connect <uuid\|name>` | Open the TUI over Bluetooth against a saved device                        |
| `meshx ble fav <uuid\|name>`     | Mark a saved device as the bare-`meshx` fallback target                   |
| `meshx ble disconnect`           | Clear the favorite flag (opposite of `fav`)                               |
| `meshx ble forget <uuid\|name>`  | Remove a saved device from persistence                                    |
| `meshx ble probe <uuid>`         | 15s diagnostic: dump every FromRadio packet the radio sends               |

## Environment-variable naming

All meshx env vars share the `MESHX_` prefix. Viper's auto-binding maps a viper
key like `debug` to env var `MESHX_DEBUG` — the dot becomes an underscore, the
entire chain uppercases. This means new subcommands inherit env support "for
free" the moment their flag is bound via `viper.BindPFlag`.

## Debug logging

`MESHX_DEBUG=1 meshx ble connect <uuid>` writes every pump event (transport
start, SendWantConfig nonce, each translated FromRadio, errors) to
`/tmp/meshx-pump.log`. Set `MESHX_DEBUG=/some/other/path` to control the
destination. Alt-screen TUIs swallow stderr, so this file is the only way to
inspect live transport flow without leaving the session.

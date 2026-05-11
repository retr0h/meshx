# Configuration

Every meshx subcommand resolves its inputs through three layers in strict
precedence:

1. **Explicit CLI flag** — e.g., `--server http://host:4404`.
2. **Environment variable** — e.g., `MESHX_CLIENT_SERVER=...`.
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

## Server (`meshx server start`)

| Flag                | Env                            | Default          | Purpose                                                                                                                                                         |
| ------------------- | ------------------------------ | ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--bind`            | `MESHX_SERVER_BIND`            | `127.0.0.1:4404` | HTTP listener. `127.0.0.1:*` is loopback-only; `:4404` or `0.0.0.0:4404` exposes the daemon                                                                     |
| `--radio`           | `MESHX_SERVER_RADIO`           | _(none)_         | Pre-register a radio at boot (`ble:<uuid>`, `/dev/cu.usb…`, `host:port`)                                                                                        |
| `--auth-token-file` | `MESHX_SERVER_AUTH_TOKEN_FILE` | _(none)_         | Path to the bearer-token file. Generated on first run (32 random bytes hex, mode 0o600). Required when `--bind` is non-loopback unless `--auth-disabled` is set |
| `--auth-disabled`   | `MESHX_SERVER_AUTH_DISABLED`   | `false`          | Explicit opt-out of auth on a non-loopback bind. Useful for trusted internal networks                                                                           |

The bind-aware auth policy:

- **Loopback bind** (`127.0.0.1:*`, `localhost:*`): unauthenticated by default.
  Operator can still opt in with `--auth-token-file`.
- **Non-loopback bind**: refuses to start without either `--auth-token-file`
  (sets up bearer auth) or `--auth-disabled` (explicit opt-out, logged loudly).
- `/healthz` is always exempt — auth middleware skips it so external liveness
  probes work without credentials.

## Client (`meshx client …`)

Persistent flags on the `client` parent — every subcommand (`status`, `scan`,
`pair`, `connect`, `list`, `forget`, `fav`, `unfav`, `send`, `tail`) inherits
them.

| Flag                | Env                            | Default                 | Purpose                                                                                                                                                    |
| ------------------- | ------------------------------ | ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--server`, `-s`    | `MESHX_CLIENT_SERVER`          | `http://127.0.0.1:4404` | Daemon URL the client talks to (`scheme://host:port`)                                                                                                      |
| `--auth-token-file` | `MESHX_CLIENT_AUTH_TOKEN_FILE` | _(none)_                | Path to the same token file the server wrote. The client reads it on startup and sends `Authorization: Bearer <token>` on every HTTP call + the SSE stream |

The client never _generates_ a token — it only reads. If the file is missing the
command errors out cleanly; if it's empty after `strings.TrimSpace`, same.
Tokens rotate by daemon restart (write a new file, restart the daemon, hand the
new file to clients).

## TUI launcher (`meshx`, `meshx usb connect`, `meshx ble connect`)

Bare `meshx` (no subcommand) auto-detects: if a favorite BLE radio is saved, it
connects to that; otherwise it tries USB. Both `usb connect` and `ble connect`
accept an optional positional radio argument and open the local TUI.

| Flag     | Env          | Default | Purpose                                                                                                       |
| -------- | ------------ | ------- | ------------------------------------------------------------------------------------------------------------- |
| `--demo` | `MESHX_DEMO` | `false` | Skip the radio dial; populate state from `internal/tui.DefaultDemo()` so the UI renders for screenshots / dev |

## Environment-variable naming

All meshx env vars share the `MESHX_` prefix. Viper's auto-binding maps a viper
key like `client.server` to env var `MESHX_CLIENT_SERVER` — the dot becomes an
underscore, the entire chain uppercases. This means new subcommands inherit env
support "for free" the moment their flag is bound via `viper.BindPFlag`.

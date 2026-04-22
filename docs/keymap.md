# meshx keymap

Quick reference for every binding and `/command`. Inspired by irssi, BitchX,
mutt, vim, and tmux.

## Modes

| Mode                | How you get there                                                         | What typing does                           |
| ------------------- | ------------------------------------------------------------------------- | ------------------------------------------ |
| **Input** (default) | launch · `Esc` from nav · `i`/`q` from nav · `/channels` / `/nodes` close | composes a message; `/` starts a command   |
| **Nav**             | `Esc` from input · `Ctrl+W k` from input · `/channels` / `/nodes` (auto)  | selection cursor in focused surface        |
| **Search**          | `/` in nav mode                                                           | live-filter prompt at the bottom           |
| **Help**            | `?` anywhere · `/help`                                                    | full-screen scrollable reference           |
| **Splash**          | only at launch                                                            | auto-dismisses after 3s; any key dismisses |

`Esc` is the universal "return to input bar" key from any sub-state. `Ctrl+X` is
the universal quit.

## Global

| Key         | Action                                                  |
| ----------- | ------------------------------------------------------- |
| `Ctrl+X`    | exit app                                                |
| `Ctrl+C`    | exit app (only on empty input — otherwise clears input) |
| `?`         | open help overlay                                       |
| `Enter`     | send message / run `/command` / activate selection      |
| `Esc`       | input → nav mode; nav → back to input; cancel modal     |
| `Tab`       | complete `/command`, `#channel`, or nick (cycles)       |
| `Shift+Tab` | cycle completion backwards                              |

## Channel switching

| Key                                   | Action                                         |
| ------------------------------------- | ---------------------------------------------- |
| `Alt+1` / `Alt+2` / `Alt+3` / `Alt+4` | jump to channel by index                       |
| `Ctrl+N` / `Ctrl+P`                   | cycle to next / prev channel                   |
| `/join <channel>`                     | switch to named channel                        |
| `/channels`                           | open channels overlay (j/k walks, Enter opens) |

## Window nav (between log and input)

| Key        | Action                                                 |
| ---------- | ------------------------------------------------------ |
| `Ctrl+W k` | from input → jump up to the message log (nav mode)     |
| `Ctrl+W j` | from nav → drop down to the input bar                  |
| `Esc`      | same as `Ctrl+W k` / `Ctrl+W j` depending on direction |

## Nav mode (after `Esc`)

| Key                 | Action                                                       |
| ------------------- | ------------------------------------------------------------ |
| `j` / `k`           | down / up (1 row in linear list, `cols` cells in users grid) |
| `h` / `l`           | left / right (column step in users grid; alias in linear)    |
| `gg` / `G`          | top / bottom                                                 |
| `Ctrl+F` / `Ctrl+U` | half-page down / up (aliases: `Ctrl+D`, `d`/`u`, `PgDn`/`PgUp`) |
| `/`                 | search within focused surface                                |
| `n` / `N`           | next / prev search hit                                       |
| `Enter` or `Space`  | detail view (hop / SNR / RSSI / hex id / whois)              |
| `Esc` / `i` / `q`   | back to input mode                                           |

### Nav quick-keys (on message / node selection)

Single-letter shortcuts that operate on whatever's highlighted:

| Key | Action                                                 |
| --- | ------------------------------------------------------ |
| `r` | reply — prefills `/reply <sender> ` into the input bar |
| `t` | traceroute selected sender                             |
| `p` | ping selected sender                                   |
| `w` | whois selected sender                                  |
| `*` | pin / unpin selected node                              |
| `m` | mute / unmute selected node                            |
| `F` | filter the log to this node's traffic                  |
| `X` | clear active filter                                    |
| `s` | cycle node sort (heard → name → state) — nodes overlay |

## Ham radio /commands

All compose a normal text message with a `!bang` prefix — any other Meshtastic
client reads them as plain chat.

| Command            | Meaning                                                           |
| ------------------ | ----------------------------------------------------------------- |
| `/cq [tail]`       | broadcast CQ with optional custom tail                            |
| `/cqr <call>`      | respond to someone's CQ with a real copy report                   |
| `/rs <call>`       | send a formatted signal report (SNR dB · RSSI dBm · hops)         |
| `/73 [call]`       | sign-off — broadcast, or directed to `<call>` when supplied       |
| `/88`              | love-and-kisses ham slang                                         |
| `/qsl [call]`      | acknowledge / confirm receipt — directed when `<call>` set        |
| `/qth [grid]`      | broadcast your location / grid square                             |
| `/grid [locator]`  | just the Maidenhead grid square                                   |
| `/sked <call>`     | propose a scheduled contact                                       |
| `/qrz`             | "who is calling me?" — prompt for ID                              |
| `/qrm <call>`      | report interference on their signal                               |
| `/qsb <call>`      | report that their signal is fading                                |
| `/sk [call]`       | final sign-off (stronger than `/73`) — directed when `<call>` set |
| `/wx [conditions]` | weather at your QTH                                               |
| `/mesh`            | live summary of the mesh you can hear (Meshtastic-specific)       |
| `/k <call>`        | "over — go ahead" (ragchew turn-taking)                           |

### Directed replies and threading

Every target-taking ham verb (`/73 <call>`, `/qsl <call>`, `/sk <call>`,
`/rs <call>`, `/cqr <call>`, `/k <call>`, `/qrm <call>`, `/qsb <call>`) is still
a **channel broadcast** — not a DM — so the exchange stays visible to everyone
on the mesh (ham etiquette). What's different is the outgoing packet carries
`Data.reply_id` pointing at `<call>`'s most recent message we've seen, so any
receiving client with threading support can display it as a reply.

meshx renders incoming replies as a dim one-line quoted reference above the row:

```
  ┌ KC7XYZ 🦀 13:52  "Test, plz confirm"
› me  13:53  /73 KC7XYZ — 73 KC7XYZ                                  ✓
```

The quote resolves from `msg.replyID` → parent `msg.packetID`, so threading only
renders when both ends are in the loaded scrollback.

## Messaging /commands

Target-taking commands default to the highlighted sender when called from nav
mode with no argument.

| Command                | Meaning                                       |
| ---------------------- | --------------------------------------------- |
| `/msg <call> <text>`   | direct message to node                        |
| `/reply [call] [text]` | reply; uses highlighted sender when omitted   |
| `/r`                   | alias for `/reply`                            |
| `/ping <call>`         | RTT + signal check                            |
| `/tr <call>`           | traceroute (aliases: `/traceroute`, `/trace`) |
| `/whois <call>`        | node metadata (alias: `/w`)                   |

## Overlay and util /commands

| Command                  | Meaning                                               |
| ------------------------ | ----------------------------------------------------- |
| `/channels`              | open channels overlay                                 |
| `/nodes`                 | open nodes overlay (BitchX-style bracketed grid)      |
| `/join <channel>`        | switch to named channel                               |
| `/channel list`          | same as `/channels`                                   |
| `/search <pattern>`      | run a search and jump to first hit (aliases: `/find`) |
| `/config`                | show radio + identity configuration                   |
| `/clear`                 | clear local scrollback (does not unsend)              |
| `/help`                  | open the help overlay                                 |
| `/exit` / `/quit` / `/q` | exit the app                                          |

## Notes on channels

Channels are configured on the **radio** (name + PSK pair), not in meshx. Create
channels via the official Meshtastic app / CLI; meshx imports them once the
radio is configured. Planned:

- `/channel add <meshtastic://url>` — import a channel shared by URL
- `/channel share <name>` — emit a QR for another client to import

## Notes on reports

Every report-producing command (`/rs`, `/cqr`, `/ping`, `/tr`, `/whois`) pulls
from **real node telemetry** — `rx_snr`, `rx_rssi`, and hop count as recorded
for the target's last-seen packet. If the node is unknown, the flash bar says so
honestly rather than making up numbers.

## Notes on persistence

Live-radio mode persists the message log to `~/.meshx/meshx.db` (SQLite, WAL
journal) so scrollback survives restarts. The last 500 messages across all
channels are replayed on boot. System/transient rows (`/whois` cards, flash
messages) are skipped — their content is derived state and would be stale on
replay. Demo mode never writes to disk by design; canned fixture data has no
business in the real log.

To wipe history: `rm ~/.meshx/meshx.db` (or `/clear` clears only the in-memory
view for this session).

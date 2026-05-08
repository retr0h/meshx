# meshX keymap

Quick reference for every binding, `/command`, and CLI subcommand. Inspired by
irssi, BitchX, mutt, vim, and tmux.

## Launching meshX

meshX's CLI is transport-first — `usb` / `tcp` / `ble` are peer subcommand
trees, each with their own verbs. Bare `meshx` picks a transport for you.

| Command                          | What it does                                                                                            |
| -------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `meshx`                          | Auto-connect: exactly-one USB radio → use it; else single saved BLE device → use it; else favorite BLE. |
| `meshx demo`                     | Canned-fixture TUI. No radio needed.                                                                    |
| `meshx usb probe`                | List USB-serial ports and label each as Meshtastic or not.                                              |
| `meshx usb connect [dev]`        | Open the TUI over serial. Auto-detects when `[dev]` is omitted.                                         |
| `meshx tcp connect host[:port]`  | Open the TUI over TCP. Port defaults to 4403.                                                           |
| `meshx ble scan`                 | 10s Bluetooth scan — table of nearby Meshtastic radios with UUID + name + RSSI.                         |
| `meshx ble pair <uuid>`          | Save a radio to `~/.meshx/meshx.db`. OS pairing dialog fires on first `meshx ble connect`.              |
| `meshx ble list`                 | Show saved Bluetooth devices (`★` marks the auto-connect favorite).                                     |
| `meshx ble connect <uuid\|name>` | Open the TUI over Bluetooth against a saved device.                                                     |
| `meshx ble fav <uuid\|name>`     | Mark a saved device as the bare-`meshx` fallback target.                                                |
| `meshx ble disconnect`           | Clear the favorite flag (opposite of `fav`).                                                            |
| `meshx ble forget <uuid\|name>`  | Remove a saved device from persistence.                                                                 |
| `meshx ble probe <uuid>`         | 15s diagnostic: dump every FromRadio packet the radio sends, summarize by kind.                         |

### Debug logging

`MESHX_DEBUG=1 meshx ble connect <uuid>` writes every pump event (transport
start, SendWantConfig nonce, each translated FromRadio, errors) to
`/tmp/meshx-pump.log`. Set `MESHX_DEBUG=/some/other/path` to control the
destination. Alt-screen TUIs swallow stderr, so this file is the only way to
inspect live transport flow without leaving the session.

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

## Tab switching (channels + DMs)

The tab strip at the bottom is `[1:#default]  2:#other  9: @peer (1!)` —
channels first by slot, DM threads after. Tab numbers continue across both
kinds, so `Alt+9` addresses whatever's in slot 9 regardless of type.

| Key                 | Action                                                       |
| ------------------- | ------------------------------------------------------------ |
| `Alt+1` … `Alt+9`   | jump to slot N (channels first, DM threads after)            |
| `Ctrl+N` / `Ctrl+P` | cycle to next / prev tab — walks channels + DMs as one strip |
| `/join <channel>`   | switch to named channel                                      |
| `/channels`         | open channels overlay (j/k walks, Enter opens)               |
| `/query <peer>`     | open or focus a DM tab for `<peer>`                          |
| `/close`            | close the active DM tab and return to the prior channel      |

## Window nav (between log and input)

| Key        | Action                                                 |
| ---------- | ------------------------------------------------------ |
| `Ctrl+W k` | from input → jump up to the message log (nav mode)     |
| `Ctrl+W j` | from nav → drop down to the input bar                  |
| `Esc`      | same as `Ctrl+W k` / `Ctrl+W j` depending on direction |

## Nav mode (after `Esc`)

| Key                 | Action                                                          |
| ------------------- | --------------------------------------------------------------- |
| `j` / `k`           | down / up (1 row in linear list, `cols` cells in users grid)    |
| `h` / `l`           | left / right (column step in users grid; alias in linear)       |
| `gg` / `G`          | top / bottom                                                    |
| `Ctrl+F` / `Ctrl+U` | half-page down / up (aliases: `Ctrl+D`, `d`/`u`, `PgDn`/`PgUp`) |
| `/`                 | search within focused surface                                   |
| `n` / `N`           | next / prev search hit                                          |
| `Enter` or `Space`  | detail view (hop / SNR / RSSI / hex id / whois)                 |
| `Esc` / `i` / `q`   | back to input mode                                              |

### Nav quick-keys (on message / node selection)

Single-letter shortcuts that operate on whatever's highlighted:

| Key | Action                                                           |
| --- | ---------------------------------------------------------------- |
| `r` | reply — prefills `/reply <sender> ` into the input bar           |
| `R` | resend — retransmit a pending (`…`) or failed (`✗`) outbound row |
| `t` | traceroute selected sender                                       |
| `p` | ping selected sender                                             |
| `w` | whois selected sender                                            |
| `P` | pin / unpin highlighted notice — pauses TTL (see below)          |
| `*` | pin / unpin selected node                                        |
| `m` | mute / unmute selected node                                      |
| `F` | filter the log to this node's traffic                            |
| `X` | clear active filter                                              |
| `s` | cycle node sort (heard → name → state) — nodes overlay           |

### Delivery status (outbound messages)

Every message you send carries a right-edge indicator that reflects what the
radio told us about delivery:

| Glyph | Status    | Meaning                                                    |
| ----- | --------- | ---------------------------------------------------------- |
| `…`   | `pending` | sent to the radio; waiting for the `ROUTING_APP` receipt   |
| `✓`   | `ack`     | radio acknowledged delivery (Routing `errorReason = NONE`) |
| `✗`   | `fail`    | delivery failed (TIMEOUT, MAX_RETRANSMIT, NO_INTERFACE, …) |

A failed row can be resent in-place from nav mode with `R` — meshX rebuilds the
`ToRadio` envelope with a fresh `MeshPacket.id` and flips the row back to `…`
until the radio reports again.

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

meshX renders incoming replies as a dim one-line quoted reference above the row:

```
  ┌ KC7XYZ 🦀 13:52  "Test, plz confirm"
› me  13:53  /73 KC7XYZ — 73 KC7XYZ                                  ✓
```

The quote resolves from `msg.ReplyID` → parent `msg.PacketID`, so threading only
renders when both ends are in the loaded scrollback.

## Messaging /commands

Target-taking commands default to the highlighted sender when called from nav
mode with no argument.

| Command                | Meaning                                                                   |
| ---------------------- | ------------------------------------------------------------------------- |
| `/msg <call> <text>`   | send a direct message; opens (or focuses) a `@call` DM tab                |
| `/query <call>`        | open (or focus) a DM tab for `<call>` without sending — irssi-style       |
| `/close`               | close the active DM tab and return to the prior channel (also `/unquery`) |
| `/reply [call] [text]` | reply; uses highlighted sender when omitted                               |
| `/r`                   | alias for `/reply`                                                        |
| `/ping <call>`         | RTT + signal check                                                        |
| `/tr <call>`           | traceroute (aliases: `/traceroute`, `/trace`)                             |
| `/whois <call>`        | node metadata (alias: `/w`)                                               |

DM threads are virtual `@callsign` tabs in the bottom tab strip, addressable by
`Ctrl+N` / `Ctrl+P` (cycles channels + DMs in one strip). Inbound messages
addressed to your node auto-open a thread for the sender. Tabs persist across
restarts via SQLite history hydration.

## Overlay and util /commands

| Command                  | Meaning                                                                 |
| ------------------------ | ----------------------------------------------------------------------- |
| `/channels`              | open channels overlay                                                   |
| `/nodes`                 | open nodes overlay (BitchX-style bracketed grid)                        |
| `/nearby`                | distance-sorted roster of peers with a GPS fix (closest first)          |
| `/radar`                 | polar scope — peers plotted by bearing + distance around you            |
| `/join <channel>`        | switch to named channel                                                 |
| `/channel list`          | same as `/channels`                                                     |
| `/channel new <name>`    | mint a fresh secondary with random AES256 PSK (RAM-only)                |
| `/channel add <url>`     | import a meshtastic://e/#... or https://meshtastic.org/e/#... share URL |
| `/channel share <name>`  | render an ASCII QR of the channel's share URL for in-person scanning    |
| `/channel del <name>`    | disable a slot (PRIMARY refused — use /config to rename)                |
| `/search <pattern>`      | run a search and jump to first hit (aliases: `/find`)                   |
| `/config`                | show radio + identity configuration                                     |
| `/info`                  | dump meshX state — own id, peer counts, unresolved placeholders         |
| `/sync`                  | ask the radio to re-dump its NodeDB (WantConfigId)                      |
| `/nick <longname>`       | set the radio's `User.long_name` (aliases: `/callsign`)                 |
| `/tag <emoji-or-text>`   | set the radio's `User.short_name` (aliases: `/emoji`)                   |
| `/clear`                 | clear local scrollback (does not unsend)                                |
| `/help`                  | open the help overlay                                                   |
| `/exit` / `/quit` / `/q` | exit the app                                                            |

## Notes on channels

Channels are stored on the **radio's flash** (name + PSK pair). meshX is a
controller — it can mint, import, share, and delete channels via the same
`AdminMessage_SetChannel` path the official Meshtastic phone apps and Python CLI
use, but the canonical store is always the radio. The PSK never lives on disk;
it sits in RAM during a session and is re-fetched from the radio's NodeDB on
every reconnect.

The full lifecycle:

- `/channel new <name>` — generate 32 random bytes via `crypto/rand`, allocate
  the first free secondary slot, push via `AdminMessage_SetChannel`. Surfaces a
  SHA-256 fingerprint (first 8 hex of `sha256(psk)`) you can read aloud to
  verify key parity with the recipient — same convention SSH uses for host key
  fingerprints. Name capped at 11 bytes per the proto field.
- `/channel share <name>` — round-trip the channel back into a `meshtastic://`
  URL (base64-url protobuf — no network call; the URL fragment after `#` stays
  client-side per HTTP spec) and render it as an ASCII QR for in-person
  scanning. Uses the half-block trick (`▀` per cell carries two QR rows) so the
  rendered code stays roughly square in a terminal where cells are taller than
  wide. Phones scan it directly.
- `/channel add <url>` — accept either a `meshtastic://e/#...` deep link or an
  `https://meshtastic.org/e/#...` universal link, decode the embedded
  `ChannelSet`, push each contained channel into the first free secondary slot.
  Additive only — never overwrites an existing channel by name. Slot 0 (PRIMARY)
  is off-limits so a malformed URL can't nuke your primary.
- `/channel del <name>` — disable the slot via `SetChannel(role=DISABLED, nil)`;
  the radio frees the slot and wipes the PSK. PRIMARY (slot 0) is refused
  because the firmware requires one to operate. Aliases: `/channel delete`,
  `/channel rm`.

The radio supports 8 channel slots: 1 PRIMARY (slot 0) + 7 SECONDARY (slots
1–7). `/channel new` and `/channel add` allocate into the first free SECONDARY;
if all 7 are taken, you'll need to `/channel del` something first.

### Sharing a secret channel safely

The PSK _is_ the secret. Whoever has it can read everything past, present, and
future on that channel — there's no forward secrecy and no per-recipient key.
"Sharing securely" means getting the PSK to your friend without anyone else
seeing it. Ranked by safety:

1. **In-person QR scan (best).** `/channel share` renders the URL as a QR; your
   friend points their phone camera at your terminal. The bytes go terminal →
   photons → camera with no network in the loop.
2. **E2E-encrypted messenger to one trusted recipient.** Send the QR image or
   URL via Signal / iMessage to one specific person. The PSK now exists in their
   messenger client's storage; if their phone is compromised later, the PSK
   leaks.
3. **Anything else (group chat, email, posted on Discord) — DON'T.** The PSK is
   plain bytes inside the URL. Whoever sees the URL joins the channel.

`meshX` itself never stores PSKs on disk. If you wipe `~/.meshx/`, you lose chat
scrollback but not your channels — they're on the radio. If the radio dies, your
channels are gone with it (no cloud backup; that's by design).

## Notes on reports

Every report-producing command (`/rs`, `/cqr`, `/ping`, `/tr`, `/whois`) pulls
from **real node telemetry** — `rx_snr`, `rx_rssi`, and hop count as recorded
for the target's last-seen packet. If the node is unknown, the flash bar says so
honestly rather than making up numbers.

## Notes on notice TTL

Command-triggered `-!-` notices (`/whois`, `/ping`, `/config`, `/env`, `/info`,
`/sync`, …) auto-expire **60 seconds** after they land in the log — the last 10
seconds fade toward the row background so you see the row age out before it
disappears. The reap is paused while you're in nav mode (ESC) so you never lose
a row mid-scroll; it catches up as soon as you drop back to the input bar.

Permanent notices that **never** auto-expire: `storage: persistence degraded`
alerts and anything routed through `m.noticePermanent` internally. The BitchX
splash banner also ages out under the same 60s rule — it's just scrollback by
the time you've been in the app for a minute.

To hold a notice past its TTL, pin it:

- **Nav mode:** highlight the row and press `P`.
- **Anywhere:** type `/pin` to toggle the last ephemeral notice.

Pinned groups render `⌜` at the top-left of the first row and `⌟` at the
bottom-right of the last row. Pin pauses the timer; the remaining time is
restored verbatim when you unpin (running `/pin` again, or pressing `P` again).

## Notes on persistence

Live-radio mode persists to `~/.meshx/meshx.db` (SQLite, WAL journal):

- **Message log** — last 500 rows replayed on boot. System/transient `-!-`
  notices are skipped (derived state, would be stale on replay).
- **Node cache** — every peer's longname / shortname / hw model, so the
  `@retr0h` tiles still render immediately on launch before the radio's NodeInfo
  dump arrives. Favorites (`*`) and mute (`m`) state persist here too.
- **Paired Bluetooth devices** — `ble_devices` table. `meshx ble pair` writes
  here, `meshx ble list` reads, `meshx ble fav` flips the favorite flag in a
  single transaction so there's never two.

Demo mode never writes to disk.

### Stale-send recovery

On boot, any outbound row still marked `pending` from a prior session's
crashed-mid-flight ACK gets flipped to `fail` automatically (5-min TTL). The
user sees the row render with `✗` and can hit `R` to resend — unlike the old
behavior where it'd zombie as `…` forever with no way to act on it. A
`-!- messages: N stale pending row(s) marked as failed — press R to resend`
systemLine fires when the sweep actually touched anything.

To wipe history: `rm ~/.meshx/meshx.db` (or `/clear` clears only the in-memory
view for this session).

## Meshtastic API mapping

Everything meshX does on the wire is standard Meshtastic protobuf — any other
client (phone app, Python CLI, web UI) can consume our packets and vice-versa.
This table is the quick cross-reference for what each slash command sends and
what protobuf field each display surface reads.

| Command                                            | Wire format                                                                                                                                                                                     | Meshtastic field / API                                                                        |
| -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| chat / `/73` / `/qsl` / `/cq` / `/wx` / `/grid`    | `MeshPacket` with `Data.portnum = TEXT_MESSAGE_APP`, `Data.payload` = UTF-8 text, `To = 0xFFFFFFFF` (broadcast)                                                                                 | text is just bytes on the wire; receivers look up `from` in their NodeDB                      |
| threaded reply (`/73 <call>`, `/cqr <call>`, etc.) | adds `Data.reply_id = <parent packet id>` to the same TEXT_MESSAGE_APP packet                                                                                                                   | firmware doesn't parse it; clients that support threading render as quote                     |
| `/nick <longname>`                                 | `ToRadio` with `AdminMessage.SetOwner.User.long_name`, port `ADMIN_APP`, addressed to own node num                                                                                              | updates `User.long_name` on the radio; persisted to flash; next NodeInfo broadcast carries it |
| `/tag <emoji>`                                     | same as `/nick` but fills `User.short_name`                                                                                                                                                     | updates `User.short_name`; up to 4 bytes                                                      |
| `/sync`                                            | `ToRadio.WantConfigId = <nonce>`                                                                                                                                                                | triggers the radio to re-dump its NodeDB + channels + configs                                 |
| `/whois <call>`                                    | local only — reads from `m.nodes[idx]` populated by earlier `FromRadio.NodeInfo` and `NODEINFO_APP` packets                                                                                     | nothing on the wire                                                                           |
| `/config`, `/info`                                 | local only — reads from the radio's initial handshake state                                                                                                                                     | nothing on the wire                                                                           |
| incoming `NODEINFO_APP`                            | `MeshPacket` with `Data.portnum = NODEINFO_APP`, payload is a `User` proto                                                                                                                      | surfaces peer longname/shortname, upgrades 👻 ghost peers to real names                       |
| incoming `POSITION_APP`                            | payload is a `Position` proto                                                                                                                                                                   | feeds `/qth <call>` and status-bar `☖ <grid>`                                                 |
| incoming `TELEMETRY_APP`                           | payload is a `Telemetry` proto with `DeviceMetrics` or `EnvironmentMetrics` variants                                                                                                            | feeds status-bar `⚡ battery` and `/env <call>`                                               |
| outgoing packet id                                 | every `TEXT_MESSAGE_APP` packet we send fills `MeshPacket.id` with a random non-zero uint32, stashed on `messageItem.PacketID` (the embedded `mdl.Message`)                                     | correlation key for the later Routing reply; without it ack tracking is blind                 |
| incoming `ROUTING_APP`                             | `Data.portnum = ROUTING_APP`, `Data.request_id = <our sent packetID>`, payload is a `Routing` proto whose `error_reason` is `NONE` on ack or a code (`TIMEOUT`, `MAX_RETRANSMIT`, …) on failure | flips the outbound row's `status` to `ack` / `fail`; drives the `…` → `✓` / `✗` indicator     |
| `R` in nav mode                                    | rebuilds the original `ToRadio` envelope with a fresh `MeshPacket.id`, re-enqueues                                                                                                              | retransmit of a failed outbound; fresh id so the next routing reply lands cleanly             |

meshX doesn't ship a radio configurator for LoRa region / modem preset / role —
those require a reboot and the official Meshtastic app / CLI handle them
robustly. `/nick` and `/tag` are the two User-record writes that are safe to do
hot, so they're the only SetOwner-flavored verbs we expose.

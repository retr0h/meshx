-- +goose Up
-- Multi-radio foundation. Every domain table grows a radio_id foreign
-- key so the future daemon can fan out to N radios without a
-- destructive schema migration. The TUI today still runs against a
-- single radio in practice; the column exists so the next refactor
-- and the daemon work after it land additively.
--
-- Strategy: create the radios table, insert ONE default-radio row
-- using a fixed deterministic UUID so existing data backfills
-- cleanly, then ALTER each domain table with the same fixed UUID as
-- DEFAULT. On first connect after this migration the model updates
-- that row's transport + addr to the actual values; subsequent
-- connects to the same transport+addr match the existing row, and
-- new transport+addr combinations create new radio rows.

CREATE TABLE IF NOT EXISTS radios (
    id          TEXT PRIMARY KEY,    -- uuid v4 (or the seed below)
    name        TEXT NOT NULL,        -- user-friendly label, e.g. "base" / "portable"
    transport   TEXT NOT NULL,        -- "usb" | "tcp" | "ble" | "unknown" (pre-connect)
    addr        TEXT NOT NULL,        -- transport-scoped: serial path, host:port, or ble uuid
    my_node_num INTEGER,              -- this radio's own node num, populated on MyNodeInfo
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen   DATETIME
);

-- Seed the default-radio row. The UUID is a fixed nil-prefix value so
-- the same constant can be used as the DEFAULT in the ALTER TABLE
-- statements below — SQLite needs DEFAULT to be a literal expression.
-- On first live connect, the model UPDATEs this row's transport +
-- addr to reflect what's actually connected.
INSERT OR IGNORE INTO radios (id, name, transport, addr) VALUES
    ('00000000-0000-0000-0000-000000000001', 'default', 'unknown', 'unknown');

-- Add radio_id to the domain tables. SQLite's ALTER TABLE only
-- supports ADD COLUMN with a literal DEFAULT (no FK enforcement on
-- ALTER), which is fine — we keep the FK loose at the schema level
-- and rely on the application to use real UUIDs.
ALTER TABLE messages ADD COLUMN
    radio_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';

ALTER TABLE nodes ADD COLUMN
    radio_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001';

-- settings.radio_id is intentionally NULLable: NULL = global pref
-- (e.g. terminal ding mute, which is a meshx-client preference, not
-- a per-radio knob). Per-radio settings (future: per-radio nicknames,
-- default channel, etc.) populate the column with a real UUID.
ALTER TABLE settings ADD COLUMN radio_id TEXT;

-- Indexes for the radio_id-scoped queries the model + future server
-- will issue on every render / API call. The existing
-- idx_messages_channel_id index stays — radio_id usually sits
-- alongside channel in WHERE clauses.
CREATE INDEX IF NOT EXISTS idx_messages_radio_channel_id
    ON messages(radio_id, channel, id);
CREATE INDEX IF NOT EXISTS idx_nodes_radio_node_num
    ON nodes(radio_id, node_num);

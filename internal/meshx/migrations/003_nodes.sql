-- +goose Up
-- Persistent NodeDB — every peer we've ever learned real User info
-- for, cached across sessions so placeholder "node 0x…" rows flip
-- to real longnames the moment we relaunch. Mirrors what the
-- official Meshtastic phone app does in its local store: the radio
-- itself forgets NodeInfo after a while (limited ESP32 memory), so
-- a client that only trusts the radio's dump gets amnesia every
-- reconnect. This table is our own durable copy.
CREATE TABLE IF NOT EXISTS nodes (
    node_num   INTEGER PRIMARY KEY,
    long_name  TEXT    NOT NULL DEFAULT '',
    short_name TEXT    NOT NULL DEFAULT '',
    hw_model   TEXT    NOT NULL DEFAULT '',
    last_seen  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS nodes;

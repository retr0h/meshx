-- +goose Up
-- Initial scrollback schema — one flat table mirroring messageItem.
-- Matches the shape that shipped in the first meshx SQLite release
-- so existing databases created with the raw CREATE-TABLE path stay
-- compatible (goose sees the table via IF NOT EXISTS as a no-op and
-- records 001 applied; future ALTERs run through the normal chain).
CREATE TABLE IF NOT EXISTS messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    channel    TEXT    NOT NULL,
    time       TEXT    NOT NULL,
    sender     TEXT    NOT NULL,
    text       TEXT    NOT NULL,
    mine       INTEGER NOT NULL,
    bang       TEXT    NOT NULL DEFAULT '',
    status     TEXT    NOT NULL DEFAULT '',
    hops       INTEGER NOT NULL DEFAULT 0,
    snr        TEXT    NOT NULL DEFAULT '',
    packet_id  INTEGER NOT NULL DEFAULT 0,
    reply_id   INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_messages_channel_id ON messages(channel, id);

-- +goose Down
DROP INDEX IF EXISTS idx_messages_channel_id;
DROP TABLE IF EXISTS messages;

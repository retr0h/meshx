-- +goose Up
-- Capture MeshPacket.from (the sender's node num) alongside every
-- stored message so the renderer can resolve the displayed callsign
-- from the live NodeDB at render time. Without this, rows that
-- landed as "node 0xdeadbeef" before NodeInfo arrived stay labeled
-- that way on every replay — even after we learn the peer's real
-- name. Defaults to 0 for rows that pre-date the field (any row
-- with fromNum == 0 on replay just keeps its frozen `sender` text).
ALTER TABLE messages ADD COLUMN from_num INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite pre-3.35 can't DROP COLUMN; leave the column in place on
-- rollback. Data is additive and non-breaking.

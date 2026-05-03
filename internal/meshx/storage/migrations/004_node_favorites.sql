-- +goose Up
-- Persist per-peer favorite + muted state across sessions so the star
-- next to a node and the "⊘ muted" filter survive meshX restarts.
-- Meshtastic firmware doesn't carry either concept on the wire — they
-- are local UX preferences, owned entirely by the client.
ALTER TABLE nodes ADD COLUMN favorite INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN muted    INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite pre-3.35 can't DROP COLUMN; leave the columns in place on
-- rollback. Data is additive and non-breaking.

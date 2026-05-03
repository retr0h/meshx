-- +goose Up
-- Partial unique index on messages.packet_id so a stray re-insert
-- from a future replay bug can't corrupt SQLite. packet_id = 0 is
-- reserved for system rows / demo seeds / pre-packetID-era history,
-- and those SHOULD be allowed to coexist (multiple "-!- whois" notes
-- with no wire origin is fine), so the constraint is partial.
--
-- Before installing the index, collapse any existing duplicates that
-- crept in during the pre-dedup era: keep the earliest INSERT
-- (lowest rowid) for each non-zero packet_id and drop the rest. In
-- practice "duplicates" here are replays of the same on-wire packet
-- where the body / sender / time / from_num all match exactly, so
-- dropping the extras is lossless.
DELETE FROM messages
WHERE id NOT IN (
    SELECT MIN(id) FROM messages
    WHERE packet_id > 0
    GROUP BY packet_id
)
AND packet_id > 0;

CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_packet_id
    ON messages(packet_id)
    WHERE packet_id > 0;

-- +goose Down
DROP INDEX IF EXISTS idx_messages_packet_id;

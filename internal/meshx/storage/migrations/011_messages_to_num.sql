-- +goose Up
-- Add the to_num column so the renderer can tell DMs (where
-- MeshPacket.to == myNodeNum) apart from broadcast traffic
-- (MeshPacket.to == 0xFFFFFFFF). Captured at pump ingest as
-- mdl.Text.ToNum and round-tripped through SaveMessage / LoadMessages.
--
-- Pre-existing rows get to_num = 0 — we can't recover the
-- distinction post-hoc (the wire ToNum was thrown away at the
-- applyTextMessage boundary before this column existed). Going
-- forward every new packet preserves the addressee.

ALTER TABLE messages ADD COLUMN to_num INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE messages DROP COLUMN to_num;

-- +goose Up
-- Rewrite radios.id (and every FK column that references it) from the
-- migration-009 placeholder UUID '00000000-0000-0000-0000-000000000001'
-- to a hex-formatted my_node_num — '0x' + the radio's own Meshtastic
-- node number, lower-cased 8 hex digits zero-padded. That's the canonical
-- identity Meshtastic itself uses on the wire and that every other tool
-- in the ecosystem (phone app, Python CLI) keys on.
--
-- Why we couldn't just use my_node_num in migration 009: the radio's
-- node num is only revealed via the MyNodeInfo handshake, which can't
-- happen until AFTER storage has opened. Migration 009 therefore had
-- to seed a placeholder so the new NOT NULL columns had a default.
-- This migration is the one-shot upgrade for any DB that ran 009,
-- connected long enough for MyNodeInfo to populate radios.my_node_num,
-- and now wants the placeholder swapped for the real identity.
--
-- For radios that ran 009 but never finished a handshake (my_node_num
-- IS NULL), this migration is a no-op — they keep the placeholder
-- until the next live connection's MyNodeInfo arrives, at which
-- point the application's claimRadioIdentity() helper does the
-- equivalent in-process rewrite.
--
-- Idempotent: after the rewrite no row matches the legacy placeholder,
-- so a re-run finds nothing to do.

-- +goose StatementBegin
UPDATE messages
SET radio_id = printf('0x%08x', (
    SELECT my_node_num FROM radios
    WHERE id = '00000000-0000-0000-0000-000000000001'
      AND my_node_num IS NOT NULL
))
WHERE radio_id = '00000000-0000-0000-0000-000000000001'
  AND EXISTS (
    SELECT 1 FROM radios
    WHERE id = '00000000-0000-0000-0000-000000000001'
      AND my_node_num IS NOT NULL
  );
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE nodes
SET radio_id = printf('0x%08x', (
    SELECT my_node_num FROM radios
    WHERE id = '00000000-0000-0000-0000-000000000001'
      AND my_node_num IS NOT NULL
))
WHERE radio_id = '00000000-0000-0000-0000-000000000001'
  AND EXISTS (
    SELECT 1 FROM radios
    WHERE id = '00000000-0000-0000-0000-000000000001'
      AND my_node_num IS NOT NULL
  );
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE settings
SET radio_id = printf('0x%08x', (
    SELECT my_node_num FROM radios
    WHERE id = '00000000-0000-0000-0000-000000000001'
      AND my_node_num IS NOT NULL
))
WHERE radio_id = '00000000-0000-0000-0000-000000000001'
  AND EXISTS (
    SELECT 1 FROM radios
    WHERE id = '00000000-0000-0000-0000-000000000001'
      AND my_node_num IS NOT NULL
  );
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE radios
SET id = printf('0x%08x', my_node_num)
WHERE id = '00000000-0000-0000-0000-000000000001'
  AND my_node_num IS NOT NULL;
-- +goose StatementEnd

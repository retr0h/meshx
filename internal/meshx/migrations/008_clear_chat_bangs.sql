-- +goose Up
-- Clear messages.bang on historical /me, /reply, and /msg rows. These
-- three commands used to route through sendBang / sendBangReply which
-- stamped msg.bang with the verb ("/me", "/reply <target>", "/msg
-- <target>") — but the chat row renderer keys off bang to choose
-- between the magenta "›" mine-marker (regular chat) and the yellow
-- "*" bang flag (/cq, /73, etc. command output). /reply, /me, /msg
-- are semantically regular chat with directed flavor (threading,
-- nick prefix, action), not command output. The send paths now route
-- through sendPlainReply / sendPlainMessage which leave bang empty,
-- but rows persisted before that change still carry the old bang
-- value and render with the wrong flag glyph.
--
-- The TEXT_MESSAGE_APP body is unchanged — /me bodies still start
-- with "* ", /msg bodies still carry "<target>: ", /reply bodies are
-- whatever the user typed plus a Data.reply_id pointer that lives in
-- messages.reply_id (separate column). Clearing bang here just lets
-- the renderer pick the right flag/styling on next paint.

-- /me rows: bang was "/me" with a body starting with "* ". Match
-- both so we don't accidentally clear a future verb that happens to
-- start with "/me" (e.g. /message). Body check is the disambiguator.
UPDATE messages SET bang = ''
WHERE bang LIKE '/me'
  AND text LIKE '* %';

-- /reply rows: bang was "/reply <target>". The reply_id column
-- already carries the threading pointer; the bang stamp is just
-- the leftover verb tag.
UPDATE messages SET bang = ''
WHERE bang LIKE '/reply %';

-- /msg rows: bang was "/msg <target>" with a body of the form
-- "<target>: <text>". The body convention stays — only the bang
-- field (which would otherwise show the wrong flag) gets cleared.
UPDATE messages SET bang = ''
WHERE bang LIKE '/msg %';

-- +goose Down
-- Down-migration is a no-op — the bang field was always advisory
-- (no foreign keys, no unique constraints), and reconstructing the
-- exact verb / target pair from the body would be guesswork at best.
-- Restoring this migration just means "the renderer falls back to
-- using msg.bang again," which we don't actually want anymore.
SELECT 1;

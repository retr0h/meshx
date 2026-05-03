-- +goose Up
-- App-scoped key/value settings — currently backs /mute (terminal ding
-- toggle) and /config's radio buzzer pref. One row per knob, stringly
-- typed because everything we persist here is small enough that a
-- typed schema would be overkill. Both keys default to "on" when
-- absent so a fresh install matches a Meshtastic radio's stock
-- behaviour (radio beeps on text + meshX dings on text).
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS settings;

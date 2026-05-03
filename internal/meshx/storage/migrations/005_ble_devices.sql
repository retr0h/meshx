-- +goose Up
-- Bluetooth-paired Meshtastic radios. One row per device the user
-- has paired via `meshx ble pair`. `favorite` is a sticky flag — when
-- the user runs bare `meshx`, we fall through to the favorite if no
-- USB radio is plugged in. Only one row at a time should have
-- favorite = 1; `setBLEFavorite` enforces that in a transaction.
--
-- uuid is the device-identifier the host OS uses to address the
-- peripheral. On macOS this is a CBPeripheral UUID (a 36-char hyphen-
-- separated identifier that's stable across reboots for a given
-- paired device). On Linux / BlueZ it's the colon-separated MAC
-- address. Stored as TEXT either way so the string round-trips
-- through the CLI unchanged.
CREATE TABLE IF NOT EXISTS ble_devices (
    uuid       TEXT    PRIMARY KEY,
    long_name  TEXT    NOT NULL DEFAULT '',
    short_name TEXT    NOT NULL DEFAULT '',
    hw_model   TEXT    NOT NULL DEFAULT '',
    favorite   INTEGER NOT NULL DEFAULT 0,
    paired_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS ble_devices;

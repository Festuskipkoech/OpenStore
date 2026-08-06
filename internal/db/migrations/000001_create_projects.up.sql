CREATE TABLE IF NOT EXISTS projects (
    id               TEXT    PRIMARY KEY,
    name             TEXT    NOT NULL,
    api_key_hash     TEXT    NOT NULL,
    webhook_url      TEXT    NOT NULL,
    webhook_secret   TEXT    NOT NULL,
    allowed_origins  TEXT    NOT NULL,
    quota_bytes      INTEGER NOT NULL DEFAULT 0,
    used_bytes       INTEGER NOT NULL DEFAULT 0,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
